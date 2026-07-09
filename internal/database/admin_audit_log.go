package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
)

type AdminAuditLog struct {
	ID               int64
	CreatedAt        time.Time
	AdminTelegramID  int64
	Action           string
	TargetTelegramID int64
	ParamInt         *int
	Outcome          string
	ErrorMessage     *string
	Source           string
}

type AdminAuditLogRepository struct {
	pool *pgxpool.Pool
}

func NewAdminAuditLogRepository(pool *pgxpool.Pool) *AdminAuditLogRepository {
	return &AdminAuditLogRepository{pool: pool}
}

func (r *AdminAuditLogRepository) Create(ctx context.Context, log *AdminAuditLog) (int64, error) {
	query := `INSERT INTO admin_audit_log
		(admin_telegram_id, action, target_telegram_id, param_int, outcome, error_message, source)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`
	var id int64
	err := r.pool.QueryRow(ctx, query,
		log.AdminTelegramID, log.Action, log.TargetTelegramID, log.ParamInt, log.Outcome, log.ErrorMessage, log.Source,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create admin audit log: %w", err)
	}
	return id, nil
}

func (r *AdminAuditLogRepository) FindRecentByTarget(ctx context.Context, targetTelegramID int64, limit int) ([]AdminAuditLog, error) {
	query := `SELECT id, created_at, admin_telegram_id, action, target_telegram_id, param_int, outcome, error_message, source
		FROM admin_audit_log
		WHERE target_telegram_id = $1
		ORDER BY created_at DESC
		LIMIT $2`
	rows, err := r.pool.Query(ctx, query, targetTelegramID, limit)
	if err != nil {
		return nil, fmt.Errorf("find recent admin audit log by target: %w", err)
	}
	defer rows.Close()

	var list []AdminAuditLog
	for rows.Next() {
		var l AdminAuditLog
		if err := rows.Scan(&l.ID, &l.CreatedAt, &l.AdminTelegramID, &l.Action, &l.TargetTelegramID, &l.ParamInt, &l.Outcome, &l.ErrorMessage, &l.Source); err != nil {
			return nil, fmt.Errorf("scan admin audit log row: %w", err)
		}
		list = append(list, l)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("error iterating admin audit log rows: %w", rows.Err())
	}
	return list, nil
}

// AdminAuditLogFilter narrows FindAllPaginated. Zero-value fields are ignored (nil pointers /
// empty strings mean "no filter on this column").
type AdminAuditLogFilter struct {
	AdminTelegramID *int64
	Action          string
	Outcome         string
	TargetID        *int64
	From            *time.Time
	To              *time.Time
}

// FindAllPaginated powers the admin webapp global audit log screen with filters. Uses raw SQL
// (matching this file's existing style) rather than squirrel.
func (r *AdminAuditLogRepository) FindAllPaginated(ctx context.Context, filter AdminAuditLogFilter, limit, offset int) ([]AdminAuditLog, int64, error) {
	var conds []string
	var args []interface{}
	add := func(cond string, val interface{}) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if filter.AdminTelegramID != nil {
		add("admin_telegram_id = $%d", *filter.AdminTelegramID)
	}
	if filter.Action != "" {
		add("action = $%d", filter.Action)
	}
	if filter.Outcome != "" {
		add("outcome = $%d", filter.Outcome)
	}
	if filter.TargetID != nil {
		add("target_telegram_id = $%d", *filter.TargetID)
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

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM admin_audit_log %s`, whereSQL)
	var total int64
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count admin audit log: %w", err)
	}

	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	query := fmt.Sprintf(`SELECT id, created_at, admin_telegram_id, action, target_telegram_id, param_int, outcome, error_message, source
		FROM admin_audit_log %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, whereSQL, limitArg, offsetArg)
	queryArgs := append(append([]interface{}{}, args...), limit, offset)

	rows, err := r.pool.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("find paginated admin audit log: %w", err)
	}
	defer rows.Close()

	var list []AdminAuditLog
	for rows.Next() {
		var l AdminAuditLog
		if err := rows.Scan(&l.ID, &l.CreatedAt, &l.AdminTelegramID, &l.Action, &l.TargetTelegramID, &l.ParamInt, &l.Outcome, &l.ErrorMessage, &l.Source); err != nil {
			return nil, 0, fmt.Errorf("scan admin audit log row: %w", err)
		}
		list = append(list, l)
	}
	if rows.Err() != nil {
		return nil, 0, fmt.Errorf("error iterating admin audit log rows: %w", rows.Err())
	}
	return list, total, nil
}
