package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

const (
	WebhookStatusPending   = "pending"
	WebhookStatusProcessed = "processed"
	WebhookStatusFailed    = "failed"
)

type WebhookInbox struct {
	ID          int64
	Payload     []byte
	EventType   string
	Status      string
	Attempts    int
	ErrorMsg    *string
	CreatedAt   time.Time
	ProcessedAt *time.Time
}

type WebhookInboxRepository struct {
	pool *pgxpool.Pool
}

func NewWebhookInboxRepository(pool *pgxpool.Pool) *WebhookInboxRepository {
	return &WebhookInboxRepository{pool: pool}
}

func (r *WebhookInboxRepository) Create(ctx context.Context, payload []byte, eventType string) (int64, error) {
	q, args, err := sq.Insert("webhook_inbox").
		Columns("payload", "event_type", "status").
		Values(payload, eventType, WebhookStatusPending).
		Suffix("RETURNING id").
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("build insert: %w", err)
	}
	var id int64
	if err := r.pool.QueryRow(ctx, q, args...).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert webhook_inbox: %w", err)
	}
	return id, nil
}

func (r *WebhookInboxRepository) MarkProcessed(ctx context.Context, id int64) error {
	now := time.Now()
	q, args, err := sq.Update("webhook_inbox").
		Set("status", WebhookStatusProcessed).
		Set("processed_at", now).
		Where(sq.Eq{"id": id}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update: %w", err)
	}
	_, err = r.pool.Exec(ctx, q, args...)
	return err
}

func (r *WebhookInboxRepository) MarkFailed(ctx context.Context, id int64, errMsg string) error {
	q, args, err := sq.Update("webhook_inbox").
		Set("status", WebhookStatusFailed).
		Set("attempts", sq.Expr("attempts + 1")).
		Set("error_msg", errMsg).
		Where(sq.Eq{"id": id}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update: %w", err)
	}
	_, err = r.pool.Exec(ctx, q, args...)
	return err
}

func (r *WebhookInboxRepository) FindRetryable(ctx context.Context, maxAttempts int, minAge time.Duration) ([]WebhookInbox, error) {
	cutoff := time.Now().Add(-minAge)
	q, args, err := sq.Select("id", "payload", "event_type", "status", "attempts", "error_msg", "created_at", "processed_at").
		From("webhook_inbox").
		Where(sq.And{
			sq.Eq{"status": WebhookStatusFailed},
			sq.Lt{"attempts": maxAttempts},
			sq.Lt{"created_at": cutoff},
		}).
		OrderBy("created_at ASC").
		Limit(50).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query retryable: %w", err)
	}
	defer rows.Close()
	var results []WebhookInbox
	for rows.Next() {
		var wh WebhookInbox
		if err := rows.Scan(&wh.ID, &wh.Payload, &wh.EventType, &wh.Status, &wh.Attempts, &wh.ErrorMsg, &wh.CreatedAt, &wh.ProcessedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		results = append(results, wh)
	}
	return results, rows.Err()
}

const webhookInboxSelectCols = "id, payload, event_type, status, attempts, error_msg, created_at, processed_at"

func scanWebhookInbox(row interface{ Scan(...interface{}) error }, wh *WebhookInbox) error {
	return row.Scan(&wh.ID, &wh.Payload, &wh.EventType, &wh.Status, &wh.Attempts, &wh.ErrorMsg, &wh.CreatedAt, &wh.ProcessedAt)
}

// FindByID fetches a single webhook_inbox row, used by the admin webapp's retry endpoint and
// detail view. Returns (nil, nil) when not found, matching the repository-wide convention.
func (r *WebhookInboxRepository) FindByID(ctx context.Context, id int64) (*WebhookInbox, error) {
	q, args, err := sq.Select(webhookInboxSelectCols).
		From("webhook_inbox").
		Where(sq.Eq{"id": id}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}
	var wh WebhookInbox
	if err := scanWebhookInbox(r.pool.QueryRow(ctx, q, args...), &wh); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query webhook_inbox by id: %w", err)
	}
	return &wh, nil
}

// FindAllPaginated powers the admin webapp webhook inbox monitoring screen. status filters when
// non-empty ("pending"/"processed"/"failed"); pass "" for all statuses.
func (r *WebhookInboxRepository) FindAllPaginated(ctx context.Context, status string, limit, offset int) ([]WebhookInbox, int64, error) {
	selectBuilder := sq.Select(webhookInboxSelectCols).From("webhook_inbox")
	countBuilder := sq.Select("COUNT(*)").From("webhook_inbox")
	if status != "" {
		selectBuilder = selectBuilder.Where(sq.Eq{"status": status})
		countBuilder = countBuilder.Where(sq.Eq{"status": status})
	}
	selectBuilder = selectBuilder.OrderBy("created_at DESC").Limit(uint64(limit)).Offset(uint64(offset)).PlaceholderFormat(sq.Dollar)

	q, args, err := selectBuilder.ToSql()
	if err != nil {
		return nil, 0, fmt.Errorf("build select: %w", err)
	}
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query paginated webhook_inbox: %w", err)
	}
	defer rows.Close()
	var results []WebhookInbox
	for rows.Next() {
		var wh WebhookInbox
		if err := scanWebhookInbox(rows, &wh); err != nil {
			return nil, 0, fmt.Errorf("scan: %w", err)
		}
		results = append(results, wh)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate rows: %w", err)
	}

	countSQL, countArgs, err := countBuilder.PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return nil, 0, fmt.Errorf("build count: %w", err)
	}
	var total int64
	if err := r.pool.QueryRow(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count webhook_inbox: %w", err)
	}
	return results, total, nil
}
