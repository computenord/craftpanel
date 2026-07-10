package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/computenord/craftpanel/internal/fsutil"
)

const totpPeriod = 30

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func generateTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return b32.EncodeToString(raw), nil
}

func totpCode(secret string, step int64) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1000000), nil
}

// validateTOTP accepts the current step plus one step of clock drift in each
// direction. lastUsed blocks replaying a code that already signed someone in.
func validateTOTP(secret, code string, now time.Time, lastUsed int64) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0, false
	}
	cur := now.Unix() / totpPeriod
	for d := int64(-1); d <= 1; d++ {
		step := cur + d
		if step <= lastUsed {
			continue
		}
		want, err := totpCode(secret, step)
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

func totpURL(username, secret string) string {
	return "otpauth://totp/" + url.PathEscape("Craftpanel:"+username) +
		"?secret=" + secret +
		"&issuer=" + url.QueryEscape("ComputeBox Craftpanel") +
		"&algorithm=SHA1&digits=6&period=30"
}

// TOTPEnabled reports whether the user has two-factor auth switched on.
func (s *Store) TOTPEnabled(username string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadUsersLocked()
	for _, u := range s.users {
		if u.Username == username {
			return u.TOTPSecret != ""
		}
	}
	return false
}

// InitTOTP generates a fresh secret and parks it as pending until the user
// proves their authenticator works via EnableTOTP.
func (s *Store) InitTOTP(username string) (secret, otpauth string, err error) {
	secret, err = generateTOTPSecret()
	if err != nil {
		return "", "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingTOTP[username] = secret
	return secret, totpURL(username, secret), nil
}

func (s *Store) EnableTOTP(username, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	secret, ok := s.pendingTOTP[username]
	if !ok {
		return errors.New("no pending two-factor setup, start again")
	}
	step, valid := validateTOTP(secret, code, time.Now(), 0)
	if !valid {
		return ErrInvalidTOTP
	}
	s.reloadUsersLocked()
	for i := range s.users {
		if s.users[i].Username == username {
			s.users[i].TOTPSecret = secret
			if err := fsutil.WriteJSONAtomic(s.usersPath, s.users); err != nil {
				return err
			}
			s.stampUsersLocked()
			s.lastTOTP[username] = step
			delete(s.pendingTOTP, username)
			return nil
		}
	}
	return ErrUserNotFound
}

func (s *Store) DisableTOTP(username, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadUsersLocked()
	for i := range s.users {
		if s.users[i].Username == username {
			if s.users[i].TOTPSecret == "" {
				return errors.New("two-factor auth is not enabled")
			}
			if _, valid := validateTOTP(s.users[i].TOTPSecret, code, time.Now(), s.lastTOTP[username]); !valid {
				return ErrInvalidTOTP
			}
			s.users[i].TOTPSecret = ""
			if err := fsutil.WriteJSONAtomic(s.usersPath, s.users); err != nil {
				return err
			}
			s.stampUsersLocked()
			delete(s.lastTOTP, username)
			return nil
		}
	}
	return ErrUserNotFound
}
