package postgresqladapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (t transaction) SetFactBadDebt(ctx context.Context, command ports.BadDebtCommand) (ports.BadDebtResult, error) {
	if err := t.requireTripManager(ctx, command.TenantID, command.ActorUserID); err != nil {
		return ports.BadDebtResult{}, err
	}
	canonical, hash, err := domain.CanonicalBadDebtRequest(command.FactType, command.FactID, command.Input)
	if err != nil {
		return ports.BadDebtResult{}, err
	}
	if canonical != command.Input || hash != command.RequestHash || command.DecisionID == "" || command.AuditEventID == "" || command.RequestID == "" || command.CreatedAt.IsZero() {
		return ports.BadDebtResult{}, domain.ErrInvalidInput
	}
	if err := domain.ValidateIdempotencyKey(command.IdempotencyKey); err != nil {
		return ports.BadDebtResult{}, err
	}
	var result ports.BadDebtResult
	var previousHash string
	err = t.tx.QueryRowContext(ctx, `SELECT id,resulting_version,marked,request_hash FROM fact_bad_debt_decisions WHERE tenant_id=? AND idempotency_key=?`, command.TenantID, command.IdempotencyKey).Scan(&result.DecisionID, &result.Version, &result.Marked, &previousHash)
	if err == nil {
		if previousHash != command.RequestHash {
			return result, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于其他坏账操作", domain.ErrConflict)
		}
		result.Replayed = true
		return result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, fmt.Errorf("read bad debt replay: %w", err)
	}
	table, column := "payments", "payment_id"
	if command.FactType == domain.DocumentInvoice {
		table, column = "invoices", "invoice_id"
	}
	var version int
	var marked bool
	err = t.tx.QueryRowContext(ctx, `SELECT version,sbm_fact_bad_debt(tenant_id,?,id) FROM `+table+` WHERE tenant_id=? AND id=? AND deleted_at IS NULL FOR UPDATE`, command.FactType, command.TenantID, command.FactID).Scan(&version, &marked)
	if errors.Is(err, sql.ErrNoRows) {
		return result, domain.ErrNotFound
	}
	if err != nil {
		return result, fmt.Errorf("lock bad debt fact: %w", err)
	}
	if version != command.Input.ExpectedVersion {
		return result, domain.ErrVersionConflict
	}
	if marked == command.Input.Marked {
		return result, domain.NewRuleError("bad_debt_unchanged", "坏账状态未变化", domain.ErrConflict)
	}
	if err := t.tripAudit(ctx, command.TenantID, command.ActorUserID, command.AuditEventID, "fact_bad_debt_changed", string(command.FactType), command.FactID, command.RequestID, map[string]any{"marked": command.Input.Marked, "version": version + 1}, command.CreatedAt); err != nil {
		return result, err
	}
	_, err = t.tx.ExecContext(ctx, `INSERT INTO fact_bad_debt_decisions (id,tenant_id,`+column+`,actor_user_id,marked,expected_version,resulting_version,reason,idempotency_key,request_hash,audit_event_id,created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, command.DecisionID, command.TenantID, command.FactID, command.ActorUserID, command.Input.Marked, version, version+1, command.Input.Reason, command.IdempotencyKey, command.RequestHash, command.AuditEventID, command.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return result, fmt.Errorf("insert bad debt decision: %w", err)
	}
	updated, err := t.tx.ExecContext(ctx, `UPDATE `+table+` SET version=version+1 WHERE tenant_id=? AND id=? AND version=?`, command.TenantID, command.FactID, version)
	if err != nil {
		return result, fmt.Errorf("advance bad debt version: %w", err)
	}
	if err := requireAffected(updated); err != nil {
		return result, domain.ErrVersionConflict
	}
	return ports.BadDebtResult{DecisionID: command.DecisionID, Version: version + 1, Marked: command.Input.Marked}, nil
}

func (t transaction) requireTripNotBadDebtLocked(ctx context.Context, tenantID, tripID string) error {
	var locked bool
	if err := t.tx.QueryRowContext(ctx, `SELECT sbm_trip_bad_debt_locked(?,?)`, tenantID, tripID).Scan(&locked); err != nil {
		return fmt.Errorf("read trip bad debt lock: %w", err)
	}
	if locked {
		return domain.NewRuleError("trip_bad_debt_locked", "行程关联坏账单据，请先处理坏账或调整关联后再删除", domain.ErrConflict)
	}
	return nil
}
