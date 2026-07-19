// ComputeBox Craftpanel is a small self-hosted panel for creating and
// managing Minecraft servers on a single host.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/computenord/craftpanel/internal/agent"
	"github.com/computenord/craftpanel/internal/auth"
	"github.com/computenord/craftpanel/internal/mc"
	"github.com/computenord/craftpanel/internal/node"
	"github.com/computenord/craftpanel/internal/selfupdate"
	"github.com/computenord/craftpanel/internal/sftpd"
	"github.com/computenord/craftpanel/internal/web"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	dataDir := flag.String("data", defaultDataDir(), "data directory")
	addr := flag.String("addr", ":8420", "HTTP listen address")
	trustProxy := flag.Bool("behind-proxy", false, "trust X-Forwarded-For from a reverse proxy for login rate limiting")
	managed := flag.Bool("managed", false, "managed hosting mode: enroll with the control plane and report telemetry")
	panelURL := flag.String("panel", "", "panel base URL when running as a remote node agent")
	nodeToken := flag.String("node-token", "", "node enroll token when running as a remote node agent")
	flag.Usage = usage
	flag.Parse()

	switch flag.Arg(0) {
	case "", "serve":
		if err := runServe(*dataDir, *addr, *trustProxy, *managed); err != nil {
			log.Fatal(err)
		}
	case "node":
		if err := runNodeAgent(*dataDir, *panelURL, *nodeToken); err != nil {
			log.Fatal(err)
		}
	case "reset-password":
		if err := runResetPassword(*dataDir, flag.Arg(1)); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "version":
		fmt.Println("craftpanel", version)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `ComputeBox Craftpanel %s

Usage:
  craftpanel [flags] [command]

Commands:
  serve                       run the panel (default)
  node                        run as a remote node agent (-panel, -node-token)
  reset-password <username>   set a new password, read from stdin
  version                     print the version

Flags:
`, version)
	flag.PrintDefaults()
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".craftpanel"
	}
	return filepath.Join(home, ".craftpanel")
}

func runServe(dataDir, addr string, trustProxy, managed bool) error {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return err
	}
	authStore, err := auth.NewStore(dataDir)
	if err != nil {
		return err
	}
	versions := mc.NewVersions()
	manager, err := mc.NewManager(dataDir, versions)
	if err != nil {
		return err
	}

	// baseCtx is handed to every request. Cancelling it releases the long lived
	// SSE console streams, which would otherwise keep Shutdown waiting.
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// The self update endpoint triggers the same graceful shutdown as a signal
	// and then exits non-zero so systemd starts the freshly swapped binary.
	ctx, cancelForRestart := context.WithCancel(sigCtx)
	defer cancelForRestart()
	var restartRequested atomic.Bool
	requestRestart := func() {
		restartRequested.Store(true)
		cancelForRestart()
	}

	// Managed hosting mode: start the control-plane agent if the VM was
	// provisioned for it (agent.json present). Nil when self-hosted.
	var lockState, ssoKey func() string
	if managed {
		// The customer signs in via SSO, never with a local password. Seed a
		// single admin account on first boot so sessions have an identity.
		if authStore.NeedsSetup() {
			pw := make([]byte, 24)
			rand.Read(pw)
			if err := authStore.CreateFirstUser("owner", hex.EncodeToString(pw)); err != nil {
				log.Printf("managed: create owner account: %v", err)
			} else {
				log.Printf("managed: seeded owner account (sign in happens via SSO)")
			}
		}
		updateFn := func(v string) error {
			uctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := selfupdate.Apply(uctx, v); err != nil {
				return err
			}
			requestRestart()
			return nil
		}
		ag, err := agent.Load(dataDir, manager, version, updateFn)
		if err != nil {
			return fmt.Errorf("load agent: %w", err)
		}
		if ag != nil {
			lockState = ag.Lock
			ssoKey = ag.SSOPublicKey
			go ag.Run(ctx)
		} else {
			log.Printf("managed mode requested but no agent.json found, running unmanaged")
		}
	}

	nodes, err := node.NewRegistry(dataDir)
	if err != nil {
		return fmt.Errorf("nodes: %w", err)
	}
	sftpSrv := &sftpd.Server{Auth: authStore, Manager: manager, DataDir: dataDir}
	if addr := manager.Settings().SFTPAddr; addr != "" {
		if err := sftpSrv.Start(addr); err != nil {
			log.Printf("sftp: %v", err)
		}
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           web.New(authStore, manager, versions, version, trustProxy, requestRestart, lockState, ssoKey, nodes, sftpSrv),
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	go manager.StartAutostarts()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("ComputeBox Craftpanel %s listening on %s (data: %s)", version, addr, dataDir)
		if authStore.NeedsSetup() {
			log.Printf("no admin account yet: open the panel in a browser to create one")
		}
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Printf("shutting down")
	cancelBase()
	sftpSrv.Stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)

	if restartRequested.Load() {
		// Self update: leave the Minecraft servers running. The new panel
		// instance adopts them via their persisted run state. Non-zero exit so
		// both Restart=always and Restart=on-failure units bring the new
		// binary up.
		manager.DetachAll()
		log.Printf("restarting into the updated binary")
		os.Exit(1)
	}

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancelStop()
	manager.StopAll(stopCtx)
	log.Printf("bye")
	return nil
}

func runNodeAgent(dataDir, panelURL, token string) error {
	panelURL = strings.TrimSpace(panelURL)
	token = strings.TrimSpace(token)
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return err
	}
	// Prefer flags; otherwise load node-agent.json written by the bootstrap script.
	if panelURL == "" || token == "" {
		cfg, err := node.LoadAgentConfig(dataDir)
		if err != nil {
			return errors.New("usage: craftpanel node -panel https://panel.example.com -node-token node_… -data /path\n(or place node-agent.json in the data directory via the panel bootstrap command)")
		}
		if panelURL == "" {
			panelURL = cfg.PanelURL
		}
		if token == "" {
			token = cfg.Token
		}
	} else {
		_ = node.WriteAgentConfig(dataDir, node.AgentConfig{PanelURL: panelURL, Token: token})
	}
	versions := mc.NewVersions()
	manager, err := mc.NewManager(dataDir, versions)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go manager.StartAutostarts()
	ag := &node.Agent{PanelURL: panelURL, Token: token, Version: version, Manager: manager}
	log.Printf("node agent: reporting to %s (data: %s)", panelURL, dataDir)
	ag.Run(ctx)
	stopCtx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	manager.StopAll(stopCtx)
	return nil
}

func runResetPassword(dataDir, username string) error {
	if username == "" {
		return errors.New("usage: craftpanel reset-password <username> (new password on stdin)")
	}
	store, err := auth.NewStore(dataDir)
	if err != nil {
		return err
	}
	fmt.Fprint(os.Stderr, "New password: ")
	reader := bufio.NewReader(os.Stdin)
	pw, err := reader.ReadString('\n')
	if err != nil && pw == "" {
		return err
	}
	pw = strings.TrimRight(pw, "\r\n")
	if err := store.SetPassword(username, pw); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Password updated.")
	return nil
}
