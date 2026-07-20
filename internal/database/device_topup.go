package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

// DeviceTopup mirrors TrafficTopup's shape for a device-slot purchase. DeviceCount is always 1
// per row (a flat, repeatable +1-slot purchase — no bulk-quantity picker) but is kept as its own
// field rather than hardcoded, matching TrafficTopup's GBAmount pattern.
type DeviceTopup struct {
	ID                int64
	TelegramID        int64
	RemnawaveUUID     string
	DeviceCount       int
	PriceAmount       float64
	Currency          string
	RollyPayPaymentID *string
	TargetDeviceLimit *int
	Status            TopupStatus
	CreatedAt         time.Time
	CompletedAt       *time.Time
}

type DeviceTopupRepository struct {
	pool *pgxpool.Pool
}

func NewDeviceTopupRepository(pool *pgxpool.Pool) *DeviceTopupRepository {
	return &DeviceTopupRepository{pool: pool}
}

const deviceTopupSelectCols = `id, telegram_id, remnawave_uuid, device_count, price_amount, currency,
	rollypay_payment_id, target_device_limit, status, created_at, completed_at`

func scanDeviceTopup(row interface {
	Scan(...any) error
}) (*DeviceTopup, error) {
	var t DeviceTopup
	err := row.Scan(
		&t.ID, &t.TelegramID, &t.RemnawaveUUID, &t.DeviceCount, &t.PriceAmount,
		&t.Currency, &t.RollyPayPaymentID, &t.TargetDeviceLimit, &t.Status,
		&t.CreatedAt, &t.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *DeviceTopupRepository) Create(ctx context.Context, t *DeviceTopup) (int64, error) {
	query := `INSERT INTO device_topups
		(telegram_id, remnawave_uuid, device_count, price_amount, currency, rollypay_payment_id, target_device_limit, status, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`
	var id int64
	err := r.pool.QueryRow(ctx, query,
		t.TelegramID, t.RemnawaveUUID, t.DeviceCount, t.PriceAmount, t.Currency,
		t.RollyPayPaymentID, t.TargetDeviceLimit, t.Status, t.CompletedAt,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create device topup: %w", err)
	}
	return id, nil
}

// FindByID fetches a single device topup row by its own id, same reasoning as
// TrafficTopupRepository.FindByID: order_id encodes the row id directly.
func (r *DeviceTopupRepository) FindByID(ctx context.Context, id int64) (*DeviceTopup, error) {
	query := `SELECT ` + deviceTopupSelectCols + ` FROM device_topups WHERE id = $1`
	t, err := scanDeviceTopup(r.pool.QueryRow(ctx, query, id))
	if err != nil {
		return nil, fmt.Errorf("find device topup by id: %w", err)
	}
	return t, nil
}

func (r *DeviceTopupRepository) FindByRollyPayPaymentID(ctx context.Context, paymentID string) (*DeviceTopup, error) {
	query := `SELECT ` + deviceTopupSelectCols + ` FROM device_topups WHERE rollypay_payment_id = $1`
	t, err := scanDeviceTopup(r.pool.QueryRow(ctx, query, paymentID))
	if err != nil {
		return nil, fmt.Errorf("find device topup by rollypay payment id: %w", err)
	}
	return t, nil
}

// FindRecentPendingByTelegramID mirrors TrafficTopupRepository's method of the same name — used
// to show a "you have a pending payment" warning before letting a user start a second purchase.
func (r *DeviceTopupRepository) FindRecentPendingByTelegramID(ctx context.Context, telegramID int64, within time.Duration) (*DeviceTopup, error) {
	query := `SELECT ` + deviceTopupSelectCols + ` FROM device_topups
		WHERE telegram_id = $1 AND status = 'pending' AND rollypay_payment_id IS NULL AND created_at > $2
		ORDER BY created_at DESC LIMIT 1`
	t, err := scanDeviceTopup(r.pool.QueryRow(ctx, query, telegramID, time.Now().Add(-within)))
	if err != nil {
		return nil, fmt.Errorf("find recent pending device topup: %w", err)
	}
	return t, nil
}

func (r *DeviceTopupRepository) MarkProcessing(ctx context.Context, id int64, paymentID string, remnaUUID string, targetLimit int) error {
	query := `UPDATE device_topups
		SET status = 'processing', rollypay_payment_id = $1, remnawave_uuid = $2, target_device_limit = $3
		WHERE id = $4`
	_, err := r.pool.Exec(ctx, query, paymentID, remnaUUID, targetLimit, id)
	if err != nil {
		return fmt.Errorf("mark device topup processing: %w", err)
	}
	return nil
}

func (r *DeviceTopupRepository) MarkCompleted(ctx context.Context, id int64) error {
	now := time.Now()
	query := `UPDATE device_topups SET status = 'completed', completed_at = $1 WHERE id = $2`
	_, err := r.pool.Exec(ctx, query, now, id)
	if err != nil {
		return fmt.Errorf("mark device topup completed: %w", err)
	}
	return nil
}

func (r *DeviceTopupRepository) MarkFailed(ctx context.Context, id int64) error {
	query := `UPDATE device_topups SET status = 'failed' WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("mark device topup failed: %w", err)
	}
	return nil
}

func (r *DeviceTopupRepository) ExpireByID(ctx context.Context, id int64) error {
	query := `UPDATE device_topups SET status = 'expired' WHERE id = $1 AND status = 'pending'`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

// ExpireOldPending expires unattached pending records older than olderThan, mirroring
// TrafficTopupRepository's cleanup method (called from the same hourly cron in main.go).
func (r *DeviceTopupRepository) ExpireOldPending(ctx context.Context, olderThan time.Duration) (int64, error) {
	query := `UPDATE device_topups SET status = 'expired'
		WHERE status = 'pending' AND rollypay_payment_id IS NULL AND created_at < $1`
	result, err := r.pool.Exec(ctx, query, time.Now().Add(-olderThan))
	if err != nil {
		return 0, fmt.Errorf("expire old pending device topups: %w", err)
	}
	return result.RowsAffected(), nil
}
