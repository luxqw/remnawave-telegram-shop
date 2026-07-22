package notification

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/remnawave"
)

type deviceAddonRepoMock struct {
	expiringBefore []*database.DeviceAddon
	graceExpired   []*database.DeviceAddon
	markGraceIDs   []int64
	markExpiredIDs []int64
	markExpiredErr error
}

func (m *deviceAddonRepoMock) FindActiveExpiringBefore(ctx context.Context, cutoff time.Time) ([]*database.DeviceAddon, error) {
	return m.expiringBefore, nil
}
func (m *deviceAddonRepoMock) FindGraceExpiredBefore(ctx context.Context, cutoff time.Time) ([]*database.DeviceAddon, error) {
	return m.graceExpired, nil
}
func (m *deviceAddonRepoMock) MarkGrace(ctx context.Context, id int64, graceUntil time.Time) error {
	m.markGraceIDs = append(m.markGraceIDs, id)
	return nil
}
func (m *deviceAddonRepoMock) MarkExpired(ctx context.Context, id int64) error {
	m.markExpiredIDs = append(m.markExpiredIDs, id)
	return m.markExpiredErr
}

type remnawaveDeviceClientMock struct {
	users             []remnawave.User
	devices           []remnawave.HwidDevice
	updatedLimit      *int
	deletedHwids      []string
	updateLimitErr    error
	getDevicesErr     error
	deleteDeviceErrOn string
}

func (m *remnawaveDeviceClientMock) GetUsersByTelegramID(ctx context.Context, telegramID int64) ([]remnawave.User, error) {
	return m.users, nil
}
func (m *remnawaveDeviceClientMock) UpdateUserDeviceLimit(ctx context.Context, userUUID uuid.UUID, newLimit int) error {
	if m.updateLimitErr != nil {
		return m.updateLimitErr
	}
	m.updatedLimit = &newLimit
	return nil
}
func (m *remnawaveDeviceClientMock) GetUserHwidDevices(ctx context.Context, userUUID uuid.UUID) ([]remnawave.HwidDevice, error) {
	return m.devices, m.getDevicesErr
}
func (m *remnawaveDeviceClientMock) DeleteUserHwidDevice(ctx context.Context, userUUID uuid.UUID, hwid string) error {
	if hwid == m.deleteDeviceErrOn {
		return context.DeadlineExceeded
	}
	m.deletedHwids = append(m.deletedHwids, hwid)
	return nil
}

func TestExpireAndShrink_ShrinksLimitAndTrimsOldestDevices(t *testing.T) {
	limit := 3
	rw := &remnawaveDeviceClientMock{
		users: []remnawave.User{{UUID: uuid.New(), HwidDeviceLimit: &limit}},
		devices: []remnawave.HwidDevice{
			{Hwid: "newest", UpdatedAt: time.Now()},
			{Hwid: "oldest", UpdatedAt: time.Now().Add(-72 * time.Hour)},
			{Hwid: "middle", UpdatedAt: time.Now().Add(-24 * time.Hour)},
		},
	}
	repo := &deviceAddonRepoMock{}
	svc := &DeviceAddonRenewalService{deviceAddonRepository: repo, remnawaveClient: rw, tm: nil, telegramBot: nil}

	addon := &database.DeviceAddon{ID: 7, TelegramID: 42, DeviceCount: 2, BillingMode: database.AddonBillingModeStandalone}
	svc.expireAndShrink(context.Background(), addon)

	if len(repo.markExpiredIDs) != 1 || repo.markExpiredIDs[0] != 7 {
		t.Fatalf("expected addon 7 marked expired, got %v", repo.markExpiredIDs)
	}
	if rw.updatedLimit == nil || *rw.updatedLimit != 1 {
		t.Fatalf("expected device limit shrunk to 1 (3-2), got %v", rw.updatedLimit)
	}
	// 3 devices connected, new limit 1 -> 2 oldest must be removed, newest kept.
	if len(rw.deletedHwids) != 2 {
		t.Fatalf("expected 2 devices trimmed, got %d: %v", len(rw.deletedHwids), rw.deletedHwids)
	}
	for _, hwid := range rw.deletedHwids {
		if hwid == "newest" {
			t.Fatalf("newest device should never be trimmed, got %v", rw.deletedHwids)
		}
	}
}

// TestEnterGrace_SkipsBundledAddons is a regression test for a bug caught in review: enterGrace
// was missing the same BillingMode == standalone guard ProcessRenewalReminders has, so a bundled
// (RollyPay) customer whose subscription lapsed unrenewed would get pushed into grace and sent a
// separate standalone device-addon pay link — exactly the per-customer variable charge decision 3
// says bundled customers must never receive. rollypayClient/tm/telegramBot are deliberately left
// nil: if the guard regresses, enterGrace would call MarkGrace then crash reaching
// sendRenewalPayLink, which itself is proof enough that the bundled addon isn't supposed to get
// there — the assertion below additionally verifies MarkGrace is never called for it.
func TestEnterGrace_SkipsBundledAddons(t *testing.T) {
	bundled := &database.DeviceAddon{ID: 3, TelegramID: 99, BillingMode: database.AddonBillingModeBundled}
	repo := &deviceAddonRepoMock{expiringBefore: []*database.DeviceAddon{bundled}}
	svc := &DeviceAddonRenewalService{deviceAddonRepository: repo}

	if err := svc.enterGrace(context.Background()); err != nil {
		t.Fatalf("enterGrace returned error: %v", err)
	}

	if len(repo.markGraceIDs) != 0 {
		t.Fatalf("bundled addon must never enter grace via this cron, got MarkGrace calls: %v", repo.markGraceIDs)
	}
}

// TestExpireAndShrink_SkipsBundledAddons is the defense-in-depth counterpart: even if a bundled
// addon somehow reached expireGraced (e.g. a row marked grace by a pre-fix build), it must not be
// expired or have its device limit trimmed by this cron — bundled device limits are governed by
// the subscription's own expiry, not by the addon-specific grace/trim mechanism.
func TestExpireAndShrink_SkipsBundledAddons(t *testing.T) {
	rw := &remnawaveDeviceClientMock{}
	repo := &deviceAddonRepoMock{}
	svc := &DeviceAddonRenewalService{deviceAddonRepository: repo, remnawaveClient: rw}

	bundled := &database.DeviceAddon{ID: 11, TelegramID: 99, DeviceCount: 2, BillingMode: database.AddonBillingModeBundled}
	svc.expireAndShrink(context.Background(), bundled)

	if len(repo.markExpiredIDs) != 0 {
		t.Fatalf("bundled addon must not be marked expired by this cron, got: %v", repo.markExpiredIDs)
	}
	if rw.updatedLimit != nil {
		t.Fatalf("bundled addon's device limit must not be touched by this cron, got: %v", *rw.updatedLimit)
	}
}

func TestExpireAndShrink_FloorsLimitAtZero(t *testing.T) {
	limit := 1
	rw := &remnawaveDeviceClientMock{
		users: []remnawave.User{{UUID: uuid.New(), HwidDeviceLimit: &limit}},
	}
	repo := &deviceAddonRepoMock{}
	svc := &DeviceAddonRenewalService{deviceAddonRepository: repo, remnawaveClient: rw}

	addon := &database.DeviceAddon{ID: 9, TelegramID: 42, DeviceCount: 5, BillingMode: database.AddonBillingModeStandalone}
	svc.expireAndShrink(context.Background(), addon)

	if rw.updatedLimit == nil || *rw.updatedLimit != 0 {
		t.Fatalf("expected device limit floored at 0, got %v", rw.updatedLimit)
	}
}

func TestTrimExcessDevices_NoTrimWhenWithinLimit(t *testing.T) {
	rw := &remnawaveDeviceClientMock{
		devices: []remnawave.HwidDevice{{Hwid: "a", UpdatedAt: time.Now()}},
	}
	svc := &DeviceAddonRenewalService{remnawaveClient: rw}

	removed := svc.trimExcessDevices(context.Background(), uuid.New(), 2)

	if removed != 0 || len(rw.deletedHwids) != 0 {
		t.Fatalf("expected no trim when device count is within limit, removed=%d deleted=%v", removed, rw.deletedHwids)
	}
}
