// Package sftpd embeds a small SSH/SFTP server jailed to Minecraft server data dirs.
package sftpd

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/computenord/craftpanel/internal/auth"
	"github.com/computenord/craftpanel/internal/fsutil"
	"github.com/computenord/craftpanel/internal/mc"
)

// Server is an optional embedded SFTP listener.
type Server struct {
	Auth    *auth.Store
	Manager *mc.Manager
	DataDir string

	mu       sync.Mutex
	listener net.Listener
	cfg      *ssh.ServerConfig
}

// Start listens on addr (e.g. ":2222"). Empty addr is a no-op.
func (s *Server) Start(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil
	}
	signer, err := s.loadOrCreateHostKey()
	if err != nil {
		return err
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: s.passwordCallback,
		ServerVersion:    "SSH-2.0-Craftpanel",
	}
	cfg.AddHostKey(signer)
	s.cfg = cfg

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()
	log.Printf("sftp: listening on %s (login as user+serverId)", addr)
	go s.acceptLoop(ln)
	return nil
}

func (s *Server) Stop() {
	s.mu.Lock()
	ln := s.listener
	s.listener = nil
	s.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
}

func (s *Server) loadOrCreateHostKey() (ssh.Signer, error) {
	path := filepath.Join(s.DataDir, "sftp_host_ed25519")
	raw, err := os.ReadFile(path)
	if err == nil {
		return ssh.ParsePrivateKey(raw)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "craftpanel-sftp")
	if err != nil {
		return nil, err
	}
	pemBytes := pem.EncodeToMemory(block)
	if err := fsutil.WriteFileAtomic(path, pemBytes, 0o600); err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(pemBytes)
}

func (s *Server) passwordCallback(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
	user := c.User()
	serverID := ""
	if i := strings.IndexByte(user, '+'); i >= 0 {
		serverID = user[i+1:]
		user = user[:i]
	}
	if serverID == "" {
		return nil, fmt.Errorf("use username+serverId")
	}
	if s.Auth.TOTPEnabled(user) {
		return nil, fmt.Errorf("disable TOTP or use the web UI — SFTP does not support 2FA")
	}
	if err := s.Auth.AuthenticatePasswordOnly(c.RemoteAddr().String(), user, string(pass)); err != nil {
		return nil, fmt.Errorf("access denied")
	}
	u, ok := s.Auth.GetUser(user)
	if !ok {
		return nil, fmt.Errorf("access denied")
	}
	access, allowed := u.AccessFor(serverID)
	if !allowed || !access.Allows(auth.PermFiles) {
		return nil, fmt.Errorf("no file access to server")
	}
	return &ssh.Permissions{
		Extensions: map[string]string{
			"username": user,
			"serverId": serverID,
		},
	}, nil
}

func (s *Server) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(nConn net.Conn) {
	defer nConn.Close()
	sshConn, chans, reqs, err := ssh.NewServerConn(nConn, s.cfg)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)

	serverID := sshConn.Permissions.Extensions["serverId"]
	root, err := s.Manager.GetServerDataDir(serverID)
	if err != nil {
		return
	}

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unknown")
			continue
		}
		ch, reqs, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go func(ch ssh.Channel, reqs <-chan *ssh.Request) {
			defer ch.Close()
			for req := range reqs {
				ok := false
				if req.Type == "subsystem" && len(req.Payload) >= 4 {
					n := int(req.Payload[0])<<24 | int(req.Payload[1])<<16 | int(req.Payload[2])<<8 | int(req.Payload[3])
					if n > 0 && 4+n <= len(req.Payload) && string(req.Payload[4:4+n]) == "sftp" {
						ok = true
					}
				}
				if req.WantReply {
					_ = req.Reply(ok, nil)
				}
				if ok {
					h := &jailedHandler{root: root}
					srv := sftp.NewRequestServer(ch, sftp.Handlers{
						FileGet:  h,
						FilePut:  h,
						FileCmd:  h,
						FileList: h,
					})
					_ = srv.Serve()
					return
				}
			}
		}(ch, reqs)
	}
}

type jailedHandler struct {
	root string
}

func (h *jailedHandler) resolve(p string) (string, error) {
	clean := filepath.Clean("/" + strings.TrimPrefix(filepath.ToSlash(p), "/"))
	rel := strings.TrimPrefix(clean, "/")
	full := filepath.Join(h.root, filepath.FromSlash(rel))
	check, err := filepath.Rel(h.root, full)
	if err != nil || strings.HasPrefix(check, "..") {
		return "", os.ErrPermission
	}
	return full, nil
}

func (h *jailedHandler) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	path, err := h.resolve(r.Filepath)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (h *jailedHandler) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	path, err := h.resolve(r.Filepath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
}

func (h *jailedHandler) Filecmd(r *sftp.Request) error {
	path, err := h.resolve(r.Filepath)
	if err != nil {
		return err
	}
	switch r.Method {
	case "Remove", "Rmdir":
		return os.Remove(path)
	case "Mkdir":
		return os.MkdirAll(path, 0o750)
	case "Rename":
		target, err := h.resolve(r.Target)
		if err != nil {
			return err
		}
		return os.Rename(path, target)
	case "Setstat":
		return nil
	default:
		return sftp.ErrSSHFxOpUnsupported
	}
}

func (h *jailedHandler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	path, err := h.resolve(r.Filepath)
	if err != nil {
		return nil, err
	}
	switch r.Method {
	case "List":
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		infos := make([]os.FileInfo, 0, len(entries))
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				continue
			}
			infos = append(infos, info)
		}
		return listerAt(infos), nil
	case "Stat", "Lstat":
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		return listerAt{info}, nil
	default:
		return nil, sftp.ErrSSHFxOpUnsupported
	}
}

type listerAt []os.FileInfo

func (l listerAt) ListAt(f []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(f, l[offset:])
	if n < len(f) {
		return n, io.EOF
	}
	return n, nil
}
