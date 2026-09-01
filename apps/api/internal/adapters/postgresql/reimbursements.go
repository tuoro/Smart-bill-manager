package postgresqladapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type reimbursementQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) BuildReimbursementPreview(
	ctx context.Context,
	tenantID, tripID string,
	assignmentIDs []string,
) (domain.ReimbursementPolicySnapshot, error) {
	return withReimbursementReadSnapshot(ctx, s.db, func(queryer reimbursementQueryer) (domain.ReimbursementPolicySnapshot, error) {
		return loadReimbursementPreview(ctx, queryer, tenantID, tripID, assignmentIDs)
	})
}

func (s *Store) ListReimbursements(
	ctx context.Context,
	tenantID string,
	query ports.ReimbursementListQuery,
) (ports.ReimbursementListPage, error) {
	statement := `
		SELECT reimbursement.id, reimbursement.trip_id,
		       reimbursement.trip_destination, reimbursement.trip_start_date::text, reimbursement.trip_end_date::text,
		       CASE WHEN trip.deleted_at IS NULL THEN 0 ELSE 1 END,
		       reimbursement.status, reimbursement.version,
		       (SELECT count(*) FROM reimbursement_items item
		        WHERE item.tenant_id = reimbursement.tenant_id AND item.reimbursement_id = reimbursement.id),
		       (SELECT count(*) FROM reimbursement_policy_findings finding
		        WHERE finding.tenant_id = reimbursement.tenant_id AND finding.reimbursement_id = reimbursement.id),
		       reimbursement.created_at, reimbursement.updated_at
		FROM reimbursements reimbursement
		JOIN trips trip ON trip.tenant_id = reimbursement.tenant_id AND trip.id = reimbursement.trip_id
		WHERE reimbursement.tenant_id = ?
	`
	arguments := []any{tenantID}
	if query.After != nil {
		statement += ` AND (reimbursement.created_at < ? OR (reimbursement.created_at = ? AND reimbursement.id < ?))`
		after := query.After.CreatedAt.UTC().Format(time.RFC3339Nano)
		arguments = append(arguments, after, after, query.After.ID)
	}
	statement += ` ORDER BY reimbursement.created_at DESC, reimbursement.id DESC LIMIT ?`
	arguments = append(arguments, query.Limit+1)
	rows, err := s.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return ports.ReimbursementListPage{}, fmt.Errorf("list reimbursements: %w", err)
	}
	defer rows.Close()
	items := make([]ports.ReimbursementSummary, 0, query.Limit+1)
	for rows.Next() {
		item, err := scanReimbursementSummary(rows)
		if err != nil {
			return ports.ReimbursementListPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ports.ReimbursementListPage{}, fmt.Errorf("iterate reimbursements: %w", err)
	}
	result := ports.ReimbursementListPage{Items: items}
	if len(items) > query.Limit {
		result.Items = items[:query.Limit]
		last := result.Items[len(result.Items)-1]
		result.NextCursor = &ports.ReimbursementCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return result, nil
}

func (s *Store) GetReimbursement(
	ctx context.Context,
	tenantID, reimbursementID string,
) (ports.ReimbursementDetail, error) {
	return withReimbursementReadSnapshot(ctx, s.db, func(queryer reimbursementQueryer) (ports.ReimbursementDetail, error) {
		return loadReimbursementDetail(ctx, queryer, tenantID, reimbursementID)
	})
}

func loadReimbursementDetail(
	ctx context.Context,
	queryer reimbursementQueryer,
	tenantID, reimbursementID string,
) (ports.ReimbursementDetail, error) {
	var detail ports.ReimbursementDetail
	var tripDeleted int
	var createdAt, updatedAt string
	err := queryer.QueryRowContext(ctx, `
		SELECT reimbursement.id, reimbursement.trip_id,
		       reimbursement.trip_destination, reimbursement.trip_start_date::text, reimbursement.trip_end_date::text,
		       CASE WHEN trip.deleted_at IS NULL THEN 0 ELSE 1 END,
		       reimbursement.status, reimbursement.version,
		       (SELECT count(*) FROM reimbursement_items item
		        WHERE item.tenant_id = reimbursement.tenant_id AND item.reimbursement_id = reimbursement.id),
		       (SELECT count(*) FROM reimbursement_policy_findings finding
		        WHERE finding.tenant_id = reimbursement.tenant_id AND finding.reimbursement_id = reimbursement.id),
		       reimbursement.created_at, reimbursement.updated_at,
		       reimbursement.policy_rule_version, reimbursement.snapshot_hash
		FROM reimbursements reimbursement
		JOIN trips trip ON trip.tenant_id = reimbursement.tenant_id AND trip.id = reimbursement.trip_id
		WHERE reimbursement.tenant_id = ? AND reimbursement.id = ?
	`, tenantID, reimbursementID).Scan(
		&detail.ID,
		&detail.Trip.ID,
		&detail.Trip.Destination,
		&detail.Trip.StartDate,
		&detail.Trip.EndDate,
		&tripDeleted,
		&detail.Status,
		&detail.Version,
		&detail.ItemCount,
		&detail.FindingCount,
		&createdAt,
		&updatedAt,
		&detail.RuleVersion,
		&detail.SnapshotHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ReimbursementDetail{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.ReimbursementDetail{}, fmt.Errorf("read reimbursement: %w", err)
	}
	detail.TripDeleted = tripDeleted == 1
	detail.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return ports.ReimbursementDetail{}, fmt.Errorf("parse reimbursement created_at: %w", err)
	}
	detail.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return ports.ReimbursementDetail{}, fmt.Errorf("parse reimbursement updated_at: %w", err)
	}
	detail.Items, err = loadReimbursementItems(ctx, queryer, tenantID, reimbursementID)
	if err != nil {
		return ports.ReimbursementDetail{}, err
	}
	detail.Findings, err = loadReimbursementFindings(ctx, queryer, tenantID, reimbursementID)
	if err != nil {
		return ports.ReimbursementDetail{}, err
	}
	detail.Decisions, err = loadReimbursementDecisions(ctx, queryer, tenantID, reimbursementID)
	if err != nil {
		return ports.ReimbursementDetail{}, err
	}
	detail.Totals, err = reimbursementSnapshotTotals(detail.Items)
	if err != nil {
		return ports.ReimbursementDetail{}, err
	}
	return detail, nil
}

func withReimbursementReadSnapshot[T any](
	ctx context.Context,
	database *sql.DB,
	read func(reimbursementQueryer) (T, error),
) (T, error) {
	var zero T
	transaction, err := database.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return zero, fmt.Errorf("begin reimbursement read snapshot: %w", err)
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
		return zero, fmt.Errorf("commit reimbursement read snapshot: %w", err)
	}
	committed = true
	return result, nil
}

func (s *Store) GetReimbursementDecisionReplay(
	ctx context.Context,
	tenantID, idempotencyKey string,
) (ports.ReimbursementDecisionReplay, error) {
	replay, found, err := loadReimbursementDecisionReplay(ctx, s.db, tenantID, idempotencyKey)
	if err != nil {
		return ports.ReimbursementDecisionReplay{}, err
	}
	if !found {
		return ports.ReimbursementDecisionReplay{}, domain.ErrNotFound
	}
	return replay, nil
}

func loadReimbursementPreview(
	ctx context.Context,
	queryer reimbursementQueryer,
	tenantID, tripID string,
	assignmentIDs []string,
) (domain.ReimbursementPolicySnapshot, error) {
	selection, err := domain.CanonicalReimbursementSelection(assignmentIDs)
	if err != nil {
		return domain.ReimbursementPolicySnapshot{}, err
	}
	input := domain.ReimbursementPolicyInput{}
	err = queryer.QueryRowContext(ctx, `
		SELECT id, destination, start_date::text, end_date::text
		FROM trips
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL
	`, tenantID, tripID).Scan(
		&input.Trip.ID,
		&input.Trip.Destination,
		&input.Trip.StartDate,
		&input.Trip.EndDate,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ReimbursementPolicySnapshot{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ReimbursementPolicySnapshot{}, fmt.Errorf("read reimbursement trip: %w", err)
	}
	input.Items, err = loadReimbursementPolicyItems(ctx, queryer, tenantID, tripID, selection)
	if err != nil {
		return domain.ReimbursementPolicySnapshot{}, err
	}
	input.Links, err = loadReimbursementPolicyLinks(ctx, queryer, tenantID, input.Items)
	if err != nil {
		return domain.ReimbursementPolicySnapshot{}, err
	}
	input.PriorUses, err = loadReimbursementPriorUses(ctx, queryer, tenantID, input.Items)
	if err != nil {
		return domain.ReimbursementPolicySnapshot{}, err
	}
	return domain.EvaluateReimbursementPolicy(input)
}

func loadReimbursementPolicyItems(
	ctx context.Context,
	queryer reimbursementQueryer,
	tenantID, tripID string,
	assignmentIDs []string,
) ([]domain.ReimbursementPolicyItem, error) {
	placeholders := sqlPlaceholders(len(assignmentIDs))
	arguments := make([]any, 0, len(assignmentIDs)+2)
	arguments = append(arguments, tenantID, tripID)
	for _, assignmentID := range assignmentIDs {
		arguments = append(arguments, assignmentID)
	}
	rows, err := queryer.QueryContext(ctx, `
		SELECT assignment.id, assignment.payment_id, assignment.invoice_id,
		       payment.merchant, payment.business_date::text,
		       payment.amount_minor, payment.currency,
		       invoice.seller_name, invoice.invoice_date::text, invoice.total_minor, invoice.currency
		FROM trip_fact_assignments assignment
		LEFT JOIN payments payment
		  ON payment.tenant_id = assignment.tenant_id
		 AND payment.id = assignment.payment_id
		 AND payment.deleted_at IS NULL
		LEFT JOIN invoices invoice
		  ON invoice.tenant_id = assignment.tenant_id
		 AND invoice.id = assignment.invoice_id
		 AND invoice.deleted_at IS NULL
		WHERE assignment.tenant_id = ? AND assignment.trip_id = ?
		  AND assignment.ended_at IS NULL
		  AND assignment.id IN (`+placeholders+`)
		ORDER BY assignment.id
	`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("load reimbursement assignments: %w", err)
	}
	defer rows.Close()
	items := make([]domain.ReimbursementPolicyItem, 0, len(assignmentIDs))
	for rows.Next() {
		var item domain.ReimbursementPolicyItem
		var paymentID, invoiceID sql.NullString
		var paymentName, paymentDate, paymentCurrency sql.NullString
		var paymentAmount sql.NullInt64
		var invoiceName, invoiceDate, invoiceCurrency sql.NullString
		var invoiceAmount sql.NullInt64
		if err := rows.Scan(
			&item.AssignmentID,
			&paymentID,
			&invoiceID,
			&paymentName,
			&paymentDate,
			&paymentAmount,
			&paymentCurrency,
			&invoiceName,
			&invoiceDate,
			&invoiceAmount,
			&invoiceCurrency,
		); err != nil {
			return nil, fmt.Errorf("scan reimbursement assignment: %w", err)
		}
		switch {
		case paymentID.Valid && paymentName.Valid && paymentDate.Valid && paymentAmount.Valid && paymentCurrency.Valid && !invoiceID.Valid:
			item.FactType = domain.DocumentPayment
			item.FactID = paymentID.String
			item.DisplayName = paymentName.String
			item.BusinessDate = paymentDate.String
			item.AmountMinor = paymentAmount.Int64
			item.Currency = domain.Currency(paymentCurrency.String)
		case invoiceID.Valid && invoiceName.Valid && invoiceDate.Valid && invoiceAmount.Valid && invoiceCurrency.Valid && !paymentID.Valid:
			item.FactType = domain.DocumentInvoice
			item.FactID = invoiceID.String
			item.DisplayName = invoiceName.String
			item.BusinessDate = invoiceDate.String
			item.AmountMinor = invoiceAmount.Int64
			item.Currency = domain.Currency(invoiceCurrency.String)
		default:
			return nil, domain.NewRuleError("reimbursement_selection_stale", "报销选择已移动、终止或删除，请刷新后重试", domain.ErrConflict)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reimbursement assignments: %w", err)
	}
	if len(items) != len(assignmentIDs) {
		return nil, domain.NewRuleError("reimbursement_selection_stale", "报销选择已移动、终止或删除，请刷新后重试", domain.ErrConflict)
	}
	return items, nil
}

func loadReimbursementPolicyLinks(
	ctx context.Context,
	queryer reimbursementQueryer,
	tenantID string,
	items []domain.ReimbursementPolicyItem,
) ([]domain.ReimbursementPolicyLink, error) {
	paymentIDs := reimbursementFactIDs(items, domain.DocumentPayment)
	invoiceIDs := reimbursementFactIDs(items, domain.DocumentInvoice)
	if len(paymentIDs) == 0 || len(invoiceIDs) == 0 {
		return []domain.ReimbursementPolicyLink{}, nil
	}
	arguments := make([]any, 0, 1+len(paymentIDs)+len(invoiceIDs))
	arguments = append(arguments, tenantID)
	for _, id := range paymentIDs {
		arguments = append(arguments, id)
	}
	for _, id := range invoiceIDs {
		arguments = append(arguments, id)
	}
	rows, err := queryer.QueryContext(ctx, `
		SELECT id, payment_id, invoice_id, allocated_minor, currency
		FROM payment_invoice_links
		WHERE tenant_id = ? AND ended_at IS NULL
		  AND payment_id IN (`+sqlPlaceholders(len(paymentIDs))+`)
		  AND invoice_id IN (`+sqlPlaceholders(len(invoiceIDs))+`)
		ORDER BY id
	`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("load reimbursement allocation links: %w", err)
	}
	defer rows.Close()
	result := make([]domain.ReimbursementPolicyLink, 0)
	for rows.Next() {
		var item domain.ReimbursementPolicyLink
		if err := rows.Scan(&item.ID, &item.PaymentID, &item.InvoiceID, &item.AllocatedMinor, &item.Currency); err != nil {
			return nil, fmt.Errorf("scan reimbursement allocation link: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadReimbursementPriorUses(
	ctx context.Context,
	queryer reimbursementQueryer,
	tenantID string,
	items []domain.ReimbursementPolicyItem,
) ([]domain.ReimbursementPriorUse, error) {
	paymentIDs := reimbursementFactIDs(items, domain.DocumentPayment)
	invoiceIDs := reimbursementFactIDs(items, domain.DocumentInvoice)
	clauses := make([]string, 0, 2)
	arguments := []any{tenantID}
	if len(paymentIDs) > 0 {
		clauses = append(clauses, `(item.fact_type = 'payment' AND item.payment_id IN (`+sqlPlaceholders(len(paymentIDs))+`))`)
		for _, id := range paymentIDs {
			arguments = append(arguments, id)
		}
	}
	if len(invoiceIDs) > 0 {
		clauses = append(clauses, `(item.fact_type = 'invoice' AND item.invoice_id IN (`+sqlPlaceholders(len(invoiceIDs))+`))`)
		for _, id := range invoiceIDs {
			arguments = append(arguments, id)
		}
	}
	rows, err := queryer.QueryContext(ctx, `
		SELECT item.fact_type, coalesce(item.payment_id, item.invoice_id),
		       reimbursement.id, reimbursement.status
		FROM reimbursement_items item
		JOIN reimbursements reimbursement
		  ON reimbursement.tenant_id = item.tenant_id
		 AND reimbursement.id = item.reimbursement_id
		WHERE item.tenant_id = ?
		  AND reimbursement.status IN ('submitted', 'reimbursed')
		  AND (`+strings.Join(clauses, " OR ")+`)
		ORDER BY item.fact_type, coalesce(item.payment_id, item.invoice_id), reimbursement.id
	`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("load reimbursement prior uses: %w", err)
	}
	defer rows.Close()
	result := make([]domain.ReimbursementPriorUse, 0)
	for rows.Next() {
		var item domain.ReimbursementPriorUse
		if err := rows.Scan(&item.FactType, &item.FactID, &item.ReimbursementID, &item.Status); err != nil {
			return nil, fmt.Errorf("scan reimbursement prior use: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (t transaction) SubmitReimbursement(
	ctx context.Context,
	command ports.ReimbursementSubmissionCommand,
) (ports.ReimbursementMutationResult, error) {
	if replay, found, err := loadReimbursementDecisionReplay(ctx, t.tx, command.TenantID, command.IdempotencyKey); err != nil {
		return ports.ReimbursementMutationResult{}, err
	} else if found {
		if replay.RequestHash != command.RequestHash {
			return ports.ReimbursementMutationResult{}, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的报销请求", domain.ErrConflict)
		}
		return replay.Result, nil
	}
	preview, err := loadReimbursementPreview(ctx, t.tx, command.TenantID, command.TripID, command.AssignmentIDs)
	if err != nil {
		return ports.ReimbursementMutationResult{}, err
	}
	if preview.SnapshotHash != command.ExpectedSnapshotHash {
		return ports.ReimbursementMutationResult{}, domain.NewRuleError("reimbursement_snapshot_stale", "报销预检输入已变化，请刷新后重新确认", domain.ErrConflict)
	}
	currentFindingKeys := reimbursementFindingKeys(preview.Findings)
	if !slices.Equal(currentFindingKeys, command.AcknowledgedFindingKeys) {
		return ports.ReimbursementMutationResult{}, domain.NewRuleError("reimbursement_findings_unacknowledged", "必须确认当前完整报销提示", domain.ErrConflict)
	}
	itemIDs, err := validateReimbursementItemDrafts(preview.Items, command.ItemDrafts)
	if err != nil {
		return ports.ReimbursementMutationResult{}, err
	}
	findingIDs, err := validateReimbursementFindingDrafts(preview.Findings, command.FindingDrafts)
	if err != nil {
		return ports.ReimbursementMutationResult{}, err
	}
	createdAt := command.CreatedAt.UTC().Format(time.RFC3339Nano)
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO reimbursements (
			id, tenant_id, trip_id, trip_destination, trip_start_date, trip_end_date,
			status, policy_rule_version, snapshot_hash, created_by_user_id,
			created_by_decision_id, created_at, updated_at, version
		) VALUES (?, ?, ?, ?, ?, ?, 'submitted', ?, ?, ?, ?, ?, ?, 1)
	`,
		command.ReimbursementID,
		command.TenantID,
		preview.Trip.ID,
		preview.Trip.Destination,
		preview.Trip.StartDate,
		preview.Trip.EndDate,
		preview.RuleVersion,
		preview.SnapshotHash,
		command.ActorUserID,
		command.DecisionID,
		createdAt,
		createdAt,
	); err != nil {
		return ports.ReimbursementMutationResult{}, reimbursementWriteError("insert reimbursement", err)
	}
	for index, item := range preview.Items {
		var paymentID, invoiceID any
		if item.FactType == domain.DocumentPayment {
			paymentID = item.FactID
		} else {
			invoiceID = item.FactID
		}
		if _, err := t.tx.ExecContext(ctx, `
			INSERT INTO reimbursement_items (
				id, tenant_id, reimbursement_id, trip_fact_assignment_id,
				fact_type, payment_id, invoice_id, display_name, business_date,
				amount_minor, currency, sort_order, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			itemIDs[item.AssignmentID],
			command.TenantID,
			command.ReimbursementID,
			item.AssignmentID,
			item.FactType,
			paymentID,
			invoiceID,
			item.DisplayName,
			item.BusinessDate,
			item.AmountMinor,
			item.Currency,
			index,
			createdAt,
		); err != nil {
			return ports.ReimbursementMutationResult{}, reimbursementWriteError("insert reimbursement item", err)
		}
	}
	for _, finding := range preview.Findings {
		var relatedID, relatedStatus any
		if finding.RelatedReimbursementID != "" {
			relatedID = finding.RelatedReimbursementID
			relatedStatus = finding.RelatedStatus
		}
		if _, err := t.tx.ExecContext(ctx, `
			INSERT INTO reimbursement_policy_findings (
				id, tenant_id, reimbursement_id, item_id, finding_key, rule_version,
				code, expected_minor, actual_minor, currency,
				related_reimbursement_id, related_reimbursement_status, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?)
		`,
			findingIDs[finding.FindingKey],
			command.TenantID,
			command.ReimbursementID,
			itemIDs[finding.AssignmentID],
			finding.FindingKey,
			preview.RuleVersion,
			finding.Code,
			finding.ExpectedMinor,
			finding.ActualMinor,
			finding.Currency,
			relatedID,
			relatedStatus,
			createdAt,
		); err != nil {
			return ports.ReimbursementMutationResult{}, reimbursementWriteError("insert reimbursement finding", err)
		}
	}
	metadata, _ := json.Marshal(map[string]any{
		"action": "submit", "status": "submitted",
		"item_count": len(preview.Items), "finding_count": len(preview.Findings),
	})
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, tenant_id, actor_user_id, action, resource_type, resource_id,
			request_id, safe_metadata_json, created_at
		) VALUES (?, ?, ?, 'reimbursement_submitted', 'reimbursement', ?, ?, ?::jsonb, ?)
	`, command.AuditEventID, command.TenantID, command.ActorUserID,
		command.ReimbursementID, command.RequestID, string(metadata), createdAt); err != nil {
		return ports.ReimbursementMutationResult{}, fmt.Errorf("insert reimbursement audit: %w", err)
	}
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO reimbursement_status_decisions (
			id, tenant_id, reimbursement_id, actor_user_id, previous_status,
			desired_status, expected_version, result_version, action,
			idempotency_key, request_hash, reason, audit_event_id, created_at
		) VALUES (?, ?, ?, ?, NULL, 'submitted', 0, 1, 'submit', ?, ?, ?, ?, ?)
	`,
		command.DecisionID,
		command.TenantID,
		command.ReimbursementID,
		command.ActorUserID,
		command.IdempotencyKey,
		command.RequestHash,
		command.Reason,
		command.AuditEventID,
		createdAt,
	); err != nil {
		return ports.ReimbursementMutationResult{}, reimbursementWriteError("insert reimbursement submit decision", err)
	}
	return ports.ReimbursementMutationResult{
		ReimbursementID: command.ReimbursementID,
		DecisionID:      command.DecisionID,
		Status:          domain.ReimbursementStatusSubmitted,
		Version:         1,
	}, nil
}

func (t transaction) ApplyReimbursementStatus(
	ctx context.Context,
	command ports.ReimbursementStatusCommand,
) (ports.ReimbursementMutationResult, error) {
	if replay, found, err := loadReimbursementDecisionReplay(ctx, t.tx, command.TenantID, command.IdempotencyKey); err != nil {
		return ports.ReimbursementMutationResult{}, err
	} else if found {
		if replay.RequestHash != command.RequestHash {
			return ports.ReimbursementMutationResult{}, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的报销状态请求", domain.ErrConflict)
		}
		return replay.Result, nil
	}
	var currentStatus domain.ReimbursementStatus
	var currentVersion int
	err := t.tx.QueryRowContext(ctx, `
		SELECT status, version FROM reimbursements WHERE tenant_id = ? AND id = ?
	`, command.TenantID, command.ReimbursementID).Scan(&currentStatus, &currentVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ReimbursementMutationResult{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.ReimbursementMutationResult{}, fmt.Errorf("read reimbursement status: %w", err)
	}
	if currentStatus != command.ExpectedStatus || currentVersion != command.ExpectedVersion {
		return ports.ReimbursementMutationResult{}, domain.NewRuleError("reimbursement_status_stale", "报销状态或版本已变化，请刷新后重试", domain.ErrConflict)
	}
	action, err := domain.ReimbursementTransitionAction(currentStatus, command.DesiredStatus)
	if err != nil {
		return ports.ReimbursementMutationResult{}, err
	}
	if action != command.Action {
		return ports.ReimbursementMutationResult{}, domain.ErrInvalidInput
	}
	createdAt := command.CreatedAt.UTC().Format(time.RFC3339Nano)
	metadata, _ := json.Marshal(map[string]any{
		"action": action, "previous_status": currentStatus, "desired_status": command.DesiredStatus,
	})
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, tenant_id, actor_user_id, action, resource_type, resource_id,
			request_id, safe_metadata_json, created_at
		) VALUES (?, ?, ?, 'reimbursement_status_changed', 'reimbursement', ?, ?, ?::jsonb, ?)
	`, command.AuditEventID, command.TenantID, command.ActorUserID,
		command.ReimbursementID, command.RequestID, string(metadata), createdAt); err != nil {
		return ports.ReimbursementMutationResult{}, fmt.Errorf("insert reimbursement status audit: %w", err)
	}
	resultVersion := currentVersion + 1
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO reimbursement_status_decisions (
			id, tenant_id, reimbursement_id, actor_user_id, previous_status,
			desired_status, expected_version, result_version, action,
			idempotency_key, request_hash, reason, audit_event_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		command.DecisionID,
		command.TenantID,
		command.ReimbursementID,
		command.ActorUserID,
		currentStatus,
		command.DesiredStatus,
		currentVersion,
		resultVersion,
		action,
		command.IdempotencyKey,
		command.RequestHash,
		command.Reason,
		command.AuditEventID,
		createdAt,
	); err != nil {
		return ports.ReimbursementMutationResult{}, reimbursementWriteError("insert reimbursement status decision", err)
	}
	var persistedStatus domain.ReimbursementStatus
	var persistedVersion int
	if err := t.tx.QueryRowContext(ctx, `
		SELECT status, version FROM reimbursements WHERE tenant_id = ? AND id = ?
	`, command.TenantID, command.ReimbursementID).Scan(&persistedStatus, &persistedVersion); err != nil {
		return ports.ReimbursementMutationResult{}, fmt.Errorf("verify reimbursement status decision: %w", err)
	}
	if persistedStatus != command.DesiredStatus || persistedVersion != resultVersion {
		return ports.ReimbursementMutationResult{}, domain.NewRuleError("reimbursement_status_stale", "报销状态或版本已变化，请刷新后重试", domain.ErrConflict)
	}
	return ports.ReimbursementMutationResult{
		ReimbursementID: command.ReimbursementID,
		DecisionID:      command.DecisionID,
		Status:          command.DesiredStatus,
		Version:         resultVersion,
	}, nil
}

func loadReimbursementDecisionReplay(
	ctx context.Context,
	queryer reimbursementQueryer,
	tenantID, idempotencyKey string,
) (ports.ReimbursementDecisionReplay, bool, error) {
	var replay ports.ReimbursementDecisionReplay
	err := queryer.QueryRowContext(ctx, `
		SELECT decision.request_hash, decision.reimbursement_id, decision.id,
		       decision.desired_status, decision.result_version
		FROM reimbursement_status_decisions decision
		WHERE decision.tenant_id = ? AND decision.idempotency_key = ?
	`, tenantID, idempotencyKey).Scan(
		&replay.RequestHash,
		&replay.Result.ReimbursementID,
		&replay.Result.DecisionID,
		&replay.Result.Status,
		&replay.Result.Version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ReimbursementDecisionReplay{}, false, nil
	}
	if err != nil {
		return ports.ReimbursementDecisionReplay{}, false, fmt.Errorf("read reimbursement idempotency record: %w", err)
	}
	replay.Result.Replayed = true
	return replay, true, nil
}

func scanReimbursementSummary(scanner interface{ Scan(...any) error }) (ports.ReimbursementSummary, error) {
	var item ports.ReimbursementSummary
	var tripDeleted int
	var createdAt, updatedAt string
	if err := scanner.Scan(
		&item.ID,
		&item.Trip.ID,
		&item.Trip.Destination,
		&item.Trip.StartDate,
		&item.Trip.EndDate,
		&tripDeleted,
		&item.Status,
		&item.Version,
		&item.ItemCount,
		&item.FindingCount,
		&createdAt,
		&updatedAt,
	); err != nil {
		return ports.ReimbursementSummary{}, fmt.Errorf("scan reimbursement summary: %w", err)
	}
	item.TripDeleted = tripDeleted == 1
	var err error
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return ports.ReimbursementSummary{}, fmt.Errorf("parse reimbursement created_at: %w", err)
	}
	item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return ports.ReimbursementSummary{}, fmt.Errorf("parse reimbursement updated_at: %w", err)
	}
	return item, nil
}

func loadReimbursementItems(
	ctx context.Context,
	queryer reimbursementQueryer,
	tenantID, reimbursementID string,
) ([]ports.ReimbursementItem, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT item.id, item.trip_fact_assignment_id, item.fact_type,
		       coalesce(item.payment_id, item.invoice_id),
		       CASE
		         WHEN item.fact_type = 'payment' THEN CASE WHEN payment.deleted_at IS NULL THEN 0 ELSE 1 END
		         ELSE CASE WHEN invoice.deleted_at IS NULL THEN 0 ELSE 1 END
		       END,
		       item.display_name, item.business_date::text, item.amount_minor,
		       item.currency, item.sort_order
		FROM reimbursement_items item
		LEFT JOIN payments payment
		  ON payment.tenant_id = item.tenant_id AND payment.id = item.payment_id
		LEFT JOIN invoices invoice
		  ON invoice.tenant_id = item.tenant_id AND invoice.id = item.invoice_id
		WHERE item.tenant_id = ? AND item.reimbursement_id = ?
		ORDER BY item.sort_order, item.id
	`, tenantID, reimbursementID)
	if err != nil {
		return nil, fmt.Errorf("load reimbursement items: %w", err)
	}
	defer rows.Close()
	items := make([]ports.ReimbursementItem, 0)
	for rows.Next() {
		var item ports.ReimbursementItem
		var sourceDeleted int
		if err := rows.Scan(
			&item.ID, &item.AssignmentID, &item.FactType, &item.FactID, &sourceDeleted,
			&item.DisplayName, &item.BusinessDate, &item.AmountMinor, &item.Currency, &item.SortOrder,
		); err != nil {
			return nil, fmt.Errorf("scan reimbursement item: %w", err)
		}
		item.SourceDeleted = sourceDeleted == 1
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadReimbursementFindings(
	ctx context.Context,
	queryer reimbursementQueryer,
	tenantID, reimbursementID string,
) ([]ports.ReimbursementFinding, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT id, item_id, finding_key, code, expected_minor, actual_minor,
		       currency, related_reimbursement_id, related_reimbursement_status
		FROM reimbursement_policy_findings
		WHERE tenant_id = ? AND reimbursement_id = ?
		ORDER BY code, finding_key
	`, tenantID, reimbursementID)
	if err != nil {
		return nil, fmt.Errorf("load reimbursement findings: %w", err)
	}
	defer rows.Close()
	items := make([]ports.ReimbursementFinding, 0)
	for rows.Next() {
		var item ports.ReimbursementFinding
		var expected, actual sql.NullInt64
		var currency, relatedID, relatedStatus sql.NullString
		if err := rows.Scan(
			&item.ID, &item.ItemID, &item.FindingKey, &item.Code,
			&expected, &actual, &currency, &relatedID, &relatedStatus,
		); err != nil {
			return nil, fmt.Errorf("scan reimbursement finding: %w", err)
		}
		item.ExpectedMinor = nullableInt64(expected)
		item.ActualMinor = nullableInt64(actual)
		if currency.Valid {
			item.Currency = domain.Currency(currency.String)
		}
		item.RelatedReimbursementID = relatedID.String
		item.RelatedStatus = domain.ReimbursementStatus(relatedStatus.String)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadReimbursementDecisions(
	ctx context.Context,
	queryer reimbursementQueryer,
	tenantID, reimbursementID string,
) ([]ports.ReimbursementDecision, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT id, action, previous_status, desired_status,
		       expected_version, result_version, reason, created_at
		FROM reimbursement_status_decisions
		WHERE tenant_id = ? AND reimbursement_id = ?
		ORDER BY result_version, id
	`, tenantID, reimbursementID)
	if err != nil {
		return nil, fmt.Errorf("load reimbursement decisions: %w", err)
	}
	defer rows.Close()
	items := make([]ports.ReimbursementDecision, 0)
	for rows.Next() {
		var item ports.ReimbursementDecision
		var previous sql.NullString
		var createdAt string
		if err := rows.Scan(
			&item.ID, &item.Action, &previous, &item.DesiredStatus,
			&item.ExpectedVersion, &item.ResultVersion, &item.Reason, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan reimbursement decision: %w", err)
		}
		if previous.Valid {
			value := domain.ReimbursementStatus(previous.String)
			item.PreviousStatus = &value
		}
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse reimbursement decision created_at: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func reimbursementSnapshotTotals(items []ports.ReimbursementItem) ([]domain.ReimbursementCurrencyTotal, error) {
	totals := make(map[domain.Currency]int64)
	for _, item := range items {
		if totals[item.Currency] > domain.MaxSafeMinorUnits-item.AmountMinor {
			return nil, domain.NewRuleError("reimbursement_amount_overflow", "报销币种合计超出安全范围", domain.ErrConflict)
		}
		totals[item.Currency] += item.AmountMinor
	}
	currencies := make([]string, 0, len(totals))
	for currency := range totals {
		currencies = append(currencies, string(currency))
	}
	sort.Strings(currencies)
	result := make([]domain.ReimbursementCurrencyTotal, 0, len(currencies))
	for _, raw := range currencies {
		currency := domain.Currency(raw)
		result = append(result, domain.ReimbursementCurrencyTotal{Currency: currency, AmountMinor: totals[currency]})
	}
	return result, nil
}

func reimbursementFactIDs(items []domain.ReimbursementPolicyItem, factType domain.DocumentType) []string {
	result := make([]string, 0)
	for _, item := range items {
		if item.FactType == factType {
			result = append(result, item.FactID)
		}
	}
	sort.Strings(result)
	return result
}

func reimbursementFindingKeys(findings []domain.ReimbursementPolicyFinding) []string {
	result := make([]string, 0, len(findings))
	for _, finding := range findings {
		result = append(result, finding.FindingKey)
	}
	sort.Strings(result)
	return result
}

func validateReimbursementItemDrafts(
	items []domain.ReimbursementPolicyItem,
	drafts []ports.ReimbursementItemDraft,
) (map[string]string, error) {
	if len(items) != len(drafts) {
		return nil, errors.New("reimbursement item draft count mismatch")
	}
	result := make(map[string]string, len(drafts))
	for _, draft := range drafts {
		if draft.AssignmentID == "" || draft.ID == "" {
			return nil, errors.New("reimbursement item draft is incomplete")
		}
		if _, exists := result[draft.AssignmentID]; exists {
			return nil, errors.New("reimbursement item draft is duplicated")
		}
		result[draft.AssignmentID] = draft.ID
	}
	for _, item := range items {
		if result[item.AssignmentID] == "" {
			return nil, errors.New("reimbursement item draft is missing")
		}
	}
	return result, nil
}

func validateReimbursementFindingDrafts(
	findings []domain.ReimbursementPolicyFinding,
	drafts []ports.ReimbursementFindingDraft,
) (map[string]string, error) {
	if len(findings) != len(drafts) {
		return nil, errors.New("reimbursement finding draft count mismatch")
	}
	result := make(map[string]string, len(drafts))
	for _, draft := range drafts {
		if draft.FindingKey == "" || draft.ID == "" {
			return nil, errors.New("reimbursement finding draft is incomplete")
		}
		if _, exists := result[draft.FindingKey]; exists {
			return nil, errors.New("reimbursement finding draft is duplicated")
		}
		result[draft.FindingKey] = draft.ID
	}
	for _, finding := range findings {
		if result[finding.FindingKey] == "" {
			return nil, errors.New("reimbursement finding draft is missing")
		}
	}
	return result, nil
}

func sqlPlaceholders(count int) string {
	placeholders := make([]string, count)
	for index := range placeholders {
		placeholders[index] = "?"
	}
	return strings.Join(placeholders, ",")
}

func reimbursementWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		switch postgresError.ConstraintName {
		case "reimbursements_trip_submitted_idx":
			return domain.NewRuleError("reimbursement_trip_already_submitted", "该行程已有待处理报销", domain.ErrConflict)
		case "reimbursement_status_decisions_tenant_id_idempotency_key_key":
			return domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的报销请求", domain.ErrConflict)
		case "reimbursement_status_decision_tenant_id_reimbursement_id_re_key":
			return domain.NewRuleError("reimbursement_status_stale", "报销状态或版本已变化，请刷新后重试", domain.ErrConflict)
		}
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "reimbursement_item_scope_mismatch"),
		strings.Contains(message, "reimbursement_finding_scope_mismatch"),
		strings.Contains(message, "reimbursement_creation_scope_mismatch"):
		return domain.NewRuleError("reimbursement_snapshot_stale", "报销预检输入已变化，请刷新后重新确认", domain.ErrConflict)
	case strings.Contains(message, "reimbursement_status_decision_required"),
		strings.Contains(message, "reimbursement_decision_scope_mismatch"):
		return domain.NewRuleError("reimbursement_status_stale", "报销状态或版本已变化，请刷新后重试", domain.ErrConflict)
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}
