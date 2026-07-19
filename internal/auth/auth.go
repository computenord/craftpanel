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
	ErrTOTPRequired       = errors.New("two-factor code required")
	ErrInvalidTOTP        = errors.New("invalid two-factor code")

	usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,32}$`)
)

type User struct {
	Username   string    `json:"username"`
	Hash       string    `json:"hash"`
	TOTPSecret string    `json:"totpSecret,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	// Role is "admin" or "user". Empty means admin (back-compat for the
	// first account created before roles existed).
	Role string `json:"role,omitempty"`
	// Access maps server id → permissions for non-admin users.
	Access map[string]ServerAccess `json:"access,omitempty"`
}

type Session struct {
	Username string    `json:"username"`
	Expires  time.Time `json:"expires"`
}

// hashSem bounds concurrent argon2 hashes. Each costs argonMemory (64 MiB) and
// pins argonThreads cores, so an unbounded login flood would exhaust the host.
var hashSem = make(chan struct{}, 2)

type Store struct {
	mu          sync.Mutex
	usersPath   string
	sessPath    string
	users       []User
	usersMod    time.Time
	usersSize   int64
	sessions    map[string]Session // key: hex(sha256(raw token))
	dummyHash   string
	fails       map[string][]time.Time
	pendingTOTP map[string]string // username -> secret awaiting confirmation
	lastTOTP    map[string]int64  // username -> last accepted time step
}

func NewStore(dataDir string) (*Store, error) {
	s := &Store{
		usersPath:   filepath.Join(dataDir, "users.json"),
		sessPath:    filepath.Join(dataDir, "sessions.json"),
		sessions:    map[string]Session{},
		fails:       map[string][]time.Time{},
		pendingTOTP: map[string]string{},
		lastTOTP:    map[string]int64{},
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

// FirstUsername returns the admin account name, or "" when none exists. Used
// by the managed-mode SSO jump, which logs in as the single panel admin.
func (s *Store) FirstUsername() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadUsersLocked()
	if len(s.users) == 0 {
		return ""
	}
	return s.users[0].Username
}

func (s *Store) CreateUser(username, password string) error {
	return s.CreateUserWithRole(username, password, RoleUser, nil)
}

// CreateUserWithRole creates a non-first user with an explicit role and access map.
func (s *Store) CreateUserWithRole(username, password, role string, access map[string]ServerAccess) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createUserLocked(username, password, false, role, access)
}

// CreateFirstUser creates the admin account, but only while no account exists.
// The check and the write happen under one lock, so two concurrent first-run
// setup requests cannot both succeed.
func (s *Store) CreateFirstUser(username, password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createUserLocked(username, password, true, RoleAdmin, nil)
}

func (s *Store) createUserLocked(username, password string, mustBeFirst bool, role string, access map[string]ServerAccess) error {
	if !usernameRe.MatchString(username) {
		return errors.New("username must be 3-32 characters (letters, digits, _ . -)")
	}
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	if role != RoleAdmin && role != RoleUser {
		role = RoleUser
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
	u := User{
		Username:  username,
		Hash:      hashPassword(password),
		CreatedAt: time.Now().UTC(),
		Role:      role,
	}
	if role != RoleAdmin && access != nil {
		u.Access = access
	}
	s.users = append(s.users, u)
	if err := fsutil.WriteJSONAtomic(s.usersPath, s.users); err != nil {
		s.users = s.users[:len(s.users)-1]
		return err
	}
	s.stampUsersLocked()
	return nil
}

// ListUsers returns a copy of all users without password hashes.
func (s *Store) ListUsers() []User {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadUsersLocked()
	out := make([]User, len(s.users))
	for i, u := range s.users {
		out[i] = User{
			Username:  u.Username,
			CreatedAt: u.CreatedAt,
			Role:      u.Role,
			Access:    cloneAccess(u.Access),
		}
		if out[i].Role == "" {
			out[i].Role = RoleAdmin
		}
	}
	return out
}

// GetUser returns a user without the password hash.
func (s *Store) GetUser(username string) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadUsersLocked()
	for _, u := range s.users {
		if u.Username == username {
			out := User{
				Username:  u.Username,
				CreatedAt: u.CreatedAt,
				Role:      u.Role,
				Access:    cloneAccess(u.Access),
			}
			if out.Role == "" {
				out.Role = RoleAdmin
			}
			if u.TOTPSecret != "" {
				out.TOTPSecret = "set"
			}
			return out, true
		}
	}
	return User{}, false
}

// UpdateUserRoleAccess updates role and per-server access. Admins ignore access.
func (s *Store) UpdateUserRoleAccess(username, role string, access map[string]ServerAccess) error {
	if role != RoleAdmin && role != RoleUser {
		return errors.New("role must be admin or user")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadUsersLocked()
	for i := range s.users {
		if s.users[i].Username != username {
			continue
		}
		s.users[i].Role = role
		if role == RoleAdmin {
			s.users[i].Access = nil
		} else {
			s.users[i].Access = cloneAccess(access)
		}
		if err := fsutil.WriteJSONAtomic(s.usersPath, s.users); err != nil {
			return err
		}
		s.stampUsersLocked()
		return nil
	}
	return ErrUserNotFound
}

// DeleteUser removes a user and their sessions. Refuses to delete the last admin.
func (s *Store) DeleteUser(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadUsersLocked()
	idx := -1
	admins := 0
	for i, u := range s.users {
		if u.IsAdmin() {
			admins++
		}
		if u.Username == username {
			idx = i
		}
	}
	if idx < 0 {
		return ErrUserNotFound
	}
	if s.users[idx].IsAdmin() && admins <= 1 {
		return errors.New("cannot delete the last admin")
	}
	for k, sess := range s.sessions {
		if sess.Username == username {
			delete(s.sessions, k)
		}
	}
	s.users = append(s.users[:idx], s.users[idx+1:]...)
	if err := fsutil.WriteJSONAtomic(s.sessPath, s.sessions); err != nil {
		return err
	}
	if err := fsutil.WriteJSONAtomic(s.usersPath, s.users); err != nil {
		return err
	}
	s.stampUsersLocked()
	return nil
}

func cloneAccess(in map[string]ServerAccess) map[string]ServerAccess {
	if in == nil {
		return nil
	}
	out := make(map[string]ServerAccess, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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

// Authenticate verifies credentials and, when the account has two-factor auth
// enabled, the TOTP code. A per-IP rate limit covers both factors.
func (s *Store) Authenticate(ip, username, password, code string) error {
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
	var hash, secret string
	found := false
	for _, u := range s.users {
		if u.Username == username {
			hash = u.Hash
			secret = u.TOTPSecret
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
	if secret != "" {
		if strings.TrimSpace(code) == "" {
			return ErrTOTPRequired
		}
		step, valid := validateTOTP(secret, code, time.Now(), s.lastTOTP[username])
		if !valid {
			return ErrInvalidTOTP
		}
		s.lastTOTP[username] = step
	}
	delete(s.fails, ip)
	return nil
}

// AuthenticatePasswordOnly verifies username/password without TOTP.
// Used by SFTP (clients cannot supply a second factor).
func (s *Store) AuthenticatePasswordOnly(ip, username, password string) error {
	s.mu.Lock()
	if !s.allowAttemptLocked(ip) {
		s.mu.Unlock()
		return ErrRateLimited
	}
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
	delete(s.fails, ip)
	s.mu.Unlock()
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
