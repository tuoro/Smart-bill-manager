package postgresqladapter

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (s *Store) GetInvoiceMaterials(ctx context.Context, tenantID, invoiceID string) (ports.InvoiceMaterialWorkspace, error) {
	result := ports.InvoiceMaterialWorkspace{InvoiceID: invoiceID, Items: []ports.InvoiceMaterial{}}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT version FROM invoices WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, tenantID, invoiceID).Scan(&result.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return result, domain.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	page, err := readFactPage(ctx, tx, `SELECT l.id, d.id, d.original_name, d.detected_mime, d.size_bytes, d.page_count, l.created_at
		FROM invoice_material_links l JOIN documents d ON d.tenant_id = l.tenant_id AND d.id = l.document_id
		WHERE l.tenant_id = ? AND l.invoice_id = ? AND l.ended_at IS NULL ORDER BY l.created_at DESC, l.id DESC LIMIT ?`,
		[]any{tenantID, invoiceID, domain.MaxInvoiceMaterials + 1}, domain.MaxInvoiceMaterials, scanInvoiceMaterial,
		func(m ports.InvoiceMaterial) domain.FactSortKey {
			return domain.FactSortKey{CreatedAt: m.CreatedAt, ID: m.ID}
		})
	if err != nil {
		return result, err
	}
	if page.Next != nil {
		return result, materialConflict("invoice_material_limit", "材料数量超过支持上限，不能截断")
	}
	result.Items = page.Items
	return result, tx.Commit()
}

func (s *Store) ListMaterialDocuments(ctx context.Context, tenantID, invoiceID string, query ports.MaterialDocumentQuery) (ports.FactPage[ports.InvoiceMaterial], error) {
	var empty ports.FactPage[ports.InvoiceMaterial]
	filter, err := domain.CanonicalFactFilter(domain.FactFilter{Query: query.Query})
	if err != nil || query.Limit < 1 || query.Limit > 100 || query.After != nil && !query.After.Valid() {
		return empty, domain.ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	var original string
	err = tx.QueryRowContext(ctx, `SELECT c.document_id FROM invoices i
		JOIN review_decisions r ON r.tenant_id = i.tenant_id AND r.id = i.source_review_decision_id
		JOIN claim_sets c ON c.tenant_id = r.tenant_id AND c.id = r.claim_set_id
		WHERE i.tenant_id = ? AND i.id = ? AND i.deleted_at IS NULL`, tenantID, invoiceID).Scan(&original)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, domain.ErrNotFound
	}
	if err != nil {
		return empty, err
	}
	statement := `SELECT d.id, d.id, d.original_name, d.detected_mime, d.size_bytes, d.page_count, d.created_at FROM documents d
		WHERE d.tenant_id = ? AND d.id <> ?
		AND NOT EXISTS (SELECT 1 FROM invoice_material_links l WHERE l.tenant_id = d.tenant_id AND l.invoice_id = ? AND l.document_id = d.id AND l.ended_at IS NULL)`
	args := []any{tenantID, original, invoiceID}
	if filter.Query != "" {
		statement += ` AND d.original_name ILIKE ? ESCAPE '\'`
		args = append(args, "%"+strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(filter.Query)+"%")
	}
	if query.After != nil {
		statement += ` AND (d.created_at,d.id)<(?::timestamptz,?)`
		args = append(args, query.After.CreatedAt, query.After.ID)
	}
	statement += ` ORDER BY d.created_at DESC,d.id DESC LIMIT ?`
	args = append(args, query.Limit+1)
	page, err := readFactPage(ctx, tx, statement, args, query.Limit, scanInvoiceMaterial,
		func(m ports.InvoiceMaterial) domain.FactSortKey {
			return domain.FactSortKey{CreatedAt: m.CreatedAt, ID: m.DocumentID}
		})
	if err != nil {
		return empty, err
	}
	return page, tx.Commit()
}

func scanInvoiceMaterial(row factScanner) (ports.InvoiceMaterial, error) {
	var item ports.InvoiceMaterial
	var created string
	err := row.Scan(&item.ID, &item.DocumentID, &item.OriginalName, &item.MIME, &item.SizeBytes, &item.PageCount, &created)
	if err == nil {
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	}
	return item, err
}

func loadReimbursementMaterials(ctx context.Context, query reimbursementQueryer, tenantID string, items []domain.ReimbursementPolicyItem) ([]domain.ReimbursementMaterial, error) {
	result := []domain.ReimbursementMaterial{}
	args := []any{tenantID}
	for _, item := range items {
		if item.FactType == domain.DocumentInvoice {
			args = append(args, item.FactID)
		}
	}
	if len(args) == 1 {
		return result, nil
	}
	rows, err := query.QueryContext(ctx, `SELECT invoice_id, id, document_id FROM invoice_material_links
		WHERE tenant_id = ? AND invoice_id IN (`+sqlPlaceholders(len(args)-1)+`) AND ended_at IS NULL
		ORDER BY invoice_id, id LIMIT 20001`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item domain.ReimbursementMaterial
		if err := rows.Scan(&item.InvoiceID, &item.LinkID, &item.DocumentID); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result) > domain.MaxInvoiceMaterials*domain.MaxReimbursementItems {
		return nil, domain.ErrConflict
	}
	return result, nil
}
