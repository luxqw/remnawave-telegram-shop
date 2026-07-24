package database

import "testing"

func TestDeviceLimitAfterDecrease(t *testing.T) {
	tests := []struct {
		name              string
		currentLimit      int
		deviceCount       int
		targetDeviceCount int
		want              int
	}{
		{name: "drop by 1 of 3", currentLimit: 8, deviceCount: 3, targetDeviceCount: 2, want: 7},
		{name: "drop all the way to 0", currentLimit: 7, deviceCount: 2, targetDeviceCount: 0, want: 5},
		{name: "no-op when target equals current count", currentLimit: 8, deviceCount: 3, targetDeviceCount: 3, want: 8},
		{name: "no-op when target somehow exceeds current count", currentLimit: 8, deviceCount: 3, targetDeviceCount: 5, want: 8},
		{name: "floors at 0 rather than going negative", currentLimit: 1, deviceCount: 3, targetDeviceCount: 0, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeviceLimitAfterDecrease(tt.currentLimit, tt.deviceCount, tt.targetDeviceCount)
			if got != tt.want {
				t.Errorf("DeviceLimitAfterDecrease(%d, %d, %d) = %d, want %d", tt.currentLimit, tt.deviceCount, tt.targetDeviceCount, got, tt.want)
			}
		})
	}
}

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
