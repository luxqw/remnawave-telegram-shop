package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
)

// MessageDirection distinguishes an admin-authored DM from a customer's reply to it, so the admin
// panel can render a single ordered thread per customer (decision 13a's "two-way reply thread").
type MessageDirection string

const (
	MessageDirectionOut MessageDirection = "out" // admin -> customer
	MessageDirectionIn  MessageDirection = "in"  // customer -> admin
)

type AdminMessage struct {
	ID                 int64
	CustomerTelegramID int64
	Direction          MessageDirection
	Text               string
	CreatedAt          time.Time
}

type AdminMessageRepository struct {
	pool *pgxpool.Pool
}

func NewAdminMessageRepository(pool *pgxpool.Pool) *AdminMessageRepository {
	return &AdminMessageRepository{pool: pool}
}

func (r *AdminMessageRepository) Create(ctx context.Context, m *AdminMessage) (int64, error) {
	query := `INSERT INTO admin_messages (customer_telegram_id, direction, text) VALUES ($1, $2, $3) RETURNING id`
	var id int64
	if err := r.pool.QueryRow(ctx, query, m.CustomerTelegramID, m.Direction, m.Text).Scan(&id); err != nil {
		return 0, fmt.Errorf("create admin message: %w", err)
	}
	return id, nil
}

// FindByCustomerTelegramID returns the customer's message thread, oldest first, capped at limit —
// this is a support-style conversation view, not a paginated log, so a simple cap is enough.
func (r *AdminMessageRepository) FindByCustomerTelegramID(ctx context.Context, telegramID int64, limit int) ([]AdminMessage, error) {
	query := `SELECT id, customer_telegram_id, direction, text, created_at FROM admin_messages
		WHERE customer_telegram_id = $1 ORDER BY created_at DESC LIMIT $2`
	rows, err := r.pool.Query(ctx, query, telegramID, limit)
	if err != nil {
		return nil, fmt.Errorf("find admin messages: %w", err)
	}
	defer rows.Close()

	var messages []AdminMessage
	for rows.Next() {
		var m AdminMessage
		if err := rows.Scan(&m.ID, &m.CustomerTelegramID, &m.Direction, &m.Text, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan admin message: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin messages: %w", err)
	}
	// Reverse to oldest-first for display — queried newest-first so LIMIT keeps the most recent
	// messages when a thread exceeds the cap, not the oldest.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}
