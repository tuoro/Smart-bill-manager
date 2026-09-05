package postgresqladapter

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (s *Store) BuildMaterialExport(ctx context.Context, tenantID string, scope domain.ExportScope) (ports.ExportInventory, error) {
	if tenantID == "" || !scope.Valid() {
		return ports.ExportInventory{}, domain.ErrInvalidInput
	}
	return withReimbursementReadSnapshot(ctx, s.db, func(q reimbursementQueryer) (ports.ExportInventory, error) {
		inventory := ports.ExportInventory{Manifest: domain.ExportManifest{Scope: scope, References: []domain.ExportReference{}, Files: []domain.ExportFile{}, Warnings: []string{}}, StorageKeys: map[string]string{}}
		m := &inventory.Manifest
		var expectedMaterials sql.NullInt64
		var err error
		if scope.Kind == "trip" {
			err = q.QueryRowContext(ctx, `SELECT id,name,start_date::text,end_date::text,timezone,version FROM trips
			 WHERE tenant_id=? AND id=? AND deleted_at IS NULL`, tenantID, scope.ID).Scan(&m.Trip.ID, &m.Trip.Name, &m.Trip.StartDate, &m.Trip.EndDate, &m.Trip.Timezone, &m.Trip.Version)
			m.Name, m.MaterialsCaptured = m.Trip.Name, true
			if m.Trip.Version != nil {
				m.Version = *m.Trip.Version
			}
		} else {
			err = q.QueryRowContext(ctx, `SELECT trip_id,trip_name,trip_start_date::text,trip_end_date::text,trip_timezone,trip_version,
				 1::bigint,snapshot_hash,materials_captured,material_count FROM reimbursements WHERE tenant_id=? AND id=?`, tenantID, scope.ID).
				Scan(&m.Trip.ID, &m.Trip.Name, &m.Trip.StartDate, &m.Trip.EndDate, &m.Trip.Timezone, &m.Trip.Version, &m.Version, &m.SnapshotHash, &m.MaterialsCaptured, &expectedMaterials)
			m.Name = m.Trip.Name
			m.Warnings = append(m.Warnings, "报销快照未捕获行程凭证集合，本包不包含 TripEvidence。")
			if !m.MaterialsCaptured {
				m.Warnings = append(m.Warnings, "此历史快照未捕获辅助材料集合；本包仅包含已知原件，不代表完整历史辅助材料。")
			}
		}
		if errors.Is(err, sql.ErrNoRows) {
			return inventory, domain.ErrNotFound
		}
		if err != nil {
			return inventory, err
		}
		statement := currentTripExportReferences
		if scope.Kind == "reimbursement" {
			statement = fixedReimbursementExportReferences
		}
		m.References, err = readExportReferences(ctx, q, statement, tenantID, scope.ID)
		if err != nil {
			return inventory, err
		}
		materialCount := int64(0)
		uncapturedReview := false
		for _, r := range m.References {
			if r.Kind == "auxiliary" {
				materialCount++
			}
			if r.Kind == "original" && r.ReviewDecisionID == nil {
				uncapturedReview = true
			}
		}
		if scope.Kind == "reimbursement" && (m.MaterialsCaptured && (!expectedMaterials.Valid || expectedMaterials.Int64 != materialCount) || !m.MaterialsCaptured && materialCount != 0) {
			return inventory, domain.NewRuleError("export_snapshot_incomplete", "报销材料快照不完整，不能生成交付包", domain.ErrConflict)
		}
		if uncapturedReview {
			m.Warnings = append(m.Warnings, "部分历史条目未捕获提交时的 Review 身份；清单保留 null，不以当前或初始 Review 回填。")
		}
		if err := readExportFiles(ctx, q, tenantID, &inventory); err != nil {
			return inventory, err
		}
		return inventory, nil
	})
}

// 先读取关系，再 LEFT JOIN 不可变来源；不能用过滤型 JOIN 把缺失材料静默变成少一行。
const currentTripExportReferences = `WITH selected AS (
	SELECT a.tenant_id,a.id AS relation_id,
	 CASE WHEN a.payment_id IS NOT NULL THEN 'payment' ELSE 'invoice' END AS fact_type,
	 coalesce(a.payment_id,a.invoice_id) AS fact_id,coalesce(p.version,i.version) AS fact_version,
	 coalesce(p.current_review_decision_id,i.current_review_decision_id) AS review_id,
	 coalesce(p.source_review_decision_id,i.source_review_decision_id) AS source_review_id,
	 coalesce(p.merchant,i.seller_name) AS display_name,coalesce(p.business_date,i.invoice_date)::text AS business_date,
	 coalesce(p.amount_minor,i.total_minor) AS amount_minor,coalesce(p.currency,i.currency) AS currency
	FROM trip_fact_assignments a
	LEFT JOIN payments p ON p.tenant_id=a.tenant_id AND p.id=a.payment_id AND p.deleted_at IS NULL
	LEFT JOIN invoices i ON i.tenant_id=a.tenant_id AND i.id=a.invoice_id AND i.deleted_at IS NULL
	WHERE a.tenant_id=? AND a.trip_id=? AND a.ended_at IS NULL
	UNION ALL
	SELECT l.tenant_id,l.id,'trip_evidence',l.evidence_id,e.version,e.current_review_decision_id,e.source_review_decision_id,
	 e.destination,e.start_date::text,NULL::bigint,'' FROM trip_material_links l
	LEFT JOIN trip_evidence_facts e ON e.tenant_id=l.tenant_id AND e.id=l.evidence_id AND e.deleted_at IS NULL
	WHERE l.tenant_id=? AND l.trip_id=? AND l.ended_at IS NULL
), refs AS (
	SELECT 'original' AS kind,f.relation_id,f.fact_type,f.fact_id,f.fact_version,f.review_id,
	 f.display_name,f.business_date,f.amount_minor,f.currency,c.document_id
	FROM selected f LEFT JOIN review_decisions r ON r.tenant_id=f.tenant_id AND r.id=f.source_review_id
	LEFT JOIN claim_sets c ON c.tenant_id=r.tenant_id AND c.id=r.claim_set_id
	UNION ALL
	SELECT 'auxiliary',l.id,f.fact_type,f.fact_id,f.fact_version,f.review_id,f.display_name,f.business_date,f.amount_minor,f.currency,l.document_id
	FROM selected f JOIN invoice_material_links l ON l.tenant_id=f.tenant_id AND l.invoice_id=f.fact_id
	WHERE f.fact_type='invoice' AND l.ended_at IS NULL
) SELECT * FROM refs ORDER BY kind,relation_id LIMIT ?`

const fixedReimbursementExportReferences = `WITH selected AS (
	SELECT item.tenant_id,item.id AS relation_id,item.fact_type,coalesce(item.payment_id,item.invoice_id) AS fact_id,
	 NULL::bigint AS fact_version,item.fact_review_decision_id AS review_id,
	 coalesce(p.source_review_decision_id,i.source_review_decision_id) AS source_review_id,
	 item.display_name,item.business_date::text AS business_date,item.amount_minor,item.currency
	FROM reimbursement_items item
	LEFT JOIN payments p ON p.tenant_id=item.tenant_id AND p.id=item.payment_id
	LEFT JOIN invoices i ON i.tenant_id=item.tenant_id AND i.id=item.invoice_id
	WHERE item.tenant_id=? AND item.reimbursement_id=?
), refs AS (
	SELECT 'original' AS kind,f.relation_id,f.fact_type,f.fact_id,f.fact_version,f.review_id,
	 f.display_name,f.business_date,f.amount_minor,f.currency,c.document_id
	FROM selected f LEFT JOIN review_decisions r ON r.tenant_id=f.tenant_id AND r.id=f.source_review_id
	LEFT JOIN claim_sets c ON c.tenant_id=r.tenant_id AND c.id=r.claim_set_id
	UNION ALL
	SELECT 'auxiliary',s.link_id,f.fact_type,f.fact_id,f.fact_version,f.review_id,f.display_name,f.business_date,f.amount_minor,f.currency,s.document_id
	FROM reimbursement_material_snapshots s LEFT JOIN selected f ON f.tenant_id=s.tenant_id AND f.fact_id=s.invoice_id AND f.fact_type='invoice'
	WHERE s.tenant_id=? AND s.reimbursement_id=?
) SELECT * FROM refs ORDER BY kind,relation_id LIMIT ?`

func readExportReferences(ctx context.Context, q reimbursementQueryer, statement, tenantID, scopeID string) ([]domain.ExportReference, error) {
	rows, err := q.QueryContext(ctx, statement, tenantID, scopeID, tenantID, scopeID, domain.MaxExportReferences+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.ExportReference{}
	for rows.Next() {
		var r domain.ExportReference
		var documentID, displayName, businessDate, currency sql.NullString
		if err := rows.Scan(&r.Kind, &r.RelationID, &r.FactType, &r.FactID, &r.FactVersion, &r.ReviewDecisionID, &displayName, &businessDate, &r.AmountMinor, &currency, &documentID); err != nil {
			return nil, err
		}
		if !documentID.Valid || !displayName.Valid || !businessDate.Valid {
			return nil, domain.NewRuleError("export_source_missing", "正式单据缺少可追溯原件，请检查 "+r.FactID, domain.ErrConflict)
		}
		r.DocumentID, r.DisplayName, r.BusinessDate, r.Currency = documentID.String, displayName.String, businessDate.String, currency.String
		result = append(result, r)
		if len(result) > domain.MaxExportReferences {
			return nil, domain.ExportLimit()
		}
	}
	return result, rows.Err()
}

func readExportFiles(ctx context.Context, q reimbursementQueryer, tenantID string, inventory *ports.ExportInventory) error {
	selected := map[string]bool{}
	for _, r := range inventory.Manifest.References {
		selected[r.DocumentID] = true
	}
	if len(selected) > domain.MaxExportFiles {
		return domain.ExportLimit()
	}
	if len(selected) == 0 {
		return nil
	}
	ids := make([]string, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	args := []any{tenantID}
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, domain.MaxExportFiles+1)
	rows, err := q.QueryContext(ctx, `SELECT id,original_name,detected_mime,size_bytes,sha256,storage_key FROM documents
	 WHERE tenant_id=? AND id IN (`+sqlPlaceholders(len(ids))+`) ORDER BY id LIMIT ?`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var f domain.ExportFile
		var key string
		if err := rows.Scan(&f.DocumentID, &f.OriginalName, &f.MIME, &f.SizeBytes, &f.SHA256, &key); err != nil {
			return err
		}
		inventory.Manifest.Files = append(inventory.Manifest.Files, f)
		inventory.StorageKeys[f.DocumentID] = key
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if inventory.StorageKeys[id] == "" {
			return domain.ExportObjectUnavailable(id)
		}
	}
	return nil
}
