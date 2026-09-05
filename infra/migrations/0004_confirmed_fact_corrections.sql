-- 首次确认保留给历史回放；当前字段版本直接引用已存在的 ReviewDecision 身份。
ALTER TABLE payments ADD COLUMN current_review_decision_id TEXT;
ALTER TABLE invoices ADD COLUMN current_review_decision_id TEXT;
ALTER TABLE trip_evidence_facts ADD COLUMN current_review_decision_id TEXT;
UPDATE payments SET current_review_decision_id = source_review_decision_id;
UPDATE invoices SET current_review_decision_id = source_review_decision_id;
UPDATE trip_evidence_facts SET current_review_decision_id = source_review_decision_id;
ALTER TABLE payments ALTER COLUMN current_review_decision_id SET NOT NULL;
ALTER TABLE invoices ALTER COLUMN current_review_decision_id SET NOT NULL;
ALTER TABLE trip_evidence_facts ALTER COLUMN current_review_decision_id SET NOT NULL;
ALTER TABLE payments ADD FOREIGN KEY (tenant_id, current_review_decision_id) REFERENCES review_decisions(tenant_id, id);
ALTER TABLE invoices ADD FOREIGN KEY (tenant_id, current_review_decision_id) REFERENCES review_decisions(tenant_id, id);
ALTER TABLE trip_evidence_facts ADD FOREIGN KEY (tenant_id, current_review_decision_id) REFERENCES review_decisions(tenant_id, id);

CREATE TABLE fact_corrections (
    tenant_id TEXT NOT NULL,
    review_decision_id TEXT PRIMARY KEY,
    previous_review_decision_id TEXT NOT NULL,
    payment_id TEXT,
    invoice_id TEXT,
    trip_evidence_id TEXT,
    expected_version BIGINT NOT NULL CHECK (expected_version >= 1),
    resulting_version BIGINT NOT NULL CHECK (resulting_version > expected_version),
    request_hash TEXT NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    preview_hash TEXT NOT NULL CHECK (preview_hash ~ '^[0-9a-f]{64}$'),
    audit_event_id TEXT NOT NULL,
    CHECK (num_nonnulls(payment_id, invoice_id, trip_evidence_id) = 1),
    UNIQUE (tenant_id, review_decision_id),
    UNIQUE (tenant_id, previous_review_decision_id),
    FOREIGN KEY (tenant_id, review_decision_id) REFERENCES review_decisions(tenant_id, id),
    FOREIGN KEY (tenant_id, previous_review_decision_id) REFERENCES review_decisions(tenant_id, id),
    FOREIGN KEY (tenant_id, payment_id) REFERENCES payments(tenant_id, id),
    FOREIGN KEY (tenant_id, invoice_id) REFERENCES invoices(tenant_id, id),
    FOREIGN KEY (tenant_id, trip_evidence_id) REFERENCES trip_evidence_facts(tenant_id, id),
    FOREIGN KEY (tenant_id, audit_event_id) REFERENCES audit_events(tenant_id, id)
);
CREATE INDEX fact_corrections_payment_idx ON fact_corrections (tenant_id, payment_id, resulting_version);
CREATE INDEX fact_corrections_invoice_idx ON fact_corrections (tenant_id, invoice_id, resulting_version);
CREATE INDEX fact_corrections_trip_evidence_idx ON fact_corrections (tenant_id, trip_evidence_id, resulting_version);

CREATE FUNCTION sbm_correction_history_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'correction_history_immutable' USING ERRCODE = 'P0001';
END;
$$;
CREATE TRIGGER fact_corrections_immutable BEFORE UPDATE OR DELETE ON fact_corrections
FOR EACH ROW EXECUTE FUNCTION sbm_correction_history_immutable();

CREATE FUNCTION sbm_fact_current_review_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    kind TEXT := TG_ARGV[0];
    ignored TEXT[] := ARRAY['updated_at', 'version', 'deleted_at', 'deleted_by_user_id', 'deletion_audit_event_id', 'trip_assignment_mode'];
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'fact_history_required' USING ERRCODE = 'P0001';
    END IF;
    IF (NEW.id, NEW.tenant_id, NEW.source_review_decision_id, NEW.created_at)
       IS DISTINCT FROM (OLD.id, OLD.tenant_id, OLD.source_review_decision_id, OLD.created_at) THEN
        RAISE EXCEPTION 'fact_identity_immutable' USING ERRCODE = 'P0001';
    END IF;
    IF (to_jsonb(NEW) - ignored) IS NOT DISTINCT FROM (to_jsonb(OLD) - ignored) THEN
        RETURN NEW;
    END IF;
    IF OLD.deleted_at IS NOT NULL OR NEW.deleted_at IS NOT NULL OR NOT EXISTS (
        SELECT 1 FROM fact_corrections correction
        JOIN review_decisions reviewed ON reviewed.tenant_id = correction.tenant_id AND reviewed.id = correction.review_decision_id
        JOIN review_decisions previous ON previous.tenant_id = correction.tenant_id AND previous.id = correction.previous_review_decision_id
        JOIN claim_sets claim ON claim.tenant_id = reviewed.tenant_id AND claim.id = reviewed.claim_set_id
        JOIN claim_sets parent ON parent.tenant_id = previous.tenant_id AND parent.id = previous.claim_set_id
        JOIN memberships actor ON actor.tenant_id = reviewed.tenant_id AND actor.user_id = reviewed.actor_user_id
        JOIN audit_events audit ON audit.tenant_id = correction.tenant_id AND audit.id = correction.audit_event_id
        WHERE correction.tenant_id = NEW.tenant_id AND correction.review_decision_id = NEW.current_review_decision_id
          AND correction.previous_review_decision_id = OLD.current_review_decision_id
          AND coalesce(correction.payment_id, correction.invoice_id, correction.trip_evidence_id) = NEW.id
          AND correction.expected_version = OLD.version AND NEW.version = OLD.version + 1
          AND correction.resulting_version >= NEW.version
          AND reviewed.action = 'confirm' AND reviewed.fact_type = kind AND previous.fact_type = kind
          AND claim.status = 'confirmed' AND claim.document_type = kind
          AND claim.document_id = parent.document_id AND claim.supersedes_claim_set_id = parent.id
          AND claim.revised_by_user_id = reviewed.actor_user_id
          AND actor.status = 'active' AND actor.role IN ('owner', 'finance')
          AND length(btrim(reviewed.reason)) BETWEEN 1 AND 500
          AND audit.action = 'fact_corrected' AND audit.resource_type = kind AND audit.resource_id = NEW.id
          AND audit.actor_user_id = reviewed.actor_user_id
          AND ((kind = 'payment' AND correction.payment_id = NEW.id)
            OR (kind = 'invoice' AND correction.invoice_id = NEW.id)
            OR (kind = 'trip' AND correction.trip_evidence_id = NEW.id))
    ) THEN
        RAISE EXCEPTION 'confirmed_correction_required' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER trip_evidence_fields_immutable ON trip_evidence_facts;
CREATE CONSTRAINT TRIGGER payments_current_review_guard AFTER UPDATE OR DELETE ON payments DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION sbm_fact_current_review_guard('payment');
CREATE CONSTRAINT TRIGGER invoices_current_review_guard AFTER UPDATE OR DELETE ON invoices DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION sbm_fact_current_review_guard('invoice');
CREATE CONSTRAINT TRIGGER trip_evidence_current_review_guard AFTER UPDATE OR DELETE ON trip_evidence_facts DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION sbm_fact_current_review_guard('trip');

CREATE FUNCTION sbm_initial_fact_review() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.current_review_decision_id IS NULL THEN
        NEW.current_review_decision_id := NEW.source_review_decision_id;
    END IF;
    IF NEW.current_review_decision_id <> NEW.source_review_decision_id THEN
        RAISE EXCEPTION 'invalid_initial_fact_revision' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER payments_initial_review BEFORE INSERT ON payments
FOR EACH ROW EXECUTE FUNCTION sbm_initial_fact_review();
CREATE TRIGGER invoices_initial_review BEFORE INSERT ON invoices
FOR EACH ROW EXECUTE FUNCTION sbm_initial_fact_review();
CREATE TRIGGER trip_evidence_initial_review BEFORE INSERT ON trip_evidence_facts
FOR EACH ROW EXECUTE FUNCTION sbm_initial_fact_review();

-- 结果版本在归属重算完成后写入；提交时核对最终聚合，不假设一次纠错只递增一次。
CREATE FUNCTION sbm_correction_final_version() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    current_version BIGINT;
    current_review TEXT;
BEGIN
    IF NEW.review_decision_id = NEW.previous_review_decision_id OR NOT EXISTS (
        SELECT 1 FROM review_decisions reviewed
        JOIN review_decisions previous ON previous.tenant_id = reviewed.tenant_id AND previous.id = NEW.previous_review_decision_id
        JOIN claim_sets claim ON claim.tenant_id = reviewed.tenant_id AND claim.id = reviewed.claim_set_id
        JOIN claim_sets parent ON parent.tenant_id = previous.tenant_id AND parent.id = previous.claim_set_id
        JOIN memberships actor ON actor.tenant_id = reviewed.tenant_id AND actor.user_id = reviewed.actor_user_id
        JOIN audit_events audit ON audit.tenant_id = reviewed.tenant_id AND audit.id = NEW.audit_event_id
        WHERE reviewed.tenant_id = NEW.tenant_id AND reviewed.id = NEW.review_decision_id
          AND reviewed.action = 'confirm' AND previous.action = 'confirm' AND reviewed.fact_type = previous.fact_type
          AND claim.status = 'confirmed' AND parent.status = 'confirmed'
          AND claim.supersedes_claim_set_id = parent.id AND claim.document_id = parent.document_id
          AND claim.revised_by_user_id = reviewed.actor_user_id AND claim.document_type = reviewed.fact_type
          AND actor.status = 'active' AND actor.role IN ('owner', 'finance')
          AND length(btrim(reviewed.reason)) BETWEEN 1 AND 500
          AND audit.action = 'fact_corrected' AND audit.resource_type = reviewed.fact_type
          AND audit.resource_id = coalesce(NEW.payment_id, NEW.invoice_id, NEW.trip_evidence_id)
          AND audit.actor_user_id = reviewed.actor_user_id
          AND ((reviewed.fact_type = 'payment' AND NEW.payment_id IS NOT NULL)
            OR (reviewed.fact_type = 'invoice' AND NEW.invoice_id IS NOT NULL)
            OR (reviewed.fact_type = 'trip' AND NEW.trip_evidence_id IS NOT NULL))
    ) THEN
        RAISE EXCEPTION 'correction_identity_mismatch' USING ERRCODE = 'P0001';
    END IF;
    IF NEW.payment_id IS NOT NULL THEN
        SELECT version, current_review_decision_id INTO current_version, current_review FROM payments
        WHERE tenant_id = NEW.tenant_id AND id = NEW.payment_id;
    ELSIF NEW.invoice_id IS NOT NULL THEN
        SELECT version, current_review_decision_id INTO current_version, current_review FROM invoices
        WHERE tenant_id = NEW.tenant_id AND id = NEW.invoice_id;
    ELSE
        SELECT version, current_review_decision_id INTO current_version, current_review FROM trip_evidence_facts
        WHERE tenant_id = NEW.tenant_id AND id = NEW.trip_evidence_id;
    END IF;
    IF current_version IS DISTINCT FROM NEW.resulting_version OR current_review IS DISTINCT FROM NEW.review_decision_id THEN
        RAISE EXCEPTION 'correction_final_version_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
CREATE CONSTRAINT TRIGGER correction_final_version AFTER INSERT ON fact_corrections DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION sbm_correction_final_version();

-- 明细和字段来源按确认版本分组，旧行和来源 ID 始终保留。
ALTER TABLE invoice_items ADD COLUMN review_decision_id TEXT;
UPDATE invoice_items item SET review_decision_id = invoice.source_review_decision_id
FROM invoices invoice WHERE invoice.tenant_id = item.tenant_id AND invoice.id = item.invoice_id;
ALTER TABLE invoice_items ALTER COLUMN review_decision_id SET NOT NULL;
ALTER TABLE invoice_items ADD FOREIGN KEY (tenant_id, review_decision_id) REFERENCES review_decisions(tenant_id, id);
ALTER TABLE invoice_items DROP CONSTRAINT invoice_items_tenant_id_invoice_id_item_key_key;
ALTER TABLE invoice_items ADD UNIQUE (tenant_id, invoice_id, review_decision_id, item_key);
ALTER TABLE fact_field_origins DROP CONSTRAINT fact_field_origins_tenant_id_payment_id_field_path_key;
ALTER TABLE fact_field_origins DROP CONSTRAINT fact_field_origins_tenant_id_invoice_id_field_path_key;
ALTER TABLE fact_field_origins DROP CONSTRAINT fact_field_origins_tenant_id_trip_id_field_path_key;
ALTER TABLE fact_field_origins ADD UNIQUE (tenant_id, payment_id, review_decision_id, field_path);
ALTER TABLE fact_field_origins ADD UNIQUE (tenant_id, invoice_id, review_decision_id, field_path);
ALTER TABLE fact_field_origins ADD UNIQUE (tenant_id, trip_id, review_decision_id, field_path);

CREATE FUNCTION sbm_invoice_item_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.review_decision_id IS NULL THEN
        SELECT current_review_decision_id INTO NEW.review_decision_id FROM invoices
        WHERE tenant_id = NEW.tenant_id AND id = NEW.invoice_id;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM invoices invoice
        JOIN review_decisions first_review ON first_review.tenant_id = invoice.tenant_id AND first_review.id = invoice.source_review_decision_id
        JOIN claim_sets first_claim ON first_claim.tenant_id = first_review.tenant_id AND first_claim.id = first_review.claim_set_id
        JOIN review_decisions reviewed ON reviewed.tenant_id = invoice.tenant_id AND reviewed.id = NEW.review_decision_id
        JOIN claim_sets claim ON claim.tenant_id = reviewed.tenant_id AND claim.id = reviewed.claim_set_id
        WHERE invoice.tenant_id = NEW.tenant_id AND invoice.id = NEW.invoice_id
          AND reviewed.action = 'confirm' AND reviewed.fact_type = 'invoice' AND claim.document_id = first_claim.document_id
          AND (reviewed.id = invoice.source_review_decision_id OR reviewed.id = invoice.current_review_decision_id
            OR EXISTS (SELECT 1 FROM fact_corrections correction WHERE correction.tenant_id = invoice.tenant_id
              AND correction.invoice_id = invoice.id AND correction.review_decision_id = reviewed.id))
    ) THEN
        RAISE EXCEPTION 'invoice_item_revision_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER invoice_item_revision BEFORE INSERT ON invoice_items
FOR EACH ROW EXECUTE FUNCTION sbm_invoice_item_revision();
CREATE TRIGGER invoice_item_history_immutable BEFORE UPDATE OR DELETE ON invoice_items
FOR EACH ROW EXECUTE FUNCTION sbm_correction_history_immutable();
CREATE TRIGGER fact_field_origins_immutable BEFORE UPDATE OR DELETE ON fact_field_origins
FOR EACH ROW EXECUTE FUNCTION sbm_correction_history_immutable();

-- 旧报销项保持 NULL 和原 snapshot hash；新项由当前用例显式记录修订身份。
ALTER TABLE reimbursement_items ADD COLUMN fact_review_decision_id TEXT;
ALTER TABLE reimbursement_items ADD FOREIGN KEY (tenant_id, fact_review_decision_id) REFERENCES review_decisions(tenant_id, id);

CREATE FUNCTION sbm_fact_origin_revision_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM payments fact WHERE NEW.payment_id IS NOT NULL AND fact.tenant_id = NEW.tenant_id AND fact.id = NEW.payment_id
          AND (fact.source_review_decision_id = NEW.review_decision_id OR fact.current_review_decision_id = NEW.review_decision_id
            OR EXISTS (SELECT 1 FROM fact_corrections c WHERE c.tenant_id = fact.tenant_id AND c.payment_id = fact.id AND c.review_decision_id = NEW.review_decision_id))
        UNION ALL
        SELECT 1 FROM invoices fact WHERE NEW.invoice_id IS NOT NULL AND fact.tenant_id = NEW.tenant_id AND fact.id = NEW.invoice_id
          AND (fact.source_review_decision_id = NEW.review_decision_id OR fact.current_review_decision_id = NEW.review_decision_id
            OR EXISTS (SELECT 1 FROM fact_corrections c WHERE c.tenant_id = fact.tenant_id AND c.invoice_id = fact.id AND c.review_decision_id = NEW.review_decision_id))
        UNION ALL
        SELECT 1 FROM trip_evidence_facts fact WHERE NEW.trip_id IS NOT NULL AND fact.tenant_id = NEW.tenant_id AND fact.id = NEW.trip_id
          AND (fact.source_review_decision_id = NEW.review_decision_id OR fact.current_review_decision_id = NEW.review_decision_id
            OR EXISTS (SELECT 1 FROM fact_corrections c WHERE c.tenant_id = fact.tenant_id AND c.trip_evidence_id = fact.id AND c.review_decision_id = NEW.review_decision_id))
        UNION ALL
        SELECT 1 FROM invoice_items item WHERE NEW.invoice_item_id IS NOT NULL AND item.tenant_id = NEW.tenant_id
          AND item.id = NEW.invoice_item_id AND item.review_decision_id = NEW.review_decision_id
    ) THEN
        RAISE EXCEPTION 'fact_origin_revision_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER fact_origin_revision_scope BEFORE INSERT ON fact_field_origins
FOR EACH ROW EXECUTE FUNCTION sbm_fact_origin_revision_scope();

CREATE FUNCTION sbm_reimbursement_item_revision_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.fact_review_decision_id IS NULL OR NOT EXISTS (
        SELECT 1 FROM payments fact WHERE NEW.fact_type = 'payment' AND fact.tenant_id = NEW.tenant_id
          AND fact.id = NEW.payment_id AND fact.current_review_decision_id = NEW.fact_review_decision_id
        UNION ALL
        SELECT 1 FROM invoices fact WHERE NEW.fact_type = 'invoice' AND fact.tenant_id = NEW.tenant_id
          AND fact.id = NEW.invoice_id AND fact.current_review_decision_id = NEW.fact_review_decision_id
    ) THEN
        RAISE EXCEPTION 'reimbursement_fact_revision_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER reimbursement_item_revision_scope BEFORE INSERT ON reimbursement_items
FOR EACH ROW EXECUTE FUNCTION sbm_reimbursement_item_revision_scope();
