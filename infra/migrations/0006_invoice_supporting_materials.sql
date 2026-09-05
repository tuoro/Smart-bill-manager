-- 辅助材料复用不可变 Document；不创建识别任务或第二对象身份。
CREATE TABLE invoice_material_decisions (
    id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, invoice_id TEXT NOT NULL,
    document_id TEXT NOT NULL, link_id TEXT NOT NULL, actor_user_id TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('upload', 'add', 'remove')),
    expected_version BIGINT NOT NULL CHECK (expected_version >= 1),
    resulting_version BIGINT NOT NULL CHECK (resulting_version = expected_version + 1),
    reason TEXT NOT NULL CHECK (length(trim(reason)) BETWEEN 1 AND 500),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 8 AND 128),
    request_hash TEXT NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    audit_event_id TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (tenant_id, id), CONSTRAINT invoice_material_request_key UNIQUE (tenant_id, idempotency_key),
    UNIQUE (tenant_id, invoice_id, resulting_version),
    FOREIGN KEY (tenant_id, invoice_id) REFERENCES invoices(tenant_id, id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES documents(tenant_id, id),
    FOREIGN KEY (tenant_id, actor_user_id) REFERENCES memberships(tenant_id, user_id),
    FOREIGN KEY (tenant_id, audit_event_id) REFERENCES audit_events(tenant_id, id)
);
CREATE TABLE invoice_material_links (
    id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, invoice_id TEXT NOT NULL, document_id TEXT NOT NULL,
    created_by_decision_id TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ, ended_by_decision_id TEXT, ended_by_audit_event_id TEXT,
    CHECK ((ended_at IS NULL AND ended_by_decision_id IS NULL AND ended_by_audit_event_id IS NULL)
        OR (ended_at IS NOT NULL AND num_nonnulls(ended_by_decision_id, ended_by_audit_event_id) = 1)),
    UNIQUE (tenant_id, id), UNIQUE (tenant_id, created_by_decision_id),
    UNIQUE (tenant_id, id, invoice_id, document_id),
    FOREIGN KEY (tenant_id, invoice_id) REFERENCES invoices(tenant_id, id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES documents(tenant_id, id),
    FOREIGN KEY (tenant_id, created_by_decision_id) REFERENCES invoice_material_decisions(tenant_id, id),
    FOREIGN KEY (tenant_id, ended_by_decision_id) REFERENCES invoice_material_decisions(tenant_id, id),
    FOREIGN KEY (tenant_id, ended_by_audit_event_id) REFERENCES audit_events(tenant_id, id)
);
ALTER TABLE invoice_material_decisions ADD FOREIGN KEY (tenant_id, link_id)
    REFERENCES invoice_material_links(tenant_id, id) DEFERRABLE INITIALLY DEFERRED;
CREATE UNIQUE INDEX invoice_material_active_pair ON invoice_material_links(tenant_id, invoice_id, document_id) WHERE ended_at IS NULL;
CREATE INDEX invoice_material_document_history ON invoice_material_links(tenant_id, document_id);
CREATE INDEX documents_material_order ON documents(tenant_id, created_at DESC, id DESC);

CREATE FUNCTION sbm_invoice_material_history_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'invoice_material_history_immutable' USING ERRCODE = 'P0001';
END;
$$;
CREATE TRIGGER invoice_material_decisions_immutable BEFORE UPDATE OR DELETE ON invoice_material_decisions
    FOR EACH ROW EXECUTE FUNCTION sbm_invoice_material_history_immutable();

CREATE FUNCTION sbm_invoice_material_decision_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE current_version BIGINT;
BEGIN
    SELECT version INTO current_version FROM invoices WHERE tenant_id = NEW.tenant_id AND id = NEW.invoice_id AND deleted_at IS NULL FOR UPDATE;
    IF current_version IS DISTINCT FROM NEW.expected_version OR NOT EXISTS (
        SELECT 1 FROM memberships m JOIN audit_events a ON a.tenant_id = m.tenant_id AND a.actor_user_id = m.user_id
        WHERE m.tenant_id = NEW.tenant_id AND m.user_id = NEW.actor_user_id AND m.status = 'active'
          AND m.role IN ('owner', 'finance') AND a.id = NEW.audit_event_id
          AND a.resource_type = 'invoice' AND a.resource_id = NEW.invoice_id
          AND a.action = 'invoice_material_' || NEW.action
    ) THEN
        RAISE EXCEPTION 'invoice_material_decision_scope_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER invoice_material_decision_scope BEFORE INSERT ON invoice_material_decisions
    FOR EACH ROW EXECUTE FUNCTION sbm_invoice_material_decision_scope();

CREATE FUNCTION sbm_invoice_material_link_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'invoice_material_history_immutable' USING ERRCODE = 'P0001';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.ended_at IS NOT NULL OR NOT EXISTS (
            SELECT 1 FROM invoice_material_decisions d WHERE d.tenant_id = NEW.tenant_id AND d.id = NEW.created_by_decision_id
              AND d.action IN ('upload', 'add') AND d.invoice_id = NEW.invoice_id AND d.document_id = NEW.document_id
              AND d.link_id = NEW.id AND d.created_at = NEW.created_at
        ) OR (SELECT count(*) FROM invoice_material_links WHERE tenant_id = NEW.tenant_id AND invoice_id = NEW.invoice_id AND ended_at IS NULL) >= 100
        THEN RAISE EXCEPTION 'invoice_material_link_scope_mismatch' USING ERRCODE = 'P0001'; END IF;
    ELSE
        IF OLD.ended_at IS NOT NULL OR NEW.ended_at IS NULL OR
           (to_jsonb(NEW) - ARRAY['ended_at', 'ended_by_decision_id', 'ended_by_audit_event_id']) IS DISTINCT FROM
           (to_jsonb(OLD) - ARRAY['ended_at', 'ended_by_decision_id', 'ended_by_audit_event_id']) THEN
            RAISE EXCEPTION 'invoice_material_history_immutable' USING ERRCODE = 'P0001';
        END IF;
        IF NOT EXISTS (SELECT 1 FROM invoice_material_decisions d WHERE d.tenant_id = NEW.tenant_id AND d.id = NEW.ended_by_decision_id
            AND d.action = 'remove' AND d.invoice_id = NEW.invoice_id AND d.document_id = NEW.document_id AND d.link_id = NEW.id AND d.created_at = NEW.ended_at)
           AND NOT EXISTS (SELECT 1 FROM audit_events a JOIN invoices i ON i.tenant_id = a.tenant_id AND i.id = a.resource_id
            WHERE a.tenant_id = NEW.tenant_id AND a.id = NEW.ended_by_audit_event_id AND a.resource_type = 'invoice'
              AND a.action = 'fact_deleted' AND i.id = NEW.invoice_id AND i.deleted_at IS NOT NULL AND i.deletion_audit_event_id = a.id)
        THEN RAISE EXCEPTION 'invoice_material_end_scope_mismatch' USING ERRCODE = 'P0001'; END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER invoice_material_link_scope BEFORE INSERT OR UPDATE OR DELETE ON invoice_material_links
    FOR EACH ROW EXECUTE FUNCTION sbm_invoice_material_link_scope();

-- 决定只有在对应关系效果和聚合版本一并提交时才成立。
CREATE FUNCTION sbm_invoice_material_decision_effect() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM invoices i JOIN invoice_material_links l
        ON l.tenant_id = i.tenant_id AND l.invoice_id = i.id
        WHERE i.tenant_id = NEW.tenant_id AND i.id = NEW.invoice_id AND i.version >= NEW.resulting_version
          AND l.id = NEW.link_id AND l.document_id = NEW.document_id
          AND ((NEW.action IN ('upload', 'add') AND l.created_by_decision_id = NEW.id)
            OR (NEW.action = 'remove' AND l.ended_by_decision_id = NEW.id))) THEN
        RAISE EXCEPTION 'invoice_material_decision_effect_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
CREATE CONSTRAINT TRIGGER invoice_material_decision_effect AFTER INSERT ON invoice_material_decisions
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION sbm_invoice_material_decision_effect();

-- 已有报销不能被当前材料回填；之后的提交明确捕获空或非空集合。
ALTER TABLE reimbursements ADD COLUMN materials_captured BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE reimbursements ALTER COLUMN materials_captured SET DEFAULT TRUE;
ALTER TABLE reimbursements ADD COLUMN material_count BIGINT;
ALTER TABLE reimbursements ALTER COLUMN material_count SET DEFAULT 0;
ALTER TABLE reimbursements ADD CHECK (
    (NOT materials_captured AND material_count IS NULL AND policy_rule_version = 'reimbursement-policy/1')
    OR (materials_captured AND material_count BETWEEN 0 AND 20000 AND policy_rule_version = 'reimbursement-policy/2'));
ALTER TABLE reimbursements DROP CONSTRAINT reimbursements_policy_rule_version_check;
ALTER TABLE reimbursements ADD CHECK (policy_rule_version IN ('reimbursement-policy/1', 'reimbursement-policy/2'));
ALTER TABLE reimbursement_policy_findings DROP CONSTRAINT reimbursement_policy_findings_rule_version_check;
ALTER TABLE reimbursement_policy_findings ADD CHECK (rule_version IN ('reimbursement-policy/1', 'reimbursement-policy/2'));
CREATE TRIGGER reimbursements_material_capture_immutable BEFORE UPDATE OF materials_captured, material_count ON reimbursements
    FOR EACH ROW EXECUTE FUNCTION sbm_invoice_material_history_immutable();

CREATE TABLE reimbursement_material_snapshots (
    tenant_id TEXT NOT NULL, reimbursement_id TEXT NOT NULL, invoice_id TEXT NOT NULL,
    link_id TEXT NOT NULL, document_id TEXT NOT NULL,
    PRIMARY KEY (tenant_id, reimbursement_id, link_id),
    FOREIGN KEY (tenant_id, reimbursement_id, invoice_id) REFERENCES reimbursement_items(tenant_id, reimbursement_id, invoice_id),
    FOREIGN KEY (tenant_id, link_id, invoice_id, document_id) REFERENCES invoice_material_links(tenant_id, id, invoice_id, document_id)
);
CREATE FUNCTION sbm_reimbursement_material_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM reimbursements r JOIN invoice_material_links l ON l.tenant_id = r.tenant_id
        WHERE r.tenant_id = NEW.tenant_id AND r.id = NEW.reimbursement_id AND r.materials_captured
          AND r.status = 'submitted' AND r.version = 1 AND l.id = NEW.link_id AND l.ended_at IS NULL) THEN
        RAISE EXCEPTION 'reimbursement_material_scope_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER reimbursement_material_scope BEFORE INSERT ON reimbursement_material_snapshots
    FOR EACH ROW EXECUTE FUNCTION sbm_reimbursement_material_scope();
CREATE TRIGGER reimbursement_material_immutable BEFORE UPDATE OR DELETE ON reimbursement_material_snapshots
    FOR EACH ROW EXECUTE FUNCTION sbm_invoice_material_history_immutable();

CREATE FUNCTION sbm_reimbursement_material_count() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target_id TEXT; expected_count BIGINT; actual_count BIGINT;
BEGIN
    IF TG_TABLE_NAME = 'reimbursements' THEN target_id := NEW.id; ELSE target_id := NEW.reimbursement_id; END IF;
    SELECT material_count INTO expected_count FROM reimbursements WHERE tenant_id = NEW.tenant_id AND id = target_id AND materials_captured;
    SELECT count(*) INTO actual_count FROM reimbursement_material_snapshots WHERE tenant_id = NEW.tenant_id AND reimbursement_id = target_id;
    IF expected_count IS DISTINCT FROM actual_count THEN
        RAISE EXCEPTION 'reimbursement_material_count_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
CREATE CONSTRAINT TRIGGER reimbursement_material_count_on_submit AFTER INSERT ON reimbursements
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.materials_captured) EXECUTE FUNCTION sbm_reimbursement_material_count();
CREATE CONSTRAINT TRIGGER reimbursement_material_count_on_material AFTER INSERT ON reimbursement_material_snapshots
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION sbm_reimbursement_material_count();
