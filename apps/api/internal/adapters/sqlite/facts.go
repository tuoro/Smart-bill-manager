package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (s *Store) ListPayments(ctx context.Context, tenantID string) ([]ports.Payment, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH allocations AS (
		    SELECT tenant_id, payment_id, sum(allocated_minor) AS allocated_minor
		    FROM payment_invoice_links
		    WHERE ended_at IS NULL
		    GROUP BY tenant_id, payment_id
		)
		SELECT p.id, p.amount_minor, coalesce(a.allocated_minor, 0),
		       p.currency, p.merchant, p.transaction_time, p.source_timezone,
		       p.payment_method, p.order_number, p.category, p.created_at
		FROM payments p
		LEFT JOIN allocations a ON a.tenant_id = p.tenant_id AND a.payment_id = p.id
		WHERE p.tenant_id = ? AND p.deleted_at IS NULL
		ORDER BY p.transaction_time DESC, p.id DESC
		LIMIT 200
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}
	defer rows.Close()
	items := make([]ports.Payment, 0)
	for rows.Next() {
		var item ports.Payment
		var paymentMethod, orderNumber, category sql.NullString
		var createdAt string
		if err := rows.Scan(
			&item.ID,
			&item.AmountMinor,
			&item.AllocatedMinor,
			&item.Currency,
			&item.Merchant,
			&item.TransactionTime,
			&item.SourceTimezone,
			&paymentMethod,
			&orderNumber,
			&category,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan payment: %w", err)
		}
		item.PaymentMethod = nullableString(paymentMethod)
		item.OrderNumber = nullableString(orderNumber)
		item.Category = nullableString(category)
		item.RemainingMinor = item.AmountMinor - item.AllocatedMinor
		item.AllocationStatus = allocationStatus(item.AmountMinor, item.AllocatedMinor)
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse payment created_at: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListInvoices(ctx context.Context, tenantID string) ([]ports.Invoice, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH allocations AS (
		    SELECT tenant_id, invoice_id, sum(allocated_minor) AS allocated_minor
		    FROM payment_invoice_links
		    WHERE ended_at IS NULL
		    GROUP BY tenant_id, invoice_id
		)
		SELECT i.id, i.invoice_number, i.invoice_date, i.total_minor,
		       coalesce(a.allocated_minor, 0), i.tax_minor, i.currency,
		       i.seller_name, i.buyer_name, i.created_at
		FROM invoices i
		LEFT JOIN allocations a ON a.tenant_id = i.tenant_id AND a.invoice_id = i.id
		WHERE i.tenant_id = ? AND i.deleted_at IS NULL
		ORDER BY i.invoice_date DESC, i.id DESC
		LIMIT 200
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list invoices: %w", err)
	}
	defer rows.Close()
	items := make([]ports.Invoice, 0)
	invoiceIndexes := make(map[string]int)
	for rows.Next() {
		var item ports.Invoice
		var tax sql.NullInt64
		var createdAt string
		if err := rows.Scan(
			&item.ID,
			&item.InvoiceNumber,
			&item.InvoiceDate,
			&item.TotalMinor,
			&item.AllocatedMinor,
			&tax,
			&item.Currency,
			&item.SellerName,
			&item.BuyerName,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan invoice: %w", err)
		}
		item.TaxMinor = nullableInt64(tax)
		item.RemainingMinor = item.TotalMinor - item.AllocatedMinor
		item.AllocationStatus = allocationStatus(item.TotalMinor, item.AllocatedMinor)
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse invoice created_at: %w", err)
		}
		item.Items = []ports.InvoiceItem{}
		invoiceIndexes[item.ID] = len(items)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.populateInvoiceItems(ctx, tenantID, items, invoiceIndexes); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) ListTrips(ctx context.Context, tenantID string) ([]ports.Trip, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT trip.id, trip.origin, trip.destination, trip.start_date, trip.end_date,
		       trip.traveler_name, trip.transport_type, trip.booking_reference,
		       coalesce(sum(CASE WHEN assignment.payment_id IS NOT NULL THEN 1 ELSE 0 END), 0),
		       coalesce(sum(CASE WHEN assignment.invoice_id IS NOT NULL THEN 1 ELSE 0 END), 0),
		       trip.created_at
		FROM trips trip
		LEFT JOIN trip_fact_assignments assignment
		  ON assignment.tenant_id = trip.tenant_id
		 AND assignment.trip_id = trip.id
		 AND assignment.ended_at IS NULL
		WHERE trip.tenant_id = ? AND trip.deleted_at IS NULL
		GROUP BY trip.tenant_id, trip.id
		ORDER BY trip.start_date DESC, trip.end_date DESC, trip.id DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list trips: %w", err)
	}
	defer rows.Close()
	items := make([]ports.Trip, 0)
	for rows.Next() {
		var item ports.Trip
		var origin, travelerName, transportType, bookingReference sql.NullString
		var createdAt string
		if err := rows.Scan(
			&item.ID,
			&origin,
			&item.Destination,
			&item.StartDate,
			&item.EndDate,
			&travelerName,
			&transportType,
			&bookingReference,
			&item.AssignedPaymentCount,
			&item.AssignedInvoiceCount,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan trip: %w", err)
		}
		item.Origin = nullableString(origin)
		item.TravelerName = nullableString(travelerName)
		item.TransportType = nullableString(transportType)
		item.BookingReference = nullableString(bookingReference)
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse trip created_at: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) populateInvoiceItems(
	ctx context.Context,
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
		ORDER BY invoice_id, sort_order, item_key
	`
	rows, err := s.db.QueryContext(ctx, query, arguments...)
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
