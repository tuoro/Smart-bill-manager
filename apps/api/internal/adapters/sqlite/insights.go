package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type insightQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) ReadInsightFacts(
	ctx context.Context,
	tenantID string,
	filter domain.InsightFilter,
) ([]domain.InsightFact, error) {
	return withInsightReadSnapshot(ctx, s.db, func(queryer insightQueryer) ([]domain.InsightFact, error) {
		if filter.TripID != "" {
			var tripID string
			err := queryer.QueryRowContext(ctx, `
				SELECT id FROM trips
				WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL
			`, tenantID, filter.TripID).Scan(&tripID)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, domain.ErrNotFound
			}
			if err != nil {
				return nil, fmt.Errorf("validate insight trip: %w", err)
			}
		}
		return queryInsightFacts(ctx, queryer, tenantID, filter)
	})
}

func queryInsightFacts(
	ctx context.Context,
	queryer insightQueryer,
	tenantID string,
	filter domain.InsightFilter,
) ([]domain.InsightFact, error) {
	rows, err := queryer.QueryContext(ctx, `
		WITH payment_allocations AS (
		    SELECT tenant_id, payment_id, sum(allocated_minor) AS allocated_minor
		    FROM payment_invoice_links
		    WHERE tenant_id = ? AND ended_at IS NULL
		    GROUP BY tenant_id, payment_id
		),
		invoice_allocations AS (
		    SELECT tenant_id, invoice_id, sum(allocated_minor) AS allocated_minor
		    FROM payment_invoice_links
		    WHERE tenant_id = ? AND ended_at IS NULL
		    GROUP BY tenant_id, invoice_id
		),
		current_assignments AS (
		    SELECT assignment.tenant_id, assignment.payment_id, assignment.invoice_id,
		           trip.id AS trip_id, trip.destination AS trip_destination,
		           trip.start_date AS trip_start_date, trip.end_date AS trip_end_date
		    FROM trip_fact_assignments assignment
		    JOIN trips trip
		      ON trip.tenant_id = assignment.tenant_id
		     AND trip.id = assignment.trip_id
		     AND trip.deleted_at IS NULL
		    WHERE assignment.tenant_id = ? AND assignment.ended_at IS NULL
		),
		fact_rows AS (
		    SELECT 'payment' AS fact_type, payment.id AS fact_id,
		           payment.business_date, payment.merchant AS display_name,
		           payment.amount_minor,
		           coalesce(allocation.allocated_minor, 0) AS allocated_minor,
		           payment.currency,
		           assignment.trip_id, assignment.trip_destination,
		           assignment.trip_start_date, assignment.trip_end_date
		    FROM payments payment
		    LEFT JOIN payment_allocations allocation
		      ON allocation.tenant_id = payment.tenant_id
		     AND allocation.payment_id = payment.id
		    LEFT JOIN current_assignments assignment
		      ON assignment.tenant_id = payment.tenant_id
		     AND assignment.payment_id = payment.id
		    WHERE payment.tenant_id = ? AND payment.deleted_at IS NULL

		    UNION ALL

		    SELECT 'invoice' AS fact_type, invoice.id AS fact_id,
		           invoice.invoice_date AS business_date, invoice.seller_name AS display_name,
		           invoice.total_minor AS amount_minor,
		           coalesce(allocation.allocated_minor, 0) AS allocated_minor,
		           invoice.currency,
		           assignment.trip_id, assignment.trip_destination,
		           assignment.trip_start_date, assignment.trip_end_date
		    FROM invoices invoice
		    LEFT JOIN invoice_allocations allocation
		      ON allocation.tenant_id = invoice.tenant_id
		     AND allocation.invoice_id = invoice.id
		    LEFT JOIN current_assignments assignment
		      ON assignment.tenant_id = invoice.tenant_id
		     AND assignment.invoice_id = invoice.id
		    WHERE invoice.tenant_id = ? AND invoice.deleted_at IS NULL
		)
		SELECT fact_type, fact_id, business_date, display_name,
		       amount_minor, allocated_minor, currency,
		       trip_id, trip_destination, trip_start_date, trip_end_date
		FROM fact_rows
		WHERE (? = 'all' OR fact_type = ?)
		  AND (? = '' OR business_date >= ?)
		  AND (? = '' OR business_date <= ?)
		  AND (? = '' OR currency = ?)
		  AND (
		      ? = 'all'
		      OR (? = 'assigned' AND trip_id IS NOT NULL)
		      OR (? = 'unassigned' AND trip_id IS NULL)
		  )
		  AND (? = '' OR trip_id = ?)
		ORDER BY business_date DESC, fact_type DESC, fact_id DESC
	`,
		tenantID,
		tenantID,
		tenantID,
		tenantID,
		tenantID,
		filter.FactType, filter.FactType,
		filter.DateFrom, filter.DateFrom,
		filter.DateTo, filter.DateTo,
		filter.Currency, filter.Currency,
		filter.TripScope,
		filter.TripScope,
		filter.TripScope,
		filter.TripID, filter.TripID,
	)
	if err != nil {
		return nil, fmt.Errorf("query insight facts: %w", err)
	}
	defer rows.Close()
	items := make([]domain.InsightFact, 0)
	for rows.Next() {
		var item domain.InsightFact
		var tripID, destination, startDate, endDate sql.NullString
		if err := rows.Scan(
			&item.FactType,
			&item.FactID,
			&item.BusinessDate,
			&item.DisplayName,
			&item.AmountMinor,
			&item.AllocatedMinor,
			&item.Currency,
			&tripID,
			&destination,
			&startDate,
			&endDate,
		); err != nil {
			return nil, fmt.Errorf("scan insight fact: %w", err)
		}
		if tripID.Valid {
			if !destination.Valid || !startDate.Valid || !endDate.Valid {
				return nil, fmt.Errorf("scan insight fact: incomplete trip projection")
			}
			item.Trip = &domain.InsightTrip{
				ID:          tripID.String,
				Destination: destination.String,
				StartDate:   startDate.String,
				EndDate:     endDate.String,
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate insight facts: %w", err)
	}
	return items, nil
}

func withInsightReadSnapshot[T any](
	ctx context.Context,
	database *sql.DB,
	read func(insightQueryer) (T, error),
) (T, error) {
	var zero T
	connection, err := database.Conn(ctx)
	if err != nil {
		return zero, fmt.Errorf("acquire insight read connection: %w", err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "BEGIN DEFERRED"); err != nil {
		return zero, fmt.Errorf("begin insight read snapshot: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = connection.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()
	result, err := read(connection)
	if err != nil {
		return zero, err
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return zero, fmt.Errorf("commit insight read snapshot: %w", err)
	}
	committed = true
	return result, nil
}
