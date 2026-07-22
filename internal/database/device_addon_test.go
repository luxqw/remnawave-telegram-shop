package database

import "testing"

func TestDetermineAddonBillingMode(t *testing.T) {
	tests := []struct {
		name     string
		tributes []Purchase
		want     AddonBillingMode
	}{
		{
			name:     "no active tributes bundles into rollypay renewal",
			tributes: nil,
			want:     AddonBillingModeBundled,
		},
		{
			name:     "active tribute forces standalone billing",
			tributes: []Purchase{{ID: 1}},
			want:     AddonBillingModeStandalone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineAddonBillingMode(tt.tributes)
			if got != tt.want {
				t.Fatalf("want %v, got %v", tt.want, got)
			}
		})
	}
}
