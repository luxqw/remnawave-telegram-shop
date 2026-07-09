package webapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TelegramUser is the subset of Telegram.WebApp.initData's "user" JSON field we need.
type TelegramUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	PhotoURL  string `json:"photo_url"`
}

// verifyInitData validates a Telegram WebApp initData string per Telegram's documented algorithm:
// remove "hash", sort the remaining key=value pairs, join with "\n", then HMAC-SHA256 that
// against a secret derived from the bot token (HMAC-SHA256("WebAppData", botToken)), and compare
// constant-time. maxAge rejects stale initData (the frontend only refreshes it when the Mini App
// instance is reopened, so a long-lived tab can carry an old auth_date).
func verifyInitData(initData, botToken string, maxAge time.Duration) (TelegramUser, error) {
	values, err := url.ParseQuery(initData)
	if err != nil {
		return TelegramUser{}, fmt.Errorf("parse init data: %w", err)
	}

	hash := values.Get("hash")
	if hash == "" {
		return TelegramUser{}, errors.New("missing hash")
	}
	values.Del("hash")

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+values.Get(k))
	}
	dataCheckString := strings.Join(pairs, "\n")

	secretMac := hmac.New(sha256.New, []byte("WebAppData"))
	secretMac.Write([]byte(botToken))
	secretKey := secretMac.Sum(nil)

	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(dataCheckString))
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(hash)) {
		return TelegramUser{}, errors.New("invalid hash")
	}

	authDateStr := values.Get("auth_date")
	authDateUnix, err := strconv.ParseInt(authDateStr, 10, 64)
	if err != nil {
		return TelegramUser{}, errors.New("invalid or missing auth_date")
	}
	authDate := time.Unix(authDateUnix, 0)
	if maxAge > 0 && time.Since(authDate) > maxAge {
		return TelegramUser{}, errors.New("initData expired")
	}

	userJSON := values.Get("user")
	if userJSON == "" {
		return TelegramUser{}, errors.New("missing user field")
	}
	var user TelegramUser
	if err := json.Unmarshal([]byte(userJSON), &user); err != nil {
		return TelegramUser{}, fmt.Errorf("parse user json: %w", err)
	}
	return user, nil
}

// sessionClaims is the payload of an admin session token. Not a spec-compliant JWT — a single
// claim doesn't justify pulling in a JWT library — but the same shape:
// base64url(payload).base64url(HMAC-SHA256(payload)).
type sessionClaims struct {
	Sub int64 `json:"sub"`
	Iat int64 `json:"iat"`
	Exp int64 `json:"exp"`
}

// issueSessionToken creates a signed session token for the given Telegram user ID, valid for ttl.
func issueSessionToken(secret string, sub int64, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := sessionClaims{Sub: sub, Iat: now.Unix(), Exp: now.Add(ttl).Unix()}
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

// verifySessionToken checks the signature and expiry of a session token issued by
// issueSessionToken. It does NOT check claims.Sub against the configured admin ID — callers
// (requireAdminSession) must do that themselves so a config change invalidates old tokens
// immediately, not just at their natural expiry.
func verifySessionToken(secret, token string) (sessionClaims, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return sessionClaims{}, errors.New("malformed token")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0]))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expectedSig), []byte(parts[1])) {
		return sessionClaims{}, errors.New("invalid signature")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return sessionClaims{}, fmt.Errorf("decode payload: %w", err)
	}
	var claims sessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return sessionClaims{}, fmt.Errorf("unmarshal claims: %w", err)
	}
	if time.Now().Unix() > claims.Exp {
		return sessionClaims{}, errors.New("token expired")
	}
	return claims, nil
}
