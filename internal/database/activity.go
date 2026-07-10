package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
)

// ActivityEvent is a normalized row in the admin dashboard's activity feed, sourced from several
// tables at query time (see ActivityRepository.FindRecent) rather than a dedicated event-sourcing
// table — there's no new write path to keep in sync, so nothing can silently stop emitting events.
type ActivityEvent struct {
	Type      string // "signup" | "purchase" | "referral_bonus" | "admin_action"
	Timestamp time.Time
	ActorID   *int64 // admin_telegram_id / referrer telegram_id; nil for signup/purchase
	TargetID  int64  // telegram_id most relevant to the row
	Detail    string
}

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
	query := `
		SELECT type, ts, actor_id, target_id, detail FROM (
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
		) combined
		ORDER BY ts DESC
		LIMIT $1`

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
