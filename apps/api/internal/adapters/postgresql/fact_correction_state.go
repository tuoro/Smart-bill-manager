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

func correctionTable(kind domain.DocumentType) (string, error) {
	switch kind {
	case domain.DocumentPayment:
		return "payments", nil
	case domain.DocumentInvoice:
		return "invoices", nil
	case domain.DocumentTrip:
		return "trip_evidence_facts", nil
	default:
		return "", domain.ErrInvalidInput
	}
}

func (s *Store) GetFactCorrectionState(ctx context.Context, tenantID string, kind domain.DocumentType, factID, proposedTime string) (ports.FactCorrectionState, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return ports.FactCorrectionState{}, fmt.Errorf("begin correction snapshot: %w", err)
	}
	defer tx.Rollback()
	return readFactCorrectionState(ctx, tx, tenantID, kind, factID, proposedTime, false)
}

func (t transaction) GetFactCorrectionState(ctx context.Context, tenantID string, kind domain.DocumentType, factID, proposedTime string) (ports.FactCorrectionState, error) {
	return readFactCorrectionState(ctx, t.tx, tenantID, kind, factID, proposedTime, true)
}

func readFactCorrectionState(ctx context.Context, queryer reimbursementQueryer, tenantID string, kind domain.DocumentType, factID, proposedTime string, lock bool) (ports.FactCorrectionState, error) {
	state := ports.FactCorrectionState{FactType: kind, FactID: factID, Links: []domain.CorrectionLink{}}
	table, err := correctionTable(kind)
	if err != nil {
		return state, err
	}
	lockSQL := ""
	if lock {
		lockSQL = " FOR UPDATE OF fact"
	}
	err = queryer.QueryRowContext(ctx, `SELECT fact.version, fact.current_review_decision_id, reviewed.claim_set_id, claim.document_id
		FROM `+table+` fact JOIN review_decisions reviewed ON reviewed.tenant_id = fact.tenant_id AND reviewed.id = fact.current_review_decision_id
		JOIN claim_sets claim ON claim.tenant_id = reviewed.tenant_id AND claim.id = reviewed.claim_set_id
		WHERE fact.tenant_id = ? AND fact.id = ? AND fact.deleted_at IS NULL`+lockSQL, tenantID, factID).Scan(&state.Version, &state.CurrentReviewDecisionID, &state.ClaimSetID, &state.DocumentID)
	if errors.Is(err, sql.ErrNoRows) {
		return state, domain.ErrNotFound
	}
	if err != nil {
		return state, fmt.Errorf("read correction anchor: %w", err)
	}
	if kind == domain.DocumentTrip {
		state.Attribution.Mode = "preserve_material_links"
		return state, nil
	}
	state.Links, err = readCorrectionLinks(ctx, queryer, tenantID, kind, factID)
	if err != nil {
		return state, err
	}
	state.Attribution.Mode = "manual"
	column := "invoice_id"
	if kind == domain.DocumentPayment {
		column = "payment_id"
		var currentTime string
		if err := queryer.QueryRowContext(ctx, `SELECT trip_assignment_mode, transaction_time FROM payments WHERE tenant_id = ? AND id = ?`, tenantID, factID).Scan(&state.Attribution.Mode, &currentTime); err != nil {
			return state, fmt.Errorf("read correction attribution preference: %w", err)
		}
		if proposedTime == "" {
			proposedTime = currentTime
		}
		if state.Attribution.Mode == "auto" {
			match, err := findAutomaticTripMatch(ctx, queryer, tenantID, proposedTime)
			if err != nil {
				return state, err
			}
			state.Attribution.DesiredTripID = match.TripID
			state.Attribution.MatchingTripCount = match.Count
			state.Attribution.MatchingTripVersion = match.Version
		}
	}
	err = queryer.QueryRowContext(ctx, `SELECT id, trip_id FROM trip_fact_assignments WHERE tenant_id = ? AND `+column+` = ? AND ended_at IS NULL`, tenantID, factID).Scan(&state.Attribution.AssignmentID, &state.Attribution.CurrentTripID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return state, fmt.Errorf("read correction assignment: %w", err)
	}
	if state.Attribution.Mode != "auto" {
		state.Attribution.DesiredTripID = state.Attribution.CurrentTripID
	}
	return state, nil
}

func readCorrectionLinks(ctx context.Context, queryer reimbursementQueryer, tenantID string, kind domain.DocumentType, factID string) ([]domain.CorrectionLink, error) {
	anchor, target, table, amount, date := "payment_id", "invoice_id", "invoices", "total_minor", "invoice_date"
	if kind == domain.DocumentInvoice {
		anchor, target, table, amount, date = "invoice_id", "payment_id", "payments", "amount_minor", "business_date"
	}
	rows, err := queryer.QueryContext(ctx, `SELECT link.id, target.id, link.allocated_minor, link.currency, target.currency, target.`+date+`::text,
		target.`+amount+`, (SELECT coalesce(sum(active.allocated_minor), 0) FROM payment_invoice_links active WHERE active.tenant_id = target.tenant_id AND active.`+target+` = target.id AND active.ended_at IS NULL),
		target.version, target.deleted_at IS NULL
		FROM payment_invoice_links link JOIN `+table+` target ON target.tenant_id = link.tenant_id AND target.id = link.`+target+`
		WHERE link.tenant_id = ? AND link.`+anchor+` = ? AND link.ended_at IS NULL ORDER BY link.id`, tenantID, factID)
	if err != nil {
		return nil, fmt.Errorf("read correction links: %w", err)
	}
	defer rows.Close()
	links := make([]domain.CorrectionLink, 0)
	for rows.Next() {
		var link domain.CorrectionLink
		if err := rows.Scan(&link.ID, &link.TargetID, &link.AllocatedMinor, &link.Currency, &link.TargetCurrency, &link.TargetBusinessDate, &link.TargetAmountMinor, &link.TargetAllocatedMinor, &link.TargetVersion, &link.TargetAvailable); err != nil {
			return nil, fmt.Errorf("scan correction link: %w", err)
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

type automaticTripMatch struct {
	TripID         string
	Count, Version int
}

func findAutomaticTripMatch(ctx context.Context, queryer reimbursementQueryer, tenantID, instant string) (automaticTripMatch, error) {
	var match automaticTripMatch
	if _, err := time.Parse(time.RFC3339Nano, instant); err != nil {
		return match, domain.ErrInvalidInput
	}
	err := queryer.QueryRowContext(ctx, `SELECT count(*), coalesce(min(id), ''), coalesce(min(version), 0) FROM trips
		WHERE tenant_id = ? AND deleted_at IS NULL AND timezone IS NOT NULL
		AND ?::timestamptz >= (start_date::timestamp AT TIME ZONE timezone)
		AND ?::timestamptz < ((end_date + 1)::timestamp AT TIME ZONE timezone)`, tenantID, instant, instant).Scan(&match.Count, &match.TripID, &match.Version)
	if err != nil {
		return match, fmt.Errorf("match correction trip interval: %w", err)
	}
	if match.Count != 1 {
		match.TripID, match.Version = "", 0
	}
	return match, nil
}
