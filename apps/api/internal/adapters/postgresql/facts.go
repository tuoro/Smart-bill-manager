package postgresqladapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (s *Store) ListTrips(ctx context.Context, tenantID string) ([]ports.Trip, error) {
	rows, err := s.db.QueryContext(ctx, tripSummarySelect+`
		WHERE trip.tenant_id = ? AND trip.deleted_at IS NULL
		ORDER BY trip.start_date DESC, trip.end_date DESC, trip.id DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list trips: %w", err)
	}
	defer rows.Close()
	items := make([]ports.Trip, 0)
	for rows.Next() {
		item, err := scanTripSummary(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func populateInvoiceItems(
	ctx context.Context,
	queryer reimbursementQueryer,
	tenantID string,
	invoices []ports.Invoice,
	invoiceIndexes map[string]int,
) error {
	if len(invoices) == 0 {
		return nil
	}
	arguments := make([]any, 0, len(invoices)+1)
	arguments = append(arguments, tenantID)
	placeholders := make([]string, 0, len(invoices))
	for _, invoice := range invoices {
		placeholders = append(placeholders, "?")
		arguments = append(arguments, invoice.ID)
	}
	query := `
		SELECT invoice_id, item_key, name, quantity, unit, unit_price_minor, amount_minor, tax_minor, sort_order
		FROM invoice_items
		WHERE tenant_id = ? AND invoice_id IN (` + strings.Join(placeholders, ",") + `)
		  AND review_decision_id = (SELECT current_review_decision_id FROM invoices WHERE invoices.tenant_id = invoice_items.tenant_id AND invoices.id = invoice_items.invoice_id)
		ORDER BY invoice_id, sort_order, item_key
	`
	rows, err := queryer.QueryContext(ctx, query, arguments...)
	if err != nil {
		return fmt.Errorf("list invoice items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var invoiceID string
		var item ports.InvoiceItem
		var quantity, unit sql.NullString
		var unitPrice, tax sql.NullInt64
		if err := rows.Scan(
			&invoiceID,
			&item.ItemKey,
			&item.Name,
			&quantity,
			&unit,
			&unitPrice,
			&item.AmountMinor,
			&tax,
			&item.SortOrder,
		); err != nil {
			return fmt.Errorf("scan invoice item: %w", err)
		}
		item.Quantity = nullableString(quantity)
		item.Unit = nullableString(unit)
		item.UnitPriceMinor = nullableInt64(unitPrice)
		item.TaxMinor = nullableInt64(tax)
		index, exists := invoiceIndexes[invoiceID]
		if !exists {
			return errors.New("invoice item query returned an unexpected invoice")
		}
		invoices[index].Items = append(invoices[index].Items, item)
	}
	return rows.Err()
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func allocationStatus(totalMinor, allocatedMinor int64) string {
	if allocatedMinor <= 0 {
		return "unallocated"
	}
	if allocatedMinor >= totalMinor {
		return "allocated"
	}
	return "partial"
}
