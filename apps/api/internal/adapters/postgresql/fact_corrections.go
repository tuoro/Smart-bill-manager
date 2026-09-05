package postgresqladapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (t transaction) InvoiceNumberConflicts(ctx context.Context, tenantID, normalizedNumber, excludedID string) (bool, error) {
	var conflict bool
	err := t.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM invoices WHERE tenant_id = ? AND normalized_invoice_number = ? AND id <> ? AND deleted_at IS NULL)`, tenantID, normalizedNumber, excludedID).Scan(&conflict)
	if err != nil {
		return false, fmt.Errorf("read invoice correction uniqueness: %w", err)
	}
	return conflict, nil
}

func (t transaction) CorrectionDuplicateIdentity(ctx context.Context, tenantID string, spec domain.DuplicateCandidateSpec) (string, error) {
	if spec.ExistingPaymentID == "" && spec.ExistingInvoiceID == "" {
		return "", nil
	}
	table, id := "payments", spec.ExistingPaymentID
	if spec.ExistingInvoiceID != "" {
		table, id = "invoices", spec.ExistingInvoiceID
	}
	var revision string
	err := t.tx.QueryRowContext(ctx, `SELECT current_review_decision_id FROM `+table+` WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, tenantID, id).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrVersionConflict
	}
	if err != nil {
		return "", fmt.Errorf("read correction duplicate identity: %w", err)
	}
	return revision, nil
}

func (s *Store) GetFactCorrectionReplay(ctx context.Context, tenantID, key string) (ports.FactCorrectionReplay, error) {
	var replay ports.FactCorrectionReplay
	err := s.db.QueryRowContext(ctx, `SELECT coalesce(correction.request_hash, ''), coalesce(reviewed.fact_type, ''),
		coalesce(correction.payment_id, correction.invoice_id, correction.trip_evidence_id, ''),
		reviewed.id, reviewed.claim_set_id, coalesce(correction.resulting_version, 0)
		FROM review_decisions reviewed LEFT JOIN fact_corrections correction ON correction.tenant_id = reviewed.tenant_id AND correction.review_decision_id = reviewed.id
		WHERE reviewed.tenant_id = ? AND reviewed.idempotency_key = ?`, tenantID, key).
		Scan(&replay.RequestHash, &replay.Result.FactType, &replay.Result.FactID, &replay.Result.ReviewDecisionID, &replay.Result.ClaimSetID, &replay.Result.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return replay, domain.ErrNotFound
	}
	if err != nil {
		return replay, fmt.Errorf("read correction replay: %w", err)
	}
	if replay.RequestHash == "" {
		return replay, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于其他审核请求", domain.ErrConflict)
	}
	replay.Result.Replayed = true
	return replay, nil
}

func (s *Store) GetFactCorrectionHistory(ctx context.Context, tenantID string, kind domain.DocumentType, id string, before, limit int) ([]ports.FactCorrectionHistory, error) {
	table, err := correctionTable(kind)
	if err != nil {
		return nil, err
	}
	if before < 0 || limit < 1 || limit > 100 {
		return nil, domain.ErrInvalidInput
	}
	var initial string
	if err := s.db.QueryRowContext(ctx, `SELECT source_review_decision_id FROM `+table+` WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, tenantID, id).Scan(&initial); errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("read correction history anchor: %w", err)
	}
	column := "payment_id"
	if kind == domain.DocumentInvoice {
		column = "invoice_id"
	} else if kind == domain.DocumentTrip {
		column = "trip_evidence_id"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT reviewed.id, coalesce(correction.previous_review_decision_id, ''), claim.id,
		claim.revision, reviewed.actor_user_id, coalesce(reviewed.reason, ''), reviewed.created_at
		FROM review_decisions reviewed JOIN claim_sets claim ON claim.tenant_id = reviewed.tenant_id AND claim.id = reviewed.claim_set_id
		LEFT JOIN fact_corrections correction ON correction.tenant_id = reviewed.tenant_id AND correction.review_decision_id = reviewed.id
		WHERE reviewed.tenant_id = ? AND (reviewed.id = ? OR correction.`+column+` = ?) AND (? = 0 OR claim.revision < ?)
		ORDER BY claim.revision DESC LIMIT ?`, tenantID, initial, id, before, before, limit)
	if err != nil {
		return nil, fmt.Errorf("read correction history: %w", err)
	}
	defer rows.Close()
	items := make([]ports.FactCorrectionHistory, 0)
	for rows.Next() {
		var item ports.FactCorrectionHistory
		if err := rows.Scan(&item.ReviewDecisionID, &item.PreviousReviewDecisionID, &item.ClaimSetID, &item.Revision, &item.ActorUserID, &item.Reason, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan correction history: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (t transaction) ApplyFactCorrection(ctx context.Context, command ports.FactCorrectionCommand) (ports.FactCorrectionResult, error) {
	c, revision, state := command.Confirmation, command.Revision, command.State
	if err := t.requireTripManager(ctx, c.TenantID, c.ActorUserID); err != nil {
		return ports.FactCorrectionResult{}, err
	}
	if revision.TenantID != c.TenantID || revision.ClaimSet.ID != c.ClaimSetID || revision.DocumentID != state.DocumentID || revision.PreviousClaimSetID != state.ClaimSetID ||
		revision.ClaimSet.DocumentType != state.FactType || c.ExpectedRevision != revision.ExpectedRevision+1 || !revision.ClaimSet.Status.CanConfirm() || len(c.CandidateDecisions) != 0 {
		return ports.FactCorrectionResult{}, domain.ErrInvalidInput
	}
	if (c.Payment != nil) != (state.FactType == domain.DocumentPayment) || (c.Invoice != nil) != (state.FactType == domain.DocumentInvoice) || (c.Trip != nil) != (state.FactType == domain.DocumentTrip) {
		return ports.FactCorrectionResult{}, domain.ErrInvalidInput
	}
	current, err := t.GetFactCorrectionState(ctx, c.TenantID, state.FactType, state.FactID, "")
	if err != nil {
		return ports.FactCorrectionResult{}, err
	}
	if current.Version != state.Version || current.CurrentReviewDecisionID != state.CurrentReviewDecisionID {
		return ports.FactCorrectionResult{}, domain.ErrVersionConflict
	}
	if err := t.insertRevisionIdentity(ctx, revision); err != nil {
		return ports.FactCorrectionResult{}, err
	}
	excludedInvoice := ""
	if c.Invoice != nil {
		excludedInvoice = state.FactID
	}
	status, err := t.persistRevisionContents(ctx, revision, excludedInvoice)
	if err != nil {
		return ports.FactCorrectionResult{}, err
	}
	if !status.CanConfirm() {
		return ports.FactCorrectionResult{}, domain.NewRuleError("correction_blocked", "纠错校验未通过", domain.ErrConflict)
	}
	createdAt := c.CreatedAt.UTC().Format(time.RFC3339Nano)
	if _, err := t.tx.ExecContext(ctx, `INSERT INTO review_decisions (id, tenant_id, claim_set_id, actor_user_id, action, fact_type,
		association_mode, duplicate_plan_hash, idempotency_key, expected_revision, reason, created_at)
		VALUES (?, ?, ?, ?, 'confirm', ?, NULLIF(?, ''), ?, ?, ?, ?, ?)`, c.ReviewDecisionID, c.TenantID, c.ClaimSetID, c.ActorUserID, state.FactType, c.AssociationMode, c.DuplicatePlanHash, c.IdempotencyKey, c.ExpectedRevision, command.Reason, createdAt); err != nil {
		return ports.FactCorrectionResult{}, fmt.Errorf("insert correction review: %w", err)
	}
	for _, decision := range c.DuplicateDecisions {
		if _, err := t.tx.ExecContext(ctx, `INSERT INTO duplicate_candidate_decisions (id, tenant_id, candidate_id, review_decision_id, action, created_at) VALUES (?, ?, ?, ?, ?, ?)`, decision.ID, c.TenantID, decision.CandidateID, c.ReviewDecisionID, decision.Action, createdAt); err != nil {
			return ports.FactCorrectionResult{}, fmt.Errorf("insert correction duplicate decision: %w", err)
		}
	}
	if _, err := t.tx.ExecContext(ctx, `UPDATE claim_sets SET status = 'confirmed', optimistic_version = optimistic_version + 1 WHERE tenant_id = ? AND id = ? AND status = 'ready_for_review'`, c.TenantID, c.ClaimSetID); err != nil {
		return ports.FactCorrectionResult{}, fmt.Errorf("confirm correction claim: %w", err)
	}
	if err := t.tripAudit(ctx, c.TenantID, c.ActorUserID, c.AuditEventID, "fact_corrected", string(state.FactType), state.FactID, c.RequestID, map[string]any{"withdrawal_count": len(command.WithdrawLinkIDs), "previous_version": state.Version}, c.CreatedAt); err != nil {
		return ports.FactCorrectionResult{}, err
	}
	// 先撤销明确选择的旧 Link，再更新金额；旧 Link 始终保留。
	column := "payment_id"
	if state.FactType == domain.DocumentInvoice {
		column = "invoice_id"
	}
	for _, id := range command.WithdrawLinkIDs {
		changed, err := t.tx.ExecContext(ctx, `UPDATE payment_invoice_links SET ended_at = ?, ended_by_audit_event_id = ?
			WHERE tenant_id = ? AND id = ? AND `+column+` = ? AND ended_at IS NULL`, createdAt, c.AuditEventID, c.TenantID, id, state.FactID)
		if err != nil {
			return ports.FactCorrectionResult{}, fmt.Errorf("withdraw correction allocation: %w", err)
		}
		if err := requireAffected(changed); err != nil {
			return ports.FactCorrectionResult{}, domain.ErrVersionConflict
		}
	}
	if err := t.updateCorrectionProjection(ctx, command, createdAt); err != nil {
		return ports.FactCorrectionResult{}, err
	}
	itemIDs := make(map[string]string)
	if c.Invoice != nil {
		if err := t.insertInvoiceItems(ctx, c, itemIDs); err != nil {
			return ports.FactCorrectionResult{}, err
		}
	}
	if err := t.insertFactOrigins(ctx, c, itemIDs, createdAt); err != nil {
		return ports.FactCorrectionResult{}, err
	}
	if c.Payment != nil {
		if err := t.reconcileTripPayments(ctx, c.TenantID, c.ActorUserID, c.RequestID, c.CreatedAt, state.FactID, nil); err != nil {
			return ports.FactCorrectionResult{}, err
		}
	}
	table, err := correctionTable(state.FactType)
	if err != nil {
		return ports.FactCorrectionResult{}, err
	}
	var version int
	if err := t.tx.QueryRowContext(ctx, `SELECT version FROM `+table+` WHERE tenant_id = ? AND id = ?`, c.TenantID, state.FactID).Scan(&version); err != nil {
		return ports.FactCorrectionResult{}, fmt.Errorf("read final correction version: %w", err)
	}
	var paymentID, invoiceID, tripID any
	switch state.FactType {
	case domain.DocumentPayment:
		paymentID = state.FactID
	case domain.DocumentInvoice:
		invoiceID = state.FactID
	case domain.DocumentTrip:
		tripID = state.FactID
	}
	if _, err := t.tx.ExecContext(ctx, `INSERT INTO fact_corrections (tenant_id, review_decision_id, previous_review_decision_id, payment_id, invoice_id, trip_evidence_id, expected_version, resulting_version, request_hash, preview_hash, audit_event_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.TenantID, c.ReviewDecisionID, state.CurrentReviewDecisionID, paymentID, invoiceID, tripID, state.Version, version, command.RequestHash, command.PreviewHash, c.AuditEventID); err != nil {
		return ports.FactCorrectionResult{}, fmt.Errorf("insert correction history: %w", err)
	}
	return ports.FactCorrectionResult{FactType: state.FactType, FactID: state.FactID, ReviewDecisionID: c.ReviewDecisionID, ClaimSetID: c.ClaimSetID, Version: version}, nil
}

func (t transaction) updateCorrectionProjection(ctx context.Context, command ports.FactCorrectionCommand, createdAt string) error {
	c, state := command.Confirmation, command.State
	var changed sql.Result
	var err error
	switch state.FactType {
	case domain.DocumentPayment:
		p := c.Payment
		if p.ID != state.FactID {
			return domain.ErrInvalidInput
		}
		changed, err = t.tx.ExecContext(ctx, `UPDATE payments SET amount_minor = ?, currency = ?, merchant = ?, transaction_time = ?, source_timezone = ?, business_date = ?, payment_method = ?, order_number = ?, category = ?, current_review_decision_id = ?, version = version + 1, updated_at = ? WHERE tenant_id = ? AND id = ? AND version = ? AND deleted_at IS NULL`, p.AmountMinor, p.Currency, p.Merchant, p.TransactionTime, p.SourceTimezone, p.BusinessDate, p.PaymentMethod, p.OrderNumber, p.Category, c.ReviewDecisionID, createdAt, c.TenantID, state.FactID, state.Version)
	case domain.DocumentInvoice:
		i := c.Invoice
		if i.ID != state.FactID {
			return domain.ErrInvalidInput
		}
		changed, err = t.tx.ExecContext(ctx, `UPDATE invoices SET invoice_number = ?, normalized_invoice_number = ?, invoice_date = ?, total_minor = ?, tax_minor = ?, currency = ?, seller_name = ?, buyer_name = ?, current_review_decision_id = ?, version = version + 1, updated_at = ? WHERE tenant_id = ? AND id = ? AND version = ? AND deleted_at IS NULL`, i.InvoiceNumber, i.NormalizedInvoiceNumber, i.InvoiceDate, i.TotalMinor, i.TaxMinor, i.Currency, i.SellerName, i.BuyerName, c.ReviewDecisionID, createdAt, c.TenantID, state.FactID, state.Version)
	case domain.DocumentTrip:
		trip := c.Trip
		if trip.ID != state.FactID {
			return domain.ErrInvalidInput
		}
		changed, err = t.tx.ExecContext(ctx, `UPDATE trip_evidence_facts SET origin = ?, destination = ?, start_date = ?, end_date = ?, traveler_name = ?, transport_type = ?, booking_reference = ?, current_review_decision_id = ?, version = version + 1, updated_at = ? WHERE tenant_id = ? AND id = ? AND version = ? AND deleted_at IS NULL`, trip.Origin, trip.Destination, trip.StartDate, trip.EndDate, trip.TravelerName, trip.TransportType, trip.BookingReference, c.ReviewDecisionID, createdAt, c.TenantID, state.FactID, state.Version)
	default:
		return domain.ErrInvalidInput
	}
	if err != nil {
		var conflict *pgconn.PgError
		if state.FactType == domain.DocumentInvoice && errors.As(err, &conflict) && conflict.Code == "23505" && conflict.ConstraintName == "invoices_number_active_idx" {
			return domain.NewRuleError("invoice_number_conflict", "同一工作区已存在相同规范化发票号码", domain.ErrConflict)
		}
		return fmt.Errorf("update corrected fact projection: %w", err)
	}
	if err := requireAffected(changed); err != nil {
		return domain.ErrVersionConflict
	}
	return nil
}
