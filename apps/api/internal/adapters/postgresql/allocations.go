package postgresqladapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type allocationQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) GetAllocationWorkspace(
	ctx context.Context,
	tenantID string,
	anchorType domain.DocumentType,
	anchorID string,
) (ports.AllocationWorkspace, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return ports.AllocationWorkspace{}, err
	}
	defer tx.Rollback()
	return loadAllocationWorkspace(ctx, tx, tenantID, anchorType, anchorID)
}

func (s *Store) GetAllocationAdjustmentReplay(
	ctx context.Context,
	tenantID, idempotencyKey string,
) (ports.AllocationAdjustmentReplay, error) {
	replay, found, err := loadAllocationAdjustmentReplay(ctx, s.db, tenantID, idempotencyKey)
	if err != nil {
		return ports.AllocationAdjustmentReplay{}, err
	}
	if !found {
		return ports.AllocationAdjustmentReplay{}, domain.ErrNotFound
	}
	return replay, nil
}

func loadAllocationWorkspace(
	ctx context.Context, queryer allocationQueryer, tenantID string, anchorType domain.DocumentType, anchorID string,
) (ports.AllocationWorkspace, error) {
	workspace, err := loadAllocationState(ctx, queryer, tenantID, anchorType, anchorID)
	if err != nil {
		return workspace, err
	}
	workspace.Targets, workspace.NextCursor, err = loadAllocationTargets(ctx, queryer, tenantID, workspace.Anchor, workspace.Links)
	return workspace, err
}

func loadAllocationState(
	ctx context.Context,
	queryer allocationQueryer,
	tenantID string,
	anchorType domain.DocumentType,
	anchorID string,
) (ports.AllocationWorkspace, error) {
	workspace := ports.AllocationWorkspace{
		Links:   []ports.AllocationWorkspaceLink{},
		Targets: []ports.AllocationTarget{},
	}
	var err error
	switch anchorType {
	case domain.DocumentPayment:
		err = loadPaymentAllocationAnchor(ctx, queryer, tenantID, anchorID, &workspace.Anchor)
	case domain.DocumentInvoice:
		err = loadInvoiceAllocationAnchor(ctx, queryer, tenantID, anchorID, &workspace.Anchor)
	default:
		return ports.AllocationWorkspace{}, domain.NewRuleError("invalid_allocation_anchor", "分配 anchor 不合法", domain.ErrInvalidInput)
	}
	if err != nil {
		return ports.AllocationWorkspace{}, err
	}
	workspace.Links, err = loadActiveAllocationLinks(ctx, queryer, tenantID, anchorType, anchorID)
	if err != nil {
		return ports.AllocationWorkspace{}, err
	}
	active := make([]domain.ActiveAllocationLink, 0, len(workspace.Links))
	for _, link := range workspace.Links {
		active = append(active, activeLinkFromWorkspace(anchorType, anchorID, link))
	}
	_, workspace.PlanHash, err = domain.CanonicalActiveAllocationPlan(anchorType, anchorID, active)
	if err != nil {
		return ports.AllocationWorkspace{}, err
	}
	return workspace, nil
}

func loadPaymentAllocationAnchor(
	ctx context.Context,
	queryer allocationQueryer,
	tenantID, anchorID string,
	anchor *ports.AllocationFactSummary,
) error {
	err := queryer.QueryRowContext(ctx, `
		SELECT p.amount_minor,
		       coalesce((SELECT sum(l.allocated_minor) FROM payment_invoice_links l
		                 WHERE l.tenant_id = p.tenant_id AND l.payment_id = p.id AND l.ended_at IS NULL), 0),
		       p.currency, p.business_date::text, p.merchant
		FROM payments p
		JOIN review_decisions r ON r.tenant_id = p.tenant_id AND r.id = p.source_review_decision_id
		WHERE p.tenant_id = ? AND p.id = ? AND p.deleted_at IS NULL AND r.action = 'confirm'
	`, tenantID, anchorID).Scan(
		&anchor.AmountMinor,
		&anchor.AllocatedMinor,
		&anchor.Currency,
		&anchor.BusinessDate,
		&anchor.DisplayName,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load payment allocation anchor: %w", err)
	}
	anchor.FactType = domain.DocumentPayment
	anchor.ID = anchorID
	anchor.RemainingMinor = anchor.AmountMinor - anchor.AllocatedMinor
	return nil
}

func loadInvoiceAllocationAnchor(
	ctx context.Context,
	queryer allocationQueryer,
	tenantID, anchorID string,
	anchor *ports.AllocationFactSummary,
) error {
	err := queryer.QueryRowContext(ctx, `
		SELECT i.total_minor,
		       coalesce((SELECT sum(l.allocated_minor) FROM payment_invoice_links l
		                 WHERE l.tenant_id = i.tenant_id AND l.invoice_id = i.id AND l.ended_at IS NULL), 0),
		       i.currency, i.invoice_date::text, i.seller_name
		FROM invoices i
		JOIN review_decisions r ON r.tenant_id = i.tenant_id AND r.id = i.source_review_decision_id
		WHERE i.tenant_id = ? AND i.id = ? AND i.deleted_at IS NULL AND r.action = 'confirm'
	`, tenantID, anchorID).Scan(
		&anchor.AmountMinor,
		&anchor.AllocatedMinor,
		&anchor.Currency,
		&anchor.BusinessDate,
		&anchor.DisplayName,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load invoice allocation anchor: %w", err)
	}
	anchor.FactType = domain.DocumentInvoice
	anchor.ID = anchorID
	anchor.RemainingMinor = anchor.AmountMinor - anchor.AllocatedMinor
	return nil
}

func loadActiveAllocationLinks(
	ctx context.Context,
	queryer allocationQueryer,
	tenantID string,
	anchorType domain.DocumentType,
	anchorID string,
) ([]ports.AllocationWorkspaceLink, error) {
	column := "payment_id"
	targetType := domain.DocumentInvoice
	if anchorType == domain.DocumentInvoice {
		column = "invoice_id"
		targetType = domain.DocumentPayment
	}
	rows, err := queryer.QueryContext(ctx, `
		SELECT id, payment_id, invoice_id, allocated_minor, currency, created_at
		FROM payment_invoice_links
		WHERE tenant_id = ? AND `+column+` = ? AND ended_at IS NULL
		ORDER BY id
	`, tenantID, anchorID)
	if err != nil {
		return nil, fmt.Errorf("load active allocation links: %w", err)
	}
	defer rows.Close()
	result := make([]ports.AllocationWorkspaceLink, 0)
	for rows.Next() {
		var item ports.AllocationWorkspaceLink
		var paymentID, invoiceID, createdAt string
		if err := rows.Scan(&item.ID, &paymentID, &invoiceID, &item.AllocatedMinor, &item.Currency, &createdAt); err != nil {
			return nil, fmt.Errorf("scan active allocation link: %w", err)
		}
		item.TargetFactType = targetType
		item.TargetFactID = invoiceID
		if anchorType == domain.DocumentInvoice {
			item.TargetFactID = paymentID
		}
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse allocation link created_at: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

const allocationTargetPageSize = 50

func (s *Store) SearchAllocationTargets(ctx context.Context, tenantID string, anchorType domain.DocumentType, anchorID string, query ports.AllocationTargetQuery) (ports.AllocationTargetPage, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return ports.AllocationTargetPage{}, err
	}
	defer tx.Rollback()
	workspace, err := loadAllocationState(ctx, tx, tenantID, anchorType, anchorID)
	if err != nil {
		return ports.AllocationTargetPage{}, err
	}
	return queryAllocationTargets(ctx, tx, tenantID, workspace.Anchor, workspace.Links, query, nil)
}

func loadAllocationTargets(ctx context.Context, queryer allocationQueryer, tenantID string, anchor ports.AllocationFactSummary, links []ports.AllocationWorkspaceLink) ([]ports.AllocationTarget, string, error) {
	page, err := queryAllocationTargets(ctx, queryer, tenantID, anchor, links, ports.AllocationTargetQuery{}, nil)
	if err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(links))
	seen := make(map[string]bool, len(page.Items))
	for _, item := range page.Items {
		seen[item.ID] = true
	}
	for _, link := range links {
		if !seen[link.TargetFactID] {
			ids = append(ids, link.TargetFactID)
		}
	}
	if len(ids) > 0 {
		current, err := queryAllocationTargets(ctx, queryer, tenantID, anchor, links, ports.AllocationTargetQuery{AllDates: true}, ids)
		if err != nil {
			return nil, "", err
		}
		page.Items = append(page.Items, current.Items...)
	}
	return page.Items, page.NextCursor, nil
}

// SQL 只读取一页或显式目标，当前关联不会因日期或搜索而从期望计划中消失。
func queryAllocationTargets(ctx context.Context, queryer allocationQueryer, tenantID string, anchor ports.AllocationFactSummary, links []ports.AllocationWorkspaceLink, query ports.AllocationTargetQuery, ids []string) (ports.AllocationTargetPage, error) {
	table, column, amount, date, name, number := "invoices", "invoice_id", "total_minor", "invoice_date", "seller_name", "invoice_number"
	targetType := domain.DocumentInvoice
	if anchor.FactType == domain.DocumentInvoice {
		table, column, amount, date, name, number = "payments", "payment_id", "amount_minor", "business_date", "merchant", "order_number"
		targetType = domain.DocumentPayment
	}
	anchorColumn := "payment_id"
	if anchor.FactType == domain.DocumentInvoice {
		anchorColumn = "invoice_id"
	}
	statement := `SELECT f.id, f.` + amount + `, balances.allocated, f.currency, f.` + date + `::text, f.` + name + `
 FROM ` + table + ` f
 JOIN review_decisions reviewed ON reviewed.tenant_id=f.tenant_id AND reviewed.id=f.current_review_decision_id AND reviewed.action='confirm'
 CROSS JOIN LATERAL (SELECT coalesce(sum(l.allocated_minor),0) allocated FROM payment_invoice_links l
 WHERE l.tenant_id=f.tenant_id AND l.` + column + `=f.id AND l.ended_at IS NULL) balances
 WHERE f.tenant_id=? AND f.deleted_at IS NULL AND f.currency=?
 AND (f.` + amount + `>balances.allocated OR EXISTS(SELECT 1 FROM payment_invoice_links own
 WHERE own.tenant_id=f.tenant_id AND own.` + column + `=f.id AND own.` + anchorColumn + `=? AND own.ended_at IS NULL))`
	args := []any{tenantID, anchor.Currency, anchor.ID}
	limit := allocationTargetPageSize
	if ids != nil {
		if len(ids) == 0 {
			return ports.AllocationTargetPage{Items: []ports.AllocationTarget{}}, nil
		}
		if len(ids) > domain.MaxAllocationTargets {
			return ports.AllocationTargetPage{}, domain.ErrInvalidInput
		}
		statement += " AND f.id = ANY(?::text[])"
		args = append(args, ids)
		limit = len(ids)
	} else {
		if !query.AllDates {
			statement += " AND f." + date + " BETWEEN ?::date-30 AND ?::date+30"
			args = append(args, anchor.BusinessDate, anchor.BusinessDate)
		}
		if query.Query != "" {
			literal := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(query.Query) + "%"
			statement += " AND (f." + name + ` ILIKE ? ESCAPE '\' OR coalesce(f.` + number + `,'') ILIKE ? ESCAPE '\' OR f.id=?`
			args = append(args, literal, literal, query.Query)
			if targetType == domain.DocumentInvoice {
				statement += ` OR f.buyer_name ILIKE ? ESCAPE '\'`
				args = append(args, literal)
			}
			statement += ")"
		}
		if query.AfterID != "" {
			statement += " AND f.id>?"
			args = append(args, query.AfterID)
		}
	}
	statement += " ORDER BY f.id LIMIT ?"
	args = append(args, limit+1)
	rows, err := queryer.QueryContext(ctx, statement, args...)
	if err != nil {
		return ports.AllocationTargetPage{}, fmt.Errorf("query allocation targets: %w", err)
	}
	defer rows.Close()
	result := ports.AllocationTargetPage{Items: []ports.AllocationTarget{}}
	current := make(map[string]ports.AllocationWorkspaceLink, len(links))
	for _, link := range links {
		current[link.TargetFactID] = link
	}
	for rows.Next() {
		target := ports.AllocationTarget{FactType: targetType}
		if err := rows.Scan(&target.ID, &target.AmountMinor, &target.AllocatedMinor, &target.Currency, &target.BusinessDate, &target.DisplayName); err != nil {
			return result, err
		}
		if _, err := includeAllocationTarget(anchor, &target, current[target.ID]); err != nil {
			return result, err
		}
		result.Items = append(result.Items, target)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.NextCursor = result.Items[limit-1].ID
	}
	return result, nil
}

func includeAllocationTarget(
	anchor ports.AllocationFactSummary,
	target *ports.AllocationTarget,
	current ports.AllocationWorkspaceLink,
) (bool, error) {
	distance, err := allocationDateDistance(anchor.BusinessDate, target.BusinessDate)
	if err != nil {
		return false, fmt.Errorf("compare allocation business dates: %w", err)
	}
	target.DateDistanceDays = distance
	target.RemainingMinor = target.AmountMinor - target.AllocatedMinor
	target.CurrentLinkID = current.ID
	target.CurrentAllocatedMinor = current.AllocatedMinor
	target.MaximumAllocatableMinor = target.RemainingMinor + current.AllocatedMinor
	target.NameExact = domain.NormalizeExact(anchor.DisplayName) == domain.NormalizeExact(target.DisplayName)
	return target.RemainingMinor > 0 || current.ID != "", nil
}

func allocationDateDistance(left, right string) (int, error) {
	leftDate, err := time.Parse("2006-01-02", left)
	if err != nil {
		return 0, err
	}
	rightDate, err := time.Parse("2006-01-02", right)
	if err != nil {
		return 0, err
	}
	days := int((leftDate.Unix() - rightDate.Unix()) / 86400)
	if days < 0 {
		days = -days
	}
	return days, nil
}

func activeLinkFromWorkspace(
	anchorType domain.DocumentType,
	anchorID string,
	link ports.AllocationWorkspaceLink,
) domain.ActiveAllocationLink {
	result := domain.ActiveAllocationLink{
		ID:             link.ID,
		AllocatedMinor: link.AllocatedMinor,
		Currency:       link.Currency,
	}
	if anchorType == domain.DocumentPayment {
		result.PaymentID = anchorID
		result.InvoiceID = link.TargetFactID
	} else {
		result.PaymentID = link.TargetFactID
		result.InvoiceID = anchorID
	}
	return result
}

func (t transaction) ApplyAllocationAdjustment(
	ctx context.Context,
	command ports.AllocationAdjustmentCommand,
) (ports.AllocationAdjustmentResult, error) {
	replay, found, err := loadAllocationAdjustmentReplay(ctx, t.tx, command.TenantID, command.IdempotencyKey)
	if err != nil {
		return ports.AllocationAdjustmentResult{}, err
	}
	if found {
		if replay.RequestHash != command.RequestHash {
			return ports.AllocationAdjustmentResult{}, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的分配调整", domain.ErrConflict)
		}
		return replay.Result, nil
	}
	desired, draftsByTarget, err := validateAllocationAdjustmentCommand(command)
	if err != nil {
		return ports.AllocationAdjustmentResult{}, err
	}
	if err := t.requireTripManager(ctx, command.TenantID, command.ActorUserID); err != nil {
		return ports.AllocationAdjustmentResult{}, err
	}
	workspace, err := loadAllocationState(ctx, t.tx, command.TenantID, command.AnchorFactType, command.AnchorFactID)
	if err != nil {
		return ports.AllocationAdjustmentResult{}, err
	}
	if workspace.PlanHash != command.ExpectedPlanHash {
		return ports.AllocationAdjustmentResult{}, domain.NewRuleError("allocation_plan_stale", "分配计划已变化，请刷新后重试", domain.ErrConflict)
	}
	ids := make([]string, 0, len(desired))
	for _, item := range desired {
		ids = append(ids, item.TargetFactID)
	}
	selected, err := queryAllocationTargets(ctx, t.tx, command.TenantID, workspace.Anchor, workspace.Links, ports.AllocationTargetQuery{AllDates: true}, ids)
	if err != nil {
		return ports.AllocationAdjustmentResult{}, err
	}
	workspace.Targets = selected.Items
	targetBalances := make([]domain.AllocationTargetBalance, 0, len(workspace.Targets))
	for _, target := range workspace.Targets {
		targetBalances = append(targetBalances, domain.AllocationTargetBalance{
			ID:                      target.ID,
			Currency:                target.Currency,
			MaximumAllocatableMinor: target.MaximumAllocatableMinor,
			Available:               true,
		})
	}
	if err := domain.ValidateDesiredAllocationPlan(
		workspace.Anchor.AmountMinor,
		workspace.Anchor.Currency,
		targetBalances,
		desired,
	); err != nil {
		return ports.AllocationAdjustmentResult{}, err
	}
	current := make([]domain.ActiveAllocationLink, 0, len(workspace.Links))
	for _, link := range workspace.Links {
		current = append(current, activeLinkFromWorkspace(command.AnchorFactType, command.AnchorFactID, link))
	}
	diff, err := domain.BuildAllocationAdjustmentDiff(command.AnchorFactType, command.AnchorFactID, current, desired)
	if err != nil {
		return ports.AllocationAdjustmentResult{}, err
	}
	resulting := append([]domain.ActiveAllocationLink(nil), diff.Unchanged...)
	createdLinks := make([]domain.ActiveAllocationLink, 0, len(diff.Create))
	for _, item := range diff.Create {
		draft := draftsByTarget[item.TargetFactID]
		link := domain.ActiveAllocationLink{
			ID:             draft.LinkID,
			AllocatedMinor: item.AllocatedMinor,
			Currency:       workspace.Anchor.Currency,
		}
		if command.AnchorFactType == domain.DocumentPayment {
			link.PaymentID = command.AnchorFactID
			link.InvoiceID = item.TargetFactID
		} else {
			link.PaymentID = item.TargetFactID
			link.InvoiceID = command.AnchorFactID
		}
		createdLinks = append(createdLinks, link)
		resulting = append(resulting, link)
	}
	_, resultingHash, err := domain.CanonicalActiveAllocationPlan(command.AnchorFactType, command.AnchorFactID, resulting)
	if err != nil {
		return ports.AllocationAdjustmentResult{}, err
	}
	result := ports.AllocationAdjustmentResult{
		AdjustmentID:   command.AdjustmentID,
		Mode:           diff.Mode,
		EndedLinkIDs:   make([]string, 0, len(diff.End)),
		CreatedLinkIDs: make([]string, 0, len(createdLinks)),
		PlanHash:       resultingHash,
		Replayed:       false,
	}
	for _, link := range diff.End {
		result.EndedLinkIDs = append(result.EndedLinkIDs, link.ID)
	}
	for _, link := range createdLinks {
		result.CreatedLinkIDs = append(result.CreatedLinkIDs, link.ID)
	}
	sort.Strings(result.CreatedLinkIDs)
	if err := t.persistAllocationAdjustment(ctx, command, diff.Mode, resultingHash, diff.End, createdLinks); err != nil {
		return ports.AllocationAdjustmentResult{}, err
	}
	return result, nil
}

func validateAllocationAdjustmentCommand(
	command ports.AllocationAdjustmentCommand,
) ([]domain.DesiredAllocation, map[string]ports.AllocationLinkDraft, error) {
	if command.TenantID == "" || command.ActorUserID == "" || command.AdjustmentID == "" ||
		command.AuditEventID == "" || command.RequestID == "" || command.CreatedAt.IsZero() {
		return nil, nil, domain.ErrInvalidInput
	}
	if err := domain.ValidateIdempotencyKey(command.IdempotencyKey); err != nil {
		return nil, nil, err
	}
	desired := make([]domain.DesiredAllocation, 0, len(command.Desired))
	drafts := make(map[string]ports.AllocationLinkDraft, len(command.Desired))
	seenLinkIDs := make(map[string]struct{}, len(command.Desired))
	for _, draft := range command.Desired {
		if draft.LinkID == "" {
			return nil, nil, domain.ErrInvalidInput
		}
		if _, duplicate := seenLinkIDs[draft.LinkID]; duplicate {
			return nil, nil, domain.ErrInvalidInput
		}
		seenLinkIDs[draft.LinkID] = struct{}{}
		desired = append(desired, domain.DesiredAllocation{
			TargetFactID:   draft.TargetFactID,
			AllocatedMinor: draft.AllocatedMinor,
		})
		drafts[draft.TargetFactID] = draft
	}
	canonical, reason, requestHash, err := domain.CanonicalAllocationAdjustmentRequest(
		command.AnchorFactType,
		command.AnchorFactID,
		command.ExpectedPlanHash,
		desired,
		command.Reason,
	)
	if err != nil {
		return nil, nil, err
	}
	if reason != command.Reason || requestHash != command.RequestHash || len(canonical) != len(command.Desired) {
		return nil, nil, domain.ErrInvalidInput
	}
	for index, item := range canonical {
		if command.Desired[index].TargetFactID != item.TargetFactID ||
			command.Desired[index].AllocatedMinor != item.AllocatedMinor {
			return nil, nil, domain.ErrInvalidInput
		}
	}
	return canonical, drafts, nil
}

func (t transaction) persistAllocationAdjustment(
	ctx context.Context,
	command ports.AllocationAdjustmentCommand,
	mode, resultingHash string,
	ended, created []domain.ActiveAllocationLink,
) error {
	createdAt := command.CreatedAt.UTC().Format(time.RFC3339Nano)
	metadata, err := json.Marshal(map[string]any{
		"mode":               mode,
		"created_link_count": len(created),
		"ended_link_count":   len(ended),
	})
	if err != nil {
		return err
	}
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, tenant_id, actor_user_id, action, resource_type,
			resource_id, request_id, safe_metadata_json, created_at
		) VALUES (?, ?, ?, 'payment_invoice_allocation_adjusted',
		          'payment_invoice_allocation_adjustment', ?, ?, ?::jsonb, ?)
	`, command.AuditEventID, command.TenantID, command.ActorUserID,
		command.AdjustmentID, command.RequestID, string(metadata), createdAt); err != nil {
		return fmt.Errorf("insert allocation adjustment audit event: %w", err)
	}
	var anchorPaymentID, anchorInvoiceID any
	if command.AnchorFactType == domain.DocumentPayment {
		anchorPaymentID = command.AnchorFactID
	} else {
		anchorInvoiceID = command.AnchorFactID
	}
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO payment_invoice_allocation_adjustments (
			id, tenant_id, actor_user_id, anchor_fact_type,
			anchor_payment_id, anchor_invoice_id, mode, idempotency_key,
			request_hash, expected_plan_hash, resulting_plan_hash, reason,
			audit_event_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, command.AdjustmentID, command.TenantID, command.ActorUserID, command.AnchorFactType,
		anchorPaymentID, anchorInvoiceID, mode, command.IdempotencyKey,
		command.RequestHash, command.ExpectedPlanHash, resultingHash, command.Reason,
		command.AuditEventID, createdAt); err != nil {
		return allocationAdjustmentWriteError("insert allocation adjustment", err)
	}
	for _, link := range ended {
		result, err := t.tx.ExecContext(ctx, `
			UPDATE payment_invoice_links
			SET ended_at = ?, ended_by_adjustment_id = ?
			WHERE tenant_id = ? AND id = ? AND ended_at IS NULL
		`, createdAt, command.AdjustmentID, command.TenantID, link.ID)
		if err != nil {
			return allocationAdjustmentWriteError("end allocation link", err)
		}
		if err := requireAffected(result); err != nil {
			return domain.NewRuleError("allocation_plan_stale", "分配计划已变化，请刷新后重试", domain.ErrConflict)
		}
	}
	for _, link := range created {
		if _, err := t.tx.ExecContext(ctx, `
			INSERT INTO payment_invoice_links (
				id, tenant_id, payment_id, invoice_id, created_by_adjustment_id,
				allocated_minor, currency, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, link.ID, command.TenantID, link.PaymentID, link.InvoiceID, command.AdjustmentID,
			link.AllocatedMinor, link.Currency, createdAt); err != nil {
			return allocationAdjustmentWriteError("insert adjusted allocation link", err)
		}
	}
	return nil
}

func allocationAdjustmentWriteError(operation string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" {
		return domain.NewRuleError("allocation_pair_conflict", "同一支付与发票已存在活动分配", domain.ErrConflict)
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "allocation_active_target_limit_exceeded"):
		return domain.NewRuleError("allocation_active_target_limit_exceeded", "单据最多保留 200 条活动分配，请先调整已有分配", domain.ErrConflict)
	case strings.Contains(message, "payment_allocation_exceeded"),
		strings.Contains(message, "invoice_allocation_exceeded"):
		return domain.NewRuleError("allocation_balance_conflict", "分配余额已被其他操作占用，请刷新后重试", domain.ErrConflict)
	case strings.Contains(message, "allocation_fact_unavailable"),
		strings.Contains(message, "allocation_anchor_unavailable"):
		return domain.NewRuleError("allocation_target_unavailable", "分配 Fact 已删除或状态已变化", domain.ErrConflict)
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}

func loadAllocationAdjustmentReplay(
	ctx context.Context,
	queryer allocationQueryer,
	tenantID, idempotencyKey string,
) (ports.AllocationAdjustmentReplay, bool, error) {
	var replay ports.AllocationAdjustmentReplay
	var endedJSON, createdJSON string
	err := queryer.QueryRowContext(ctx, `
		SELECT a.request_hash, a.id, a.mode, a.resulting_plan_hash,
		       coalesce((SELECT json_agg(link_id ORDER BY link_id) FROM (
		           SELECT l.id AS link_id FROM payment_invoice_links l
		           WHERE l.tenant_id = a.tenant_id AND l.ended_by_adjustment_id = a.id
		           ORDER BY l.id
		       ) links), '[]'::json)::text,
		       coalesce((SELECT json_agg(link_id ORDER BY link_id) FROM (
		           SELECT l.id AS link_id FROM payment_invoice_links l
		           WHERE l.tenant_id = a.tenant_id AND l.created_by_adjustment_id = a.id
		           ORDER BY l.id
		       ) links), '[]'::json)::text
		FROM payment_invoice_allocation_adjustments a
		WHERE a.tenant_id = ? AND a.idempotency_key = ?
	`, tenantID, idempotencyKey).Scan(
		&replay.RequestHash,
		&replay.Result.AdjustmentID,
		&replay.Result.Mode,
		&replay.Result.PlanHash,
		&endedJSON,
		&createdJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.AllocationAdjustmentReplay{}, false, nil
	}
	if err != nil {
		return ports.AllocationAdjustmentReplay{}, false, fmt.Errorf("load allocation adjustment replay: %w", err)
	}
	if err := json.Unmarshal([]byte(endedJSON), &replay.Result.EndedLinkIDs); err != nil {
		return ports.AllocationAdjustmentReplay{}, false, fmt.Errorf("decode ended allocation links: %w", err)
	}
	if err := json.Unmarshal([]byte(createdJSON), &replay.Result.CreatedLinkIDs); err != nil {
		return ports.AllocationAdjustmentReplay{}, false, fmt.Errorf("decode created allocation links: %w", err)
	}
	replay.Result.Replayed = true
	return replay, true, nil
}
