package postgresqladapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const paymentColumns = `f.id, f.amount_minor, a.allocated_minor, f.currency, f.merchant,
 f.transaction_time, f.source_timezone, f.business_date::text, f.payment_method, f.order_number, f.category, f.created_at, sbm_fact_bad_debt(f.tenant_id,'payment',f.id)`
const invoiceColumns = `f.id, f.invoice_number, f.invoice_date::text, f.total_minor, a.allocated_minor,
 f.tax_minor, f.currency, f.seller_name, f.buyer_name, f.created_at,
 (SELECT count(*) FROM invoice_items item WHERE item.tenant_id=f.tenant_id AND item.invoice_id=f.id AND item.review_decision_id=f.current_review_decision_id), sbm_fact_bad_debt(f.tenant_id,'invoice',f.id)`

func factSelect(kind domain.DocumentType) string {
	table, columns, linkColumn := "payments", paymentColumns, "payment_id"
	if kind == domain.DocumentInvoice {
		table, columns, linkColumn = "invoices", invoiceColumns, "invoice_id"
	}
	return `SELECT ` + columns + ` FROM ` + table + ` f
 LEFT JOIN LATERAL (SELECT coalesce(sum(link.allocated_minor),0) allocated_minor FROM payment_invoice_links link
 WHERE link.tenant_id=f.tenant_id AND link.` + linkColumn + `=f.id AND link.ended_at IS NULL) a ON true`
}

func factQuerySQL(tenantID string, kind domain.DocumentType, query ports.FactQuery) (string, []any, error) {
	filter, err := domain.CanonicalFactFilter(query.Filter)
	if err != nil || query.Limit < 1 || query.Limit > 100 || query.After != nil && !query.After.Valid() {
		return "", nil, domain.ErrInvalidInput
	}
	statement := factSelect(kind) + ` WHERE f.tenant_id=? AND f.deleted_at IS NULL`
	args := []any{tenantID}
	date, amount, search := "f.business_date", "f.amount_minor", `(f.merchant ILIKE ? ESCAPE '\' OR coalesce(f.order_number,'') ILIKE ? ESCAPE '\')`
	if kind == domain.DocumentInvoice {
		date, amount, search = "f.invoice_date", "f.total_minor", `(f.seller_name ILIKE ? ESCAPE '\' OR f.buyer_name ILIKE ? ESCAPE '\' OR f.invoice_number ILIKE ? ESCAPE '\')`
	}
	if filter.DateFrom != "" {
		statement += " AND " + date + ">=?::date"
		args = append(args, filter.DateFrom)
	}
	if filter.DateTo != "" {
		statement += " AND " + date + "<=?::date"
		args = append(args, filter.DateTo)
	}
	if filter.Query != "" {
		literal := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(filter.Query) + "%"
		statement += " AND " + search
		args = append(args, literal, literal)
		if kind == domain.DocumentInvoice {
			args = append(args, literal)
		}
	}
	if filter.AllocationStatus != domain.InsightStatusAll {
		statement += ` AND CASE WHEN a.allocated_minor<=0 THEN 'unallocated' WHEN a.allocated_minor>=` + amount + ` THEN 'allocated' ELSE 'partial' END=?`
		args = append(args, filter.AllocationStatus)
	}
	if query.After != nil {
		statement += ` AND (f.created_at,f.id)<(?::timestamptz,?)`
		args = append(args, query.After.CreatedAt, query.After.ID)
	}
	statement += ` ORDER BY f.created_at DESC,f.id DESC LIMIT ?`
	args = append(args, query.Limit+1)
	return statement, args, nil
}

func (s *Store) ReadPaymentPage(ctx context.Context, tenantID string, query ports.FactQuery) (ports.FactPage[ports.Payment], error) {
	statement, args, err := factQuerySQL(tenantID, domain.DocumentPayment, query)
	if err != nil {
		return ports.FactPage[ports.Payment]{}, err
	}
	return readFactPage(ctx, s.db, statement, args, query.Limit, scanPayment, func(p ports.Payment) domain.FactSortKey { return domain.FactSortKey{CreatedAt: p.CreatedAt, ID: p.ID} })
}

func (s *Store) ReadInvoicePage(ctx context.Context, tenantID string, query ports.FactQuery) (ports.FactPage[ports.Invoice], error) {
	statement, args, err := factQuerySQL(tenantID, domain.DocumentInvoice, query)
	if err != nil {
		return ports.FactPage[ports.Invoice]{}, err
	}
	return readFactPage(ctx, s.db, statement, args, query.Limit, scanInvoice, func(i ports.Invoice) domain.FactSortKey { return domain.FactSortKey{CreatedAt: i.CreatedAt, ID: i.ID} })
}

type factScanner interface{ Scan(...any) error }

func readFactPage[T any](ctx context.Context, q reimbursementQueryer, statement string, args []any, limit int, scan func(factScanner) (T, error), key func(T) domain.FactSortKey) (ports.FactPage[T], error) {
	page := ports.FactPage[T]{Items: []T{}}
	rows, err := q.QueryContext(ctx, statement, args...)
	if err != nil {
		return page, fmt.Errorf("query fact page: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		next := key(page.Items[limit-1])
		page.Next = &next
		page.Items = page.Items[:limit]
	}
	return page, nil
}

func scanPayment(row factScanner) (ports.Payment, error) {
	var item ports.Payment
	var method, order, category sql.NullString
	var created string
	if err := row.Scan(&item.ID, &item.AmountMinor, &item.AllocatedMinor, &item.Currency, &item.Merchant, &item.TransactionTime, &item.SourceTimezone, &item.BusinessDate, &method, &order, &category, &created, &item.BadDebt); err != nil {
		return item, err
	}
	item.PaymentMethod, item.OrderNumber, item.Category = nullableString(method), nullableString(order), nullableString(category)
	item.RemainingMinor = item.AmountMinor - item.AllocatedMinor
	item.AllocationStatus = allocationStatus(item.AmountMinor, item.AllocatedMinor)
	var err error
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	return item, err
}

func scanInvoice(row factScanner) (ports.Invoice, error) {
	var item ports.Invoice
	var tax sql.NullInt64
	var created string
	if err := row.Scan(&item.ID, &item.InvoiceNumber, &item.InvoiceDate, &item.TotalMinor, &item.AllocatedMinor, &tax, &item.Currency, &item.SellerName, &item.BuyerName, &created, &item.ItemCount, &item.BadDebt); err != nil {
		return item, err
	}
	item.TaxMinor = nullableInt64(tax)
	item.RemainingMinor = item.TotalMinor - item.AllocatedMinor
	item.AllocationStatus = allocationStatus(item.TotalMinor, item.AllocatedMinor)
	var err error
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	return item, err
}

func (s *Store) ReadFactDetail(ctx context.Context, tenantID string, kind domain.DocumentType, id string, includeSource bool) (ports.FactDetail, error) {
	detail := ports.FactDetail{FactType: kind, Links: []domain.CorrectionLink{}}
	if kind != domain.DocumentPayment && kind != domain.DocumentInvoice {
		return detail, domain.ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return detail, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, factSelect(kind)+` WHERE f.tenant_id=? AND f.id=? AND f.deleted_at IS NULL`, tenantID, id)
	if kind == domain.DocumentPayment {
		item, readErr := scanPayment(row)
		detail.Payment = &item
		err = readErr
	} else {
		item, readErr := scanInvoice(row)
		err = readErr
		if err == nil {
			items := []ports.Invoice{item}
			items[0].Items = []ports.InvoiceItem{}
			err = populateInvoiceItems(ctx, tx, tenantID, items, map[string]int{id: 0})
			item = items[0]
		}
		detail.Invoice = &item
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ports.FactDetail{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.FactDetail{}, err
	}
	table, column := "payments", "payment_id"
	if kind == domain.DocumentInvoice {
		table, column = "invoices", "invoice_id"
	}
	if err := tx.QueryRowContext(ctx, `SELECT version FROM `+table+` WHERE tenant_id=? AND id=?`, tenantID, id).Scan(&detail.Version); err != nil {
		return ports.FactDetail{}, err
	}
	detail.Links, err = readCorrectionLinks(ctx, tx, tenantID, kind, id)
	if err != nil {
		return ports.FactDetail{}, err
	}
	var trip domain.InsightTrip
	err = tx.QueryRowContext(ctx, `SELECT trip.id,trip.name,trip.start_date::text,trip.end_date::text FROM trip_fact_assignments assignment
 JOIN trips trip ON trip.tenant_id=assignment.tenant_id AND trip.id=assignment.trip_id
 WHERE assignment.tenant_id=? AND assignment.`+column+`=? AND assignment.ended_at IS NULL AND trip.deleted_at IS NULL`, tenantID, id).Scan(&trip.ID, &trip.Name, &trip.StartDate, &trip.EndDate)
	if err == nil {
		detail.Trip = &trip
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ports.FactDetail{}, err
	}
	if includeSource {
		var source ports.FactSource
		if err := tx.QueryRowContext(ctx, `SELECT c.document_id,c.id,r.id,c.revision,CASE WHEN c.origin_ai_run_id IS NULL THEN 'manual' ELSE 'ai' END,d.original_name,d.page_count
 FROM `+table+` f JOIN review_decisions r ON r.tenant_id=f.tenant_id AND r.id=f.current_review_decision_id
 JOIN claim_sets c ON c.tenant_id=r.tenant_id AND c.id=r.claim_set_id JOIN documents d ON d.tenant_id=c.tenant_id AND d.id=c.document_id
 WHERE f.tenant_id=? AND f.id=?`, tenantID, id).Scan(&source.DocumentID, &source.ClaimSetID, &source.ReviewDecisionID, &source.Revision, &source.OriginKind, &source.OriginalName, &source.PageCount); err != nil {
			return ports.FactDetail{}, err
		}
		detail.Source = &source
	}
	return detail, nil
}
