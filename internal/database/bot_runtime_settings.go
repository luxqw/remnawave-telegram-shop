package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v4/pgxpool"
)

// BotRuntimeSettingsRepository stores admin-editable overrides for a whitelisted set of .env
// values (see config.RuntimeSettingKeys) so an admin can change prices without a container
// restart. Deliberately a flat key/value table — the whitelist and validation live in config, not
// here, matching how customer table's UpdateFields is a thin generic setter too.
type BotRuntimeSettingsRepository struct {
	pool *pgxpool.Pool
}

func NewBotRuntimeSettingsRepository(pool *pgxpool.Pool) *BotRuntimeSettingsRepository {
	return &BotRuntimeSettingsRepository{pool: pool}
}

func (r *BotRuntimeSettingsRepository) FindAll(ctx context.Context) (map[string]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT key, value FROM bot_runtime_settings`)
	if err != nil {
		return nil, fmt.Errorf("find runtime settings: %w", err)
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan runtime setting: %w", err)
		}
		settings[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime settings: %w", err)
	}
	return settings, nil
}

// SetMany upserts every key/value pair in one statement per call, all-or-nothing via a single
// transaction — an admin PATCH updating several prices at once shouldn't apply half of them.
func (r *BotRuntimeSettingsRepository) SetMany(ctx context.Context, updates map[string]string) error {
	if len(updates) == 0 {
		return nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	for k, v := range updates {
		if _, err := tx.Exec(ctx, `INSERT INTO bot_runtime_settings (key, value, updated_at) VALUES ($1, $2, NOW())
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`, k, v); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("upsert runtime setting %q: %w", k, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit runtime settings: %w", err)
	}
	return nil
}
