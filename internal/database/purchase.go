package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

type InvoiceType string

const (
	InvoiceTypeCrypto   InvoiceType = "crypto"   // historical only, no longer creatable
	InvoiceTypeYookasa  InvoiceType = "yookasa"  // historical only, no longer creatable
	InvoiceTypeTelegram InvoiceType = "telegram" // historical only, no longer creatable
	InvoiceTypeTribute  InvoiceType = "tribute"
	InvoiceTypeRollyPay InvoiceType = "rollypay"
)

type PurchaseStatus string

const (
	PurchaseStatusNew     PurchaseStatus = "new"
	PurchaseStatusPending PurchaseStatus = "pending"
	PurchaseStatusPaid    PurchaseStatus = "paid"
	PurchaseStatusCancel  PurchaseStatus = "cancel"
)

type Purchase struct {
	ID                int64          `db:"id"`
	Amount            float64        `db:"amount"`
	CustomerID        int64          `db:"customer_id"`
	CreatedAt         time.Time      `db:"created_at"`
	Month             int            `db:"month"`
	PaidAt            *time.Time     `db:"paid_at"`
	Currency          string         `db:"currency"`
	ExpireAt          *time.Time     `db:"expire_at"`
	Status            PurchaseStatus `db:"status"`
	InvoiceType       InvoiceType    `db:"invoice_type"`
	CryptoInvoiceID   *int64         `db:"crypto_invoice_id"`
	CryptoInvoiceLink *string        `db:"crypto_invoice_url"`
	YookasaURL        *string        `db:"yookasa_url"`
	YookasaID         *uuid.UUID     `db:"yookasa_id"`
	ProviderPaymentID *string        `db:"provider_payment_id"`
	ProviderPayURL    *string        `db:"provider_pay_url"`
}

type PurchaseRepository struct {
	pool *pgxpool.Pool
}

func NewPurchaseRepository(pool *pgxpool.Pool) *PurchaseRepository {
	return &PurchaseRepository{
		pool: pool,
	}
}

func (cr *PurchaseRepository) Create(ctx context.Context, purchase *Purchase) (int64, error) {
	buildInsert := sq.Insert("purchase").
		Columns("amount", "customer_id", "month", "currency", "expire_at", "status", "invoice_type", "crypto_invoice_id", "crypto_invoice_url", "yookasa_url", "yookasa_id", "provider_payment_id", "provider_pay_url").
		Values(purchase.Amount, purchase.CustomerID, purchase.Month, purchase.Currency, purchase.ExpireAt, purchase.Status, purchase.InvoiceType, purchase.CryptoInvoiceID, purchase.CryptoInvoiceLink, purchase.YookasaURL, purchase.YookasaID, purchase.ProviderPaymentID, purchase.ProviderPayURL).
		Suffix("RETURNING id").
		PlaceholderFormat(sq.Dollar)

	sql, args, err := buildInsert.ToSql()
	if err != nil {
		return 0, err
	}

	var id int64
	err = cr.pool.QueryRow(ctx, sql, args...).Scan(&id)
	if err != nil {
		return 0, err
	}

	return id, nil
}

func (cr *PurchaseRepository) FindByInvoiceTypeAndStatus(ctx context.Context, invoiceType InvoiceType, status PurchaseStatus) (*[]Purchase, error) {
	buildSelect := sq.Select("*").
		From("purchase").
		Where(sq.And{
			sq.Eq{"invoice_type": invoiceType},
			sq.Eq{"status": status},
		}).
		PlaceholderFormat(sq.Dollar)

	sql, args, err := buildSelect.ToSql()
	if err != nil {
		return nil, err
	}

	rows, err := cr.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query purchases: %w", err)
	}
	defer rows.Close()

	purchases := []Purchase{}
	for rows.Next() {
		purchase := Purchase{}
		err = rows.Scan(
			&purchase.ID,
			&purchase.Amount,
			&purchase.CustomerID,
			&purchase.CreatedAt,
			&purchase.Month,
			&purchase.PaidAt,
			&purchase.Currency,
			&purchase.ExpireAt,
			&purchase.Status,
			&purchase.InvoiceType,
			&purchase.CryptoInvoiceID,
			&purchase.CryptoInvoiceLink,
			&purchase.YookasaURL,
			&purchase.YookasaID,
			&purchase.ProviderPaymentID,
			&purchase.ProviderPayURL,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan purchase: %w", err)
		}
		purchases = append(purchases, purchase)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return &purchases, nil
}

func (cr *PurchaseRepository) FindById(ctx context.Context, id int64) (*Purchase, error) {
	buildSelect := sq.Select("*").
		From("purchase").
		Where(sq.Eq{"id": id}).
		PlaceholderFormat(sq.Dollar)

	sql, args, err := buildSelect.ToSql()
	if err != nil {
		return nil, err
	}
	purchase := &Purchase{}

	err = cr.pool.QueryRow(ctx, sql, args...).Scan(
		&purchase.ID,
		&purchase.Amount,
		&purchase.CustomerID,
		&purchase.CreatedAt,
		&purchase.Month,
		&purchase.PaidAt,
		&purchase.Currency,
		&purchase.ExpireAt,
		&purchase.Status,
		&purchase.InvoiceType,
		&purchase.CryptoInvoiceID,
		&purchase.CryptoInvoiceLink,
		&purchase.YookasaURL,
		&purchase.YookasaID,
		&purchase.ProviderPaymentID,
		&purchase.ProviderPayURL,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query purchase: %w", err)
	}

	return purchase, nil
}

func (p *PurchaseRepository) UpdateFields(ctx context.Context, id int64, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}

	buildUpdate := sq.Update("purchase").
		PlaceholderFormat(sq.Dollar).
		Where(sq.Eq{"id": id})

	for field, value := range updates {
		buildUpdate = buildUpdate.Set(field, value)
	}

	sql, args, err := buildUpdate.ToSql()
	if err != nil {
		return fmt.Errorf("failed to build update query: %w", err)
	}

	result, err := p.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("failed to update customer: %w", err)
	}

	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("no customer found with id: %d", id)
	}

	return nil
}

func (pr *PurchaseRepository) MarkAsPaid(ctx context.Context, purchaseID int64) error {
	currentTime := time.Now()

	updates := map[string]interface{}{
		"status":  PurchaseStatusPaid,
		"paid_at": currentTime,
	}

	return pr.UpdateFields(ctx, purchaseID, updates)
}

func buildLatestActiveTributesQuery(customerIDs []int64) sq.SelectBuilder {
	return sq.
		Select("*").
		From("purchase").
		Where(sq.And{
			sq.Eq{"invoice_type": InvoiceTypeTribute},
			sq.Eq{"customer_id": customerIDs},
			sq.Expr("created_at = (SELECT MAX(created_at) FROM purchase p2 WHERE p2.customer_id = purchase.customer_id AND p2.invoice_type = ?)", InvoiceTypeTribute),
		}).
		Where(sq.NotEq{"status": PurchaseStatusCancel})
}

func (pr *PurchaseRepository) FindLatestActiveTributesByCustomerIDs(
	ctx context.Context,
	customerIDs []int64,
) (*[]Purchase, error) {
	if len(customerIDs) == 0 {
		empty := make([]Purchase, 0)
		return &empty, nil
	}

	builder := buildLatestActiveTributesQuery(customerIDs).PlaceholderFormat(sq.Dollar)

	sql, args, err := builder.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	rows, err := pr.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query purchases: %w", err)
	}
	defer rows.Close()

	var purchases []Purchase
	for rows.Next() {
		var p Purchase
		if err := rows.Scan(
			&p.ID, &p.Amount, &p.CustomerID, &p.CreatedAt, &p.Month,
			&p.PaidAt, &p.Currency, &p.ExpireAt, &p.Status, &p.InvoiceType,
			&p.CryptoInvoiceID, &p.CryptoInvoiceLink, &p.YookasaURL, &p.YookasaID, &p.ProviderPaymentID, &p.ProviderPayURL,
		); err != nil {
			return nil, fmt.Errorf("scan purchase: %w", err)
		}
		purchases = append(purchases, p)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	return &purchases, nil
}

func (pr *PurchaseRepository) FindByCustomerIDAndInvoiceTypeLast(
	ctx context.Context,
	customerID int64,
	invoiceType InvoiceType,
) (*Purchase, error) {

	query := sq.Select("*").
		From("purchase").
		Where(sq.And{
			sq.Eq{"customer_id": customerID},
			sq.Eq{"invoice_type": invoiceType},
		}).
		OrderBy("created_at DESC").
		Limit(1).
		PlaceholderFormat(sq.Dollar)

	sql, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	p := &Purchase{}
	err = pr.pool.QueryRow(ctx, sql, args...).Scan(
		&p.ID, &p.Amount, &p.CustomerID, &p.CreatedAt, &p.Month,
		&p.PaidAt, &p.Currency, &p.ExpireAt, &p.Status, &p.InvoiceType,
		&p.CryptoInvoiceID, &p.CryptoInvoiceLink, &p.YookasaURL, &p.YookasaID, &p.ProviderPaymentID, &p.ProviderPayURL,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query purchase: %w", err)
	}

	return p, nil
}

// FindRecentPendingByCustomerID returns the most recent unattached pending RollyPay purchase for
// a customer within the given window — mirrors TrafficTopupRepository.FindRecentPendingByTelegramID,
// used to block a customer from spawning a second subscription invoice while one is still in
// flight.
func (pr *PurchaseRepository) FindRecentPendingByCustomerID(ctx context.Context, customerID int64, within time.Duration) (*Purchase, error) {
	query := sq.Select("*").
		From("purchase").
		Where(sq.And{
			sq.Eq{"customer_id": customerID},
			sq.Eq{"status": PurchaseStatusPending},
			sq.Eq{"invoice_type": InvoiceTypeRollyPay},
			sq.Expr("provider_payment_id IS NULL"),
			sq.Gt{"created_at": time.Now().Add(-within)},
		}).
		OrderBy("created_at DESC").
		Limit(1).
		PlaceholderFormat(sq.Dollar)

	sqlStr, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}
	p := &Purchase{}
	if err := scanPurchaseRow(pr.pool.QueryRow(ctx, sqlStr, args...), p); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find recent pending purchase: %w", err)
	}
	return p, nil
}

func scanPurchaseRow(row interface{ Scan(...interface{}) error }, p *Purchase) error {
	return row.Scan(
		&p.ID, &p.Amount, &p.CustomerID, &p.CreatedAt, &p.Month,
		&p.PaidAt, &p.Currency, &p.ExpireAt, &p.Status, &p.InvoiceType,
		&p.CryptoInvoiceID, &p.CryptoInvoiceLink, &p.YookasaURL, &p.YookasaID, &p.ProviderPaymentID, &p.ProviderPayURL,
	)
}

// FindAllPaginated powers the admin webapp orders list. status/invoiceType filter when non-empty;
// customerID narrows to a single customer when non-nil. Returns the page plus the total row count
// matching the filter.
func (pr *PurchaseRepository) FindAllPaginated(ctx context.Context, status, invoiceType string, customerID *int64, limit, offset int) ([]Purchase, int64, error) {
	var where sq.And
	if status != "" {
		where = append(where, sq.Eq{"status": status})
	}
	if invoiceType != "" {
		where = append(where, sq.Eq{"invoice_type": invoiceType})
	}
	if customerID != nil {
		where = append(where, sq.Eq{"customer_id": *customerID})
	}

	selectBuilder := sq.Select("*").From("purchase")
	countBuilder := sq.Select("COUNT(*)").From("purchase")
	if len(where) > 0 {
		selectBuilder = selectBuilder.Where(where)
		countBuilder = countBuilder.Where(where)
	}
	selectBuilder = selectBuilder.OrderBy("created_at DESC").Limit(uint64(limit)).Offset(uint64(offset)).PlaceholderFormat(sq.Dollar)

	sqlStr, args, err := selectBuilder.ToSql()
	if err != nil {
		return nil, 0, fmt.Errorf("build paginated purchase query: %w", err)
	}
	rows, err := pr.pool.Query(ctx, sqlStr, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query paginated purchases: %w", err)
	}
	defer rows.Close()
	var purchases []Purchase
	for rows.Next() {
		var p Purchase
		if err := scanPurchaseRow(rows, &p); err != nil {
			return nil, 0, fmt.Errorf("scan purchase: %w", err)
		}
		purchases = append(purchases, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate purchase rows: %w", err)
	}

	countSQL, countArgs, err := countBuilder.PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return nil, 0, fmt.Errorf("build count query: %w", err)
	}
	var total int64
	if err := pr.pool.QueryRow(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count purchases: %w", err)
	}
	return purchases, total, nil
}

// FindByCustomerID returns a page of purchases for one customer (newest first), used by the
// user-detail screen's order history tab.
func (pr *PurchaseRepository) FindByCustomerID(ctx context.Context, customerID int64, limit, offset int) ([]Purchase, int64, error) {
	return pr.FindAllPaginated(ctx, "", "", &customerID, limit, offset)
}

// RevenueDay is one bucket of the dashboard revenue-by-day chart.
type RevenueDay struct {
	Day     time.Time
	Revenue float64
	Count   int64
}

// RevenueByDay aggregates paid-purchase revenue per day over the last `days` days, oldest first.
// Replaces loading every purchase into memory for the dashboard revenue chart.
func (pr *PurchaseRepository) RevenueByDay(ctx context.Context, days int) ([]RevenueDay, error) {
	query := `SELECT date_trunc('day', paid_at) AS day, COALESCE(SUM(amount), 0), COUNT(*)
		FROM purchase
		WHERE status = 'paid' AND paid_at IS NOT NULL AND paid_at >= NOW() - make_interval(days => $1)
		GROUP BY day
		ORDER BY day ASC`
	rows, err := pr.pool.Query(ctx, query, days)
	if err != nil {
		return nil, fmt.Errorf("query revenue by day: %w", err)
	}
	defer rows.Close()
	var result []RevenueDay
	for rows.Next() {
		var d RevenueDay
		if err := rows.Scan(&d.Day, &d.Revenue, &d.Count); err != nil {
			return nil, fmt.Errorf("scan revenue day row: %w", err)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate revenue day rows: %w", err)
	}
	return result, nil
}

// RevenueSince sums paid-purchase amounts with paid_at on/after the given timestamp — a single
// scalar query (as opposed to RevenueByDay's per-day series), used by the admin header metrics
// strip's trailing-30-day revenue approximation.
func (pr *PurchaseRepository) RevenueSince(ctx context.Context, since time.Time) (float64, error) {
	query := `SELECT COALESCE(SUM(amount), 0) FROM purchase WHERE status = 'paid' AND paid_at IS NOT NULL AND paid_at >= $1`
	var total float64
	if err := pr.pool.QueryRow(ctx, query, since).Scan(&total); err != nil {
		return 0, fmt.Errorf("query revenue since: %w", err)
	}
	return total, nil
}
