package postgresqladapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

// 每批只保留 ID；日期调整只重算新旧时间范围，不加载租户全部费用。
func (t transaction) reconcileTripPayments(ctx context.Context, tenantID, actorID, requestID string, now time.Time, paymentID string, ranges []domain.TripDetails) error {
	if paymentID != "" {
		return t.reconcileOnePayment(ctx, tenantID, actorID, requestID, paymentID, now)
	}
	filters := make([]string, 0, len(ranges))
	values := make([]any, 0, len(ranges)*4)
	for _, interval := range ranges {
		if interval.Timezone == "" {
			continue
		}
		filters = append(filters, `(transaction_time >= (?::date::timestamp AT TIME ZONE ?) AND transaction_time < ((?::date + 1)::timestamp AT TIME ZONE ?))`)
		values = append(values, interval.StartDate, interval.Timezone, interval.EndDate, interval.Timezone)
	}
	if len(filters) == 0 {
		return nil
	}
	afterID := ""
	for {
		args := append([]any{tenantID, afterID}, values...)
		rows, err := t.tx.QueryContext(ctx, `SELECT id FROM payments WHERE tenant_id = ? AND id > ?
			AND deleted_at IS NULL AND trip_assignment_mode = 'auto' AND (`+strings.Join(filters, " OR ")+`) ORDER BY id LIMIT 100`, args...)
		if err != nil {
			return fmt.Errorf("list automatic trip payments: %w", err)
		}
		ids := make([]string, 0, 100)
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return fmt.Errorf("scan automatic trip payment: %w", err)
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return fmt.Errorf("iterate automatic trip payments: %w", err)
		}
		for _, id := range ids {
			if err := t.reconcileOnePayment(ctx, tenantID, actorID, requestID, id, now); err != nil {
				return err
			}
		}
		if len(ids) < 100 {
			return nil
		}
		afterID = ids[len(ids)-1]
	}
}

// linkedTrips 取一张单据所有活动关联对方当前所在的行程（去重）。
func (t transaction) linkedTrips(ctx context.Context, tenantID string, factType domain.DocumentType, factID string) ([]string, error) {
	rows, err := t.tx.QueryContext(ctx, `
		SELECT DISTINCT a.trip_id
		FROM payment_invoice_links l
		JOIN trip_fact_assignments a ON a.tenant_id = l.tenant_id AND a.ended_at IS NULL
		  AND ((? = 'payment' AND a.invoice_id = l.invoice_id) OR (? = 'invoice' AND a.payment_id = l.payment_id))
		JOIN trips trip ON trip.tenant_id = a.tenant_id AND trip.id = a.trip_id AND trip.deleted_at IS NULL
		WHERE l.tenant_id = ? AND l.ended_at IS NULL
		  AND ((? = 'payment' AND l.payment_id = ?) OR (? = 'invoice' AND l.invoice_id = ?))
		ORDER BY a.trip_id`, factType, factType, tenantID, factType, factID, factType, factID)
	if err != nil {
		return nil, fmt.Errorf("list linked trips: %w", err)
	}
	defer rows.Close()
	var trips []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		trips = append(trips, id)
	}
	return trips, rows.Err()
}

// linkedCounterparts 列出一张单据所有活动关联的对方单据。
func (t transaction) linkedCounterparts(ctx context.Context, tenantID string, factType domain.DocumentType, factID string) ([]string, error) {
	rows, err := t.tx.QueryContext(ctx, `
		SELECT CASE WHEN ? = 'payment' THEN invoice_id ELSE payment_id END
		FROM payment_invoice_links
		WHERE tenant_id = ? AND ended_at IS NULL
		  AND ((? = 'payment' AND payment_id = ?) OR (? = 'invoice' AND invoice_id = ?))
		ORDER BY 1`, factType, tenantID, factType, factID, factType, factID)
	if err != nil {
		return nil, fmt.Errorf("list linked counterparts: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// reconcileOnePayment 重算一笔自动模式支付的归属：时间规则的唯一命中，并上已确认
// 关联发票所在的行程；恰好一个不同行程才归属，多个即冲突留人工。
func (t transaction) reconcileOnePayment(ctx context.Context, tenantID, actorID, requestID, paymentID string, now time.Time) error {
	version, mode, err := t.lockTripAssignmentFact(ctx, tenantID, domain.DocumentPayment, paymentID)
	if err != nil {
		return err
	}
	if mode != "auto" {
		return nil
	}
	var instant string
	if err := t.tx.QueryRowContext(ctx, `SELECT transaction_time FROM payments WHERE tenant_id = ? AND id = ?`, tenantID, paymentID).Scan(&instant); err != nil {
		return fmt.Errorf("read payment trip instant: %w", err)
	}
	match, err := findAutomaticTripMatch(ctx, t.tx, tenantID, instant)
	if err != nil {
		return err
	}
	linked, err := t.linkedTrips(ctx, tenantID, domain.DocumentPayment, paymentID)
	if err != nil {
		return err
	}
	desired, rule := resolveTripCandidates(match.TripID, linked)
	return t.applyReconciledTrip(ctx, tenantID, actorID, requestID, domain.DocumentPayment, paymentID, desired, rule, version, now)
}

// reconcileOneInvoice 重算一张自动模式发票的归属：只看已确认关联支付所在的行程。
// 发票没有交易时刻，不按日期匹配。
func (t transaction) reconcileOneInvoice(ctx context.Context, tenantID, actorID, requestID, invoiceID string, now time.Time) error {
	version, mode, err := t.lockTripAssignmentFact(ctx, tenantID, domain.DocumentInvoice, invoiceID)
	if err != nil {
		return err
	}
	if mode != "auto" {
		return nil
	}
	linked, err := t.linkedTrips(ctx, tenantID, domain.DocumentInvoice, invoiceID)
	if err != nil {
		return err
	}
	desired, rule := resolveTripCandidates("", linked)
	return t.applyReconciledTrip(ctx, tenantID, actorID, requestID, domain.DocumentInvoice, invoiceID, desired, rule, version, now)
}

// resolveTripCandidates 把时间命中与关联行程合成一个结论：恰好一个不同行程才归属。
// 返回的规则版本说明这次结论由谁给出：关联参与了就记关联规则。
func resolveTripCandidates(timeTrip string, linked []string) (string, string) {
	distinct := map[string]struct{}{}
	if timeTrip != "" {
		distinct[timeTrip] = struct{}{}
	}
	for _, id := range linked {
		distinct[id] = struct{}{}
	}
	if len(distinct) != 1 {
		return "", domain.TripTimeAttributionVersion
	}
	for id := range distinct {
		if len(linked) > 0 {
			return id, domain.TripLinkAttributionVersion
		}
		return id, domain.TripTimeAttributionVersion
	}
	return "", domain.TripTimeAttributionVersion
}

func (t transaction) applyReconciledTrip(ctx context.Context, tenantID, actorID, requestID string, factType domain.DocumentType, factID, desired, rule string, version int, now time.Time) error {
	current, currentTrip, err := t.currentTripAssignment(ctx, tenantID, factType, factID)
	if err != nil {
		return err
	}
	if currentTrip == desired {
		return nil
	}
	reason := "按行程时区与完整日期范围确定性重算"
	if rule == domain.TripLinkAttributionVersion {
		reason = "跟随已确认关联单据的行程"
	} else if factType == domain.DocumentInvoice {
		reason = "关联支付不再指向唯一行程"
	}
	_, err = t.applyGeneratedTripAssignment(ctx, tenantID, actorID, requestID, factType, factID, current, desired,
		"automatic", rule, reason, version, now)
	return err
}

// reconcileLinkedCounterparts 在一张单据归属变化后重算它所有关联对方。递归会收敛：
// 规则是确定性的，对方算出同一结论就不再写入。
func (t transaction) reconcileLinkedCounterparts(ctx context.Context, tenantID, actorID, requestID string, factType domain.DocumentType, factID string, now time.Time) error {
	ids, err := t.linkedCounterparts(ctx, tenantID, factType, factID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if factType == domain.DocumentPayment {
			err = t.reconcileOneInvoice(ctx, tenantID, actorID, requestID, id, now)
		} else {
			err = t.reconcileOnePayment(ctx, tenantID, actorID, requestID, id, now)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// reconcileFactAndCounterparts 在关联关系本身变化（确认、调整）后调用：先算这张，再算对方。
func (t transaction) reconcileFactAndCounterparts(ctx context.Context, tenantID, actorID, requestID string, factType domain.DocumentType, factID string, now time.Time) error {
	if factType == domain.DocumentPayment {
		if err := t.reconcileOnePayment(ctx, tenantID, actorID, requestID, factID, now); err != nil {
			return err
		}
	} else if err := t.reconcileOneInvoice(ctx, tenantID, actorID, requestID, factID, now); err != nil {
		return err
	}
	return t.reconcileLinkedCounterparts(ctx, tenantID, actorID, requestID, factType, factID, now)
}

func (t transaction) applyGeneratedTripAssignment(ctx context.Context, tenantID, actorID, requestID string, factType domain.DocumentType, factID, current, desired, source, rule, reason string, version int, now time.Time) (ports.TripAssignmentResult, error) {
	var decisionID, linkID, auditID string
	if err := t.tx.QueryRowContext(ctx, `SELECT gen_random_uuid()::text, gen_random_uuid()::text, gen_random_uuid()::text`).Scan(&decisionID, &linkID, &auditID); err != nil {
		return ports.TripAssignmentResult{}, fmt.Errorf("create automatic assignment identifiers: %w", err)
	}
	digest := sha256.Sum256([]byte(source + ":" + decisionID))
	return t.ApplyTripAssignment(ctx, ports.TripAssignmentCommand{
		TenantID: tenantID, ActorUserID: actorID, FactType: factType, FactID: factID,
		ExpectedAssignmentID: current, DesiredTripID: desired, ExpectedFactVersion: version,
		DecisionSource: source, RuleVersion: rule, Reason: reason, DecisionID: decisionID, AssignmentID: linkID,
		AuditEventID: auditID, IdempotencyKey: "trip-rule:" + decisionID, RequestHash: hex.EncodeToString(digest[:]),
		RequestID: requestID, CreatedAt: now,
	})
}

func (t transaction) ChangeTripPreference(ctx context.Context, command ports.TripPreferenceCommand) error {
	if err := t.requireTripManager(ctx, command.TenantID, command.ActorUserID); err != nil {
		return err
	}
	if !domain.ValidTripAssignmentFactType(command.FactType) {
		return domain.ErrInvalidInput
	}
	version, _, err := t.lockTripAssignmentFact(ctx, command.TenantID, command.FactType, command.FactID)
	if err != nil {
		return err
	}
	if version != command.ExpectedVersion {
		return tripStale()
	}
	current, _, err := t.currentTripAssignment(ctx, command.TenantID, command.FactType, command.FactID)
	if err != nil {
		return err
	}
	// 恢复自动是明确的人工选择，先结束旧关联，避免后续重算篡改其来源。
	if current != "" {
		if _, err := t.applyGeneratedTripAssignment(ctx, command.TenantID, command.ActorUserID, command.RequestID,
			command.FactType, command.FactID, current, "", "manual", "", "人工变更自动归属偏好", version, command.CreatedAt); err != nil {
			return err
		}
		version++
	}
	if err := t.tripAudit(ctx, command.TenantID, command.ActorUserID, command.AuditEventID,
		"trip_preference_changed", string(command.FactType), command.FactID, command.RequestID,
		map[string]string{"mode": command.Mode}, command.CreatedAt); err != nil {
		return err
	}
	table := "payments"
	if command.FactType == domain.DocumentInvoice {
		table = "invoices"
	}
	if _, err := t.tx.ExecContext(ctx, `UPDATE `+table+` SET trip_assignment_mode = ?, version = version + 1 WHERE tenant_id = ? AND id = ?`,
		command.Mode, command.TenantID, command.FactID); err != nil {
		return fmt.Errorf("change trip preference: %w", err)
	}
	if command.Mode == "auto" {
		if command.FactType == domain.DocumentPayment {
			return t.reconcileOnePayment(ctx, command.TenantID, command.ActorUserID, command.RequestID, command.FactID, command.CreatedAt)
		}
		return t.reconcileOneInvoice(ctx, command.TenantID, command.ActorUserID, command.RequestID, command.FactID, command.CreatedAt)
	}
	// 「保持无归属」也影响对方：对方若曾因这张而归属，重算后按剩余信号决定。
	return t.reconcileLinkedCounterparts(ctx, command.TenantID, command.ActorUserID, command.RequestID, command.FactType, command.FactID, command.CreatedAt)
}

// ListTripLinkDrift 粗筛归属与「已确认关联」规则不一致的单据：自动模式、且某个活动
// 关联对方当前所在行程与自己不同（未归属记作空）。真正是否要改由规则重算决定。
//
// 正常情况下这个查询返回零行——事件驱动的重算已经维持了一致。它存在是为了修复
// 规则上线前写下的归属，以及任何漏掉的事件。
func (s *Store) ListTripLinkDrift(ctx context.Context, limit int) ([]ports.TripLinkDrift, error) {
	if limit < 1 {
		return nil, domain.ErrInvalidInput
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT fact.tenant_id, fact.fact_type, fact.fact_id, owner.user_id
		FROM (
		    SELECT p.tenant_id, 'payment' AS fact_type, p.id AS fact_id,
		           coalesce((SELECT a.trip_id FROM trip_fact_assignments a
		                     WHERE a.tenant_id = p.tenant_id AND a.payment_id = p.id AND a.ended_at IS NULL), '') AS trip_id
		    FROM payments p WHERE p.deleted_at IS NULL AND p.trip_assignment_mode = 'auto'
		    UNION ALL
		    SELECT i.tenant_id, 'invoice', i.id,
		           coalesce((SELECT a.trip_id FROM trip_fact_assignments a
		                     WHERE a.tenant_id = i.tenant_id AND a.invoice_id = i.id AND a.ended_at IS NULL), '')
		    FROM invoices i WHERE i.deleted_at IS NULL AND i.trip_assignment_mode = 'auto'
		) fact
		JOIN LATERAL (
		    SELECT m.user_id FROM memberships m
		    WHERE m.tenant_id = fact.tenant_id AND m.status = 'active' AND m.role = 'owner'
		    ORDER BY m.created_at, m.user_id LIMIT 1
		) owner ON TRUE
		WHERE EXISTS (
		    SELECT 1 FROM payment_invoice_links link
		    JOIN trip_fact_assignments counterpart
		      ON counterpart.tenant_id = link.tenant_id AND counterpart.ended_at IS NULL
		     AND ((fact.fact_type = 'payment' AND counterpart.invoice_id = link.invoice_id)
		       OR (fact.fact_type = 'invoice' AND counterpart.payment_id = link.payment_id))
		    JOIN trips trip ON trip.tenant_id = counterpart.tenant_id AND trip.id = counterpart.trip_id AND trip.deleted_at IS NULL
		    WHERE link.tenant_id = fact.tenant_id AND link.ended_at IS NULL
		      AND ((fact.fact_type = 'payment' AND link.payment_id = fact.fact_id)
		        OR (fact.fact_type = 'invoice' AND link.invoice_id = fact.fact_id))
		      AND counterpart.trip_id <> fact.trip_id
		)
		ORDER BY fact.tenant_id, fact.fact_type, fact.fact_id
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list trip link drift: %w", err)
	}
	defer rows.Close()
	items := make([]ports.TripLinkDrift, 0)
	for rows.Next() {
		var item ports.TripLinkDrift
		if err := rows.Scan(&item.TenantID, &item.FactType, &item.FactID, &item.ActorUserID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ReconcileTripLinks 用与运行期完全相同的规则重算一张单据；规则算出的结论与当前
// 一致时不写入，因此重复执行是安全的。
func (t transaction) ReconcileTripLinks(ctx context.Context, drift ports.TripLinkDrift, requestID string, now time.Time) error {
	if drift.FactType == domain.DocumentPayment {
		return t.reconcileOnePayment(ctx, drift.TenantID, drift.ActorUserID, requestID, drift.FactID, now)
	}
	if drift.FactType == domain.DocumentInvoice {
		return t.reconcileOneInvoice(ctx, drift.TenantID, drift.ActorUserID, requestID, drift.FactID, now)
	}
	return domain.ErrInvalidInput
}
