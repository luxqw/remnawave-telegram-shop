package adminsession

import (
	"testing"
	"time"
)

func TestSessionTokenRoundTrip(t *testing.T) {
	secret := "session-secret"

	token, err := Issue(secret, 42, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	claims, err := Verify(secret, token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Sub != 42 {
		t.Errorf("claims.Sub = %d, want 42", claims.Sub)
	}
}

func TestVerify(t *testing.T) {
	secret := "session-secret"
	validToken, err := Issue(secret, 42, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	expiredToken, err := Issue(secret, 42, -time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	tests := []struct {
		name    string
		secret  string
		token   string
		wantErr bool
	}{
		{name: "valid token", secret: secret, token: validToken},
		{name: "wrong secret", secret: "other-secret", token: validToken, wantErr: true},
		{name: "expired token", secret: secret, token: expiredToken, wantErr: true},
		{name: "malformed token", secret: secret, token: "not-a-token", wantErr: true},
		{name: "tampered signature", secret: secret, token: validToken[:len(validToken)-2] + "xx", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Verify(tt.secret, tt.token)
			if (err != nil) != tt.wantErr {
				t.Errorf("Verify() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
