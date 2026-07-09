package adminops

import (
	"testing"
	"time"

	"remnawave-tg-shop-bot/internal/database"
)

func TestAudienceFilter(t *testing.T) {
	now := time.Now()
	future := now.Add(24 * time.Hour)
	past := now.Add(-24 * time.Hour)

	customers := map[string]database.Customer{
		"active":  {TelegramID: 1, ExpireAt: &future},
		"expired": {TelegramID: 2, ExpireAt: &past},
		"never":   {TelegramID: 3, ExpireAt: nil},
	}

	tests := []struct {
		segment string
		want    map[string]bool
	}{
		{segment: "active", want: map[string]bool{"active": true, "expired": false, "never": false}},
		{segment: "expired", want: map[string]bool{"active": false, "expired": true, "never": false}},
		{segment: "inactive", want: map[string]bool{"active": false, "expired": true, "never": true}},
		{segment: "new", want: map[string]bool{"active": false, "expired": false, "never": true}},
		{segment: "all", want: map[string]bool{"active": true, "expired": true, "never": true}},
	}

	for _, tt := range tests {
		t.Run(tt.segment, func(t *testing.T) {
			filter, err := audienceFilter(tt.segment)
			if err != nil {
				t.Fatalf("audienceFilter(%q): %v", tt.segment, err)
			}
			for name, customer := range customers {
				got := filter(customer, now)
				if got != tt.want[name] {
					t.Errorf("segment %q, customer %q: got %v, want %v", tt.segment, name, got, tt.want[name])
				}
			}
		})
	}
}

func TestAudienceFilterUnknownSegment(t *testing.T) {
	if _, err := audienceFilter("bogus"); err == nil {
		t.Fatal("expected error for unknown segment")
	}
}

func TestIsUserUnreachable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "forbidden", err: errString("Forbidden: bot was blocked by the user"), want: true},
		{name: "deactivated", err: errString("Bad Request: user is deactivated"), want: true},
		{name: "chat not found", err: errString("Bad Request: chat not found"), want: true},
		{name: "other error", err: errString("connection reset"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUserUnreachable(tt.err); got != tt.want {
				t.Errorf("isUserUnreachable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

type errString string

func (e errString) Error() string { return string(e) }
