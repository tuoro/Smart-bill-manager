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

func (s *Store) ListTripEvidence(ctx context.Context, tenantID, tripID, after string, limit int) ([]ports.TripEvidence, error) {
	if tripID != "" {
		if _, err := s.getTripSummary(ctx, tenantID, tripID); err != nil {
			return nil, err
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id, c.document_id, e.destination, e.start_date::text, e.end_date::text,
		e.origin, e.transport_type, e.booking_reference, e.version, coalesce(l.id, ''), coalesce(l.trip_id, ''), coalesce(t.name, '')
		FROM trip_evidence_facts e JOIN review_decisions r ON r.tenant_id = e.tenant_id AND r.id = e.source_review_decision_id
		JOIN claim_sets c ON c.tenant_id = r.tenant_id AND c.id = r.claim_set_id
		LEFT JOIN trip_material_links l ON l.tenant_id = e.tenant_id AND l.evidence_id = e.id AND l.ended_at IS NULL
		LEFT JOIN trips t ON t.tenant_id = l.tenant_id AND t.id = l.trip_id
		WHERE e.tenant_id = ? AND e.deleted_at IS NULL AND e.id > ? AND (? = '' OR l.trip_id = ?)
		ORDER BY e.id LIMIT ?`, tenantID, after, tripID, tripID, limit)
	if err != nil {
		return nil, fmt.Errorf("list trip evidence: %w", err)
	}
	defer rows.Close()
	items := make([]ports.TripEvidence, 0)
	for rows.Next() {
		var item ports.TripEvidence
		var origin, transport, booking sql.NullString
		if err := rows.Scan(&item.ID, &item.DocumentID, &item.Destination, &item.StartDate, &item.EndDate,
			&origin, &transport, &booking, &item.Version, &item.CurrentLinkID, &item.CurrentTripID, &item.CurrentTripName); err != nil {
			return nil, fmt.Errorf("scan trip evidence: %w", err)
		}
		item.Origin, item.TransportType, item.BookingReference = nullableString(origin), nullableString(transport), nullableString(booking)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (t transaction) AssignTripMaterial(ctx context.Context, command ports.TripMaterialCommand) (ports.TripMaterialResult, error) {
	if err := t.requireTripManager(ctx, command.TenantID, command.ActorUserID); err != nil {
		return ports.TripMaterialResult{}, err
	}
	var replay ports.TripMaterialResult
	var requestHash string
	err := t.tx.QueryRowContext(ctx, `SELECT d.request_hash, d.expected_version + 1, coalesce(l.id, '') FROM trip_material_decisions d
		LEFT JOIN trip_material_links l ON l.tenant_id = d.tenant_id AND l.created_by_decision_id = d.id
		WHERE d.tenant_id = ? AND d.idempotency_key = ?`, command.TenantID, command.IdempotencyKey).Scan(&requestHash, &replay.Version, &replay.LinkID)
	if err == nil {
		if requestHash != command.RequestHash {
			return ports.TripMaterialResult{}, tripIdempotencyConflict()
		}
		replay.Replayed = true
		return replay, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ports.TripMaterialResult{}, fmt.Errorf("read trip material replay: %w", err)
	}
	var version int
	err = t.tx.QueryRowContext(ctx, `SELECT version FROM trip_evidence_facts WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL FOR UPDATE`, command.TenantID, command.EvidenceID).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.TripMaterialResult{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.TripMaterialResult{}, fmt.Errorf("read material version: %w", err)
	}
	if version != command.ExpectedVersion {
		return ports.TripMaterialResult{}, tripStale()
	}
	var currentID, currentTripID string
	err = t.tx.QueryRowContext(ctx, `SELECT id, trip_id FROM trip_material_links WHERE tenant_id = ? AND evidence_id = ? AND ended_at IS NULL`, command.TenantID, command.EvidenceID).Scan(&currentID, &currentTripID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ports.TripMaterialResult{}, fmt.Errorf("read current material link: %w", err)
	}
	if currentID != command.ExpectedLinkID {
		return ports.TripMaterialResult{}, tripStale()
	}
	if currentTripID == command.DesiredTripID {
		return ports.TripMaterialResult{}, domain.NewRuleError("trip_material_no_change", "材料归属没有变化", domain.ErrConflict)
	}
	if command.DesiredTripID != "" {
		var available bool
		if err := t.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM trips WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL)`, command.TenantID, command.DesiredTripID).Scan(&available); err != nil {
			return ports.TripMaterialResult{}, err
		}
		if !available {
			return ports.TripMaterialResult{}, domain.ErrNotFound
		}
	}
	action := "assign"
	if command.DesiredTripID == "" {
		action = "unassign"
	} else if currentID != "" {
		action = "move"
	}
	if err := t.tripAudit(ctx, command.TenantID, command.ActorUserID, command.AuditEventID, "trip_material_changed", "trip_evidence", command.EvidenceID, command.RequestID,
		map[string]any{"action": action, "version": version + 1}, command.CreatedAt); err != nil {
		return ports.TripMaterialResult{}, err
	}
	createdAt := command.CreatedAt.UTC().Format(time.RFC3339Nano)
	_, err = t.tx.ExecContext(ctx, `INSERT INTO trip_material_decisions
		(id, tenant_id, evidence_id, actor_user_id, decision_source, previous_link_id, desired_trip_id,
		expected_version, action, reason, idempotency_key, request_hash, audit_event_id, created_at)
		VALUES (?, ?, ?, ?, 'manual', NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?)`, command.DecisionID, command.TenantID,
		command.EvidenceID, command.ActorUserID, currentID, command.DesiredTripID, version, action, command.Reason,
		command.IdempotencyKey, command.RequestHash, command.AuditEventID, createdAt)
	if err != nil {
		return ports.TripMaterialResult{}, fmt.Errorf("insert trip material decision: %w", err)
	}
	if currentID != "" {
		if _, err := t.tx.ExecContext(ctx, `UPDATE trip_material_links SET ended_at = ?, ended_by_decision_id = ? WHERE tenant_id = ? AND id = ? AND ended_at IS NULL`, createdAt, command.DecisionID, command.TenantID, currentID); err != nil {
			return ports.TripMaterialResult{}, fmt.Errorf("end trip material link: %w", err)
		}
	}
	result := ports.TripMaterialResult{Version: version + 1}
	if command.DesiredTripID != "" {
		_, err = t.tx.ExecContext(ctx, `INSERT INTO trip_material_links (id, tenant_id, trip_id, evidence_id, created_by_decision_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`, command.LinkID, command.TenantID, command.DesiredTripID, command.EvidenceID, command.DecisionID, createdAt)
		if err != nil {
			return ports.TripMaterialResult{}, fmt.Errorf("insert trip material link: %w", err)
		}
		result.LinkID = command.LinkID
	}
	if _, err := t.tx.ExecContext(ctx, `UPDATE trip_evidence_facts SET version = version + 1 WHERE tenant_id = ? AND id = ?`, command.TenantID, command.EvidenceID); err != nil {
		return ports.TripMaterialResult{}, fmt.Errorf("advance material version: %w", err)
	}
	return result, nil
}
