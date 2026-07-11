package web

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
)

// ssoClaims are the fields the Craft Cloud control plane signs for a
// single-sign-on jump into a managed panel.
type ssoClaims struct {
	InstanceID string `json:"instanceId"`
	Sub        string `json:"sub"`
	Exp        int64  `json:"exp"`
	Iat        int64  `json:"iat"`
}

// sso verifies an Ed25519-signed JWT from the control plane against the key
// pinned at enrollment and, on success, logs the customer in as the panel
// admin. Only available in managed mode.
func (h *Handler) sso(w http.ResponseWriter, r *http.Request) {
	if h.SSOKey == nil {
		http.NotFound(w, r)
		return
	}
	pubPEM := h.SSOKey()
	if pubPEM == "" {
		http.Error(w, "single sign-on is not available yet", http.StatusServiceUnavailable)
		return
	}
	if _, err := verifyEdDSAJWT(r.URL.Query().Get("token"), pubPEM); err != nil {
		http.Error(w, "invalid single sign-on token", http.StatusUnauthorized)
		return
	}
	user := h.Auth.FirstUsername()
	if user == "" {
		http.Error(w, "no account on this panel", http.StatusConflict)
		return
	}
	token, err := h.Auth.CreateSession(user)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.setSessionCookie(w, r, token, 7*24*3600)
	log.Printf("sso: signed in %q via control-plane token", user)
	http.Redirect(w, r, "/", http.StatusFound)
}

// verifyEdDSAJWT validates a compact JWS (EdDSA) and returns its claims. Uses
// only the standard library — Ed25519 keeps verification to one call.
func verifyEdDSAJWT(token, pubPEM string) (*ssoClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed token")
	}
	hdrRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	var hdr struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(hdrRaw, &hdr); err != nil {
		return nil, err
	}
	if hdr.Alg != "EdDSA" {
		return nil, errors.New("unexpected signing algorithm")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	pub, err := parseEd25519PEM(pubPEM)
	if err != nil {
		return nil, err
	}
	if !ed25519.Verify(pub, []byte(parts[0]+"."+parts[1]), sig) {
		return nil, errors.New("bad signature")
	}
	claimsRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var c ssoClaims
	if err := json.Unmarshal(claimsRaw, &c); err != nil {
		return nil, err
	}
	if c.Exp == 0 || time.Now().Unix() > c.Exp {
		return nil, errors.New("token expired")
	}
	return &c, nil
}

func parseEd25519PEM(pemStr string) (ed25519.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("not a PEM key")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("not an Ed25519 key")
	}
	return pub, nil
}
