package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (s *Store) PrepareUnconfirmedDocumentDeletion(
	ctx context.Context,
	tenantID, documentID string,
) (ports.DocumentDeletionPlan, error) {
	var storageKey, originalHash string
	err := s.db.QueryRowContext(ctx, `
		SELECT storage_key, sha256 FROM documents WHERE tenant_id = ? AND id = ?
	`, tenantID, documentID).Scan(&storageKey, &originalHash)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.DocumentDeletionPlan{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.DocumentDeletionPlan{}, fmt.Errorf("read document deletion source: %w", err)
	}
	var hasFact int
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM claim_sets c
			JOIN review_decisions r ON r.tenant_id = c.tenant_id AND r.claim_set_id = c.id
			WHERE c.tenant_id = ? AND c.document_id = ? AND r.action = 'confirm'
		)
	`, tenantID, documentID).Scan(&hasFact); err != nil {
		return ports.DocumentDeletionPlan{}, fmt.Errorf("inspect document facts: %w", err)
	}
	if hasFact == 1 {
		return ports.DocumentDeletionPlan{}, domain.NewRuleError(
			"document_has_fact",
			"已形成 Fact 的 Document 聚合不能物理删除",
			domain.ErrConflict,
		)
	}
	plan := ports.DocumentDeletionPlan{
		DocumentID:     documentID,
		StorageKeys:    []string{storageKey},
		ObjectHashes:   []string{originalHash},
		ResourceCounts: map[string]int{"documents": 1},
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT derived_image_storage_key, sha256
		FROM document_pages
		WHERE tenant_id = ? AND document_id = ?
		ORDER BY page_number
	`, tenantID, documentID)
	if err != nil {
		return ports.DocumentDeletionPlan{}, fmt.Errorf("list derived document objects: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, hash string
		if err := rows.Scan(&key, &hash); err != nil {
			return ports.DocumentDeletionPlan{}, fmt.Errorf("scan derived document object: %w", err)
		}
		plan.StorageKeys = append(plan.StorageKeys, key)
		plan.ObjectHashes = append(plan.ObjectHashes, hash)
	}
	if err := rows.Err(); err != nil {
		return ports.DocumentDeletionPlan{}, fmt.Errorf("iterate derived document objects: %w", err)
	}
	var pages, jobs, runs, claims, fields, evidence, validations, candidates, decisions, audits int
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT count(*) FROM document_pages WHERE tenant_id = ? AND document_id = ?),
			(SELECT count(*) FROM processing_jobs WHERE tenant_id = ? AND document_id = ?),
			(SELECT count(*) FROM ai_runs a JOIN processing_jobs j ON j.tenant_id = a.tenant_id AND j.id = a.job_id WHERE j.tenant_id = ? AND j.document_id = ?),
			(SELECT count(*) FROM claim_sets WHERE tenant_id = ? AND document_id = ?),
			(SELECT count(*) FROM field_claims f JOIN claim_sets c ON c.tenant_id = f.tenant_id AND c.id = f.claim_set_id WHERE c.tenant_id = ? AND c.document_id = ?),
			(SELECT count(*) FROM evidence e JOIN field_claims f ON f.tenant_id = e.tenant_id AND f.id = e.field_claim_id JOIN claim_sets c ON c.tenant_id = f.tenant_id AND c.id = f.claim_set_id WHERE c.tenant_id = ? AND c.document_id = ?),
			(SELECT count(*) FROM validation_results v LEFT JOIN claim_sets c ON c.tenant_id = v.tenant_id AND c.id = v.claim_set_id LEFT JOIN ai_runs a ON a.tenant_id = v.tenant_id AND a.id = v.ai_run_id LEFT JOIN processing_jobs j ON j.tenant_id = a.tenant_id AND j.id = a.job_id WHERE v.tenant_id = ? AND (c.document_id = ? OR j.document_id = ?)),
			(SELECT count(*) FROM payment_invoice_link_candidates l JOIN claim_sets c ON c.tenant_id = l.tenant_id AND c.id = l.claim_set_id WHERE c.tenant_id = ? AND c.document_id = ?),
			(SELECT count(*) FROM review_decisions r JOIN claim_sets c ON c.tenant_id = r.tenant_id AND c.id = r.claim_set_id WHERE c.tenant_id = ? AND c.document_id = ?),
			(SELECT count(*) FROM audit_events e WHERE e.tenant_id = ? AND ((e.resource_type = 'claim_set' AND e.resource_id IN (SELECT id FROM claim_sets WHERE tenant_id = ? AND document_id = ?)) OR (e.resource_type = 'document' AND e.resource_id = ?)))
	`,
		tenantID, documentID,
		tenantID, documentID,
		tenantID, documentID,
		tenantID, documentID,
		tenantID, documentID,
		tenantID, documentID,
		tenantID, documentID, documentID,
		tenantID, documentID,
		tenantID, documentID,
		tenantID, tenantID, documentID, documentID,
	).Scan(&pages, &jobs, &runs, &claims, &fields, &evidence, &validations, &candidates, &decisions, &audits); err != nil {
		return ports.DocumentDeletionPlan{}, fmt.Errorf("count document aggregate: %w", err)
	}
	for name, count := range map[string]int{
		"document_pages":     pages,
		"processing_jobs":    jobs,
		"ai_runs":            runs,
		"claim_sets":         claims,
		"field_claims":       fields,
		"evidence":           evidence,
		"validation_results": validations,
		"link_candidates":    candidates,
		"review_decisions":   decisions,
		"audit_events":       audits,
	} {
		plan.ResourceCounts[name] = count
	}
	return plan, nil
}

func (s *Store) DeletionTombstoneExists(ctx context.Context, tombstoneID string) (bool, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM deletion_tombstones WHERE id = ?)
	`, tombstoneID).Scan(&exists); err != nil {
		return false, fmt.Errorf("inspect deletion tombstone: %w", err)
	}
	return exists == 1, nil
}

func (t transaction) DeleteDocumentAggregate(ctx context.Context, command ports.DocumentDeleteCommand) error {
	var exists, hasFact int
	if err := t.tx.QueryRowContext(ctx, `
		SELECT
			EXISTS(SELECT 1 FROM documents WHERE tenant_id = ? AND id = ?),
			EXISTS(
				SELECT 1
				FROM claim_sets c
				JOIN review_decisions r ON r.tenant_id = c.tenant_id AND r.claim_set_id = c.id
				WHERE c.tenant_id = ? AND c.document_id = ? AND r.action = 'confirm'
			)
	`, command.TenantID, command.DocumentID, command.TenantID, command.DocumentID).Scan(&exists, &hasFact); err != nil {
		return fmt.Errorf("validate document deletion: %w", err)
	}
	if exists != 1 {
		return domain.ErrNotFound
	}
	if hasFact == 1 {
		return domain.NewRuleError("document_has_fact", "已形成 Fact 的 Document 聚合不能物理删除", domain.ErrConflict)
	}
	if _, err := t.tx.ExecContext(ctx, `
		DELETE FROM audit_events
		WHERE tenant_id = ? AND (
			(resource_type = 'claim_set' AND resource_id IN (
				SELECT id FROM claim_sets WHERE tenant_id = ? AND document_id = ?
			)) OR (resource_type = 'document' AND resource_id = ?)
		)
	`, command.TenantID, command.TenantID, command.DocumentID, command.DocumentID); err != nil {
		return fmt.Errorf("delete document audit chain: %w", err)
	}
	if _, err := t.tx.ExecContext(ctx, `
		DELETE FROM review_decisions
		WHERE tenant_id = ? AND claim_set_id IN (
			SELECT id FROM claim_sets WHERE tenant_id = ? AND document_id = ?
		)
	`, command.TenantID, command.TenantID, command.DocumentID); err != nil {
		return fmt.Errorf("delete document review decisions: %w", err)
	}
	result, err := t.tx.ExecContext(ctx, `
		DELETE FROM documents WHERE tenant_id = ? AND id = ?
	`, command.TenantID, command.DocumentID)
	if err != nil {
		return fmt.Errorf("delete document aggregate: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return domain.ErrConflict
	}
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO deletion_tombstones (
			id, tenant_id, actor_user_id, resource_type, resource_id_hash,
			object_hashes_json, resource_counts_json, request_id, created_at
		) VALUES (?, ?, ?, 'document_aggregate', ?, ?, ?, ?, ?)
	`,
		command.TombstoneID,
		command.TenantID,
		command.ActorUserID,
		command.ResourceIDHash,
		command.ObjectHashesJSON,
		command.ResourceCountsJSON,
		command.RequestID,
		command.DeletedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("insert document deletion tombstone: %w", err)
	}
	return nil
}
