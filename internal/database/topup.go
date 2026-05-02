package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

type TopupStatus string

const (
	TopupStatusPending    TopupStatus = "pending"
	TopupStatusProcessing TopupStatus = "processing"
	TopupStatusCompleted  TopupStatus = "completed"
	TopupStatusFailed     TopupStatus = "failed"
	TopupStatusExpired    TopupStatus = "expired"
)

type TrafficTopup struct {
	ID                      int64
	TelegramID              int64
	RemnawaveUUID           string
	GBAmount                int
	PriceAmount             float64
	Currency                string
	TributePaymentID        *string
	TargetTrafficLimitBytes *int64
	Status                  TopupStatus
	CreatedAt               time.Time
	CompletedAt             *time.Time
}

type TrafficTopupRepository struct {
	pool *pgxpool.Pool
}

func NewTrafficTopupRepository(pool *pgxpool.Pool) *TrafficTopupRepository {
	return &TrafficTopupRepository{pool: pool}
}

const topupSelectCols = `id, telegram_id, remnawave_uuid, gb_amount, price_amount, currency,
	tribute_payment_id, target_traffic_limit_bytes, status, created_at, completed_at`

func scanTopup(row interface {
	Scan(...interface{}) error
}) (*TrafficTopup, error) {
	var t TrafficTopup
	err := row.Scan(
		&t.ID, &t.TelegramID, &t.RemnawaveUUID, &t.GBAmount, &t.PriceAmount,
		&t.Currency, &t.TributePaymentID, &t.TargetTrafficLimitBytes, &t.Status,
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

func (r *TrafficTopupRepository) Create(ctx context.Context, t *TrafficTopup) (int64, error) {
	query := `INSERT INTO traffic_topups
		(telegram_id, remnawave_uuid, gb_amount, price_amount, currency, tribute_payment_id, target_traffic_limit_bytes, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`
	var id int64
	err := r.pool.QueryRow(ctx, query,
		t.TelegramID, t.RemnawaveUUID, t.GBAmount, t.PriceAmount, t.Currency,
		t.TributePaymentID, t.TargetTrafficLimitBytes, t.Status,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create topup: %w", err)
	}
	return id, nil
}

func (r *TrafficTopupRepository) FindByTributePaymentID(ctx context.Context, paymentID string) (*TrafficTopup, error) {
	query := `SELECT ` + topupSelectCols + ` FROM traffic_topups WHERE tribute_payment_id = $1`
	t, err := scanTopup(r.pool.QueryRow(ctx, query, paymentID))
	if err != nil {
		return nil, fmt.Errorf("find topup by payment id: %w", err)
	}
	return t, nil
}

// FindPendingByTelegramIDAndGB returns the most recent pending (no payment_id yet) record for a user+package.
func (r *TrafficTopupRepository) FindPendingByTelegramIDAndGB(ctx context.Context, telegramID int64, gbAmount int) (*TrafficTopup, error) {
	query := `SELECT ` + topupSelectCols + ` FROM traffic_topups
		WHERE telegram_id = $1 AND gb_amount = $2 AND status = 'pending' AND tribute_payment_id IS NULL
		ORDER BY created_at DESC LIMIT 1`
	t, err := scanTopup(r.pool.QueryRow(ctx, query, telegramID, gbAmount))
	if err != nil {
		return nil, fmt.Errorf("find pending topup: %w", err)
	}
	return t, nil
}

// FindRecentPendingByTelegramID returns any unattached pending record for a user within the given duration.
// Used to show "you have a pending payment" warning on the UI.
func (r *TrafficTopupRepository) FindRecentPendingByTelegramID(ctx context.Context, telegramID int64, within time.Duration) (*TrafficTopup, error) {
	query := `SELECT ` + topupSelectCols + ` FROM traffic_topups
		WHERE telegram_id = $1 AND status = 'pending' AND tribute_payment_id IS NULL AND created_at > $2
		ORDER BY created_at DESC LIMIT 1`
	t, err := scanTopup(r.pool.QueryRow(ctx, query, telegramID, time.Now().Add(-within)))
	if err != nil {
		return nil, fmt.Errorf("find recent pending topup: %w", err)
	}
	return t, nil
}

// MarkProcessing sets tribute_payment_id, remnawave_uuid, target bytes, and status=processing on a record.
func (r *TrafficTopupRepository) MarkProcessing(ctx context.Context, id int64, paymentID string, remnaUUID string, targetBytes int64) error {
	query := `UPDATE traffic_topups
		SET status = 'processing', tribute_payment_id = $1, remnawave_uuid = $2, target_traffic_limit_bytes = $3
		WHERE id = $4`
	_, err := r.pool.Exec(ctx, query, paymentID, remnaUUID, targetBytes, id)
	if err != nil {
		return fmt.Errorf("mark topup processing: %w", err)
	}
	return nil
}

func (r *TrafficTopupRepository) MarkCompleted(ctx context.Context, id int64) error {
	now := time.Now()
	query := `UPDATE traffic_topups SET status = 'completed', completed_at = $1 WHERE id = $2`
	_, err := r.pool.Exec(ctx, query, now, id)
	if err != nil {
		return fmt.Errorf("mark topup completed: %w", err)
	}
	return nil
}

func (r *TrafficTopupRepository) MarkFailed(ctx context.Context, id int64) error {
	query := `UPDATE traffic_topups SET status = 'failed' WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("mark topup failed: %w", err)
	}
	return nil
}

func (r *TrafficTopupRepository) ExpireByID(ctx context.Context, id int64) error {
	query := `UPDATE traffic_topups SET status = 'expired' WHERE id = $1 AND status = 'pending'`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

// FindAllLatestCompletedPerUser returns the most recent completed topup for each user
// that has at least one. Used by the topup integrity job to detect and fix resets.
func (r *TrafficTopupRepository) FindAllLatestCompletedPerUser(ctx context.Context) ([]*TrafficTopup, error) {
	query := `SELECT DISTINCT ON (telegram_id) ` + topupSelectCols + `
		FROM traffic_topups
		WHERE status = 'completed' AND target_traffic_limit_bytes IS NOT NULL
		ORDER BY telegram_id, completed_at DESC NULLS LAST`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("find all latest completed topups: %w", err)
	}
	defer rows.Close()
	var result []*TrafficTopup
	for rows.Next() {
		t, err := scanTopup(rows)
		if err != nil {
			return nil, fmt.Errorf("scan topup row: %w", err)
		}
		if t != nil {
			result = append(result, t)
		}
	}
	return result, rows.Err()
}

// ExpireOldPending expires unattached pending records older than olderThan.
func (r *TrafficTopupRepository) ExpireOldPending(ctx context.Context, olderThan time.Duration) (int64, error) {
	query := `UPDATE traffic_topups SET status = 'expired'
		WHERE status = 'pending' AND tribute_payment_id IS NULL AND created_at < $1`
	result, err := r.pool.Exec(ctx, query, time.Now().Add(-olderThan))
	if err != nil {
		return 0, fmt.Errorf("expire old pending topups: %w", err)
	}
	return result.RowsAffected(), nil
}
