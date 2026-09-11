-- 角色收成两档：管理员（owner）与成员（member）。
-- 这个产品的实际用法是几个人一起记账报销，每个人都要上传、审核、整理；专职
-- 「审核员」「只读成员」「财务」的分工在这里只会让邀请时多想一步、邀错档位。
-- 成员拿原来 finance 的全部业务权限，另加邮箱来源的增删（邮箱是每个人自己的）。
--
-- 历史成员统一并入 member。触发器里写死的角色判断随之改写：函数体从原迁移
-- 原样复制，只把角色元组换掉；reviewer 的自动归属特例随角色一起删除。

UPDATE memberships SET role = 'member' WHERE role IN ('finance', 'reviewer', 'viewer');
UPDATE member_invitations SET role = 'member' WHERE role IN ('finance', 'reviewer', 'viewer');

ALTER TABLE memberships DROP CONSTRAINT memberships_role_check;
ALTER TABLE memberships ADD CONSTRAINT memberships_role_check CHECK (role IN ('owner', 'member'));
ALTER TABLE member_invitations DROP CONSTRAINT member_invitations_role_check;
ALTER TABLE member_invitations ADD CONSTRAINT member_invitations_role_check CHECK (role IN ('owner', 'member'));

-- 来自 0002_manual_trip_workspaces.sql
CREATE OR REPLACE FUNCTION sbm_trip_management_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' AND (NEW.origin_kind <> 'manual' OR NEW.deleted_at IS NOT NULL) THEN
        RAISE EXCEPTION 'invalid_trip_creation_origin' USING ERRCODE = 'P0001';
    END IF;
    IF TG_OP = 'UPDATE' AND (NEW.id <> OLD.id OR NEW.tenant_id <> OLD.tenant_id
        OR NEW.created_at <> OLD.created_at OR NEW.origin_kind <> OLD.origin_kind
        OR NEW.version <> OLD.version + 1 OR OLD.deleted_at IS NOT NULL) THEN
        RAISE EXCEPTION 'trip_management_stale' USING ERRCODE = 'P0001';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM trip_management_decisions d
        JOIN memberships m ON m.tenant_id = d.tenant_id AND m.user_id = d.actor_user_id
        JOIN audit_events a ON a.tenant_id = d.tenant_id AND a.id = d.audit_event_id
        WHERE d.tenant_id = NEW.tenant_id AND d.id = NEW.last_management_decision_id
          AND d.trip_id = NEW.id AND d.resulting_version = NEW.version
          AND d.name = NEW.name AND d.start_date = NEW.start_date AND d.end_date = NEW.end_date
          AND d.timezone IS NOT DISTINCT FROM NEW.timezone AND d.notes = NEW.notes
          AND m.status = 'active' AND m.role IN ('owner', 'member')
          AND a.action = 'trip_workspace_' || d.action AND a.resource_type = 'trip_workspace'
          AND a.resource_id = NEW.id AND a.actor_user_id = d.actor_user_id
          AND ((TG_OP = 'INSERT' AND d.action = 'create' AND d.expected_version = 0)
            OR (TG_OP = 'UPDATE' AND d.expected_version = OLD.version
              AND ((d.action = 'edit' AND NEW.deleted_at IS NULL)
                OR (d.action = 'delete' AND NEW.deleted_at IS NOT NULL AND m.role = 'owner'
                    AND NEW.deletion_audit_event_id = d.audit_event_id AND NEW.deleted_by_user_id = d.actor_user_id))))
    ) THEN
        RAISE EXCEPTION 'trip_management_scope_mismatch' USING ERRCODE = 'P0001';
    END IF;
    IF NEW.timezone IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_timezone_names WHERE name = NEW.timezone) THEN
        RAISE EXCEPTION 'invalid_trip_timezone' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

-- 来自 0002_manual_trip_workspaces.sql
CREATE OR REPLACE FUNCTION sbm_trip_assignment_version_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE fact_version BIGINT; preference TEXT;
BEGIN
    IF NEW.fact_type = 'payment' THEN
        SELECT version, trip_assignment_mode INTO fact_version, preference FROM payments
        WHERE tenant_id = NEW.tenant_id AND id = NEW.payment_id AND deleted_at IS NULL;
    ELSE
        SELECT version INTO fact_version FROM invoices
        WHERE tenant_id = NEW.tenant_id AND id = NEW.invoice_id AND deleted_at IS NULL;
    END IF;
    IF fact_version IS NULL OR NEW.expected_fact_version <> fact_version
      OR NOT EXISTS (SELECT 1 FROM memberships m WHERE m.tenant_id = NEW.tenant_id AND m.user_id = NEW.actor_user_id
        AND m.status = 'active' AND (m.role IN ('owner', 'member')))
      OR (NEW.decision_source = 'automatic' AND (NEW.fact_type <> 'payment' OR preference <> 'auto'
        OR NEW.rule_version IS DISTINCT FROM 'trip-time-attribution/1'))
      OR (NEW.decision_source = 'manual' AND NEW.rule_version IS NOT NULL) THEN
        RAISE EXCEPTION 'trip_assignment_stale' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

-- 来自 0002_manual_trip_workspaces.sql
CREATE OR REPLACE FUNCTION sbm_trip_material_decision_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.decision_source <> 'manual' OR NOT EXISTS (
        SELECT 1 FROM trip_evidence_facts e
        JOIN memberships m ON m.tenant_id = e.tenant_id AND m.user_id = NEW.actor_user_id
        JOIN audit_events a ON a.tenant_id = e.tenant_id AND a.id = NEW.audit_event_id
        WHERE e.tenant_id = NEW.tenant_id AND e.id = NEW.evidence_id AND e.deleted_at IS NULL
          AND e.version = NEW.expected_version AND m.status = 'active' AND m.role IN ('owner', 'member')
          AND a.actor_user_id = NEW.actor_user_id AND a.action = 'trip_material_changed'
          AND a.resource_type = 'trip_evidence' AND a.resource_id = e.id
    ) OR (NEW.desired_trip_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM trips WHERE tenant_id = NEW.tenant_id AND id = NEW.desired_trip_id AND deleted_at IS NULL
    )) OR (NEW.action = 'assign' AND EXISTS (
        SELECT 1 FROM trip_material_links WHERE tenant_id = NEW.tenant_id AND evidence_id = NEW.evidence_id AND ended_at IS NULL
    )) OR (NEW.action IN ('move', 'unassign') AND NOT EXISTS (
        SELECT 1 FROM trip_material_links l WHERE l.tenant_id = NEW.tenant_id AND l.id = NEW.previous_link_id
          AND l.evidence_id = NEW.evidence_id AND l.ended_at IS NULL
          AND (NEW.action = 'unassign' OR l.trip_id <> NEW.desired_trip_id)
    )) THEN RAISE EXCEPTION 'trip_material_stale' USING ERRCODE = 'P0001'; END IF;
    RETURN NEW;
END;
$$;

-- 来自 0004_confirmed_fact_corrections.sql
CREATE OR REPLACE FUNCTION sbm_fact_current_review_guard() RETURNS trigger LANGUAGE plpgsql AS $$
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
          AND actor.status = 'active' AND actor.role IN ('owner', 'member')
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

-- 来自 0004_confirmed_fact_corrections.sql
CREATE OR REPLACE FUNCTION sbm_correction_final_version() RETURNS trigger LANGUAGE plpgsql AS $$
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
          AND actor.status = 'active' AND actor.role IN ('owner', 'member')
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

-- 来自 0006_invoice_supporting_materials.sql
CREATE OR REPLACE FUNCTION sbm_invoice_material_decision_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE current_version BIGINT;
BEGIN
    SELECT version INTO current_version FROM invoices WHERE tenant_id = NEW.tenant_id AND id = NEW.invoice_id AND deleted_at IS NULL FOR UPDATE;
    IF current_version IS DISTINCT FROM NEW.expected_version OR NOT EXISTS (
        SELECT 1 FROM memberships m JOIN audit_events a ON a.tenant_id = m.tenant_id AND a.actor_user_id = m.user_id
        WHERE m.tenant_id = NEW.tenant_id AND m.user_id = NEW.actor_user_id AND m.status = 'active'
          AND m.role IN ('owner', 'member') AND a.id = NEW.audit_event_id
          AND a.resource_type = 'invoice' AND a.resource_id = NEW.invoice_id
          AND a.action = 'invoice_material_' || NEW.action
    ) THEN
        RAISE EXCEPTION 'invoice_material_decision_scope_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

-- 来自 0008_allocation_search_and_bad_debt.sql
CREATE OR REPLACE FUNCTION sbm_bad_debt_decision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE fact_version BIGINT; fact_deleted TIMESTAMPTZ; kind TEXT;
BEGIN
 kind := CASE WHEN NEW.payment_id IS NOT NULL THEN 'payment' ELSE 'invoice' END;
 IF sbm_fact_bad_debt(NEW.tenant_id,kind,coalesce(NEW.payment_id,NEW.invoice_id))=NEW.marked THEN
  RAISE EXCEPTION 'bad_debt_state_unchanged';
 END IF;
 IF NEW.payment_id IS NOT NULL THEN
  SELECT version,deleted_at INTO fact_version,fact_deleted FROM payments WHERE tenant_id=NEW.tenant_id AND id=NEW.payment_id FOR UPDATE;
 ELSE
  SELECT version,deleted_at INTO fact_version,fact_deleted FROM invoices WHERE tenant_id=NEW.tenant_id AND id=NEW.invoice_id FOR UPDATE;
 END IF;
 IF fact_deleted IS NOT NULL OR fact_version IS DISTINCT FROM NEW.expected_version OR NOT EXISTS (
  SELECT 1 FROM audit_events audit JOIN memberships actor ON actor.tenant_id=audit.tenant_id AND actor.user_id=audit.actor_user_id
  WHERE audit.tenant_id=NEW.tenant_id AND audit.id=NEW.audit_event_id AND audit.action='fact_bad_debt_changed'
   AND audit.actor_user_id=NEW.actor_user_id AND audit.resource_type=kind AND audit.resource_id=coalesce(NEW.payment_id,NEW.invoice_id)
   AND actor.status='active' AND actor.role IN ('owner','member')
 ) THEN RAISE EXCEPTION 'bad_debt_decision_invalid'; END IF;
 RETURN NEW;
END;
$$;
