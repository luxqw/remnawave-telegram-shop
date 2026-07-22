package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

// AddonStatus tracks a device_addons row through its billing cycle: active while paid for the
// current period, grace during the post-expiry reminder window, expired once the device count
// has actually been reduced.
type AddonStatus string

const (
	AddonStatusActive  AddonStatus = "active"
	AddonStatusGrace   AddonStatus = "grace"
	AddonStatusExpired AddonStatus = "expired"
)

// AddonBillingMode controls how a device addon's recurring cost is collected. Bundled means the
// cost is folded into the customer's own RollyPay subscription renewal invoice; standalone means
// it is billed as its own separate RollyPay invoice on its own cycle — required for Tribute
// customers, since Tribute's charge amount cannot be varied per-customer.
type AddonBillingMode string

const (
	AddonBillingModeBundled    AddonBillingMode = "bundled"
	AddonBillingModeStandalone AddonBillingMode = "standalone"
)

// DeviceAddon is the current recurring-device-slot state for a customer — one active row per
// telegram_id. Historical payments for it are recorded separately in device_topups, matching how
// TrafficTopup already separates "current state" (customer.expire_at) from "payment history".
type DeviceAddon struct {
	ID             int64
	TelegramID     int64
	DeviceCount    int
	BillingMode    AddonBillingMode
	CycleExpiresAt time.Time
	GraceUntil     *time.Time
	Status         AddonStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// DetermineAddonBillingMode decides bundled vs standalone from the customer's active Tribute
// purchases: any active Tribute purchase forces standalone billing, since Tribute's charge amount
// cannot vary to include a device addon cost (see AddonBillingMode doc above).
func DetermineAddonBillingMode(activeTributes []Purchase) AddonBillingMode {
	if len(activeTributes) > 0 {
		return AddonBillingModeStandalone
	}
	return AddonBillingModeBundled
}

type DeviceAddonRepository struct {
	pool *pgxpool.Pool
}

func NewDeviceAddonRepository(pool *pgxpool.Pool) *DeviceAddonRepository {
	return &DeviceAddonRepository{pool: pool}
}

const deviceAddonSelectCols = `id, telegram_id, device_count, billing_mode, cycle_expires_at,
	grace_until, status, created_at, updated_at`

func scanDeviceAddon(row interface {
	Scan(...any) error
}) (*DeviceAddon, error) {
	var a DeviceAddon
	err := row.Scan(
		&a.ID, &a.TelegramID, &a.DeviceCount, &a.BillingMode, &a.CycleExpiresAt,
		&a.GraceUntil, &a.Status, &a.CreatedAt, &a.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *DeviceAddonRepository) Create(ctx context.Context, a *DeviceAddon) (int64, error) {
	query := `INSERT INTO device_addons
		(telegram_id, device_count, billing_mode, cycle_expires_at, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`
	var id int64
	err := r.pool.QueryRow(ctx, query,
		a.TelegramID, a.DeviceCount, a.BillingMode, a.CycleExpiresAt, a.Status,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create device addon: %w", err)
	}
	return id, nil
}

// FindActiveByTelegramID returns the customer's current device addon row, whatever its status
// (active/grace/expired) — callers branch on Status themselves. Returns nil, nil if the customer
// has never bought a device addon.
func (r *DeviceAddonRepository) FindActiveByTelegramID(ctx context.Context, telegramID int64) (*DeviceAddon, error) {
	query := `SELECT ` + deviceAddonSelectCols + ` FROM device_addons
		WHERE telegram_id = $1 ORDER BY created_at DESC LIMIT 1`
	a, err := scanDeviceAddon(r.pool.QueryRow(ctx, query, telegramID))
	if err != nil {
		return nil, fmt.Errorf("find device addon by telegram id: %w", err)
	}
	return a, nil
}

// FindByID looks up a single addon by its own id — used by the RollyPay webhook to resolve a
// standalone renewal payment's order_id back to the addon it renews.
func (r *DeviceAddonRepository) FindByID(ctx context.Context, id int64) (*DeviceAddon, error) {
	query := `SELECT ` + deviceAddonSelectCols + ` FROM device_addons WHERE id = $1`
	a, err := scanDeviceAddon(r.pool.QueryRow(ctx, query, id))
	if err != nil {
		return nil, fmt.Errorf("find device addon by id: %w", err)
	}
	return a, nil
}

// UpdateDeviceCount changes the slot count on an existing addon (e.g. buying additional slots
// mid-cycle) without touching its cycle or status.
func (r *DeviceAddonRepository) UpdateDeviceCount(ctx context.Context, id int64, deviceCount int) error {
	query := `UPDATE device_addons SET device_count = $1, updated_at = NOW() WHERE id = $2`
	_, err := r.pool.Exec(ctx, query, deviceCount, id)
	if err != nil {
		return fmt.Errorf("update device addon count: %w", err)
	}
	return nil
}

// ExtendCycle records a successful renewal payment: pushes the cycle forward and clears any grace
// state, returning the addon to active.
func (r *DeviceAddonRepository) ExtendCycle(ctx context.Context, id int64, newCycleExpiresAt time.Time) error {
	query := `UPDATE device_addons
		SET cycle_expires_at = $1, grace_until = NULL, status = $2, updated_at = NOW()
		WHERE id = $3`
	_, err := r.pool.Exec(ctx, query, newCycleExpiresAt, AddonStatusActive, id)
	if err != nil {
		return fmt.Errorf("extend device addon cycle: %w", err)
	}
	return nil
}

// MarkGrace moves an addon whose cycle has lapsed into the grace window.
func (r *DeviceAddonRepository) MarkGrace(ctx context.Context, id int64, graceUntil time.Time) error {
	query := `UPDATE device_addons SET status = $1, grace_until = $2, updated_at = NOW() WHERE id = $3`
	_, err := r.pool.Exec(ctx, query, AddonStatusGrace, graceUntil, id)
	if err != nil {
		return fmt.Errorf("mark device addon grace: %w", err)
	}
	return nil
}

// MarkExpired marks an addon as expired after its grace window lapsed unpaid. The caller is
// responsible for actually shrinking HwidDeviceLimit and trimming devices in Remnawave.
func (r *DeviceAddonRepository) MarkExpired(ctx context.Context, id int64) error {
	query := `UPDATE device_addons SET status = $1, updated_at = NOW() WHERE id = $2`
	_, err := r.pool.Exec(ctx, query, AddonStatusExpired, id)
	if err != nil {
		return fmt.Errorf("mark device addon expired: %w", err)
	}
	return nil
}

// FindActiveExpiringBefore returns active addons whose cycle ends before cutoff — used by the
// reminder cron to warn customers before their device slot cycle lapses.
func (r *DeviceAddonRepository) FindActiveExpiringBefore(ctx context.Context, cutoff time.Time) ([]*DeviceAddon, error) {
	query := `SELECT ` + deviceAddonSelectCols + ` FROM device_addons
		WHERE status = $1 AND cycle_expires_at < $2`
	rows, err := r.pool.Query(ctx, query, AddonStatusActive, cutoff)
	if err != nil {
		return nil, fmt.Errorf("find expiring device addons: %w", err)
	}
	defer rows.Close()
	return collectDeviceAddons(rows)
}

// FindGraceExpiredBefore returns addons still in grace whose grace window has lapsed before
// cutoff — used by the cron to actually shrink the device limit once grace runs out.
func (r *DeviceAddonRepository) FindGraceExpiredBefore(ctx context.Context, cutoff time.Time) ([]*DeviceAddon, error) {
	query := `SELECT ` + deviceAddonSelectCols + ` FROM device_addons
		WHERE status = $1 AND grace_until < $2`
	rows, err := r.pool.Query(ctx, query, AddonStatusGrace, cutoff)
	if err != nil {
		return nil, fmt.Errorf("find grace-expired device addons: %w", err)
	}
	defer rows.Close()
	return collectDeviceAddons(rows)
}

func collectDeviceAddons(rows pgx.Rows) ([]*DeviceAddon, error) {
	var result []*DeviceAddon
	for rows.Next() {
		a, err := scanDeviceAddon(rows)
		if err != nil {
			return nil, fmt.Errorf("scan device addon row: %w", err)
		}
		if a != nil {
			result = append(result, a)
		}
	}
	return result, rows.Err()
}
