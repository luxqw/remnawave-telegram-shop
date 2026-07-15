package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
)

type NotificationLog struct {
	ID                 int64
	CreatedAt          time.Time
	CustomerTelegramID int64
	NotificationType   string
	Status             string
	Detail             *string
	ErrorMessage       *string
	Source             string
}

type NotificationLogRepository struct {
	pool *pgxpool.Pool
}

func NewNotificationLogRepository(pool *pgxpool.Pool) *NotificationLogRepository {
	return &NotificationLogRepository{pool: pool}
}

func (r *NotificationLogRepository) Create(ctx context.Context, entry NotificationLog) error {
	query := `INSERT INTO notification_log
		(customer_telegram_id, notification_type, status, detail, error_message, source)
		VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := r.pool.Exec(ctx, query,
		entry.CustomerTelegramID, entry.NotificationType, entry.Status, entry.Detail, entry.ErrorMessage, entry.Source,
	)
	if err != nil {
		return fmt.Errorf("create notification log: %w", err)
	}
	return nil
}

func (r *NotificationLogRepository) FindRecentByCustomer(ctx context.Context, customerTelegramID int64, limit int) ([]NotificationLog, error) {
	query := `SELECT id, created_at, customer_telegram_id, notification_type, status, detail, error_message, source
		FROM notification_log
		WHERE customer_telegram_id = $1
		ORDER BY created_at DESC
		LIMIT $2`
	rows, err := r.pool.Query(ctx, query, customerTelegramID, limit)
	if err != nil {
		return nil, fmt.Errorf("find recent notification log by customer: %w", err)
	}
	defer rows.Close()

	var list []NotificationLog
	for rows.Next() {
		var l NotificationLog
		if err := rows.Scan(&l.ID, &l.CreatedAt, &l.CustomerTelegramID, &l.NotificationType, &l.Status, &l.Detail, &l.ErrorMessage, &l.Source); err != nil {
			return nil, fmt.Errorf("scan notification log row: %w", err)
		}
		list = append(list, l)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("error iterating notification log rows: %w", rows.Err())
	}
	return list, nil
}

// NotificationLogFilter narrows FindAllPaginated. Zero-value fields are ignored (nil pointers /
// empty strings mean "no filter on this column").
type NotificationLogFilter struct {
	CustomerTelegramID *int64
	NotificationType   string
	Status             string
	From               *time.Time
	To                 *time.Time
}

// FindAllPaginated powers the admin webapp notification log screen with filters. Uses raw SQL
// (matching admin_audit_log.go's style) rather than squirrel.
func (r *NotificationLogRepository) FindAllPaginated(ctx context.Context, filter NotificationLogFilter, limit, offset int) ([]NotificationLog, int64, error) {
	var conds []string
	var args []interface{}
	add := func(cond string, val interface{}) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if filter.CustomerTelegramID != nil {
		add("customer_telegram_id = $%d", *filter.CustomerTelegramID)
	}
	if filter.NotificationType != "" {
		add("notification_type = $%d", filter.NotificationType)
	}
	if filter.Status != "" {
		add("status = $%d", filter.Status)
	}
	if filter.From != nil {
		add("created_at >= $%d", *filter.From)
	}
	if filter.To != nil {
		add("created_at <= $%d", *filter.To)
	}

	whereSQL := ""
	if len(conds) > 0 {
		whereSQL = "WHERE " + strings.Join(conds, " AND ")
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM notification_log %s`, whereSQL)
	var total int64
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count notification log: %w", err)
	}

	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	query := fmt.Sprintf(`SELECT id, created_at, customer_telegram_id, notification_type, status, detail, error_message, source
		FROM notification_log %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, whereSQL, limitArg, offsetArg)
	queryArgs := append(append([]interface{}{}, args...), limit, offset)

	rows, err := r.pool.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("find paginated notification log: %w", err)
	}
	defer rows.Close()

	var list []NotificationLog
	for rows.Next() {
		var l NotificationLog
		if err := rows.Scan(&l.ID, &l.CreatedAt, &l.CustomerTelegramID, &l.NotificationType, &l.Status, &l.Detail, &l.ErrorMessage, &l.Source); err != nil {
			return nil, 0, fmt.Errorf("scan notification log row: %w", err)
		}
		list = append(list, l)
	}
	if rows.Err() != nil {
		return nil, 0, fmt.Errorf("error iterating notification log rows: %w", rows.Err())
	}
	return list, total, nil
}
