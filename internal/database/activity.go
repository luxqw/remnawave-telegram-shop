package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
)

// ActivityEvent is a normalized row in the admin dashboard's activity feed, sourced from several
// tables at query time (see ActivityRepository.FindRecent) rather than a dedicated event-sourcing
// table — there's no new write path to keep in sync, so nothing can silently stop emitting events.
type ActivityEvent struct {
	Type      string // "signup" | "purchase" | "referral_bonus" | "admin_action" | "notification"
	Timestamp time.Time
	ActorID   *int64 // admin_telegram_id / referrer telegram_id; nil for signup/purchase/notification
	TargetID  int64  // telegram_id most relevant to the row
	Detail    string
}

// activityUnionSQL is the 5-arm UNION that sources every activity feed row. Shared between
// FindRecent (unfiltered, top-N) and FindAllPaginated (wrapped as a CTE and filtered), so the two
// query shapes can never drift out of sync with each other.
const activityUnionSQL = `
	(SELECT 'signup' AS type, created_at AS ts, NULL::bigint AS actor_id, telegram_id AS target_id, '' AS detail
		FROM customer
		WHERE telegram_id IS NOT NULL
		ORDER BY created_at DESC
		LIMIT $1)
	UNION ALL
	(SELECT 'purchase' AS type, p.paid_at AS ts, NULL::bigint AS actor_id, c.telegram_id AS target_id,
			(p.amount::text || ' ' || COALESCE(p.currency, '')) AS detail
		FROM purchase p
		JOIN customer c ON c.id = p.customer_id
		WHERE p.status = 'paid' AND p.paid_at IS NOT NULL
		ORDER BY p.paid_at DESC
		LIMIT $1)
	UNION ALL
	(SELECT 'referral_bonus' AS type, used_at AS ts, referrer_id AS actor_id, referee_id AS target_id, '' AS detail
		FROM referral
		WHERE bonus_granted = true
		ORDER BY used_at DESC
		LIMIT $1)
	UNION ALL
	(SELECT 'admin_action' AS type, created_at AS ts, admin_telegram_id AS actor_id, target_telegram_id AS target_id,
			(action || ':' || outcome) AS detail
		FROM admin_audit_log
		ORDER BY created_at DESC
		LIMIT $1)
	UNION ALL
	(SELECT 'notification' AS type, created_at AS ts, NULL::bigint AS actor_id, customer_telegram_id AS target_id,
			(notification_type || ':' || status || COALESCE(' — ' || detail, '')) AS detail
		FROM notification_log
		ORDER BY created_at DESC
		LIMIT $1)`

type ActivityRepository struct {
	pool *pgxpool.Pool
}

func NewActivityRepository(pool *pgxpool.Pool) *ActivityRepository {
	return &ActivityRepository{pool: pool}
}

// FindRecent returns the most recent activity across signups, completed purchases, granted
// referral bonuses, and admin actions, merged and sorted by timestamp. This is a feed, not an
// audit trail (the full per-action audit log already exists separately), so it intentionally has
// no deep pagination — just the most recent `limit` rows.
func (r *ActivityRepository) FindRecent(ctx context.Context, limit int) ([]ActivityEvent, error) {
	query := fmt.Sprintf(`
		SELECT type, ts, actor_id, target_id, detail FROM (
			%s
		) combined
		ORDER BY ts DESC
		LIMIT $1`, activityUnionSQL)

	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("find recent activity: %w", err)
	}
	defer rows.Close()

	var list []ActivityEvent
	for rows.Next() {
		var e ActivityEvent
		if err := rows.Scan(&e.Type, &e.Timestamp, &e.ActorID, &e.TargetID, &e.Detail); err != nil {
			return nil, fmt.Errorf("scan activity row: %w", err)
		}
		list = append(list, e)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("error iterating activity rows: %w", rows.Err())
	}
	return list, nil
}

// activityArmLimit bounds how many rows each UNION arm contributes into the CTE before filtering
// in FindAllPaginated. Not tied to the caller's page size — filters (e.g. type=admin_action) are
// applied after the union, so each arm needs enough headroom to support several pages of a single
// filtered type. This is a generous single-query cap for an interactive admin tool, not a data
// pipeline (same reasoning as handleOrdersExportCSV's 10k row cap).
const activityArmLimit = 5000

// ActivityFilter narrows FindAllPaginated. Zero-value fields are ignored (nil pointers / empty
// strings mean "no filter on this column").
type ActivityFilter struct {
	Type     string // "" | signup | purchase | referral_bonus | admin_action | notification
	ActorID  *int64
	TargetID *int64
	From     *time.Time
	To       *time.Time
}

// FindAllPaginated powers the admin webapp's full activity page with filters. Wraps the same
// 5-arm UNION used by FindRecent as a CTE and filters/paginates over its output columns, rather
// than duplicating filter predicates into each sub-select.
func (r *ActivityRepository) FindAllPaginated(ctx context.Context, filter ActivityFilter, limit, offset int) ([]ActivityEvent, int64, error) {
	args := []interface{}{activityArmLimit}
	var conds []string
	add := func(cond string, val interface{}) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if filter.Type != "" {
		add("type = $%d", filter.Type)
	}
	if filter.ActorID != nil {
		add("actor_id = $%d", *filter.ActorID)
	}
	if filter.TargetID != nil {
		add("target_id = $%d", *filter.TargetID)
	}
	if filter.From != nil {
		add("ts >= $%d", *filter.From)
	}
	if filter.To != nil {
		add("ts <= $%d", *filter.To)
	}

	whereSQL := ""
	if len(conds) > 0 {
		whereSQL = "WHERE " + strings.Join(conds, " AND ")
	}

	cte := fmt.Sprintf("WITH activity AS (%s)", activityUnionSQL)

	countQuery := fmt.Sprintf(`%s SELECT COUNT(*) FROM activity %s`, cte, whereSQL)
	var total int64
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count activity: %w", err)
	}

	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	query := fmt.Sprintf(`%s
		SELECT type, ts, actor_id, target_id, detail FROM activity %s
		ORDER BY ts DESC
		LIMIT $%d OFFSET $%d`, cte, whereSQL, limitArg, offsetArg)
	queryArgs := append(append([]interface{}{}, args...), limit, offset)

	rows, err := r.pool.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("find paginated activity: %w", err)
	}
	defer rows.Close()

	var list []ActivityEvent
	for rows.Next() {
		var e ActivityEvent
		if err := rows.Scan(&e.Type, &e.Timestamp, &e.ActorID, &e.TargetID, &e.Detail); err != nil {
			return nil, 0, fmt.Errorf("scan activity row: %w", err)
		}
		list = append(list, e)
	}
	if rows.Err() != nil {
		return nil, 0, fmt.Errorf("error iterating activity rows: %w", rows.Err())
	}
	return list, total, nil
}
