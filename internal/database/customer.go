package database

import (
	"context"
	"errors"
	"fmt"
	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"log/slog"
	"remnawave-tg-shop-bot/utils"
	"strconv"
	"time"
)

type CustomerRepository struct {
	pool *pgxpool.Pool
}

func NewCustomerRepository(poll *pgxpool.Pool) *CustomerRepository {
	return &CustomerRepository{pool: poll}
}

type Customer struct {
	ID                   int64      `db:"id"`
	TelegramID           int64      `db:"telegram_id"`
	ExpireAt             *time.Time `db:"expire_at"`
	CreatedAt            time.Time  `db:"created_at"`
	SubscriptionLink     *string    `db:"subscription_link"`
	Language             string     `db:"language"`
	IsTrial              bool       `db:"is_trial"`
	LastTrafficWarningAt *time.Time `db:"last_traffic_warning_at"`
}

var cols = []string{"id", "telegram_id", "expire_at", "created_at", "subscription_link", "language", "is_trial", "last_traffic_warning_at"}

func scanCustomer(row interface{ Scan(...interface{}) error }, customer *Customer) error {
	return row.Scan(
		&customer.ID,
		&customer.TelegramID,
		&customer.ExpireAt,
		&customer.CreatedAt,
		&customer.SubscriptionLink,
		&customer.Language,
		&customer.IsTrial,
		&customer.LastTrafficWarningAt,
	)
}

func (cr *CustomerRepository) FindByExpirationRange(ctx context.Context, startDate, endDate time.Time) (*[]Customer, error) {
	sql, args, err := sq.Select(cols...).From("customer").
		Where(sq.And{
			sq.NotEq{"expire_at": nil},
			sq.GtOrEq{"expire_at": startDate},
			sq.LtOrEq{"expire_at": endDate},
		}).PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build select query: %w", err)
	}
	rows, err := cr.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query customers by expiration range: %w", err)
	}
	defer rows.Close()
	var customers []Customer
	for rows.Next() {
		var c Customer
		if err := scanCustomer(rows, &c); err != nil {
			return nil, fmt.Errorf("failed to scan customer row: %w", err)
		}
		customers = append(customers, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over customer rows: %w", err)
	}
	return &customers, nil
}

func (cr *CustomerRepository) FindById(ctx context.Context, id int64) (*Customer, error) {
	sql, args, err := sq.Select(cols...).From("customer").Where(sq.Eq{"id": id}).PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build select query: %w", err)
	}
	var c Customer
	if err := scanCustomer(cr.pool.QueryRow(ctx, sql, args...), &c); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query customer: %w", err)
	}
	return &c, nil
}

func (cr *CustomerRepository) FindByTelegramId(ctx context.Context, telegramId int64) (*Customer, error) {
	sql, args, err := sq.Select(cols...).From("customer").Where(sq.Eq{"telegram_id": telegramId}).PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build select query: %w", err)
	}
	var c Customer
	if err := scanCustomer(cr.pool.QueryRow(ctx, sql, args...), &c); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query customer: %w", err)
	}
	return &c, nil
}

func (cr *CustomerRepository) Create(ctx context.Context, customer *Customer) (*Customer, error) {
	return cr.FindOrCreate(ctx, customer)
}

func (cr *CustomerRepository) FindOrCreate(ctx context.Context, customer *Customer) (*Customer, error) {
	query := `
		INSERT INTO customer (telegram_id, expire_at, language)
		VALUES ($1, $2, $3)
		ON CONFLICT (telegram_id) DO UPDATE SET telegram_id = customer.telegram_id
		RETURNING id, telegram_id, expire_at, created_at, subscription_link, language, is_trial, last_traffic_warning_at
	`
	var result Customer
	if err := scanCustomer(cr.pool.QueryRow(ctx, query, customer.TelegramID, customer.ExpireAt, customer.Language), &result); err != nil {
		return nil, fmt.Errorf("failed to find or create customer: %w", err)
	}
	slog.Info("user found or created in bot database", "telegramId", utils.MaskHalfInt64(result.TelegramID))
	return &result, nil
}

func (cr *CustomerRepository) UpdateFields(ctx context.Context, id int64, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}
	buildUpdate := sq.Update("customer").PlaceholderFormat(sq.Dollar).Where(sq.Eq{"id": id})
	for field, value := range updates {
		buildUpdate = buildUpdate.Set(field, value)
	}
	sql, args, err := buildUpdate.ToSql()
	if err != nil {
		return fmt.Errorf("failed to build update query: %w", err)
	}
	tx, err := cr.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	result, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("failed to update customer: %w", err)
	}
	if result.RowsAffected() == 0 {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("no customer found with id: %s", utils.MaskHalfInt64(id))
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

func (cr *CustomerRepository) FindAll(ctx context.Context) ([]Customer, error) {
	sql, args, err := sq.Select(cols...).From("customer").PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build select query: %w", err)
	}
	rows, err := cr.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query all customers: %w", err)
	}
	defer rows.Close()
	var customers []Customer
	for rows.Next() {
		var c Customer
		if err := scanCustomer(rows, &c); err != nil {
			return nil, fmt.Errorf("failed to scan customer row: %w", err)
		}
		customers = append(customers, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating customer rows: %w", err)
	}
	return customers, nil
}

func (cr *CustomerRepository) FindByTelegramIds(ctx context.Context, telegramIDs []int64) ([]Customer, error) {
	sql, args, err := sq.Select(cols...).From("customer").Where(sq.Eq{"telegram_id": telegramIDs}).PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build select query: %w", err)
	}
	rows, err := cr.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query customers: %w", err)
	}
	defer rows.Close()
	var customers []Customer
	for rows.Next() {
		var c Customer
		if err := scanCustomer(rows, &c); err != nil {
			return nil, fmt.Errorf("failed to scan customer row: %w", err)
		}
		customers = append(customers, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over customer rows: %w", err)
	}
	return customers, nil
}

func (cr *CustomerRepository) CreateBatch(ctx context.Context, customers []Customer) error {
	if len(customers) == 0 {
		return nil
	}
	builder := sq.Insert("customer").Columns("telegram_id", "expire_at", "language", "subscription_link").PlaceholderFormat(sq.Dollar)
	for _, cust := range customers {
		builder = builder.Values(cust.TelegramID, cust.ExpireAt, cust.Language, cust.SubscriptionLink)
	}
	sqlStr, args, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("failed to build batch insert query: %w", err)
	}
	tx, err := cr.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	_, err = tx.Exec(ctx, sqlStr, args...)
	if err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("failed to execute batch insert: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

func (cr *CustomerRepository) UpdateBatch(ctx context.Context, customers []Customer) error {
	if len(customers) == 0 {
		return nil
	}
	query := "UPDATE customer SET expire_at = c.expire_at, subscription_link = c.subscription_link FROM (VALUES "
	var args []interface{}
	for i, cust := range customers {
		if i > 0 {
			query += ", "
		}
		query += fmt.Sprintf("($%d::bigint, $%d::timestamp, $%d::text)", i*3+1, i*3+2, i*3+3)
		args = append(args, cust.TelegramID, cust.ExpireAt, cust.SubscriptionLink)
	}
	query += ") AS c(telegram_id, expire_at, subscription_link) WHERE customer.telegram_id = c.telegram_id"
	tx, err := cr.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	_, err = tx.Exec(ctx, query, args...)
	if err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("failed to execute batch update: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// CustomerStats holds aggregate counts for the admin dashboard. Computed with SQL COUNT FILTER
// instead of loading every customer row into memory and iterating (as AdminStatsCommandHandler
// currently does), so it stays cheap as the customer table grows.
type CustomerStats struct {
	Total       int64
	ActivePaid  int64
	ActiveTrial int64
	Expired     int64
	NoSub       int64
}

func (cr *CustomerRepository) CountStats(ctx context.Context) (CustomerStats, error) {
	query := `SELECT
		COUNT(*) AS total,
		COUNT(*) FILTER (WHERE expire_at IS NOT NULL AND expire_at >= NOW() AND is_trial = false) AS active_paid,
		COUNT(*) FILTER (WHERE expire_at IS NOT NULL AND expire_at >= NOW() AND is_trial = true) AS active_trial,
		COUNT(*) FILTER (WHERE expire_at IS NOT NULL AND expire_at < NOW()) AS expired,
		COUNT(*) FILTER (WHERE expire_at IS NULL) AS no_sub
		FROM customer`
	var s CustomerStats
	if err := cr.pool.QueryRow(ctx, query).Scan(&s.Total, &s.ActivePaid, &s.ActiveTrial, &s.Expired, &s.NoSub); err != nil {
		return CustomerStats{}, fmt.Errorf("count customer stats: %w", err)
	}
	return s, nil
}

// customerFilterWhere builds the shared WHERE clause for FindAllPaginated. filter narrows by
// subscription state, search matches an exact telegram_id (non-numeric search terms are ignored,
// matching how the bot's /admin_user lookup works today). Returns nil when neither is set.
func customerFilterWhere(filter, search string) sq.And {
	var conds sq.And
	switch filter {
	case "active":
		conds = append(conds, sq.NotEq{"expire_at": nil}, sq.Expr("expire_at >= NOW()"), sq.Eq{"is_trial": false})
	case "trial":
		conds = append(conds, sq.NotEq{"expire_at": nil}, sq.Expr("expire_at >= NOW()"), sq.Eq{"is_trial": true})
	case "expired":
		conds = append(conds, sq.NotEq{"expire_at": nil}, sq.Expr("expire_at < NOW()"))
	case "no_sub":
		conds = append(conds, sq.Eq{"expire_at": nil})
	}
	if id, err := strconv.ParseInt(search, 10, 64); err == nil && search != "" {
		conds = append(conds, sq.Eq{"telegram_id": id})
	}
	return conds
}

// FindAllPaginated powers the admin webapp users list: filter is one of "", "active", "trial",
// "expired", "no_sub"; search (when a valid int64) narrows to a single telegram_id. Returns the
// page of customers plus the total row count matching the filter (for pagination UI).
func (cr *CustomerRepository) FindAllPaginated(ctx context.Context, filter, search string, limit, offset int) ([]Customer, int64, error) {
	where := customerFilterWhere(filter, search)

	selectBuilder := sq.Select(cols...).From("customer")
	countBuilder := sq.Select("COUNT(*)").From("customer")
	if len(where) > 0 {
		selectBuilder = selectBuilder.Where(where)
		countBuilder = countBuilder.Where(where)
	}
	selectBuilder = selectBuilder.OrderBy("created_at DESC").Limit(uint64(limit)).Offset(uint64(offset)).PlaceholderFormat(sq.Dollar)

	sqlStr, args, err := selectBuilder.ToSql()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to build paginated select query: %w", err)
	}
	rows, err := cr.pool.Query(ctx, sqlStr, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query paginated customers: %w", err)
	}
	defer rows.Close()
	var customers []Customer
	for rows.Next() {
		var c Customer
		if err := scanCustomer(rows, &c); err != nil {
			return nil, 0, fmt.Errorf("failed to scan customer row: %w", err)
		}
		customers = append(customers, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating over customer rows: %w", err)
	}

	countSQL, countArgs, err := countBuilder.PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to build count query: %w", err)
	}
	var total int64
	if err := cr.pool.QueryRow(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count customers: %w", err)
	}
	return customers, total, nil
}

// DayCount is one bucket of a per-day time series (used by dashboard charts).
type DayCount struct {
	Day   time.Time
	Count int64
}

// NewCustomersByDay returns the number of new customers per day over the last `days` days,
// oldest first, for the dashboard growth chart. Empty days are omitted by the query; the caller
// fills gaps when rendering.
func (cr *CustomerRepository) NewCustomersByDay(ctx context.Context, days int) ([]DayCount, error) {
	query := `SELECT date_trunc('day', created_at) AS day, COUNT(*)
		FROM customer
		WHERE created_at >= NOW() - make_interval(days => $1)
		GROUP BY day
		ORDER BY day ASC`
	rows, err := cr.pool.Query(ctx, query, days)
	if err != nil {
		return nil, fmt.Errorf("failed to query new customers by day: %w", err)
	}
	defer rows.Close()
	var result []DayCount
	for rows.Next() {
		var d DayCount
		if err := rows.Scan(&d.Day, &d.Count); err != nil {
			return nil, fmt.Errorf("failed to scan day count row: %w", err)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating day count rows: %w", err)
	}
	return result, nil
}

func (cr *CustomerRepository) DeleteByNotInTelegramIds(ctx context.Context, telegramIDs []int64) error {
	if len(telegramIDs) == 0 {
		return fmt.Errorf("DeleteByNotInTelegramIds: refusing to delete all customers with empty ID list")
	}
	var buildDelete sq.DeleteBuilder
	{
		buildDelete = sq.Delete("customer").PlaceholderFormat(sq.Dollar).Where(sq.NotEq{"telegram_id": telegramIDs})
	}
	sqlStr, args, err := buildDelete.ToSql()
	if err != nil {
		return fmt.Errorf("failed to build delete query: %w", err)
	}
	_, err = cr.pool.Exec(ctx, sqlStr, args...)
	if err != nil {
		return fmt.Errorf("failed to delete customers: %w", err)
	}
	return nil
}
