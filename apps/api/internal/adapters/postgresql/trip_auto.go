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
	desired := match.TripID
	current, currentTrip, err := t.currentTripAssignment(ctx, tenantID, domain.DocumentPayment, paymentID)
	if err != nil {
		return err
	}
	if currentTrip == desired {
		return nil
	}
	_, err = t.applyGeneratedTripAssignment(ctx, tenantID, actorID, requestID, paymentID, current, desired,
		"automatic", "按行程时区与完整日期范围确定性重算", version, now)
	return err
}

func (t transaction) applyGeneratedTripAssignment(ctx context.Context, tenantID, actorID, requestID, paymentID, current, desired, source, reason string, version int, now time.Time) (ports.TripAssignmentResult, error) {
	var decisionID, linkID, auditID string
	if err := t.tx.QueryRowContext(ctx, `SELECT gen_random_uuid()::text, gen_random_uuid()::text, gen_random_uuid()::text`).Scan(&decisionID, &linkID, &auditID); err != nil {
		return ports.TripAssignmentResult{}, fmt.Errorf("create automatic assignment identifiers: %w", err)
	}
	digest := sha256.Sum256([]byte(source + ":" + decisionID))
	return t.ApplyTripAssignment(ctx, ports.TripAssignmentCommand{
		TenantID: tenantID, ActorUserID: actorID, FactType: domain.DocumentPayment, FactID: paymentID,
		ExpectedAssignmentID: current, DesiredTripID: desired, ExpectedFactVersion: version,
		DecisionSource: source, Reason: reason, DecisionID: decisionID, AssignmentID: linkID,
		AuditEventID: auditID, IdempotencyKey: "trip-rule:" + decisionID, RequestHash: hex.EncodeToString(digest[:]),
		RequestID: requestID, CreatedAt: now,
	})
}

func (t transaction) ChangeTripPreference(ctx context.Context, command ports.TripPreferenceCommand) error {
	if err := t.requireTripManager(ctx, command.TenantID, command.ActorUserID); err != nil {
		return err
	}
	version, _, err := t.lockTripAssignmentFact(ctx, command.TenantID, domain.DocumentPayment, command.PaymentID)
	if err != nil {
		return err
	}
	if version != command.ExpectedVersion {
		return tripStale()
	}
	current, _, err := t.currentTripAssignment(ctx, command.TenantID, domain.DocumentPayment, command.PaymentID)
	if err != nil {
		return err
	}
	// 恢复自动是明确的人工选择，先结束旧关联，避免后续重算篡改其来源。
	if current != "" {
		if _, err := t.applyGeneratedTripAssignment(ctx, command.TenantID, command.ActorUserID, command.RequestID,
			command.PaymentID, current, "", "manual", "人工变更自动归属偏好", version, command.CreatedAt); err != nil {
			return err
		}
	}
	if err := t.tripAudit(ctx, command.TenantID, command.ActorUserID, command.AuditEventID,
		"trip_preference_changed", "payment", command.PaymentID, command.RequestID,
		map[string]string{"mode": command.Mode}, command.CreatedAt); err != nil {
		return err
	}
	if _, err := t.tx.ExecContext(ctx, `UPDATE payments SET trip_assignment_mode = ?, version = version + 1 WHERE tenant_id = ? AND id = ?`,
		command.Mode, command.TenantID, command.PaymentID); err != nil {
		return fmt.Errorf("change trip preference: %w", err)
	}
	if command.Mode == "auto" {
		return t.reconcileOnePayment(ctx, command.TenantID, command.ActorUserID, command.RequestID, command.PaymentID, command.CreatedAt)
	}
	return nil
}
