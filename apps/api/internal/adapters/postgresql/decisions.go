package postgresqladapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type persistedCandidate struct {
	ID                string
	ExistingPaymentID string
	ExistingInvoiceID string
}

type persistedDuplicateCandidate struct {
	ID           string
	CandidateKey string
}

func (t transaction) ConfirmReview(ctx context.Context, command ports.ConfirmCommand) (ports.ConfirmResult, error) {
	if replay, exists, err := t.confirmReplay(ctx, command.TenantID, command.IdempotencyKey); err != nil {
		return ports.ConfirmResult{}, err
	} else if exists {
		if replay.recordedJobID != command.JobID ||
			replay.value.ClaimSetID != command.ClaimSetID ||
			replay.value.ExpectedRevision != command.ExpectedRevision ||
			replay.value.AssociationMode != command.AssociationMode ||
			replay.value.AllocationPlanHash != command.AllocationPlanHash ||
			replay.value.DuplicatePlanHash != command.DuplicatePlanHash {
			return ports.ConfirmResult{}, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的请求", domain.ErrConflict)
		}
		return replay.value.Result, nil
	}
	var documentID string
	var documentType domain.DocumentType
	var claimStatus domain.ClaimStatus
	var jobStatus domain.JobStatus
	var revision int
	err := t.tx.QueryRowContext(ctx, `
		SELECT c.document_id, c.document_type, c.status, c.revision, j.status
		FROM claim_sets c
		JOIN processing_jobs j ON j.tenant_id = c.tenant_id AND j.document_id = c.document_id
		WHERE c.tenant_id = ? AND c.id = ? AND j.id = ?
	`, command.TenantID, command.ClaimSetID, command.JobID).Scan(
		&documentID,
		&documentType,
		&claimStatus,
		&revision,
		&jobStatus,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ConfirmResult{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.ConfirmResult{}, fmt.Errorf("read claim for confirmation: %w", err)
	}
	if claimStatus != domain.ClaimReadyForReview || jobStatus != domain.JobNeedsReview || revision != command.ExpectedRevision {
		return ports.ConfirmResult{}, domain.ErrVersionConflict
	}
	var blockers int
	if err := t.tx.QueryRowContext(ctx, `
		SELECT count(*) FROM validation_results
		WHERE tenant_id = ? AND claim_set_id = ? AND status IN ('error', 'blocked')
	`, command.TenantID, command.ClaimSetID).Scan(&blockers); err != nil {
		return ports.ConfirmResult{}, fmt.Errorf("check confirmation validations: %w", err)
	}
	if blockers != 0 {
		return ports.ConfirmResult{}, domain.NewRuleError("claim_validation_blocked", "Claim 仍包含阻断校验", domain.ErrConflict)
	}
	candidates, err := t.loadCandidates(ctx, command.TenantID, command.ClaimSetID)
	if err != nil {
		return ports.ConfirmResult{}, err
	}
	if err := validatePersistedAssociation(candidates, command); err != nil {
		return ports.ConfirmResult{}, err
	}
	if err := t.validateDuplicateConfirmation(ctx, command, documentID); err != nil {
		return ports.ConfirmResult{}, err
	}
	for _, decision := range command.CandidateDecisions {
		if decision.Action != "accept" {
			continue
		}
		candidate := candidateByID(candidates, decision.CandidateID)
		if err := t.requireAllocationAvailable(
			ctx,
			command.TenantID,
			candidate,
			*decision.AllocatedMinor,
			decision.Currency,
		); err != nil {
			return ports.ConfirmResult{}, err
		}
	}
	if (documentType == domain.DocumentPayment) != (command.Payment != nil) ||
		(documentType == domain.DocumentInvoice) != (command.Invoice != nil) ||
		(documentType == domain.DocumentTrip) != (command.Trip != nil) {
		return ports.ConfirmResult{}, domain.ErrInvalidInput
	}
	createdAt := command.CreatedAt.UTC().Format(time.RFC3339Nano)
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO review_decisions (
			id, tenant_id, claim_set_id, actor_user_id, action, fact_type, association_mode,
			association_plan_hash, duplicate_plan_hash, idempotency_key,
			expected_revision, created_at
		) VALUES (?, ?, ?, ?, 'confirm', ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?)
	`,
		command.ReviewDecisionID,
		command.TenantID,
		command.ClaimSetID,
		command.ActorUserID,
		documentType,
		command.AssociationMode,
		command.AllocationPlanHash,
		command.DuplicatePlanHash,
		command.IdempotencyKey,
		command.ExpectedRevision,
		createdAt,
	); err != nil {
		return ports.ConfirmResult{}, fmt.Errorf("insert confirm decision: %w", err)
	}
	result := ports.ConfirmResult{
		ReviewDecisionID: command.ReviewDecisionID,
		FactType:         documentType,
		LinkIDs:          []string{},
	}
	for _, decision := range command.DuplicateDecisions {
		if _, err := t.tx.ExecContext(ctx, `
			INSERT INTO duplicate_candidate_decisions (
				id, tenant_id, candidate_id, review_decision_id, action, created_at
			) VALUES (?, ?, ?, ?, ?, ?)
		`,
			decision.ID,
			command.TenantID,
			decision.CandidateID,
			command.ReviewDecisionID,
			decision.Action,
			createdAt,
		); err != nil {
			return ports.ConfirmResult{}, fmt.Errorf("insert duplicate candidate decision: %w", err)
		}
	}
	itemIDs := make(map[string]string)
	if command.Payment != nil {
		if err := t.insertPayment(ctx, command, createdAt); err != nil {
			return ports.ConfirmResult{}, err
		}
		result.FactID = command.Payment.ID
	} else if command.Invoice != nil {
		if err := t.insertInvoice(ctx, command, createdAt, itemIDs); err != nil {
			return ports.ConfirmResult{}, err
		}
		result.FactID = command.Invoice.ID
	} else {
		if err := t.insertTrip(ctx, command, createdAt); err != nil {
			return ports.ConfirmResult{}, err
		}
		result.FactID = command.Trip.ID
	}
	if err := t.insertFactOrigins(ctx, command, itemIDs, createdAt); err != nil {
		return ports.ConfirmResult{}, err
	}
	for _, decision := range command.CandidateDecisions {
		if _, err := t.tx.ExecContext(ctx, `
			INSERT INTO payment_invoice_link_decisions (
				id, tenant_id, candidate_id, review_decision_id, action,
				allocated_minor, currency, created_at
			) VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?)
		`,
			decision.ID,
			command.TenantID,
			decision.CandidateID,
			command.ReviewDecisionID,
			decision.Action,
			decision.AllocatedMinor,
			decision.Currency,
			createdAt,
		); err != nil {
			return ports.ConfirmResult{}, fmt.Errorf("insert candidate decision: %w", err)
		}
		if decision.Action != "accept" {
			continue
		}
		candidate := candidateByID(candidates, decision.CandidateID)
		paymentID := candidate.ExistingPaymentID
		invoiceID := candidate.ExistingInvoiceID
		if command.Payment != nil {
			paymentID = command.Payment.ID
		}
		if command.Invoice != nil {
			invoiceID = command.Invoice.ID
		}
		if _, err := t.tx.ExecContext(ctx, `
			INSERT INTO payment_invoice_links (
				id, tenant_id, payment_id, invoice_id, link_decision_id,
				allocated_minor, currency, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`,
			decision.LinkID,
			command.TenantID,
			paymentID,
			invoiceID,
			decision.ID,
			*decision.AllocatedMinor,
			decision.Currency,
			createdAt,
		); err != nil {
			return ports.ConfirmResult{}, allocationInsertError(err)
		}
		result.LinkIDs = append(result.LinkIDs, decision.LinkID)
	}
	metadataValues := map[string]any{
		"allocation_count": len(result.LinkIDs),
		"duplicate_count":  len(command.DuplicateDecisions),
		"fact_type":        string(documentType),
	}
	if command.AssociationMode != "" {
		metadataValues["association_mode"] = command.AssociationMode
	}
	metadata, _ := json.Marshal(metadataValues)
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, tenant_id, actor_user_id, action, resource_type, resource_id,
			request_id, safe_metadata_json, created_at
		) VALUES (?, ?, ?, 'fact_confirmed', ?, ?, ?, ?::jsonb, ?)
	`,
		command.AuditEventID,
		command.TenantID,
		command.ActorUserID,
		documentType,
		result.FactID,
		command.RequestID,
		string(metadata),
		createdAt,
	); err != nil {
		return ports.ConfirmResult{}, fmt.Errorf("insert confirm audit event: %w", err)
	}
	updated, err := t.tx.ExecContext(ctx, `
		UPDATE claim_sets SET status = 'confirmed', optimistic_version = optimistic_version + 1
		WHERE tenant_id = ? AND id = ? AND revision = ? AND status = 'ready_for_review'
	`, command.TenantID, command.ClaimSetID, command.ExpectedRevision)
	if err != nil {
		return ports.ConfirmResult{}, fmt.Errorf("confirm claim: %w", err)
	}
	if err := requireAffected(updated); err != nil {
		return ports.ConfirmResult{}, domain.ErrVersionConflict
	}
	updated, err = t.tx.ExecContext(ctx, `
		UPDATE processing_jobs
		SET status = 'completed', finished_at = ?, version = version + 1
		WHERE tenant_id = ? AND id = ? AND document_id = ? AND status = 'needs_review'
	`, createdAt, command.TenantID, command.JobID, documentID)
	if err != nil {
		return ports.ConfirmResult{}, fmt.Errorf("complete review job: %w", err)
	}
	if err := requireAffected(updated); err != nil {
		return ports.ConfirmResult{}, domain.ErrConflict
	}
	if _, err := t.tx.ExecContext(ctx, `
		UPDATE documents SET status = 'completed' WHERE tenant_id = ? AND id = ?
	`, command.TenantID, documentID); err != nil {
		return ports.ConfirmResult{}, fmt.Errorf("complete review document: %w", err)
	}
	return result, nil
}
func (t transaction) RejectReview(ctx context.Context, command ports.RejectCommand) error {
	var existingAction, existingClaimSetID, existingReason, recordedJobID string
	var existingRevision int
	err := t.tx.QueryRowContext(ctx, `
		SELECT r.action, c.id, r.expected_revision, coalesce(r.reason, ''), j.id
		FROM review_decisions r
		JOIN claim_sets c ON c.tenant_id = r.tenant_id AND c.id = r.claim_set_id
		JOIN processing_jobs j ON j.tenant_id = c.tenant_id AND j.document_id = c.document_id
		WHERE r.tenant_id = ? AND r.idempotency_key = ?
	`, command.TenantID, command.IdempotencyKey).Scan(
		&existingAction,
		&existingClaimSetID,
		&existingRevision,
		&existingReason,
		&recordedJobID,
	)
	if err == nil {
		if existingAction == "reject" && recordedJobID == command.JobID &&
			existingClaimSetID == command.ClaimSetID && existingRevision == command.ExpectedRevision &&
			existingReason == command.Reason {
			return nil
		}
		return domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的请求", domain.ErrConflict)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read reject idempotency record: %w", err)
	}
	var documentID string
	var revision int
	var claimStatus domain.ClaimStatus
	var jobStatus domain.JobStatus
	err = t.tx.QueryRowContext(ctx, `
		SELECT c.document_id, c.revision, c.status, j.status
		FROM claim_sets c
		JOIN processing_jobs j ON j.tenant_id = c.tenant_id AND j.document_id = c.document_id
		WHERE c.tenant_id = ? AND c.id = ? AND j.id = ?
	`, command.TenantID, command.ClaimSetID, command.JobID).Scan(
		&documentID,
		&revision,
		&claimStatus,
		&jobStatus,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read claim for rejection: %w", err)
	}
	if revision != command.ExpectedRevision ||
		(claimStatus != domain.ClaimReadyForReview && claimStatus != domain.ClaimBlocked) ||
		(jobStatus != domain.JobNeedsReview && jobStatus != domain.JobBlocked) {
		return domain.ErrVersionConflict
	}
	createdAt := command.CreatedAt.UTC().Format(time.RFC3339Nano)
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO review_decisions (
			id, tenant_id, claim_set_id, actor_user_id, action, idempotency_key,
			expected_revision, reason, created_at
		) VALUES (?, ?, ?, ?, 'reject', ?, ?, NULLIF(?, ''), ?)
	`,
		command.ReviewDecisionID,
		command.TenantID,
		command.ClaimSetID,
		command.ActorUserID,
		command.IdempotencyKey,
		command.ExpectedRevision,
		command.Reason,
		createdAt,
	); err != nil {
		return fmt.Errorf("insert reject decision: %w", err)
	}
	metadata, _ := json.Marshal(map[string]bool{"reason_provided": command.Reason != ""})
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, tenant_id, actor_user_id, action, resource_type, resource_id,
			request_id, safe_metadata_json, created_at
		) VALUES (?, ?, ?, 'claim_rejected', 'claim_set', ?, ?, ?::jsonb, ?)
	`,
		command.AuditEventID,
		command.TenantID,
		command.ActorUserID,
		command.ClaimSetID,
		command.RequestID,
		string(metadata),
		createdAt,
	); err != nil {
		return fmt.Errorf("insert reject audit event: %w", err)
	}
	updated, err := t.tx.ExecContext(ctx, `
		UPDATE claim_sets SET status = 'rejected', optimistic_version = optimistic_version + 1
		WHERE tenant_id = ? AND id = ? AND revision = ? AND status IN ('ready_for_review', 'blocked')
	`, command.TenantID, command.ClaimSetID, command.ExpectedRevision)
	if err != nil {
		return fmt.Errorf("reject claim: %w", err)
	}
	if err := requireAffected(updated); err != nil {
		return domain.ErrVersionConflict
	}
	updated, err = t.tx.ExecContext(ctx, `
		UPDATE processing_jobs
		SET status = 'rejected', finished_at = ?, version = version + 1
		WHERE tenant_id = ? AND id = ? AND document_id = ? AND status IN ('needs_review', 'blocked')
	`, createdAt, command.TenantID, command.JobID, documentID)
	if err != nil {
		return fmt.Errorf("reject review job: %w", err)
	}
	if err := requireAffected(updated); err != nil {
		return domain.ErrConflict
	}
	if _, err := t.tx.ExecContext(ctx, `
		UPDATE documents SET status = 'rejected' WHERE tenant_id = ? AND id = ?
	`, command.TenantID, documentID); err != nil {
		return fmt.Errorf("reject review document: %w", err)
	}
	return nil
}

func (t transaction) insertPayment(ctx context.Context, command ports.ConfirmCommand, createdAt string) error {
	payment := command.Payment
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO payments (
			id, tenant_id, source_review_decision_id, amount_minor, currency,
			merchant, transaction_time, source_timezone, business_date, payment_method,
			order_number, category, created_at, updated_at, version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
	`,
		payment.ID,
		command.TenantID,
		command.ReviewDecisionID,
		payment.AmountMinor,
		payment.Currency,
		payment.Merchant,
		payment.TransactionTime,
		payment.SourceTimezone,
		payment.BusinessDate,
		payment.PaymentMethod,
		payment.OrderNumber,
		payment.Category,
		createdAt,
		createdAt,
	)
	if err != nil {
		return fmt.Errorf("insert payment fact: %w", err)
	}
	return nil
}

func (t transaction) insertInvoice(
	ctx context.Context,
	command ports.ConfirmCommand,
	createdAt string,
	itemIDs map[string]string,
) error {
	invoice := command.Invoice
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO invoices (
			id, tenant_id, source_review_decision_id, invoice_number,
			normalized_invoice_number, invoice_date, total_minor, tax_minor,
			currency, seller_name, buyer_name, created_at, updated_at, version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
	`,
		invoice.ID,
		command.TenantID,
		command.ReviewDecisionID,
		invoice.InvoiceNumber,
		invoice.NormalizedInvoiceNumber,
		invoice.InvoiceDate,
		invoice.TotalMinor,
		invoice.TaxMinor,
		invoice.Currency,
		invoice.SellerName,
		invoice.BuyerName,
		createdAt,
		createdAt,
	)
	if err != nil {
		return domain.NewRuleError("invoice_number_conflict", "同一工作区已存在相同规范化发票号码", domain.ErrConflict)
	}
	for _, item := range invoice.Items {
		if _, exists := itemIDs[item.ItemKey]; exists {
			return domain.ErrInvalidInput
		}
		itemIDs[item.ItemKey] = item.ID
		if _, err := t.tx.ExecContext(ctx, `
			INSERT INTO invoice_items (
				id, tenant_id, invoice_id, item_key, name, quantity, unit,
				unit_price_minor, amount_minor, tax_minor, sort_order
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			item.ID,
			command.TenantID,
			invoice.ID,
			item.ItemKey,
			item.Name,
			item.Quantity,
			item.Unit,
			item.UnitPriceMinor,
			item.AmountMinor,
			item.TaxMinor,
			item.SortOrder,
		); err != nil {
			return fmt.Errorf("insert invoice item: %w", err)
		}
	}
	return nil
}

func (t transaction) insertTrip(ctx context.Context, command ports.ConfirmCommand, createdAt string) error {
	trip := command.Trip
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO trips (
			id, tenant_id, source_review_decision_id, origin, destination,
			start_date, end_date, traveler_name, transport_type, booking_reference,
			created_at, updated_at, version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
	`,
		trip.ID,
		command.TenantID,
		command.ReviewDecisionID,
		trip.Origin,
		trip.Destination,
		trip.StartDate,
		trip.EndDate,
		trip.TravelerName,
		trip.TransportType,
		trip.BookingReference,
		createdAt,
		createdAt,
	)
	if err != nil {
		return fmt.Errorf("insert trip fact: %w", err)
	}
	return nil
}

func (t transaction) insertFactOrigins(
	ctx context.Context,
	command ports.ConfirmCommand,
	itemIDs map[string]string,
	createdAt string,
) error {
	var expected int
	if err := t.tx.QueryRowContext(ctx, `
		SELECT count(*) FROM field_claims
		WHERE tenant_id = ? AND claim_set_id = ? AND presence = 'present'
		  AND field_path NOT IN ('document_type', 'supplementary_fields')
	`, command.TenantID, command.ClaimSetID).Scan(&expected); err != nil {
		return fmt.Errorf("count fact source fields: %w", err)
	}
	if expected != len(command.Origins) {
		return domain.NewRuleError("fact_origin_incomplete", "Fact 字段来源映射不完整", domain.ErrConflict)
	}
	seenFields := make(map[string]struct{}, len(command.Origins))
	for _, origin := range command.Origins {
		if _, duplicate := seenFields[origin.FieldClaimID]; duplicate {
			return domain.ErrInvalidInput
		}
		seenFields[origin.FieldClaimID] = struct{}{}
		var valid bool
		if err := t.tx.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM field_claims
				WHERE tenant_id = ? AND claim_set_id = ? AND id = ?
				  AND field_path = ? AND presence = 'present'
			)
		`, command.TenantID, command.ClaimSetID, origin.FieldClaimID, origin.FieldPath).Scan(&valid); err != nil {
			return fmt.Errorf("verify fact origin: %w", err)
		}
		if !valid {
			return domain.ErrConflict
		}
		var paymentID, invoiceID, invoiceItemID, tripID any
		switch origin.FactScope {
		case "payment":
			paymentID = command.Payment.ID
		case "invoice":
			invoiceID = command.Invoice.ID
		case "item":
			invoiceItemID = itemIDs[origin.ItemKey]
			if invoiceItemID == "" {
				return domain.ErrInvalidInput
			}
		case "trip":
			tripID = command.Trip.ID
		default:
			return domain.ErrInvalidInput
		}
		if _, err := t.tx.ExecContext(ctx, `
			INSERT INTO fact_field_origins (
				id, tenant_id, payment_id, invoice_id, invoice_item_id, trip_id,
				field_path, field_claim_id, review_decision_id, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			origin.ID,
			command.TenantID,
			paymentID,
			invoiceID,
			invoiceItemID,
			tripID,
			origin.FieldPath,
			origin.FieldClaimID,
			command.ReviewDecisionID,
			createdAt,
		); err != nil {
			return fmt.Errorf("insert fact field origin: %w", err)
		}
	}
	return nil
}

func (t transaction) loadCandidates(ctx context.Context, tenantID, claimSetID string) ([]persistedCandidate, error) {
	rows, err := t.tx.QueryContext(ctx, `
		SELECT id, coalesce(existing_payment_id, ''), coalesce(existing_invoice_id, '')
		FROM payment_invoice_link_candidates
		WHERE tenant_id = ? AND claim_set_id = ?
		ORDER BY id
	`, tenantID, claimSetID)
	if err != nil {
		return nil, fmt.Errorf("load confirmation candidates: %w", err)
	}
	defer rows.Close()
	items := make([]persistedCandidate, 0)
	for rows.Next() {
		var item persistedCandidate
		if err := rows.Scan(&item.ID, &item.ExistingPaymentID, &item.ExistingInvoiceID); err != nil {
			return nil, fmt.Errorf("scan confirmation candidate: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func validatePersistedAssociation(candidates []persistedCandidate, command ports.ConfirmCommand) error {
	if command.Trip != nil {
		if command.Payment != nil || command.Invoice != nil || len(candidates) != 0 ||
			len(command.CandidateDecisions) != 0 || command.AssociationMode != "" || command.AllocationPlanHash != "" {
			return domain.NewRuleError("invalid_trip_association", "Trip 确认不能包含支付或发票金额分配", domain.ErrInvalidInput)
		}
		return nil
	}
	decisions := make(map[string]ports.CandidateDecisionDraft, len(command.CandidateDecisions))
	for _, decision := range command.CandidateDecisions {
		if decision.Action != "accept" && decision.Action != "reject" {
			return domain.ErrInvalidInput
		}
		if _, duplicate := decisions[decision.CandidateID]; duplicate {
			return domain.ErrInvalidInput
		}
		if decision.Action == "accept" {
			if decision.AllocatedMinor == nil || decision.Currency == "" || decision.LinkID == "" {
				return domain.ErrInvalidInput
			}
		} else if decision.AllocatedMinor != nil || decision.Currency != "" || decision.LinkID != "" {
			return domain.ErrInvalidInput
		}
		decisions[decision.CandidateID] = decision
	}
	if len(decisions) != len(candidates) {
		return domain.NewRuleError("candidate_set_changed", "候选集合已变化，请刷新后重试", domain.ErrConflict)
	}
	amountMinor, currency, ok := commandFactTerms(command)
	if !ok {
		return domain.ErrInvalidInput
	}
	plan := make([]domain.AllocationRequest, 0)
	allocationCandidates := make([]domain.AllocationCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		decision, exists := decisions[candidate.ID]
		if !exists {
			return domain.NewRuleError("candidate_set_changed", "候选集合已变化，请刷新后重试", domain.ErrConflict)
		}
		if decision.Action == "accept" {
			if decision.Currency != currency ||
				(command.Payment != nil && candidate.ExistingInvoiceID == "") ||
				(command.Invoice != nil && candidate.ExistingPaymentID == "") {
				return domain.ErrInvalidInput
			}
			plan = append(plan, domain.AllocationRequest{
				CandidateID:    decision.CandidateID,
				AllocatedMinor: *decision.AllocatedMinor,
			})
		}
		allocationCandidates = append(allocationCandidates, domain.AllocationCandidate{
			ID:             candidate.ID,
			Currency:       currency,
			RemainingMinor: domain.MaxSafeMinorUnits,
			Available:      true,
		})
	}
	switch command.AssociationMode {
	case "no_candidate":
		if len(candidates) != 0 || len(plan) != 0 || command.AllocationPlanHash != "" {
			return domain.NewRuleError("invalid_association", "no_candidate 与当前候选集合不一致", domain.ErrConflict)
		}
	case "reject_all":
		if len(candidates) == 0 || len(plan) != 0 || command.AllocationPlanHash != "" {
			return domain.NewRuleError("invalid_association", "reject_all 与当前候选集合不一致", domain.ErrConflict)
		}
	case "allocate_candidates":
		if len(candidates) == 0 || len(plan) == 0 || command.AllocationPlanHash == "" {
			return domain.NewRuleError("invalid_association", "候选分配请求不完整", domain.ErrConflict)
		}
		canonical, planHash, err := domain.CanonicalAllocationPlan(plan)
		if err != nil {
			return err
		}
		if planHash != command.AllocationPlanHash {
			return domain.ErrInvalidInput
		}
		if err := domain.ValidateAllocationPlan(amountMinor, currency, allocationCandidates, canonical); err != nil {
			return err
		}
	default:
		return domain.ErrInvalidInput
	}
	return nil
}

func commandFactTerms(command ports.ConfirmCommand) (int64, string, bool) {
	if command.Payment != nil && command.Invoice == nil {
		return command.Payment.AmountMinor, command.Payment.Currency, true
	}
	if command.Invoice != nil && command.Payment == nil && command.Trip == nil {
		return command.Invoice.TotalMinor, command.Invoice.Currency, true
	}
	return 0, "", false
}

func candidateByID(candidates []persistedCandidate, candidateID string) persistedCandidate {
	for _, candidate := range candidates {
		if candidate.ID == candidateID {
			return candidate
		}
	}
	return persistedCandidate{}
}

func (t transaction) requireAllocationAvailable(
	ctx context.Context,
	tenantID string,
	candidate persistedCandidate,
	allocatedMinor int64,
	currency string,
) error {
	var persistedCurrency string
	var active bool
	var remainingMinor int64
	var err error
	if candidate.ExistingPaymentID != "" {
		err = t.tx.QueryRowContext(ctx, `
			SELECT p.currency, p.deleted_at IS NULL,
			       p.amount_minor - coalesce((
			           SELECT sum(l.allocated_minor)
			           FROM payment_invoice_links l
			           WHERE l.tenant_id = p.tenant_id
			             AND l.payment_id = p.id
			             AND l.ended_at IS NULL
			       ), 0)
			FROM payments p
			WHERE p.tenant_id = ? AND p.id = ?
		`, tenantID, candidate.ExistingPaymentID).Scan(&persistedCurrency, &active, &remainingMinor)
	} else if candidate.ExistingInvoiceID != "" {
		err = t.tx.QueryRowContext(ctx, `
			SELECT i.currency, i.deleted_at IS NULL,
			       i.total_minor - coalesce((
			           SELECT sum(l.allocated_minor)
			           FROM payment_invoice_links l
			           WHERE l.tenant_id = i.tenant_id
			             AND l.invoice_id = i.id
			             AND l.ended_at IS NULL
			       ), 0)
			FROM invoices i
			WHERE i.tenant_id = ? AND i.id = ?
		`, tenantID, candidate.ExistingInvoiceID).Scan(&persistedCurrency, &active, &remainingMinor)
	} else {
		return domain.ErrInvalidInput
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.NewRuleError("allocation_candidate_unavailable", "候选已删除或没有可分配余额", domain.ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("check allocation candidate: %w", err)
	}
	if !active || remainingMinor <= 0 {
		return domain.NewRuleError("allocation_candidate_unavailable", "候选已删除或没有可分配余额", domain.ErrConflict)
	}
	if persistedCurrency != currency {
		return domain.NewRuleError("allocation_currency_mismatch", "分配双方币种必须一致", domain.ErrConflict)
	}
	if allocatedMinor > remainingMinor {
		return domain.NewRuleError("allocation_exceeds_target_balance", "分配金额超过候选当前剩余余额", domain.ErrConflict)
	}
	return nil
}

func allocationInsertError(err error) error {
	message := err.Error()
	if strings.Contains(message, "payment_allocation_exceeded") ||
		strings.Contains(message, "invoice_allocation_exceeded") {
		return domain.NewRuleError("allocation_balance_conflict", "分配余额已被其他确认占用，请刷新后重试", domain.ErrConflict)
	}
	if strings.Contains(message, "allocation_fact_unavailable") {
		return domain.NewRuleError("allocation_candidate_unavailable", "候选已删除或币种状态已变化", domain.ErrConflict)
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" &&
		postgresError.ConstraintName == "payment_invoice_links_pair_active_idx" {
		return domain.NewRuleError("allocation_pair_conflict", "同一支付与发票已存在活动分配", domain.ErrConflict)
	}
	return fmt.Errorf("insert payment/invoice allocation: %w", err)
}

type confirmReplayRecord struct {
	value         ports.ConfirmReplay
	recordedJobID string
}

func (t transaction) confirmReplay(
	ctx context.Context,
	tenantID, idempotencyKey string,
) (confirmReplayRecord, bool, error) {
	var result confirmReplayRecord
	var action string
	var linkIDsJSON string
	err := t.tx.QueryRowContext(ctx, `
		SELECT r.action, r.id, j.id, c.id, r.expected_revision,
		       coalesce(r.association_mode, ''), coalesce(r.association_plan_hash, ''),
		       coalesce(r.duplicate_plan_hash, ''),
		       c.document_type, coalesce(p.id, i.id, trip.id),
		       coalesce((SELECT json_agg(link_id ORDER BY sort_key) FROM (
		           SELECT l.id AS link_id, d.candidate_id AS sort_key
		           FROM payment_invoice_link_decisions d
		           JOIN payment_invoice_links l
		             ON l.tenant_id = d.tenant_id AND l.link_decision_id = d.id
		           WHERE d.tenant_id = r.tenant_id AND d.review_decision_id = r.id
		           ORDER BY d.candidate_id
		       ) links), '[]'::json)::text
		FROM review_decisions r
		JOIN claim_sets c ON c.tenant_id = r.tenant_id AND c.id = r.claim_set_id
		JOIN processing_jobs j ON j.tenant_id = c.tenant_id AND j.document_id = c.document_id
		LEFT JOIN payments p ON p.tenant_id = r.tenant_id AND p.source_review_decision_id = r.id
		LEFT JOIN invoices i ON i.tenant_id = r.tenant_id AND i.source_review_decision_id = r.id
		LEFT JOIN trips trip ON trip.tenant_id = r.tenant_id AND trip.source_review_decision_id = r.id
		WHERE r.tenant_id = ? AND r.idempotency_key = ?
	`, tenantID, idempotencyKey).Scan(
		&action,
		&result.value.Result.ReviewDecisionID,
		&result.recordedJobID,
		&result.value.ClaimSetID,
		&result.value.ExpectedRevision,
		&result.value.AssociationMode,
		&result.value.AllocationPlanHash,
		&result.value.DuplicatePlanHash,
		&result.value.Result.FactType,
		&result.value.Result.FactID,
		&linkIDsJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return confirmReplayRecord{}, false, nil
	}
	if err != nil {
		return confirmReplayRecord{}, false, fmt.Errorf("read confirm idempotency record: %w", err)
	}
	if action != "confirm" {
		return confirmReplayRecord{}, false, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的请求", domain.ErrConflict)
	}
	if err := json.Unmarshal([]byte(linkIDsJSON), &result.value.Result.LinkIDs); err != nil {
		return confirmReplayRecord{}, false, fmt.Errorf("decode confirm replay links: %w", err)
	}
	result.value.Result.Replayed = true
	return result, true, nil
}
