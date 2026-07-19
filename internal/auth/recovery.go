package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/computenord/craftpanel/internal/fsutil"
)

const recoveryCodeCount = 10

// GenerateRecoveryCodes returns plaintext codes (shown once) and their hashes.
func GenerateRecoveryCodes() (plain []string, hashes []string, err error) {
	plain = make([]string, 0, recoveryCodeCount)
	hashes = make([]string, 0, recoveryCodeCount)
	for i := 0; i < recoveryCodeCount; i++ {
		raw := make([]byte, 5)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, err
		}
		h := hex.EncodeToString(raw)
		code := h[:4] + "-" + h[4:8]
		plain = append(plain, code)
		hashes = append(hashes, hashRecoveryCode(code))
	}
	return plain, hashes, nil
}

func hashRecoveryCode(code string) string {
	norm := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(code, " ", "")))
	norm = strings.ReplaceAll(norm, "-", "")
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}

func looksLikeRecoveryCode(code string) bool {
	code = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(code, " ", "")))
	code = strings.ReplaceAll(code, "-", "")
	if len(code) != 8 {
		return false
	}
	for _, r := range code {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func (s *Store) consumeRecoveryHashLocked(u *User, code string) bool {
	want := hashRecoveryCode(code)
	for j, h := range u.RecoveryHashes {
		if subtle.ConstantTimeCompare([]byte(h), []byte(want)) == 1 {
			u.RecoveryHashes = append(u.RecoveryHashes[:j], u.RecoveryHashes[j+1:]...)
			return true
		}
	}
	return false
}

// RegenerateRecoveryCodes replaces all recovery codes. Requires a valid TOTP code.
func (s *Store) RegenerateRecoveryCodes(username, totpCode string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadUsersLocked()
	for i := range s.users {
		if s.users[i].Username != username {
			continue
		}
		if s.users[i].TOTPSecret == "" {
			return nil, errors.New("two-factor auth is not enabled")
		}
		used, ok := validateTOTP(s.users[i].TOTPSecret, totpCode, time.Now(), s.lastTOTP[username])
		if !ok {
			return nil, ErrInvalidTOTP
		}
		s.lastTOTP[username] = used
		plain, hashes, err := GenerateRecoveryCodes()
		if err != nil {
			return nil, err
		}
		s.users[i].RecoveryHashes = hashes
		if err := fsutil.WriteJSONAtomic(s.usersPath, s.users); err != nil {
			return nil, err
		}
		s.stampUsersLocked()
		return plain, nil
	}
	return nil, ErrUserNotFound
}

// RecoveryCodesRemaining returns how many unused recovery codes the user has.
func (s *Store) RecoveryCodesRemaining(username string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadUsersLocked()
	for _, u := range s.users {
		if u.Username == username {
			return len(u.RecoveryHashes)
		}
	}
	return 0
}
