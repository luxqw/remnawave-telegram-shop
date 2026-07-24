package webapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

const testBotToken = "123456:TEST-BOT-TOKEN-FOR-UNIT-TESTS"

// signInitData builds a valid Telegram WebApp initData query string for the given fields, using
// the same algorithm verifyInitData checks against. Used to build both valid fixtures and, by
// tweaking the result afterward, invalid ones.
func signInitData(t *testing.T, botToken string, fields map[string]string) string {
	t.Helper()

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+fields[k])
	}
	dataCheckString := strings.Join(pairs, "\n")

	secretMac := hmac.New(sha256.New, []byte("WebAppData"))
	secretMac.Write([]byte(botToken))
	secretKey := secretMac.Sum(nil)

	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(dataCheckString))
	hash := hex.EncodeToString(mac.Sum(nil))

	values := url.Values{}
	for k, v := range fields {
		values.Set(k, v)
	}
	values.Set("hash", hash)
	return values.Encode()
}

func TestVerifyInitData(t *testing.T) {
	validUser := `{"id":42,"first_name":"Admin","username":"admin_user"}`

	tests := []struct {
		name     string
		botToken string
		fields   map[string]string
		maxAge   time.Duration
		mutate   func(raw string) string
		wantErr  bool
		wantID   int64
	}{
		{
			name:     "valid initData",
			botToken: testBotToken,
			fields: map[string]string{
				"auth_date": fmt.Sprintf("%d", time.Now().Unix()),
				"user":      validUser,
				"query_id":  "AAabc123",
			},
			maxAge: 24 * time.Hour,
			wantID: 42,
		},
		{
			name:     "tampered payload after signing",
			botToken: testBotToken,
			fields: map[string]string{
				"auth_date": fmt.Sprintf("%d", time.Now().Unix()),
				"user":      validUser,
			},
			maxAge: 24 * time.Hour,
			mutate: func(raw string) string {
				return strings.Replace(raw, "id%22%3A42", "id%22%3A999", 1)
			},
			wantErr: true,
		},
		{
			name:     "wrong bot token used to verify",
			botToken: "different-token",
			fields: map[string]string{
				"auth_date": fmt.Sprintf("%d", time.Now().Unix()),
				"user":      validUser,
			},
			maxAge:  24 * time.Hour,
			wantErr: true,
		},
		{
			name:     "expired auth_date",
			botToken: testBotToken,
			fields: map[string]string{
				"auth_date": fmt.Sprintf("%d", time.Now().Add(-48*time.Hour).Unix()),
				"user":      validUser,
			},
			maxAge:  24 * time.Hour,
			wantErr: true,
		},
		{
			name:     "missing user field",
			botToken: testBotToken,
			fields: map[string]string{
				"auth_date": fmt.Sprintf("%d", time.Now().Unix()),
			},
			maxAge:  24 * time.Hour,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := signInitData(t, testBotToken, tt.fields)
			if tt.mutate != nil {
				raw = tt.mutate(raw)
			}

			user, err := verifyInitData(raw, tt.botToken, tt.maxAge)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got user %+v", user)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if user.ID != tt.wantID {
				t.Errorf("user.ID = %d, want %d", user.ID, tt.wantID)
			}
		})
	}
}

// Session token round-trip/verify tests moved to internal/adminsession/token_test.go along with
// the code they exercise (see auth.go's comment on why it moved).
