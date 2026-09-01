package postgresqladapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type insightQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const insightFactRowsCTE = `
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
		       coalesce(allocation.allocated_minor, 0)::bigint AS allocated_minor,
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
		       coalesce(allocation.allocated_minor, 0)::bigint AS allocated_minor,
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
`

const insightBaseFilter = `
	WHERE (? = 'all' OR fact_type = ?)
	  AND (? = '' OR business_date >= NULLIF(?, '')::date)
	  AND (? = '' OR business_date <= NULLIF(?, '')::date)
	  AND (? = '' OR currency = ?)
	  AND (
	      ? = 'all'
	      OR (? = 'assigned' AND trip_id IS NOT NULL)
	      OR (? = 'unassigned' AND trip_id IS NULL)
	  )
	  AND (? = '' OR trip_id = ?)
`

const insightAllocationPredicate = `(
	? = 'all'
	OR (? = 'unallocated' AND allocated_minor = 0)
	OR (? = 'partial' AND allocated_minor > 0 AND allocated_minor < amount_minor)
	OR (? = 'allocated' AND allocated_minor = amount_minor)
)`

const insightAllocationFilter = "\n\t  AND " + insightAllocationPredicate + "\n"

const insightAggregateSplit int64 = 1_000_000

func (s *Store) ReadInsightPage(
	ctx context.Context,
	tenantID string,
	filter domain.InsightFilter,
	after *domain.InsightSortKey,
	limit int,
) (domain.InsightPage, error) {
	canonical, _, err := domain.CanonicalInsightFilter(filter)
	if err != nil {
		return domain.InsightPage{}, err
	}
	if isDefaultInsightFilter(canonical) && after == nil {
		groups, items, hasMore, err := queryDefaultInsightProjection(ctx, s.db, tenantID, limit)
		if err != nil {
			return domain.InsightPage{}, err
		}
		return domain.BuildProjectedInsightPage(canonical, groups, items, nil, limit, hasMore)
	}
	return withInsightReadSnapshot(ctx, s.db, func(queryer insightQueryer) (domain.InsightPage, error) {
		if canonical.TripID != "" {
			var tripID string
			err := queryer.QueryRowContext(ctx, `
				SELECT id FROM trips
				WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL
			`, tenantID, canonical.TripID).Scan(&tripID)
			if errors.Is(err, sql.ErrNoRows) {
				return domain.InsightPage{}, domain.ErrNotFound
			}
			if err != nil {
				return domain.InsightPage{}, fmt.Errorf("validate insight trip: %w", err)
			}
		}
		groups, items, hasMore, err := queryInsightProjection(ctx, queryer, tenantID, canonical, after, limit)
		if err != nil {
			return domain.InsightPage{}, err
		}
		if after != nil {
			exists, err := insightCursorExists(ctx, queryer, tenantID, canonical, *after)
			if err != nil {
				return domain.InsightPage{}, err
			}
			if !exists {
				return domain.InsightPage{}, domain.NewRuleError(
					"invalid_insight_cursor",
					"洞察游标已不属于当前筛选结果",
					domain.ErrInvalidInput,
				)
			}
		}
		return domain.BuildProjectedInsightPage(canonical, groups, items, after, limit, hasMore)
	})
}

func isDefaultInsightFilter(filter domain.InsightFilter) bool {
	return filter.FactType == domain.InsightFactTypeAll &&
		filter.DateFrom == "" &&
		filter.DateTo == "" &&
		filter.Currency == "" &&
		filter.AllocationStatus == domain.InsightStatusAll &&
		filter.TripScope == domain.InsightTripScopeAll &&
		filter.TripID == ""
}

func queryInsightProjection(
	ctx context.Context,
	queryer insightQueryer,
	tenantID string,
	filter domain.InsightFilter,
	after *domain.InsightSortKey,
	limit int,
) ([]domain.InsightAggregate, []domain.InsightFact, bool, error) {
	enabled := 0
	afterValue := domain.InsightSortKey{}
	if after != nil {
		enabled = 1
		afterValue = *after
	}
	query := insightFactRowsCTE + `,
		scoped_facts AS MATERIALIZED (
			SELECT * FROM fact_rows
	` + insightBaseFilter + `
		),
		grouped_facts AS (
			SELECT currency, fact_type, count(*) AS fact_count,
		       '0'::text AS total_major_parts,
		       sum(amount_minor)::text AS total_remainder_parts,
		       '0'::text AS allocated_major_parts,
		       sum(allocated_minor)::text AS allocated_remainder_parts,
		       '0'::text AS remaining_major_parts,
		       sum(amount_minor - allocated_minor)::text AS remaining_remainder_parts,
		       sum(CASE WHEN allocated_minor = 0 THEN 1 ELSE 0 END) AS unallocated_count,
		       sum(CASE WHEN allocated_minor > 0 AND allocated_minor < amount_minor THEN 1 ELSE 0 END) AS partial_count,
		       sum(CASE WHEN allocated_minor = amount_minor THEN 1 ELSE 0 END) AS allocated_count
			FROM scoped_facts
			WHERE ` + insightAllocationPredicate + `
			GROUP BY currency, fact_type
		),
		paged_facts AS (
			SELECT fact_type, fact_id, business_date, display_name, amount_minor,
		       allocated_minor, currency, trip_id, trip_destination,
		       trip_start_date, trip_end_date
			FROM scoped_facts
			WHERE ` + insightAllocationPredicate + `
			  AND (
			      ? = 0
			      OR business_date < NULLIF(?, '')::date
			      OR (business_date = NULLIF(?, '')::date AND fact_type < ?)
			      OR (business_date = NULLIF(?, '')::date AND fact_type = ? AND fact_id < ?)
			  )
			ORDER BY business_date DESC, fact_type DESC, fact_id DESC
			LIMIT ?
		),
		projection_rows AS (
			SELECT 0 AS row_kind,
		       currency AS group_currency, fact_type AS group_fact_type, fact_count,
		       total_major_parts, total_remainder_parts,
		       allocated_major_parts, allocated_remainder_parts,
		       remaining_major_parts, remaining_remainder_parts,
		       unallocated_count, partial_count, allocated_count,
		       0 AS invalid_projection,
		       ''::text AS item_fact_type, ''::text AS item_fact_id,
		       ''::text AS item_business_date, ''::text AS item_display_name,
		       0::bigint AS item_amount_minor, 0::bigint AS item_allocated_minor,
		       ''::text AS item_currency, NULL::text AS item_trip_id,
		       NULL::text AS item_trip_destination, NULL::text AS item_trip_start_date,
		       NULL::text AS item_trip_end_date
			FROM grouped_facts

			UNION ALL

			SELECT 1, '', '', 0, '0', '0', '0', '0', '0', '0', 0, 0, 0,
		       CASE WHEN EXISTS(
		           SELECT 1 FROM scoped_facts
		           WHERE amount_minor < 0 OR amount_minor > ?
		              OR allocated_minor < 0 OR allocated_minor > amount_minor
		       ) THEN 1 ELSE 0 END,
		       '', '', '', '', 0, 0, '', NULL, NULL, NULL, NULL

			UNION ALL

			SELECT 2, '', '', 0, '0', '0', '0', '0', '0', '0', 0, 0, 0, 0,
		       fact_type, fact_id, business_date::text, display_name,
		       amount_minor, allocated_minor, currency,
		       trip_id, trip_destination, trip_start_date::text, trip_end_date::text
			FROM paged_facts
		)
		SELECT row_kind,
		       group_currency, group_fact_type, fact_count,
		       total_major_parts, total_remainder_parts,
		       allocated_major_parts, allocated_remainder_parts,
		       remaining_major_parts, remaining_remainder_parts,
		       unallocated_count, partial_count, allocated_count, invalid_projection,
		       item_fact_type, item_fact_id, item_business_date, item_display_name,
		       item_amount_minor, item_allocated_minor, item_currency,
		       item_trip_id, item_trip_destination, item_trip_start_date, item_trip_end_date
		FROM projection_rows
		ORDER BY row_kind, group_currency, group_fact_type DESC,
		         item_business_date DESC, item_fact_type DESC, item_fact_id DESC
	`
	arguments := insightFilterArguments(tenantID, filter)
	arguments = append(arguments, insightAllocationArguments(filter)...)
	arguments = append(arguments,
		enabled,
		afterValue.BusinessDate,
		afterValue.BusinessDate,
		afterValue.FactType,
		afterValue.BusinessDate,
		afterValue.FactType,
		afterValue.FactID,
		limit+1,
		domain.MaxSafeMinorUnits,
	)
	rows, err := queryer.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, nil, false, fmt.Errorf("query insight projection: %w", err)
	}
	defer rows.Close()
	return scanInsightProjection(rows, limit)
}

func queryDefaultInsightProjection(
	ctx context.Context,
	queryer insightQueryer,
	tenantID string,
	limit int,
) ([]domain.InsightAggregate, []domain.InsightFact, bool, error) {
	query := insightFactRowsCTE + `,
		scoped_facts AS MATERIALIZED (
			SELECT * FROM fact_rows
		),
		grouped_facts AS (
			SELECT currency, fact_type, count(*) AS fact_count,
		       '0'::text AS total_major_parts,
		       sum(amount_minor)::text AS total_remainder_parts,
		       '0'::text AS allocated_major_parts,
		       sum(allocated_minor)::text AS allocated_remainder_parts,
		       '0'::text AS remaining_major_parts,
		       sum(amount_minor - allocated_minor)::text AS remaining_remainder_parts,
		       sum(CASE WHEN allocated_minor = 0 THEN 1 ELSE 0 END) AS unallocated_count,
		       sum(CASE WHEN allocated_minor > 0 AND allocated_minor < amount_minor THEN 1 ELSE 0 END) AS partial_count,
		       sum(CASE WHEN allocated_minor = amount_minor THEN 1 ELSE 0 END) AS allocated_count
			FROM scoped_facts
			GROUP BY currency, fact_type
		),
		page_candidates AS (
			(SELECT 'payment' AS fact_type, id AS fact_id, business_date
			 FROM payments
			 WHERE tenant_id = ? AND deleted_at IS NULL
			 ORDER BY business_date DESC, id DESC
			 LIMIT ?)

			UNION ALL

			(SELECT 'invoice' AS fact_type, id AS fact_id, invoice_date AS business_date
			 FROM invoices
			 WHERE tenant_id = ? AND deleted_at IS NULL
			 ORDER BY invoice_date DESC, id DESC
			 LIMIT ?)
		),
		paged_keys AS (
			SELECT fact_type, fact_id, business_date
			FROM page_candidates
			ORDER BY business_date DESC, fact_type DESC, fact_id DESC
			LIMIT ?
		),
		paged_facts AS (
			SELECT fact.fact_type, fact.fact_id, fact.business_date, fact.display_name,
		       fact.amount_minor, fact.allocated_minor, fact.currency,
		       fact.trip_id, fact.trip_destination, fact.trip_start_date, fact.trip_end_date
			FROM paged_keys key
			JOIN scoped_facts fact
			  ON fact.fact_type = key.fact_type AND fact.fact_id = key.fact_id
		),
		projection_rows AS (
			SELECT 0 AS row_kind,
		       currency AS group_currency, fact_type AS group_fact_type, fact_count,
		       total_major_parts, total_remainder_parts,
		       allocated_major_parts, allocated_remainder_parts,
		       remaining_major_parts, remaining_remainder_parts,
		       unallocated_count, partial_count, allocated_count,
		       0 AS invalid_projection,
		       ''::text AS item_fact_type, ''::text AS item_fact_id,
		       ''::text AS item_business_date, ''::text AS item_display_name,
		       0::bigint AS item_amount_minor, 0::bigint AS item_allocated_minor,
		       ''::text AS item_currency, NULL::text AS item_trip_id,
		       NULL::text AS item_trip_destination, NULL::text AS item_trip_start_date,
		       NULL::text AS item_trip_end_date
			FROM grouped_facts

			UNION ALL

			SELECT 1, '', '', 0, '0', '0', '0', '0', '0', '0', 0, 0, 0,
		       CASE WHEN EXISTS(
		           SELECT 1 FROM scoped_facts
		           WHERE amount_minor < 0 OR amount_minor > ?
		              OR allocated_minor < 0 OR allocated_minor > amount_minor
		       ) THEN 1 ELSE 0 END,
		       '', '', '', '', 0, 0, '', NULL, NULL, NULL, NULL

			UNION ALL

			SELECT 2, '', '', 0, '0', '0', '0', '0', '0', '0', 0, 0, 0, 0,
		       fact_type, fact_id, business_date::text, display_name,
		       amount_minor, allocated_minor, currency,
		       trip_id, trip_destination, trip_start_date::text, trip_end_date::text
			FROM paged_facts
		)
		SELECT row_kind,
		       group_currency, group_fact_type, fact_count,
		       total_major_parts, total_remainder_parts,
		       allocated_major_parts, allocated_remainder_parts,
		       remaining_major_parts, remaining_remainder_parts,
		       unallocated_count, partial_count, allocated_count, invalid_projection,
		       item_fact_type, item_fact_id, item_business_date, item_display_name,
		       item_amount_minor, item_allocated_minor, item_currency,
		       item_trip_id, item_trip_destination, item_trip_start_date, item_trip_end_date
		FROM projection_rows
		ORDER BY row_kind, group_currency, group_fact_type DESC,
		         item_business_date DESC, item_fact_type DESC, item_fact_id DESC
	`
	pageSize := limit + 1
	arguments := []any{
		tenantID, tenantID, tenantID, tenantID, tenantID,
		tenantID, pageSize,
		tenantID, pageSize,
		pageSize,
		domain.MaxSafeMinorUnits,
	}
	rows, err := queryer.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, nil, false, fmt.Errorf("query default insight projection: %w", err)
	}
	defer rows.Close()
	return scanInsightProjection(rows, limit)
}

func scanInsightProjection(rows *sql.Rows, limit int) ([]domain.InsightAggregate, []domain.InsightFact, bool, error) {
	groups := make([]domain.InsightAggregate, 0, 8)
	items := make([]domain.InsightFact, 0, limit+1)
	validationSeen := false
	var err error
	for rows.Next() {
		var rowKind, invalidProjection int
		var group domain.InsightAggregate
		var totalMajor, totalRemainder, allocatedMajor, allocatedRemainder string
		var remainingMajor, remainingRemainder string
		var item domain.InsightFact
		var tripID, destination, startDate, endDate sql.NullString
		if err := rows.Scan(
			&rowKind,
			&group.Currency, &group.FactType, &group.Count,
			&totalMajor, &totalRemainder, &allocatedMajor, &allocatedRemainder,
			&remainingMajor, &remainingRemainder,
			&group.UnallocatedCount, &group.PartialCount, &group.AllocatedCount, &invalidProjection,
			&item.FactType, &item.FactID, &item.BusinessDate, &item.DisplayName,
			&item.AmountMinor, &item.AllocatedMinor, &item.Currency,
			&tripID, &destination, &startDate, &endDate,
		); err != nil {
			return nil, nil, false, fmt.Errorf("scan insight projection: %w", err)
		}
		switch rowKind {
		case 0:
			if invalidProjection != 0 {
				return nil, nil, false, fmt.Errorf("scan insight projection: invalid group marker")
			}
			if group.TotalMinor, err = combineInsightAggregateParts(totalMajor, totalRemainder); err != nil {
				return nil, nil, false, err
			}
			if group.AllocatedMinor, err = combineInsightAggregateParts(allocatedMajor, allocatedRemainder); err != nil {
				return nil, nil, false, err
			}
			if group.RemainingMinor, err = combineInsightAggregateParts(remainingMajor, remainingRemainder); err != nil {
				return nil, nil, false, err
			}
			groups = append(groups, group)
		case 1:
			if validationSeen || invalidProjection != 0 {
				if invalidProjection != 0 {
					return nil, nil, false, invalidInsightAggregate()
				}
				return nil, nil, false, fmt.Errorf("scan insight projection: duplicate validation row")
			}
			validationSeen = true
		case 2:
			if invalidProjection != 0 {
				return nil, nil, false, fmt.Errorf("scan insight projection: invalid item marker")
			}
			if tripID.Valid {
				if !destination.Valid || !startDate.Valid || !endDate.Valid {
					return nil, nil, false, fmt.Errorf("scan insight projection: incomplete trip")
				}
				item.Trip = &domain.InsightTrip{ID: tripID.String, Destination: destination.String, StartDate: startDate.String, EndDate: endDate.String}
			}
			items = append(items, item)
		default:
			return nil, nil, false, fmt.Errorf("scan insight projection: invalid row kind")
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, false, fmt.Errorf("iterate insight projection: %w", err)
	}
	if !validationSeen {
		return nil, nil, false, fmt.Errorf("iterate insight projection: missing validation row")
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return groups, items, hasMore, nil
}

func insightAllocationArguments(filter domain.InsightFilter) []any {
	return []any{filter.AllocationStatus, filter.AllocationStatus, filter.AllocationStatus, filter.AllocationStatus}
}

func combineInsightAggregateParts(majorText, remainderText string) (int64, error) {
	majorParts, majorOK := new(big.Int).SetString(majorText, 10)
	remainderParts, remainderOK := new(big.Int).SetString(remainderText, 10)
	if !majorOK || !remainderOK || majorParts.Sign() < 0 || remainderParts.Sign() < 0 {
		return 0, invalidInsightAggregate()
	}
	split := big.NewInt(insightAggregateSplit)
	total := new(big.Int).Mul(majorParts, split)
	total.Add(total, remainderParts)
	if total.Cmp(big.NewInt(domain.MaxSafeMinorUnits)) > 0 {
		return 0, insightAggregateOverflow()
	}
	return total.Int64(), nil
}

func invalidInsightAggregate() error {
	return domain.NewRuleError("insight_projection_invalid", "洞察数据状态不合法", domain.ErrConflict)
}

func insightAggregateOverflow() error {
	return domain.NewRuleError(
		"insight_amount_overflow",
		"洞察金额汇总超出安全整数范围",
		domain.ErrConflict,
	)
}

func insightCursorExists(
	ctx context.Context,
	queryer insightQueryer,
	tenantID string,
	filter domain.InsightFilter,
	after domain.InsightSortKey,
) (bool, error) {
	query := insightFactRowsCTE + `
		SELECT EXISTS(
			SELECT 1 FROM fact_rows
	` + insightBaseFilter + insightAllocationFilter + `
			  AND business_date = ? AND fact_type = ? AND fact_id = ?
		)
	`
	arguments := append(insightFilterArguments(tenantID, filter), after.BusinessDate, after.FactType, after.FactID)
	var exists bool
	if err := queryer.QueryRowContext(ctx, query, arguments...).Scan(&exists); err != nil {
		return false, fmt.Errorf("validate insight cursor: %w", err)
	}
	return exists, nil
}

func insightBaseArguments(tenantID string, filter domain.InsightFilter) []any {
	return []any{
		tenantID,
		tenantID,
		tenantID,
		tenantID,
		tenantID,
		filter.FactType,
		filter.FactType,
		filter.DateFrom,
		filter.DateFrom,
		filter.DateTo,
		filter.DateTo,
		filter.Currency,
		filter.Currency,
		filter.TripScope,
		filter.TripScope,
		filter.TripScope,
		filter.TripID,
		filter.TripID,
	}
}

func insightFilterArguments(tenantID string, filter domain.InsightFilter) []any {
	return append(
		insightBaseArguments(tenantID, filter),
		filter.AllocationStatus,
		filter.AllocationStatus,
		filter.AllocationStatus,
		filter.AllocationStatus,
	)
}

func withInsightReadSnapshot[T any](
	ctx context.Context,
	database *sql.DB,
	read func(insightQueryer) (T, error),
) (T, error) {
	var zero T
	transaction, err := database.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return zero, fmt.Errorf("begin insight read snapshot: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "SET LOCAL plan_cache_mode = force_custom_plan"); err != nil {
		_ = transaction.Rollback()
		return zero, fmt.Errorf("configure insight read snapshot: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()
	result, err := read(transaction)
	if err != nil {
		return zero, err
	}
	if err := transaction.Commit(); err != nil {
		return zero, fmt.Errorf("commit insight read snapshot: %w", err)
	}
	committed = true
	return result, nil
}
