package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/computenord/craftpanel/internal/fsutil"
)

const tokenPrefix = "cp_"

// APIToken is a long-lived bearer credential. The raw secret is never stored.
type APIToken struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Hash      string     `json:"hash"`
	Username  string     `json:"username"`
	CreatedAt time.Time  `json:"createdAt"`
	LastUsed  *time.Time `json:"lastUsed,omitempty"`
}

// TokenView is the safe representation returned by the API.
type TokenView struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Username  string     `json:"username"`
	CreatedAt time.Time  `json:"createdAt"`
	LastUsed  *time.Time `json:"lastUsed,omitempty"`
	// Token is only set on create, once.
	Token string `json:"token,omitempty"`
}

func (s *Store) tokensPath() string {
	return filepath.Join(filepath.Dir(s.usersPath), "api-tokens.json")
}

func (s *Store) loadTokensLocked() []APIToken {
	var list []APIToken
	_ = fsutil.ReadJSON(s.tokensPath(), &list)
	if list == nil {
		list = []APIToken{}
	}
	return list
}

func (s *Store) saveTokensLocked(list []APIToken) error {
	return fsutil.WriteJSONAtomic(s.tokensPath(), list)
}

// CreateAPIToken mints a new bearer token for username. The raw token is
// returned once in TokenView.Token.
func (s *Store) CreateAPIToken(username, name string) (TokenView, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return TokenView{}, errors.New("name must be 1-64 characters")
	}
	if _, ok := s.GetUser(username); !ok {
		return TokenView{}, ErrUserNotFound
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return TokenView{}, err
	}
	idRaw := make([]byte, 8)
	if _, err := rand.Read(idRaw); err != nil {
		return TokenView{}, err
	}
	secret := tokenPrefix + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(secret))
	tok := APIToken{
		ID:        hex.EncodeToString(idRaw),
		Name:      name,
		Hash:      hex.EncodeToString(sum[:]),
		Username:  username,
		CreatedAt: time.Now().UTC(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.loadTokensLocked()
	list = append(list, tok)
	if err := s.saveTokensLocked(list); err != nil {
		return TokenView{}, err
	}
	return TokenView{
		ID: tok.ID, Name: tok.Name, Username: tok.Username,
		CreatedAt: tok.CreatedAt, Token: secret,
	}, nil
}

// ListAPITokens returns tokens for a user (or all when username is empty and caller is admin).
func (s *Store) ListAPITokens(username string) []TokenView {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.loadTokensLocked()
	out := []TokenView{}
	for _, t := range list {
		if username != "" && t.Username != username {
			continue
		}
		out = append(out, TokenView{
			ID: t.ID, Name: t.Name, Username: t.Username,
			CreatedAt: t.CreatedAt, LastUsed: t.LastUsed,
		})
	}
	return out
}

// DeleteAPIToken removes a token by id. Non-admins may only delete their own.
func (s *Store) DeleteAPIToken(id, asUser string, admin bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.loadTokensLocked()
	for i, t := range list {
		if t.ID != id {
			continue
		}
		if !admin && t.Username != asUser {
			return errors.New("forbidden")
		}
		list = append(list[:i], list[i+1:]...)
		return s.saveTokensLocked(list)
	}
	return errors.New("token not found")
}

// ValidateAPIToken checks a Bearer secret and returns the owning user.
func (s *Store) ValidateAPIToken(raw string) (User, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, tokenPrefix) {
		return User{}, false
	}
	sum := sha256.Sum256([]byte(raw))
	hash := hex.EncodeToString(sum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.loadTokensLocked()
	for i, t := range list {
		if t.Hash != hash {
			continue
		}
		now := time.Now().UTC()
		list[i].LastUsed = &now
		_ = s.saveTokensLocked(list)
		s.reloadUsersLocked()
		for _, u := range s.users {
			if u.Username == t.Username {
				out := User{
					Username:  u.Username,
					CreatedAt: u.CreatedAt,
					Role:      u.Role,
					Access:    cloneAccess(u.Access),
				}
				if out.Role == "" {
					out.Role = RoleAdmin
				}
				return out, true
			}
		}
		return User{}, false
	}
	return User{}, false
}
