// Package adminsession issues and verifies the admin webapp's session tokens. Split out of
// internal/webapp so internal/handler can mint a token too (for the bot's /admin "open in
// browser" link, which has no Telegram.WebApp.initData to log in with) without an import cycle:
// internal/handler already reaches internal/webapp transitively through internal/notification.
package adminsession

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Claims is the payload of an admin session token. Not a spec-compliant JWT — a single claim
// doesn't justify pulling in a JWT library — but the same shape:
// base64url(payload).base64url(HMAC-SHA256(payload)).
type Claims struct {
	Sub int64 `json:"sub"`
	Iat int64 `json:"iat"`
	Exp int64 `json:"exp"`
}

// Issue creates a signed session token for the given Telegram user ID, valid for ttl.
func Issue(secret string, sub int64, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{Sub: sub, Iat: now.Unix(), Exp: now.Add(ttl).Unix()}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payloadB64))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return payloadB64 + "." + sig, nil
}

// Verify checks the signature and expiry of a token issued by Issue. It does NOT check
// claims.Sub against the configured admin ID — callers (webapp.requireAdminSession) must do that
// themselves so a config change invalidates old tokens immediately, not just at their natural
// expiry.
func Verify(secret, token string) (Claims, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return Claims{}, errors.New("malformed token")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0]))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expectedSig), []byte(parts[1])) {
		return Claims{}, errors.New("invalid signature")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, fmt.Errorf("decode payload: %w", err)
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, fmt.Errorf("unmarshal claims: %w", err)
	}
	if time.Now().Unix() > claims.Exp {
		return Claims{}, errors.New("token expired")
	}
	return claims, nil
}
