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
	markExpiredIDs []int64
	markExpiredErr error
}

func (m *deviceAddonRepoMock) FindActiveExpiringBefore(ctx context.Context, cutoff time.Time) ([]*database.DeviceAddon, error) {
	return nil, nil
}
func (m *deviceAddonRepoMock) FindGraceExpiredBefore(ctx context.Context, cutoff time.Time) ([]*database.DeviceAddon, error) {
	return nil, nil
}
func (m *deviceAddonRepoMock) MarkGrace(ctx context.Context, id int64, graceUntil time.Time) error {
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

	addon := &database.DeviceAddon{ID: 7, TelegramID: 42, DeviceCount: 2}
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

func TestExpireAndShrink_FloorsLimitAtZero(t *testing.T) {
	limit := 1
	rw := &remnawaveDeviceClientMock{
		users: []remnawave.User{{UUID: uuid.New(), HwidDeviceLimit: &limit}},
	}
	repo := &deviceAddonRepoMock{}
	svc := &DeviceAddonRenewalService{deviceAddonRepository: repo, remnawaveClient: rw}

	addon := &database.DeviceAddon{ID: 9, TelegramID: 42, DeviceCount: 5}
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
