// Package auth implements local user accounts, argon2id password hashing,
// cookie sessions and login rate limiting for the panel.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/computenord/craftpanel/internal/fsutil"
)

const (
	SessionCookie = "cp_session"
	sessionTTL    = 7 * 24 * time.Hour

	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16

	failWindow = 15 * time.Minute
	maxFails   = 8
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrRateLimited        = errors.New("too many attempts")
	ErrUserExists         = errors.New("user already exists")
	ErrUserNotFound       = errors.New("user not found")

	usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,32}$`)
)

type User struct {
	Username  string    `json:"username"`
	Hash      string    `json:"hash"`
	CreatedAt time.Time `json:"createdAt"`
}

type Session struct {
	Username string    `json:"username"`
	Expires  time.Time `json:"expires"`
}

// hashSem bounds concurrent argon2 hashes. Each costs argonMemory (64 MiB) and
// pins argonThreads cores, so an unbounded login flood would exhaust the host.
var hashSem = make(chan struct{}, 2)

type Store struct {
	mu        sync.Mutex
	usersPath string
	sessPath  string
	users     []User
	usersMod  time.Time
	usersSize int64
	sessions  map[string]Session // key: hex(sha256(raw token))
	dummyHash string
	fails     map[string][]time.Time
}

func NewStore(dataDir string) (*Store, error) {
	s := &Store{
		usersPath: filepath.Join(dataDir, "users.json"),
		sessPath:  filepath.Join(dataDir, "sessions.json"),
		sessions:  map[string]Session{},
		fails:     map[string][]time.Time{},
	}
	if err := fsutil.ReadJSON(s.usersPath, &s.users); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	s.stampUsersLocked()
	if err := fsutil.ReadJSON(s.sessPath, &s.sessions); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if s.sessions == nil {
		s.sessions = map[string]Session{}
	}
	s.pruneSessionsLocked()
	// Used to keep response timing constant when the username does not exist.
	s.dummyHash = hashPassword("craftpanel-dummy-password")
	return s, nil
}

// stampUsersLocked records the on-disk identity of users.json so that changes
// made by a separate process (the reset-password command) are picked up.
func (s *Store) stampUsersLocked() {
	if fi, err := os.Stat(s.usersPath); err == nil {
		s.usersMod = fi.ModTime()
		s.usersSize = fi.Size()
	}
}

// reloadUsersLocked re-reads users.json when it changed on disk.
func (s *Store) reloadUsersLocked() {
	fi, err := os.Stat(s.usersPath)
	if err != nil {
		return
	}
	if fi.ModTime().Equal(s.usersMod) && fi.Size() == s.usersSize {
		return
	}
	var users []User
	if err := fsutil.ReadJSON(s.usersPath, &users); err != nil {
		return
	}
	s.users = users
	s.usersMod = fi.ModTime()
	s.usersSize = fi.Size()
}

// NeedsSetup reports whether no user account exists yet.
func (s *Store) NeedsSetup() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadUsersLocked()
	return len(s.users) == 0
}

func (s *Store) CreateUser(username, password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createUserLocked(username, password, false)
}

// CreateFirstUser creates the admin account, but only while no account exists.
// The check and the write happen under one lock, so two concurrent first-run
// setup requests cannot both succeed.
func (s *Store) CreateFirstUser(username, password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createUserLocked(username, password, true)
}

func (s *Store) createUserLocked(username, password string, mustBeFirst bool) error {
	if !usernameRe.MatchString(username) {
		return errors.New("username must be 3-32 characters (letters, digits, _ . -)")
	}
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	s.reloadUsersLocked()
	if mustBeFirst && len(s.users) > 0 {
		return ErrUserExists
	}
	for _, u := range s.users {
		if strings.EqualFold(u.Username, username) {
			return ErrUserExists
		}
	}
	s.users = append(s.users, User{
		Username:  username,
		Hash:      hashPassword(password),
		CreatedAt: time.Now().UTC(),
	})
	if err := fsutil.WriteJSONAtomic(s.usersPath, s.users); err != nil {
		s.users = s.users[:len(s.users)-1]
		return err
	}
	s.stampUsersLocked()
	return nil
}

func (s *Store) SetPassword(username, password string) error {
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadUsersLocked()
	for i := range s.users {
		if s.users[i].Username == username {
			s.users[i].Hash = hashPassword(password)
			// Invalidate all sessions of this user.
			for k, sess := range s.sessions {
				if sess.Username == username {
					delete(s.sessions, k)
				}
			}
			if err := fsutil.WriteJSONAtomic(s.sessPath, s.sessions); err != nil {
				return err
			}
			if err := fsutil.WriteJSONAtomic(s.usersPath, s.users); err != nil {
				return err
			}
			s.stampUsersLocked()
			return nil
		}
	}
	return ErrUserNotFound
}

// Authenticate verifies credentials, applying a per-IP rate limit.
func (s *Store) Authenticate(ip, username, password string) error {
	s.mu.Lock()
	if !s.allowAttemptLocked(ip) {
		s.mu.Unlock()
		return ErrRateLimited
	}
	// Record the attempt as failed before doing the expensive hash. Otherwise a
	// burst of concurrent requests all pass the check before any of them writes
	// a failure, and the limit never bites. A success clears the bucket again.
	s.fails[ip] = append(s.fails[ip], time.Now())
	s.reloadUsersLocked()
	var hash string
	found := false
	for _, u := range s.users {
		if u.Username == username {
			hash = u.Hash
			found = true
			break
		}
	}
	if !found {
		hash = s.dummyHash
	}
	s.mu.Unlock()

	hashSem <- struct{}{}
	ok := verifyPassword(password, hash) && found
	<-hashSem

	if !ok {
		return ErrInvalidCredentials
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.fails, ip)
	return nil
}

func (s *Store) allowAttemptLocked(ip string) bool {
	now := time.Now()
	// Keep the map bounded even under many-source floods.
	if len(s.fails) > 4096 {
		for k, ts := range s.fails {
			if len(ts) == 0 || now.Sub(ts[len(ts)-1]) > failWindow {
				delete(s.fails, k)
			}
		}
	}
	ts := s.fails[ip]
	kept := ts[:0]
	for _, t := range ts {
		if now.Sub(t) <= failWindow {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(s.fails, ip)
	} else {
		s.fails[ip] = kept
	}
	return len(kept) < maxFails
}

// CreateSession returns a new raw session token for the user.
func (s *Store) CreateSession(username string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneSessionsLocked()
	s.sessions[tokenKey(token)] = Session{
		Username: username,
		Expires:  time.Now().Add(sessionTTL),
	}
	return token, fsutil.WriteJSONAtomic(s.sessPath, s.sessions)
}

// ValidateSession resolves a raw token to a username, renewing it when it is
// past half of its lifetime.
func (s *Store) ValidateSession(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	key := tokenKey(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[key]
	if !ok || time.Now().After(sess.Expires) {
		if ok {
			delete(s.sessions, key)
		}
		return "", false
	}
	if time.Until(sess.Expires) < sessionTTL/2 {
		sess.Expires = time.Now().Add(sessionTTL)
		s.sessions[key] = sess
		_ = fsutil.WriteJSONAtomic(s.sessPath, s.sessions)
	}
	return sess.Username, true
}

func (s *Store) DestroySession(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, tokenKey(token))
	_ = fsutil.WriteJSONAtomic(s.sessPath, s.sessions)
}

func (s *Store) pruneSessionsLocked() {
	now := time.Now()
	changed := false
	for k, sess := range s.sessions {
		if now.After(sess.Expires) {
			delete(s.sessions, k)
			changed = true
		}
	}
	if changed {
		_ = fsutil.WriteJSONAtomic(s.sessPath, s.sessions)
	}
}

func tokenKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func hashPassword(password string) string {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var mem, iters uint32
	var par uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &iters, &par); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, iters, mem, par, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
