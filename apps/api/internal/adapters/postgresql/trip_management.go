package postgresqladapter

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

func (t transaction) ManageTrip(ctx context.Context, command ports.TripManagementCommand) (ports.TripManagementResult, error) {
	if err := t.requireTripManager(ctx, command.TenantID, command.ActorUserID); err != nil {
		return ports.TripManagementResult{}, err
	}
	var replay ports.TripManagementResult
	var hash string
	err := t.tx.QueryRowContext(ctx, `SELECT trip_id, resulting_version, request_hash FROM trip_management_decisions
		WHERE tenant_id = ? AND idempotency_key = ?`, command.TenantID, command.IdempotencyKey).Scan(&replay.TripID, &replay.Version, &hash)
	if err == nil {
		if hash != command.RequestHash {
			return ports.TripManagementResult{}, tripIdempotencyConflict()
		}
		replay.Replayed = true
		return replay, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ports.TripManagementResult{}, fmt.Errorf("read trip management replay: %w", err)
	}
	var previous domain.TripDetails
	if command.Action != "create" {
		var version int
		err = t.tx.QueryRowContext(ctx, `SELECT name, start_date::text, end_date::text, coalesce(timezone, ''), notes, version
			FROM trips WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL FOR UPDATE`, command.TenantID, command.TripID).
			Scan(&previous.Name, &previous.StartDate, &previous.EndDate, &previous.Timezone, &previous.Notes, &version)
		if errors.Is(err, sql.ErrNoRows) {
			return ports.TripManagementResult{}, domain.ErrNotFound
		}
		if err != nil {
			return ports.TripManagementResult{}, fmt.Errorf("read trip management target: %w", err)
		}
		if version != command.ExpectedVersion {
			return ports.TripManagementResult{}, domain.ErrVersionConflict
		}
	}
	details := command.Details
	if command.Action == "delete" {
		if err := t.requireTripNotBadDebtLocked(ctx, command.TenantID, command.TripID); err != nil {
			return ports.TripManagementResult{}, err
		}
		details = previous
	}
	version := command.ExpectedVersion + 1
	createdAt := command.CreatedAt.UTC().Format(time.RFC3339Nano)
	if err := t.tripAudit(ctx, command.TenantID, command.ActorUserID, command.AuditEventID,
		"trip_workspace_"+command.Action, "trip_workspace", command.TripID, command.RequestID,
		map[string]any{"version": version}, command.CreatedAt); err != nil {
		return ports.TripManagementResult{}, err
	}
	_, err = t.tx.ExecContext(ctx, `INSERT INTO trip_management_decisions
		(id, tenant_id, trip_id, actor_user_id, action, expected_version, resulting_version,
		name, start_date, end_date, timezone, notes, reason, idempotency_key, request_hash, audit_event_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?)`,
		command.DecisionID, command.TenantID, command.TripID, command.ActorUserID, command.Action,
		command.ExpectedVersion, version, details.Name, details.StartDate, details.EndDate, details.Timezone,
		details.Notes, command.Reason, command.IdempotencyKey, command.RequestHash, command.AuditEventID, createdAt)
	if err != nil {
		return ports.TripManagementResult{}, fmt.Errorf("insert trip management decision: %w", err)
	}
	if command.Action == "create" {
		_, err = t.tx.ExecContext(ctx, `INSERT INTO trips
			(id, tenant_id, name, start_date, end_date, timezone, notes, origin_kind, last_management_decision_id, created_at, updated_at, version)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'manual', ?, ?, ?, 1)`, command.TripID, command.TenantID,
			details.Name, details.StartDate, details.EndDate, details.Timezone, details.Notes, command.DecisionID, createdAt, createdAt)
	} else {
		var deletedAt, deletedBy, auditID any
		if command.Action == "delete" {
			deletedAt, deletedBy, auditID = createdAt, command.ActorUserID, command.AuditEventID
		}
		_, err = t.tx.ExecContext(ctx, `UPDATE trips SET name = ?, start_date = ?, end_date = ?, timezone = NULLIF(?, ''),
			notes = ?, version = ?, last_management_decision_id = ?, updated_at = ?,
			deleted_at = ?, deleted_by_user_id = ?, deletion_audit_event_id = ? WHERE tenant_id = ? AND id = ?`,
			details.Name, details.StartDate, details.EndDate, details.Timezone, details.Notes, version, command.DecisionID,
			createdAt, deletedAt, deletedBy, auditID, command.TenantID, command.TripID)
	}
	if err != nil {
		return ports.TripManagementResult{}, fmt.Errorf("write trip workspace: %w", err)
	}
	if command.Action == "delete" {
		if err := t.endWorkspaceLinks(ctx, command, createdAt); err != nil {
			return ports.TripManagementResult{}, err
		}
	}
	if command.Action != "edit" || previous.StartDate != details.StartDate || previous.EndDate != details.EndDate || previous.Timezone != details.Timezone {
		ranges := []domain.TripDetails{previous}
		if command.Action != "delete" {
			ranges = append(ranges, details)
		}
		if err := t.reconcileTripPayments(ctx, command.TenantID, command.ActorUserID, command.RequestID, command.CreatedAt, "", ranges); err != nil {
			return ports.TripManagementResult{}, err
		}
	}
	return ports.TripManagementResult{TripID: command.TripID, Version: version}, nil
}

func (t transaction) endWorkspaceLinks(ctx context.Context, command ports.TripManagementCommand, createdAt string) error {
	for _, table := range []string{"payments", "invoices"} {
		column := "payment_id"
		if table == "invoices" {
			column = "invoice_id"
		}
		_, err := t.tx.ExecContext(ctx, `UPDATE `+table+` SET version = version + 1 WHERE tenant_id = ? AND id IN (
			SELECT `+column+` FROM trip_fact_assignments WHERE tenant_id = ? AND trip_id = ? AND ended_at IS NULL)`, command.TenantID, command.TenantID, command.TripID)
		if err != nil {
			return fmt.Errorf("advance workspace expense versions: %w", err)
		}
	}
	if _, err := t.tx.ExecContext(ctx, `UPDATE trip_evidence_facts SET version = version + 1 WHERE tenant_id = ? AND id IN
		(SELECT evidence_id FROM trip_material_links WHERE tenant_id = ? AND trip_id = ? AND ended_at IS NULL)`, command.TenantID, command.TenantID, command.TripID); err != nil {
		return fmt.Errorf("advance material versions: %w", err)
	}
	for _, table := range []string{"trip_fact_assignments", "trip_material_links"} {
		if _, err := t.tx.ExecContext(ctx, `UPDATE `+table+` SET ended_at = ?, ended_by_audit_event_id = ?
			WHERE tenant_id = ? AND trip_id = ? AND ended_at IS NULL`, createdAt, command.AuditEventID, command.TenantID, command.TripID); err != nil {
			return fmt.Errorf("end workspace links: %w", err)
		}
	}
	return nil
}

func (t transaction) tripAudit(ctx context.Context, tenantID, actorID, auditID, action, resourceType, resourceID, requestID string, metadata any, now time.Time) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx, `INSERT INTO audit_events
		(id, tenant_id, actor_user_id, action, resource_type, resource_id, request_id, safe_metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?)`, auditID, tenantID, actorID, action, resourceType, resourceID, requestID, string(encoded), now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert trip audit: %w", err)
	}
	return nil
}

func tripIdempotencyConflict() error {
	return domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的行程操作", domain.ErrConflict)
}

func tripStale() error {
	return domain.NewRuleError("trip_assignment_stale", "行程归属或记录版本已变化，请刷新后重试", domain.ErrConflict)
}

// 锁定成员行，避免请求开始后的角色撤销与归属写入交错。
func (t transaction) requireTripManager(ctx context.Context, tenantID, actorID string) error {
	var role string
	err := t.tx.QueryRowContext(ctx, `SELECT role FROM memberships WHERE tenant_id = ? AND user_id = ? AND status = 'active' FOR SHARE`, tenantID, actorID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrForbidden
	}
	if err != nil {
		return fmt.Errorf("check trip manager membership: %w", err)
	}
	if role != "owner" && role != "finance" {
		return domain.ErrForbidden
	}
	return nil
}
