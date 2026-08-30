package sqliteadapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (s *Store) ListTripAttributionCandidates(
	ctx context.Context,
	tenantID string,
	query ports.TripAttributionQuery,
) (ports.TripAttributionPage, error) {
	trip, err := s.getTripSummary(ctx, tenantID, query.TripID)
	if err != nil {
		return ports.TripAttributionPage{}, err
	}
	statement := `
		WITH fact_rows AS (
		    SELECT 'payment' AS fact_type, payment.id AS fact_id,
		           payment.merchant AS display_name,
		           payment.business_date AS business_date,
		           payment.amount_minor AS amount_minor, payment.currency AS currency
		    FROM payments payment
		    WHERE payment.tenant_id = ? AND payment.deleted_at IS NULL
		    UNION ALL
		    SELECT 'invoice', invoice.id, invoice.seller_name, invoice.invoice_date,
		           invoice.total_minor, invoice.currency
		    FROM invoices invoice
		    WHERE invoice.tenant_id = ? AND invoice.deleted_at IS NULL
		), signals AS (
		    SELECT fact.*,
		           coalesce(assignment.id, '') AS current_assignment_id,
		           coalesce(assignment.trip_id, '') AS current_trip_id,
		           coalesce(current_trip.destination, '') AS current_trip_destination,
		           fact.business_date BETWEEN ? AND ? AS inside_trip,
		           fact.business_date >= date(?, '-3 day') AND fact.business_date < ? AS near_before,
		           fact.business_date > ? AND fact.business_date <= date(?, '+3 day') AS near_after,
		           EXISTS (
		               SELECT 1
		               FROM payment_invoice_links link
		               JOIN trip_fact_assignments counterpart
		                 ON counterpart.tenant_id = link.tenant_id
		                AND counterpart.trip_id = ?
		                AND counterpart.ended_at IS NULL
		                AND ((fact.fact_type = 'payment' AND counterpart.invoice_id = link.invoice_id)
		                  OR (fact.fact_type = 'invoice' AND counterpart.payment_id = link.payment_id))
		               WHERE link.tenant_id = ? AND link.ended_at IS NULL
		                 AND ((fact.fact_type = 'payment' AND link.payment_id = fact.fact_id)
		                   OR (fact.fact_type = 'invoice' AND link.invoice_id = fact.fact_id))
		           ) AS linked_to_trip
		    FROM fact_rows fact
		    LEFT JOIN trip_fact_assignments assignment
		      ON assignment.tenant_id = ?
		     AND assignment.ended_at IS NULL
		     AND ((fact.fact_type = 'payment' AND assignment.payment_id = fact.fact_id)
		       OR (fact.fact_type = 'invoice' AND assignment.invoice_id = fact.fact_id))
		    LEFT JOIN trips current_trip
		      ON current_trip.tenant_id = assignment.tenant_id
		     AND current_trip.id = assignment.trip_id
		), ranked AS (
		    SELECT signals.*,
		           inside_trip OR near_before OR near_after OR linked_to_trip AS suggested,
		           CASE
		             WHEN current_trip_id = ? THEN 0
		             WHEN inside_trip OR near_before OR near_after OR linked_to_trip THEN 1
		             ELSE 2
		           END AS rank
		    FROM signals
		)
		SELECT fact_type, fact_id, display_name, business_date, amount_minor, currency,
		       current_assignment_id, current_trip_id, current_trip_destination,
		       inside_trip, near_before, near_after, linked_to_trip, suggested, rank
		FROM ranked
		WHERE (? = 'all' OR (? = 'suggested' AND suggested) OR (? = 'assigned' AND current_trip_id = ?))
	`
	arguments := []any{
		tenantID, tenantID,
		trip.StartDate, trip.EndDate,
		trip.StartDate, trip.StartDate,
		trip.EndDate, trip.EndDate,
		query.TripID, tenantID, tenantID,
		query.TripID,
		query.View, query.View, query.View, query.TripID,
	}
	if query.After != nil {
		statement += `
		  AND (rank > ?
		    OR (rank = ? AND business_date < ?)
		    OR (rank = ? AND business_date = ? AND fact_type > ?)
		    OR (rank = ? AND business_date = ? AND fact_type = ? AND fact_id > ?))
		`
		after := query.After
		arguments = append(arguments,
			after.Rank,
			after.Rank, after.BusinessDate,
			after.Rank, after.BusinessDate, after.FactType,
			after.Rank, after.BusinessDate, after.FactType, after.FactID,
		)
	}
	statement += ` ORDER BY rank, business_date DESC, fact_type, fact_id LIMIT ?`
	arguments = append(arguments, query.Limit+1)
	rows, err := s.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return ports.TripAttributionPage{}, fmt.Errorf("list trip attribution candidates: %w", err)
	}
	defer rows.Close()
	items := make([]ports.TripAttributionCandidate, 0, query.Limit+1)
	for rows.Next() {
		var item ports.TripAttributionCandidate
		var inside, before, after, linked int
		if err := rows.Scan(
			&item.FactType,
			&item.FactID,
			&item.DisplayName,
			&item.BusinessDate,
			&item.AmountMinor,
			&item.Currency,
			&item.CurrentAssignmentID,
			&item.CurrentTripID,
			&item.CurrentTripDestination,
			&inside,
			&before,
			&after,
			&linked,
			&item.Suggested,
			&item.Rank,
		); err != nil {
			return ports.TripAttributionPage{}, fmt.Errorf("scan trip attribution candidate: %w", err)
		}
		item.ReasonCodes = make([]string, 0, 5)
		if item.CurrentTripID == query.TripID {
			item.ReasonCodes = append(item.ReasonCodes, "currently_assigned")
		}
		if inside == 1 {
			item.ReasonCodes = append(item.ReasonCodes, "date_inside_trip")
		}
		if before == 1 {
			item.ReasonCodes = append(item.ReasonCodes, "date_within_3_days_before")
		}
		if after == 1 {
			item.ReasonCodes = append(item.ReasonCodes, "date_within_3_days_after")
		}
		if linked == 1 {
			item.ReasonCodes = append(item.ReasonCodes, "linked_fact_assigned_to_trip")
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ports.TripAttributionPage{}, fmt.Errorf("iterate trip attribution candidates: %w", err)
	}
	result := ports.TripAttributionPage{Trip: trip, Items: items}
	if len(items) > query.Limit {
		result.Items = items[:query.Limit]
		last := result.Items[len(result.Items)-1]
		result.NextCursor = &ports.TripAttributionCursor{
			Rank: last.Rank, BusinessDate: last.BusinessDate,
			FactType: last.FactType, FactID: last.FactID,
		}
	}
	return result, nil
}

func (s *Store) getTripSummary(ctx context.Context, tenantID, tripID string) (ports.Trip, error) {
	var result ports.Trip
	var origin, travelerName, transportType, bookingReference sql.NullString
	var createdAt string
	err := s.db.QueryRowContext(ctx, `
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
		WHERE trip.tenant_id = ? AND trip.id = ? AND trip.deleted_at IS NULL
		GROUP BY trip.tenant_id, trip.id
	`, tenantID, tripID).Scan(
		&result.ID,
		&origin,
		&result.Destination,
		&result.StartDate,
		&result.EndDate,
		&travelerName,
		&transportType,
		&bookingReference,
		&result.AssignedPaymentCount,
		&result.AssignedInvoiceCount,
		&createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.Trip{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.Trip{}, fmt.Errorf("read trip: %w", err)
	}
	result.Origin = nullableString(origin)
	result.TravelerName = nullableString(travelerName)
	result.TransportType = nullableString(transportType)
	result.BookingReference = nullableString(bookingReference)
	result.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return ports.Trip{}, fmt.Errorf("parse trip created_at: %w", err)
	}
	return result, nil
}

func (s *Store) GetTripAssignmentReplay(
	ctx context.Context,
	tenantID, idempotencyKey string,
) (ports.TripAssignmentReplay, error) {
	var replay ports.TripAssignmentReplay
	err := s.db.QueryRowContext(ctx, `
		SELECT decision.request_hash, decision.id, decision.action,
		       coalesce(decision.previous_assignment_id, ''), coalesce(created.id, '')
		FROM trip_fact_assignment_decisions decision
		LEFT JOIN trip_fact_assignments created
		  ON created.tenant_id = decision.tenant_id
		 AND created.created_by_decision_id = decision.id
		WHERE decision.tenant_id = ? AND decision.idempotency_key = ?
	`, tenantID, idempotencyKey).Scan(
		&replay.RequestHash,
		&replay.Result.DecisionID,
		&replay.Result.Action,
		&replay.Result.PreviousAssignmentID,
		&replay.Result.AssignmentID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.TripAssignmentReplay{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.TripAssignmentReplay{}, fmt.Errorf("read trip assignment replay: %w", err)
	}
	replay.Result.Replayed = true
	return replay, nil
}

func (t transaction) ApplyTripAssignment(
	ctx context.Context,
	command ports.TripAssignmentCommand,
) (ports.TripAssignmentResult, error) {
	if replay, exists, err := t.tripAssignmentReplay(ctx, command.TenantID, command.IdempotencyKey); err != nil {
		return ports.TripAssignmentResult{}, err
	} else if exists {
		if replay.RequestHash != command.RequestHash {
			return ports.TripAssignmentResult{}, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的行程归属请求", domain.ErrConflict)
		}
		return replay.Result, nil
	}
	if !domain.ValidTripAssignmentFactType(command.FactType) {
		return ports.TripAssignmentResult{}, domain.ErrInvalidInput
	}
	available, err := t.tripAssignmentFactAvailable(ctx, command.TenantID, command.FactType, command.FactID)
	if err != nil {
		return ports.TripAssignmentResult{}, err
	}
	if !available {
		return ports.TripAssignmentResult{}, domain.ErrNotFound
	}
	if command.DesiredTripID != "" {
		var tripAvailable int
		if err := t.tx.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM trips WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL)
		`, command.TenantID, command.DesiredTripID).Scan(&tripAvailable); err != nil {
			return ports.TripAssignmentResult{}, fmt.Errorf("check desired trip: %w", err)
		}
		if tripAvailable != 1 {
			return ports.TripAssignmentResult{}, domain.ErrNotFound
		}
	}
	currentID, currentTripID, err := t.currentTripAssignment(ctx, command.TenantID, command.FactType, command.FactID)
	if err != nil {
		return ports.TripAssignmentResult{}, err
	}
	if currentID != command.ExpectedAssignmentID {
		return ports.TripAssignmentResult{}, domain.NewRuleError("trip_assignment_stale", "当前行程归属已变化，请刷新后重试", domain.ErrConflict)
	}
	action := "assign"
	if currentID != "" && command.DesiredTripID == "" {
		action = "unassign"
	} else if currentID != "" && command.DesiredTripID != "" && currentTripID != command.DesiredTripID {
		action = "move"
	} else if currentID == "" && command.DesiredTripID == "" {
		return ports.TripAssignmentResult{}, domain.NewRuleError("trip_assignment_no_change", "该 Fact 当前没有可撤销的行程归属", domain.ErrConflict)
	} else if currentTripID == command.DesiredTripID {
		return ports.TripAssignmentResult{}, domain.NewRuleError("trip_assignment_no_change", "该 Fact 已归属于目标行程", domain.ErrConflict)
	}
	createdAt := command.CreatedAt.UTC().Format(time.RFC3339Nano)
	metadata, _ := json.Marshal(map[string]string{"action": action, "fact_type": string(command.FactType)})
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, tenant_id, actor_user_id, action, resource_type, resource_id,
			request_id, safe_metadata_json, created_at
		) VALUES (?, ?, ?, 'trip_fact_assignment_changed', ?, ?, ?, ?, ?)
	`,
		command.AuditEventID,
		command.TenantID,
		command.ActorUserID,
		command.FactType,
		command.FactID,
		command.RequestID,
		string(metadata),
		createdAt,
	); err != nil {
		return ports.TripAssignmentResult{}, fmt.Errorf("insert trip assignment audit: %w", err)
	}
	var paymentID, invoiceID any
	if command.FactType == domain.DocumentPayment {
		paymentID = command.FactID
	} else {
		invoiceID = command.FactID
	}
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO trip_fact_assignment_decisions (
			id, tenant_id, actor_user_id, fact_type, payment_id, invoice_id,
			previous_assignment_id, desired_trip_id, action, idempotency_key,
			request_hash, reason, audit_event_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?, ?)
	`,
		command.DecisionID,
		command.TenantID,
		command.ActorUserID,
		command.FactType,
		paymentID,
		invoiceID,
		currentID,
		command.DesiredTripID,
		action,
		command.IdempotencyKey,
		command.RequestHash,
		command.Reason,
		command.AuditEventID,
		createdAt,
	); err != nil {
		return ports.TripAssignmentResult{}, tripAssignmentWriteError("insert trip assignment decision", err)
	}
	if currentID != "" {
		updated, err := t.tx.ExecContext(ctx, `
			UPDATE trip_fact_assignments
			SET ended_at = ?, ended_by_decision_id = ?
			WHERE tenant_id = ? AND id = ? AND ended_at IS NULL
		`, createdAt, command.DecisionID, command.TenantID, currentID)
		if err != nil {
			return ports.TripAssignmentResult{}, tripAssignmentWriteError("end previous trip assignment", err)
		}
		if err := requireAffected(updated); err != nil {
			return ports.TripAssignmentResult{}, domain.NewRuleError("trip_assignment_stale", "当前行程归属已变化，请刷新后重试", domain.ErrConflict)
		}
	}
	result := ports.TripAssignmentResult{
		DecisionID: command.DecisionID, Action: action, PreviousAssignmentID: currentID,
	}
	if action != "unassign" {
		if _, err := t.tx.ExecContext(ctx, `
			INSERT INTO trip_fact_assignments (
				id, tenant_id, trip_id, payment_id, invoice_id,
				created_by_decision_id, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?)
		`,
			command.AssignmentID,
			command.TenantID,
			command.DesiredTripID,
			paymentID,
			invoiceID,
			command.DecisionID,
			createdAt,
		); err != nil {
			return ports.TripAssignmentResult{}, tripAssignmentWriteError("insert trip assignment", err)
		}
		result.AssignmentID = command.AssignmentID
	}
	return result, nil
}

func (t transaction) tripAssignmentFactAvailable(
	ctx context.Context,
	tenantID string,
	factType domain.DocumentType,
	factID string,
) (bool, error) {
	var available int
	var err error
	if factType == domain.DocumentPayment {
		err = t.tx.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM payments WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL)
		`, tenantID, factID).Scan(&available)
	} else {
		err = t.tx.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM invoices WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL)
		`, tenantID, factID).Scan(&available)
	}
	if err != nil {
		return false, fmt.Errorf("check trip assignment fact: %w", err)
	}
	return available == 1, nil
}

func (t transaction) currentTripAssignment(
	ctx context.Context,
	tenantID string,
	factType domain.DocumentType,
	factID string,
) (string, string, error) {
	var assignmentID, tripID string
	err := t.tx.QueryRowContext(ctx, `
		SELECT id, trip_id
		FROM trip_fact_assignments
		WHERE tenant_id = ? AND ended_at IS NULL
		  AND ((? = 'payment' AND payment_id = ?) OR (? = 'invoice' AND invoice_id = ?))
	`, tenantID, factType, factID, factType, factID).Scan(&assignmentID, &tripID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("read current trip assignment: %w", err)
	}
	return assignmentID, tripID, nil
}

func (t transaction) tripAssignmentReplay(
	ctx context.Context,
	tenantID, idempotencyKey string,
) (ports.TripAssignmentReplay, bool, error) {
	var replay ports.TripAssignmentReplay
	err := t.tx.QueryRowContext(ctx, `
		SELECT decision.request_hash, decision.id, decision.action,
		       coalesce(decision.previous_assignment_id, ''), coalesce(created.id, '')
		FROM trip_fact_assignment_decisions decision
		LEFT JOIN trip_fact_assignments created
		  ON created.tenant_id = decision.tenant_id
		 AND created.created_by_decision_id = decision.id
		WHERE decision.tenant_id = ? AND decision.idempotency_key = ?
	`, tenantID, idempotencyKey).Scan(
		&replay.RequestHash,
		&replay.Result.DecisionID,
		&replay.Result.Action,
		&replay.Result.PreviousAssignmentID,
		&replay.Result.AssignmentID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.TripAssignmentReplay{}, false, nil
	}
	if err != nil {
		return ports.TripAssignmentReplay{}, false, fmt.Errorf("read trip assignment idempotency record: %w", err)
	}
	replay.Result.Replayed = true
	return replay, true, nil
}

func tripAssignmentWriteError(operation string, err error) error {
	message := err.Error()
	if containsAny(message,
		"trip_assignment_decision_scope_mismatch",
		"trip_assignment_creation_scope_mismatch",
		"trip_assignment_end_scope_mismatch",
		"UNIQUE constraint failed: trip_fact_assignments",
	) {
		return domain.NewRuleError("trip_assignment_stale", "当前行程归属已变化，请刷新后重试", domain.ErrConflict)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if len(candidate) > 0 && len(value) >= len(candidate) {
			for index := 0; index+len(candidate) <= len(value); index++ {
				if value[index:index+len(candidate)] == candidate {
					return true
				}
			}
		}
	}
	return false
}
