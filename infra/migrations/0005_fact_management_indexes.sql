-- 业务日期可被显式纠错；管理分页使用不可变的确认入库顺序。
CREATE INDEX payments_tenant_created_active_idx
ON payments (tenant_id, created_at DESC, id DESC)
WHERE deleted_at IS NULL;

CREATE INDEX invoices_tenant_created_active_idx
ON invoices (tenant_id, created_at DESC, id DESC)
WHERE deleted_at IS NULL;
