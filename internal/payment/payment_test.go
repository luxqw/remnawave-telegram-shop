package payment

import (
	"testing"
	"time"

	"remnawave-tg-shop-bot/internal/database"
)

func TestProrateDeviceCost(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name          string
		expireAt      *time.Time
		deviceCount   int
		dailyPriceRUB float64
		wantAmount    float64
		wantDays      int
	}{
		{
			name:          "nil expire at",
			expireAt:      nil,
			deviceCount:   1,
			dailyPriceRUB: 10,
			wantAmount:    0,
			wantDays:      0,
		},
		{
			name:          "already expired",
			expireAt:      timePtr(now.Add(-time.Hour)),
			deviceCount:   1,
			dailyPriceRUB: 10,
			wantAmount:    0,
			wantDays:      0,
		},
		{
			name:          "exactly five days remaining",
			expireAt:      timePtr(now.Add(5 * 24 * time.Hour)),
			deviceCount:   1,
			dailyPriceRUB: 10,
			wantAmount:    50,
			wantDays:      5,
		},
		{
			name:          "partial day rounds up",
			expireAt:      timePtr(now.Add(1 * time.Hour)),
			deviceCount:   1,
			dailyPriceRUB: 10,
			wantAmount:    10,
			wantDays:      1,
		},
		{
			name:          "multiple device slots multiply cost",
			expireAt:      timePtr(now.Add(3 * 24 * time.Hour)),
			deviceCount:   4,
			dailyPriceRUB: 10,
			wantAmount:    120,
			wantDays:      3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAmount, gotDays := prorateDeviceCost(tt.expireAt, tt.deviceCount, tt.dailyPriceRUB)
			if gotDays != tt.wantDays {
				t.Fatalf("days: want %d, got %d", tt.wantDays, gotDays)
			}
			if gotAmount != tt.wantAmount {
				t.Fatalf("amount: want %v, got %v", tt.wantAmount, gotAmount)
			}
		})
	}
}

func TestBillingModeForTributes(t *testing.T) {
	tests := []struct {
		name     string
		tributes []database.Purchase
		wantMode database.AddonBillingMode
	}{
		{
			name:     "no active tributes bundles into rollypay renewal",
			tributes: nil,
			wantMode: database.AddonBillingModeBundled,
		},
		{
			name:     "active tribute forces standalone billing",
			tributes: []database.Purchase{{ID: 1}},
			wantMode: database.AddonBillingModeStandalone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := billingModeForTributes(tt.tributes)
			if got != tt.wantMode {
				t.Fatalf("want %v, got %v", tt.wantMode, got)
			}
		})
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
