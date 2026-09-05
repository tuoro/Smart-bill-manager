package postgresqladapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (t transaction) LockMaterialPublication(ctx context.Context, id string) error {
	_, err := t.tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "invoice-material-publication:"+id)
	return err
}

func (t transaction) MaterialPublicationCommitted(ctx context.Context, p ports.MaterialPublication) (bool, error) {
	var total, matched int
	err := t.tx.QueryRowContext(ctx, `SELECT count(*), count(*) FILTER (WHERE tenant_id = ? AND id = ? AND sha256 = ? AND storage_key = ? AND size_bytes = ?)
		FROM documents WHERE storage_key = ? OR id = ?`, p.TenantID, p.DocumentID, p.Staged.SHA256, p.StorageKey, p.Staged.Size, p.StorageKey, p.DocumentID).Scan(&total, &matched)
	if err != nil {
		return false, err
	}
	if total == 0 {
		return false, nil
	}
	if total != 1 || matched != 1 {
		return false, errors.New("material publication identity mismatch")
	}
	return true, nil
}

func (t transaction) ChangeInvoiceMaterial(ctx context.Context, c ports.InvoiceMaterialCommand) (ports.InvoiceMaterialResult, error) {
	var result ports.InvoiceMaterialResult
	var version int
	var deleted bool
	var originalID string
	err := t.tx.QueryRowContext(ctx, `SELECT i.version, i.deleted_at IS NOT NULL, cs.document_id
		FROM invoices i JOIN review_decisions r ON r.tenant_id = i.tenant_id AND r.id = i.source_review_decision_id
		JOIN claim_sets cs ON cs.tenant_id = r.tenant_id AND cs.id = r.claim_set_id
		WHERE i.tenant_id = ? AND i.id = ? FOR UPDATE OF i`, c.TenantID, c.InvoiceID).Scan(&version, &deleted, &originalID)
	if errors.Is(err, sql.ErrNoRows) {
		return result, domain.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	var authorized bool
	if err := t.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE tenant_id = ? AND user_id = ?
		AND status = 'active' AND role IN ('owner','finance'))`, c.TenantID, c.ActorUserID).Scan(&authorized); err != nil {
		return result, err
	}
	if !authorized {
		return result, domain.ErrForbidden
	}
	var requestHash string
	err = t.tx.QueryRowContext(ctx, `SELECT invoice_id, link_id, document_id, resulting_version, request_hash
		FROM invoice_material_decisions WHERE tenant_id = ? AND idempotency_key = ?`, c.TenantID, c.IdempotencyKey).
		Scan(&result.InvoiceID, &result.LinkID, &result.DocumentID, &result.Version, &requestHash)
	if err == nil {
		if requestHash != c.RequestHash {
			return result, materialConflict("idempotency_key_conflict", "请求标识已用于其他操作")
		}
		result.Replayed = true
		result.Document, err = t.lockMaterialDocument(ctx, c.TenantID, result.DocumentID)
		return result, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	if deleted {
		return result, domain.ErrNotFound
	}
	if version != c.ExpectedVersion {
		return result, domain.ErrVersionConflict
	}
	document, linkID, err := t.resolveMaterialTarget(ctx, c)
	if err != nil {
		return result, err
	}
	if c.Action != "remove" && document.ID == originalID {
		return result, materialConflict("invoice_material_is_original", "该文件已经是本发票原件，无需重复添加")
	}
	if c.Action != "remove" {
		var count int
		var duplicate bool
		if err := t.tx.QueryRowContext(ctx, `SELECT count(*), coalesce(bool_or(document_id = ?), false)
			FROM invoice_material_links WHERE tenant_id = ? AND invoice_id = ? AND ended_at IS NULL`, document.ID, c.TenantID, c.InvoiceID).Scan(&count, &duplicate); err != nil {
			return result, err
		}
		if duplicate {
			return result, materialConflict("invoice_material_exists", "此辅助材料已经关联到该发票")
		}
		if count >= domain.MaxInvoiceMaterials {
			return result, materialConflict("invoice_material_limit", "一张发票最多关联 100 份辅助材料")
		}
	}
	metadata, _ := json.Marshal(map[string]any{"action": c.Action, "resulting_version": version + 1})
	_, err = t.tx.ExecContext(ctx, `INSERT INTO audit_events (id, tenant_id, actor_user_id, action, resource_type, resource_id,
		request_id, safe_metadata_json, created_at) VALUES (?, ?, ?, ?, 'invoice', ?, ?, ?::jsonb, ?)`,
		c.AuditEventID, c.TenantID, c.ActorUserID, "invoice_material_"+c.Action, c.InvoiceID, c.RequestID, string(metadata), c.CreatedAt)
	if err != nil {
		return result, err
	}
	_, err = t.tx.ExecContext(ctx, `INSERT INTO invoice_material_decisions (id, tenant_id, invoice_id, document_id, link_id,
		actor_user_id, action, expected_version, resulting_version, reason, idempotency_key, request_hash, audit_event_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.DecisionID, c.TenantID, c.InvoiceID, document.ID, linkID,
		c.ActorUserID, c.Action, c.ExpectedVersion, version+1, c.Reason, c.IdempotencyKey, c.RequestHash, c.AuditEventID, c.CreatedAt)
	if err != nil {
		return result, invoiceMaterialWriteError(err)
	}
	if c.Action == "remove" {
		_, err = t.tx.ExecContext(ctx, `UPDATE invoice_material_links SET ended_at = ?, ended_by_decision_id = ?
			WHERE tenant_id = ? AND id = ? AND ended_at IS NULL`, c.CreatedAt, c.DecisionID, c.TenantID, linkID)
	} else {
		_, err = t.tx.ExecContext(ctx, `INSERT INTO invoice_material_links (id, tenant_id, invoice_id, document_id, created_by_decision_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`, linkID, c.TenantID, c.InvoiceID, document.ID, c.DecisionID, c.CreatedAt)
	}
	if err != nil {
		return result, invoiceMaterialWriteError(err)
	}
	_, err = t.tx.ExecContext(ctx, `UPDATE invoices SET version = version + 1 WHERE tenant_id = ? AND id = ?`, c.TenantID, c.InvoiceID)
	if err != nil {
		return result, err
	}
	return ports.InvoiceMaterialResult{InvoiceID: c.InvoiceID, LinkID: linkID, DocumentID: document.ID, Version: version + 1, Document: document}, nil
}

func (t transaction) resolveMaterialTarget(ctx context.Context, c ports.InvoiceMaterialCommand) (ports.Document, string, error) {
	id, linkID := c.DocumentID, c.NewLinkID
	if c.Action == "remove" {
		linkID = c.LinkID
		err := t.tx.QueryRowContext(ctx, `SELECT document_id FROM invoice_material_links
			WHERE tenant_id = ? AND invoice_id = ? AND id = ? AND ended_at IS NULL FOR UPDATE`, c.TenantID, c.InvoiceID, linkID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return ports.Document{}, "", domain.ErrVersionConflict
		}
		if err != nil {
			return ports.Document{}, "", err
		}
	} else if c.Action == "upload" {
		if c.UploadDocument == nil || c.UploadDocument.TenantID != c.TenantID || c.UploadDocument.SHA256 != c.UploadSHA256 {
			return ports.Document{}, "", domain.ErrInvalidInput
		}
		_, err := t.tx.ExecContext(ctx, insertDocumentSQL+` ON CONFLICT (tenant_id, sha256) DO NOTHING`, documentArguments(*c.UploadDocument)...)
		if err != nil {
			return ports.Document{}, "", err
		}
		id, err = t.FindDocumentIDBySHA(ctx, c.TenantID, c.UploadSHA256)
		if err != nil {
			return ports.Document{}, "", err
		}
	} else if c.Action != "add" {
		return ports.Document{}, "", domain.ErrInvalidInput
	}
	document, err := t.lockMaterialDocument(ctx, c.TenantID, id)
	return document, linkID, err
}

func (t transaction) lockMaterialDocument(ctx context.Context, tenantID, id string) (ports.Document, error) {
	var d ports.Document
	var created string
	err := t.tx.QueryRowContext(ctx, `SELECT id, tenant_id, storage_key, original_name, detected_mime, size_bytes, sha256, page_count, created_at
		FROM documents WHERE tenant_id = ? AND id = ? FOR UPDATE`, tenantID, id).
		Scan(&d.ID, &d.TenantID, &d.StorageKey, &d.OriginalName, &d.DetectedMIME, &d.SizeBytes, &d.SHA256, &d.PageCount, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return d, domain.ErrNotFound
	}
	if err == nil {
		d.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	}
	return d, err
}

func materialConflict(code, message string) error {
	return domain.NewRuleError(code, message, domain.ErrConflict)
}

func ensureNoMaterialHistory(ctx context.Context, query reimbursementQueryer, tenantID, documentID string) error {
	var retained bool
	if err := query.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM invoice_material_links WHERE tenant_id = ? AND document_id = ?)`, tenantID, documentID).Scan(&retained); err != nil {
		return err
	}
	if retained {
		return materialConflict("document_has_material_history", "该原件已被发票辅助材料历史引用，只可解除关联，不能物理删除")
	}
	return nil
}

func invoiceMaterialWriteError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && (pg.ConstraintName == "invoice_material_request_key" || pg.ConstraintName == "invoice_material_active_pair") {
		return materialConflict("invoice_material_conflict", "辅助材料或请求标识已变化，请刷新核对")
	}
	return fmt.Errorf("write invoice material: %w", err)
}
