CREATE INDEX payments_tenant_business_date_active_idx
ON payments (tenant_id, business_date DESC, currency, id DESC)
WHERE deleted_at IS NULL;

CREATE INDEX invoices_tenant_insight_active_idx
ON invoices (tenant_id, invoice_date DESC, currency, id DESC)
WHERE deleted_at IS NULL;

INSERT INTO schema_migrations (version, name, applied_at)
VALUES (2, 'fact_insight_indexes', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
