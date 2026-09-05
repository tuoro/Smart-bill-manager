-- 保留已发布事实的完整来源；费用容器不再充当票面 Fact。
CREATE TABLE trip_evidence_facts (LIKE trips INCLUDING ALL);
INSERT INTO trip_evidence_facts SELECT * FROM trips;
ALTER TABLE trip_evidence_facts
    ADD FOREIGN KEY (tenant_id, source_review_decision_id) REFERENCES review_decisions(tenant_id, id),
    ADD FOREIGN KEY (tenant_id, deleted_by_user_id) REFERENCES memberships(tenant_id, user_id),
    ADD FOREIGN KEY (tenant_id, deletion_audit_event_id) REFERENCES audit_events(tenant_id, id);
ALTER TABLE fact_field_origins DROP CONSTRAINT fk_fact_field_origins_4;
ALTER TABLE fact_field_origins ADD CONSTRAINT fk_fact_field_origins_trip_evidence
    FOREIGN KEY (tenant_id, trip_id) REFERENCES trip_evidence_facts(tenant_id, id);
DROP TRIGGER trips_require_confirmed_review ON trips;
CREATE TRIGGER trip_evidence_require_confirmed_review BEFORE INSERT ON trip_evidence_facts
    FOR EACH ROW EXECUTE FUNCTION sbm_t_57_trips_require_confirmed_review();

ALTER TABLE trips ADD COLUMN name TEXT;
UPDATE trips SET name = trim(destination);
ALTER TABLE trips ALTER COLUMN name SET NOT NULL;
ALTER TABLE trips ADD COLUMN timezone TEXT;
ALTER TABLE trips ADD COLUMN notes TEXT NOT NULL DEFAULT '';
ALTER TABLE trips ADD COLUMN origin_kind TEXT NOT NULL DEFAULT 'manual';
UPDATE trips SET origin_kind = 'migrated_review';
ALTER TABLE trips ADD COLUMN last_management_decision_id TEXT;
ALTER TABLE trips
    DROP COLUMN source_review_decision_id,
    DROP COLUMN origin,
    DROP COLUMN destination,
    DROP COLUMN traveler_name,
    DROP COLUMN transport_type,
    DROP COLUMN booking_reference,
    ADD CHECK (name = trim(name) AND length(name) BETWEEN 1 AND 500),
    ADD CHECK (length(notes) <= 2000),
    ADD CHECK (origin_kind IN ('manual', 'migrated_review')),
    ADD CHECK (origin_kind = 'migrated_review' OR timezone IS NOT NULL);

CREATE TABLE trip_management_decisions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    trip_id TEXT NOT NULL,
    actor_user_id TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('create', 'edit', 'delete')),
    expected_version BIGINT NOT NULL CHECK (expected_version >= 0),
    resulting_version BIGINT NOT NULL CHECK (resulting_version = expected_version + 1),
    name TEXT NOT NULL,
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    timezone TEXT,
    notes TEXT NOT NULL,
    reason TEXT NOT NULL CHECK (length(trim(reason)) BETWEEN 1 AND 500),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 8 AND 128),
    request_hash TEXT NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    audit_event_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, idempotency_key),
    UNIQUE (tenant_id, trip_id, resulting_version),
    FOREIGN KEY (tenant_id, trip_id) REFERENCES trips(tenant_id, id) DEFERRABLE INITIALLY DEFERRED,
    FOREIGN KEY (tenant_id, actor_user_id) REFERENCES memberships(tenant_id, user_id),
    FOREIGN KEY (tenant_id, audit_event_id) REFERENCES audit_events(tenant_id, id)
);
ALTER TABLE trips ADD FOREIGN KEY (tenant_id, last_management_decision_id)
    REFERENCES trip_management_decisions(tenant_id, id);

CREATE FUNCTION sbm_trip_history_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'trip_history_immutable' USING ERRCODE = 'P0001';
END;
$$;
CREATE TRIGGER trip_management_history_immutable BEFORE UPDATE OR DELETE ON trip_management_decisions
    FOR EACH ROW EXECUTE FUNCTION sbm_trip_history_immutable();
CREATE TRIGGER trip_evidence_fields_immutable
    BEFORE UPDATE OF id, tenant_id, source_review_decision_id, origin, destination,
        start_date, end_date, traveler_name, transport_type, booking_reference, created_at
    OR DELETE ON trip_evidence_facts
    FOR EACH ROW EXECUTE FUNCTION sbm_trip_history_immutable();

CREATE FUNCTION sbm_trip_management_scope() RETURNS trigger LANGUAGE plpgsql AS $$
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
          AND m.status = 'active' AND m.role IN ('owner', 'finance')
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
CREATE TRIGGER trip_management_scope BEFORE INSERT OR UPDATE ON trips
    FOR EACH ROW EXECUTE FUNCTION sbm_trip_management_scope();
CREATE TRIGGER trips_history_delete_forbidden BEFORE DELETE ON trips
    FOR EACH ROW EXECUTE FUNCTION sbm_trip_history_immutable();

-- 版本属于聚合并发状态，自动/人工归属不改变票面字段。
ALTER TABLE payments ADD COLUMN trip_assignment_mode TEXT NOT NULL DEFAULT 'auto'
    CHECK (trip_assignment_mode IN ('auto', 'manual', 'blocked'));
UPDATE payments p SET trip_assignment_mode = CASE WHEN EXISTS (
    SELECT 1 FROM trip_fact_assignments a WHERE a.tenant_id = p.tenant_id AND a.payment_id = p.id AND a.ended_at IS NULL
) THEN 'manual' ELSE 'blocked' END
WHERE EXISTS (SELECT 1 FROM trip_fact_assignment_decisions d WHERE d.tenant_id = p.tenant_id AND d.payment_id = p.id);
ALTER TABLE trip_fact_assignment_decisions
    ADD COLUMN decision_source TEXT NOT NULL DEFAULT 'manual' CHECK (decision_source IN ('manual', 'automatic')),
    ADD COLUMN expected_fact_version BIGINT NOT NULL DEFAULT 0 CHECK (expected_fact_version >= 0),
    ADD COLUMN rule_version TEXT;
CREATE FUNCTION sbm_trip_assignment_version_scope() RETURNS trigger LANGUAGE plpgsql AS $$
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
        AND m.status = 'active' AND (m.role IN ('owner', 'finance') OR (NEW.decision_source = 'automatic' AND m.role = 'reviewer')))
      OR (NEW.decision_source = 'automatic' AND (NEW.fact_type <> 'payment' OR preference <> 'auto'
        OR NEW.rule_version IS DISTINCT FROM 'trip-time-attribution/1'))
      OR (NEW.decision_source = 'manual' AND NEW.rule_version IS NOT NULL) THEN
        RAISE EXCEPTION 'trip_assignment_stale' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trip_assignment_version_scope BEFORE INSERT ON trip_fact_assignment_decisions
    FOR EACH ROW EXECUTE FUNCTION sbm_trip_assignment_version_scope();

CREATE TABLE trip_material_decisions (
    id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, evidence_id TEXT NOT NULL,
    actor_user_id TEXT, decision_source TEXT NOT NULL CHECK (decision_source IN ('manual', 'migration')),
    previous_link_id TEXT, desired_trip_id TEXT,
    expected_version BIGINT NOT NULL CHECK (expected_version >= 1),
    action TEXT NOT NULL CHECK (action IN ('assign', 'move', 'unassign')),
    reason TEXT NOT NULL CHECK (length(trim(reason)) BETWEEN 1 AND 500),
    idempotency_key TEXT NOT NULL, request_hash TEXT NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    audit_event_id TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL,
    CHECK ((action = 'assign' AND previous_link_id IS NULL AND desired_trip_id IS NOT NULL)
        OR (action = 'move' AND previous_link_id IS NOT NULL AND desired_trip_id IS NOT NULL)
        OR (action = 'unassign' AND previous_link_id IS NOT NULL AND desired_trip_id IS NULL)),
    CHECK ((decision_source = 'manual' AND actor_user_id IS NOT NULL)
        OR (decision_source = 'migration' AND actor_user_id IS NULL)),
    UNIQUE (tenant_id, id), UNIQUE (tenant_id, idempotency_key),
    FOREIGN KEY (tenant_id, evidence_id) REFERENCES trip_evidence_facts(tenant_id, id),
    FOREIGN KEY (tenant_id, actor_user_id) REFERENCES memberships(tenant_id, user_id),
    FOREIGN KEY (tenant_id, desired_trip_id) REFERENCES trips(tenant_id, id),
    FOREIGN KEY (tenant_id, audit_event_id) REFERENCES audit_events(tenant_id, id)
);
CREATE TABLE trip_material_links (
    id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, trip_id TEXT NOT NULL, evidence_id TEXT NOT NULL,
    created_by_decision_id TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ, ended_by_decision_id TEXT, ended_by_audit_event_id TEXT,
    CHECK ((ended_at IS NULL AND ended_by_decision_id IS NULL AND ended_by_audit_event_id IS NULL)
        OR (ended_at IS NOT NULL AND num_nonnulls(ended_by_decision_id, ended_by_audit_event_id) = 1)),
    UNIQUE (tenant_id, id), UNIQUE (tenant_id, created_by_decision_id),
    FOREIGN KEY (tenant_id, trip_id) REFERENCES trips(tenant_id, id),
    FOREIGN KEY (tenant_id, evidence_id) REFERENCES trip_evidence_facts(tenant_id, id),
    FOREIGN KEY (tenant_id, created_by_decision_id) REFERENCES trip_material_decisions(tenant_id, id),
    FOREIGN KEY (tenant_id, ended_by_decision_id) REFERENCES trip_material_decisions(tenant_id, id),
    FOREIGN KEY (tenant_id, ended_by_audit_event_id) REFERENCES audit_events(tenant_id, id)
);
ALTER TABLE trip_material_decisions ADD FOREIGN KEY (tenant_id, previous_link_id)
    REFERENCES trip_material_links(tenant_id, id);
CREATE UNIQUE INDEX trip_material_active_evidence ON trip_material_links(tenant_id, evidence_id) WHERE ended_at IS NULL;
CREATE INDEX trip_material_active_container ON trip_material_links(tenant_id, trip_id, id) WHERE ended_at IS NULL;

-- 原容器/凭证的对应关系有明确迁移来源，不伪装为新增人工决定。
INSERT INTO audit_events (id, tenant_id, actor_user_id, action, resource_type, resource_id, request_id, safe_metadata_json, created_at)
SELECT md5('trip-material-audit:' || id)::uuid::text, tenant_id, NULL, 'trip_material_migrated', 'trip', id,
    'migration-0002', '{"source":"migration"}'::jsonb, CURRENT_TIMESTAMP FROM trips WHERE deleted_at IS NULL;
INSERT INTO trip_material_decisions
    (id, tenant_id, evidence_id, decision_source, desired_trip_id, expected_version, action,
     reason, idempotency_key, request_hash, audit_event_id, created_at)
SELECT md5('trip-material-decision:' || id)::uuid::text, tenant_id, id, 'migration', id, version, 'assign',
    '保留已发布行程与其审核凭证的原始对应关系', 'migration-0002:' || id,
    encode(sha256(convert_to('migration-0002:' || id, 'UTF8')), 'hex'),
    md5('trip-material-audit:' || id)::uuid::text, CURRENT_TIMESTAMP FROM trips WHERE deleted_at IS NULL;
INSERT INTO trip_material_links (id, tenant_id, trip_id, evidence_id, created_by_decision_id, created_at)
SELECT md5('trip-material-link:' || id)::uuid::text, tenant_id, id, id,
    md5('trip-material-decision:' || id)::uuid::text, CURRENT_TIMESTAMP FROM trips WHERE deleted_at IS NULL;
CREATE TRIGGER trip_material_decisions_immutable BEFORE UPDATE OR DELETE ON trip_material_decisions
    FOR EACH ROW EXECUTE FUNCTION sbm_trip_history_immutable();

CREATE FUNCTION sbm_trip_material_decision_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.decision_source <> 'manual' OR NOT EXISTS (
        SELECT 1 FROM trip_evidence_facts e
        JOIN memberships m ON m.tenant_id = e.tenant_id AND m.user_id = NEW.actor_user_id
        JOIN audit_events a ON a.tenant_id = e.tenant_id AND a.id = NEW.audit_event_id
        WHERE e.tenant_id = NEW.tenant_id AND e.id = NEW.evidence_id AND e.deleted_at IS NULL
          AND e.version = NEW.expected_version AND m.status = 'active' AND m.role IN ('owner', 'finance')
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
CREATE TRIGGER trip_material_decision_scope BEFORE INSERT ON trip_material_decisions
    FOR EACH ROW EXECUTE FUNCTION sbm_trip_material_decision_scope();

CREATE FUNCTION sbm_trip_material_link_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.ended_at IS NOT NULL OR NOT EXISTS (
            SELECT 1 FROM trip_material_decisions d
            JOIN trips t ON t.tenant_id = d.tenant_id AND t.id = d.desired_trip_id AND t.deleted_at IS NULL
            JOIN trip_evidence_facts e ON e.tenant_id = d.tenant_id AND e.id = d.evidence_id AND e.deleted_at IS NULL
            WHERE d.tenant_id = NEW.tenant_id AND d.id = NEW.created_by_decision_id
              AND d.desired_trip_id = NEW.trip_id AND d.evidence_id = NEW.evidence_id
              AND d.action IN ('assign', 'move') AND d.decision_source = 'manual'
              AND (d.action = 'assign' OR EXISTS (
                  SELECT 1 FROM trip_material_links previous WHERE previous.tenant_id = d.tenant_id
                    AND previous.id = d.previous_link_id AND previous.ended_at IS NOT NULL
                    AND previous.ended_by_decision_id = d.id
              ))
        ) THEN RAISE EXCEPTION 'trip_material_creation_mismatch' USING ERRCODE = 'P0001'; END IF;
    ELSE
        IF OLD.ended_at IS NOT NULL OR NEW.ended_at IS NULL OR
           ROW(NEW.id, NEW.tenant_id, NEW.trip_id, NEW.evidence_id, NEW.created_by_decision_id, NEW.created_at)
           IS DISTINCT FROM ROW(OLD.id, OLD.tenant_id, OLD.trip_id, OLD.evidence_id, OLD.created_by_decision_id, OLD.created_at)
           OR NOT (
               (NEW.ended_by_decision_id IS NOT NULL AND EXISTS (
                   SELECT 1 FROM trip_material_decisions d WHERE d.tenant_id = OLD.tenant_id
                     AND d.id = NEW.ended_by_decision_id AND d.previous_link_id = OLD.id AND d.action IN ('move', 'unassign')
               )) OR (NEW.ended_by_audit_event_id IS NOT NULL AND EXISTS (
                   SELECT 1 FROM audit_events a WHERE a.tenant_id = OLD.tenant_id AND a.id = NEW.ended_by_audit_event_id
                     AND ((a.action = 'trip_workspace_delete' AND a.resource_type = 'trip_workspace' AND a.resource_id = OLD.trip_id)
                       OR (a.action = 'fact_deleted' AND a.resource_type = 'trip_evidence' AND a.resource_id = OLD.evidence_id))
               ))
           ) THEN RAISE EXCEPTION 'trip_material_end_mismatch' USING ERRCODE = 'P0001'; END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trip_material_link_scope BEFORE INSERT OR UPDATE ON trip_material_links
    FOR EACH ROW EXECUTE FUNCTION sbm_trip_material_link_scope();
CREATE TRIGGER trip_material_links_delete_forbidden BEFORE DELETE ON trip_material_links
    FOR EACH ROW EXECUTE FUNCTION sbm_trip_history_immutable();

CREATE OR REPLACE FUNCTION sbm_t_37_trip_fact_assignments_end_scop() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT (
        (NEW.ended_by_decision_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM trip_fact_assignment_decisions d WHERE d.tenant_id = OLD.tenant_id
              AND d.id = NEW.ended_by_decision_id AND d.previous_assignment_id = OLD.id AND d.action IN ('move', 'unassign')
        )) OR (NEW.ended_by_audit_event_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM audit_events a WHERE a.tenant_id = OLD.tenant_id AND a.id = NEW.ended_by_audit_event_id
              AND ((a.action = 'trip_workspace_delete' AND a.resource_type = 'trip_workspace' AND a.resource_id = OLD.trip_id)
                OR (a.action = 'fact_deleted' AND ((a.resource_type = 'payment' AND a.resource_id = OLD.payment_id)
                  OR (a.resource_type = 'invoice' AND a.resource_id = OLD.invoice_id))))
        ))
    ) THEN RAISE EXCEPTION 'trip_assignment_end_scope_mismatch' USING ERRCODE = 'P0001'; END IF;
    RETURN NEW;
END;
$$;

CREATE INDEX payments_auto_trip_time ON payments(tenant_id, transaction_time, id)
    WHERE deleted_at IS NULL AND trip_assignment_mode = 'auto';

-- 历史快照的值与 hash 不变；名称语义与票面目的地脱钩。
ALTER TABLE reimbursements RENAME COLUMN trip_destination TO trip_name;
ALTER TABLE reimbursements ADD COLUMN trip_timezone TEXT;
ALTER TABLE reimbursements ADD COLUMN trip_version BIGINT CHECK (trip_version >= 1);
CREATE OR REPLACE FUNCTION sbm_t_39_reimbursements_creation_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM trips t JOIN memberships m ON m.tenant_id = t.tenant_id AND m.user_id = NEW.created_by_user_id
        WHERE t.tenant_id = NEW.tenant_id AND t.id = NEW.trip_id AND t.deleted_at IS NULL
          AND m.status = 'active' AND NEW.trip_name = t.name AND NEW.trip_start_date = t.start_date
          AND NEW.trip_end_date = t.end_date AND NEW.trip_timezone IS NOT DISTINCT FROM t.timezone
          AND NEW.trip_version = t.version AND NEW.status = 'submitted' AND NEW.version = 1
    ) THEN RAISE EXCEPTION 'reimbursement_creation_scope_mismatch' USING ERRCODE = 'P0001'; END IF;
    RETURN NEW;
END;
$$;
CREATE OR REPLACE FUNCTION sbm_t_40_reimbursements_immutable_field() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(NEW.id, NEW.tenant_id, NEW.trip_id, NEW.trip_name, NEW.trip_start_date, NEW.trip_end_date,
           NEW.trip_timezone, NEW.trip_version, NEW.policy_rule_version, NEW.snapshot_hash,
           NEW.created_by_user_id, NEW.created_by_decision_id, NEW.created_at)
       IS DISTINCT FROM
       ROW(OLD.id, OLD.tenant_id, OLD.trip_id, OLD.trip_name, OLD.trip_start_date, OLD.trip_end_date,
           OLD.trip_timezone, OLD.trip_version, OLD.policy_rule_version, OLD.snapshot_hash,
           OLD.created_by_user_id, OLD.created_by_decision_id, OLD.created_at) THEN
        RAISE EXCEPTION 'reimbursement_immutable' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER reimbursements_immutable_fields ON reimbursements;
CREATE TRIGGER reimbursements_immutable_fields BEFORE UPDATE ON reimbursements
    FOR EACH ROW EXECUTE FUNCTION sbm_t_40_reimbursements_immutable_field();
