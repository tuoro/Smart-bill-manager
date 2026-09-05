-- 目标页和双侧活动余额查询保持有界；不改写历史 Link 或现有业务数据。
CREATE INDEX payments_allocation_target_idx ON payments (tenant_id, currency, id) WHERE deleted_at IS NULL;
CREATE INDEX invoices_allocation_target_idx ON invoices (tenant_id, currency, id) WHERE deleted_at IS NULL;
CREATE INDEX allocation_payment_balance_idx ON payment_invoice_links (tenant_id, payment_id) INCLUDE (allocated_minor, invoice_id) WHERE ended_at IS NULL;
CREATE INDEX allocation_invoice_balance_idx ON payment_invoice_links (tenant_id, invoice_id) INCLUDE (allocated_minor, payment_id) WHERE ended_at IS NULL;

-- 完整期望计划的 200 项边界同时约束两端；超限旧状态明确阻断升级，绝不截断或删除。
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM payment_invoice_links WHERE ended_at IS NULL GROUP BY tenant_id,payment_id HAVING count(*)>200)
 OR EXISTS(SELECT 1 FROM payment_invoice_links WHERE ended_at IS NULL GROUP BY tenant_id,invoice_id HAVING count(*)>200) THEN
  RAISE EXCEPTION 'allocation_active_target_limit_exceeded';
 END IF;
END; $$;
CREATE FUNCTION sbm_allocation_target_limit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.ended_at IS NOT NULL THEN RETURN NEW; END IF;
 PERFORM 1 FROM payments WHERE tenant_id=NEW.tenant_id AND id=NEW.payment_id FOR UPDATE;
 PERFORM 1 FROM invoices WHERE tenant_id=NEW.tenant_id AND id=NEW.invoice_id FOR UPDATE;
 IF (SELECT count(*) FROM payment_invoice_links WHERE tenant_id=NEW.tenant_id AND payment_id=NEW.payment_id AND ended_at IS NULL)>=200
 OR (SELECT count(*) FROM payment_invoice_links WHERE tenant_id=NEW.tenant_id AND invoice_id=NEW.invoice_id AND ended_at IS NULL)>=200 THEN
  RAISE EXCEPTION 'allocation_active_target_limit_exceeded';
 END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER zz_allocation_target_limit BEFORE INSERT ON payment_invoice_links
FOR EACH ROW EXECUTE FUNCTION sbm_allocation_target_limit();

-- 决定历史是坏账唯一事实源；当前值为该 Fact 的最后决定，默认 false。
CREATE TABLE fact_bad_debt_decisions (
 id TEXT PRIMARY KEY,
 tenant_id TEXT NOT NULL,
 payment_id TEXT,
 invoice_id TEXT,
 actor_user_id TEXT NOT NULL,
 marked BOOLEAN NOT NULL,
 expected_version BIGINT NOT NULL CHECK (expected_version > 0),
 resulting_version BIGINT NOT NULL CHECK (resulting_version = expected_version + 1),
 reason TEXT NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 8 AND 128),
 request_hash TEXT NOT NULL CHECK (request_hash ~ '^[a-f0-9]{64}$'),
 audit_event_id TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL,
 CHECK (num_nonnulls(payment_id,invoice_id)=1),
 UNIQUE (tenant_id,id),
 CONSTRAINT fact_bad_debt_idempotency_unique UNIQUE (tenant_id,idempotency_key),
 UNIQUE (tenant_id,payment_id,resulting_version),
 UNIQUE (tenant_id,invoice_id,resulting_version),
 FOREIGN KEY (tenant_id,payment_id) REFERENCES payments(tenant_id,id),
 FOREIGN KEY (tenant_id,invoice_id) REFERENCES invoices(tenant_id,id),
 FOREIGN KEY (tenant_id,actor_user_id) REFERENCES memberships(tenant_id,user_id),
 FOREIGN KEY (tenant_id,audit_event_id) REFERENCES audit_events(tenant_id,id)
);
CREATE INDEX bad_debt_payment_state_idx ON fact_bad_debt_decisions (tenant_id,payment_id,resulting_version DESC) INCLUDE(marked) WHERE payment_id IS NOT NULL;
CREATE INDEX bad_debt_invoice_state_idx ON fact_bad_debt_decisions (tenant_id,invoice_id,resulting_version DESC) INCLUDE(marked) WHERE invoice_id IS NOT NULL;
CREATE TRIGGER bad_debt_history_immutable BEFORE UPDATE OR DELETE ON fact_bad_debt_decisions
FOR EACH ROW EXECUTE FUNCTION sbm_correction_history_immutable();

CREATE FUNCTION sbm_fact_bad_debt(tenant TEXT, kind TEXT, fact TEXT) RETURNS BOOLEAN
LANGUAGE sql STABLE AS $$
 SELECT coalesce((SELECT marked FROM fact_bad_debt_decisions
 WHERE tenant_id=tenant AND ((kind='payment' AND payment_id=fact) OR (kind='invoice' AND invoice_id=fact))
 ORDER BY resulting_version DESC LIMIT 1), false);
$$;

CREATE FUNCTION sbm_trip_bad_debt_locked(tenant TEXT, trip TEXT) RETURNS BOOLEAN
LANGUAGE sql STABLE AS $$
 SELECT EXISTS (
 SELECT 1 FROM trip_fact_assignments a
 WHERE a.tenant_id=tenant AND a.trip_id=trip AND a.ended_at IS NULL AND (
 EXISTS(SELECT 1 FROM payments p WHERE p.tenant_id=a.tenant_id AND p.id=a.payment_id AND p.deleted_at IS NULL AND sbm_fact_bad_debt(tenant,'payment',p.id))
 OR EXISTS(SELECT 1 FROM invoices i WHERE i.tenant_id=a.tenant_id AND i.id=a.invoice_id AND i.deleted_at IS NULL AND sbm_fact_bad_debt(tenant,'invoice',i.id))
 OR EXISTS(SELECT 1 FROM payment_invoice_links l JOIN invoices i ON i.tenant_id=l.tenant_id AND i.id=l.invoice_id
 JOIN payments p ON p.tenant_id=l.tenant_id AND p.id=l.payment_id
 WHERE l.tenant_id=a.tenant_id AND l.payment_id=a.payment_id AND l.ended_at IS NULL AND i.deleted_at IS NULL AND p.deleted_at IS NULL AND sbm_fact_bad_debt(tenant,'invoice',i.id))));
$$;

CREATE FUNCTION sbm_bad_debt_decision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
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
   AND actor.status='active' AND actor.role IN ('owner','finance')
 ) THEN RAISE EXCEPTION 'bad_debt_decision_invalid'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER bad_debt_decision_guard BEFORE INSERT ON fact_bad_debt_decisions
FOR EACH ROW EXECUTE FUNCTION sbm_bad_debt_decision_guard();

-- 即使绕过应用入口，也不能在仍有坏账关系时删除行程。
CREATE FUNCTION sbm_trip_bad_debt_delete_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL AND sbm_trip_bad_debt_locked(OLD.tenant_id,OLD.id) THEN
  RAISE EXCEPTION 'trip_bad_debt_locked';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER trip_bad_debt_delete_guard BEFORE UPDATE OF deleted_at ON trips
FOR EACH ROW EXECUTE FUNCTION sbm_trip_bad_debt_delete_guard();

CREATE FUNCTION sbm_bad_debt_related_trips(tenant TEXT,kind TEXT,fact TEXT) RETURNS TABLE(id TEXT)
LANGUAGE sql STABLE AS $$
 SELECT a.trip_id FROM trip_fact_assignments a WHERE a.tenant_id=tenant AND a.ended_at IS NULL AND (
 (kind='payment' AND a.payment_id=fact) OR (kind='invoice' AND a.invoice_id=fact) OR
 (kind='invoice' AND EXISTS(SELECT 1 FROM payment_invoice_links l WHERE l.tenant_id=a.tenant_id AND l.payment_id=a.payment_id AND l.invoice_id=fact AND l.ended_at IS NULL)));
$$;

-- 所有能增加保护图的入口按同一 Trip 行锁与删除串行，不建立租户级全局锁。
CREATE FUNCTION sbm_bad_debt_graph_lock() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='trip_fact_assignments' THEN
  PERFORM 1 FROM trips WHERE tenant_id=NEW.tenant_id AND id=NEW.trip_id FOR UPDATE;
 ELSIF TG_TABLE_NAME='payment_invoice_links' THEN
  PERFORM 1 FROM trips WHERE tenant_id=NEW.tenant_id AND id IN (SELECT id FROM sbm_bad_debt_related_trips(NEW.tenant_id,'payment',NEW.payment_id)) ORDER BY id FOR UPDATE;
 ELSE
  PERFORM 1 FROM trips WHERE tenant_id=NEW.tenant_id AND id IN (SELECT id FROM sbm_bad_debt_related_trips(NEW.tenant_id,CASE WHEN NEW.payment_id IS NOT NULL THEN 'payment' ELSE 'invoice' END,coalesce(NEW.payment_id,NEW.invoice_id))) ORDER BY id FOR UPDATE;
 END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER bad_debt_graph_lock BEFORE INSERT ON fact_bad_debt_decisions FOR EACH ROW EXECUTE FUNCTION sbm_bad_debt_graph_lock();
CREATE TRIGGER bad_debt_graph_lock BEFORE INSERT ON payment_invoice_links FOR EACH ROW EXECUTE FUNCTION sbm_bad_debt_graph_lock();
CREATE TRIGGER bad_debt_graph_lock BEFORE INSERT ON trip_fact_assignments FOR EACH ROW EXECUTE FUNCTION sbm_bad_debt_graph_lock();

CREATE FUNCTION sbm_bad_debt_final_effect() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE fact_version BIGINT; kind TEXT;
BEGIN
 kind:=CASE WHEN NEW.payment_id IS NOT NULL THEN 'payment' ELSE 'invoice' END;
 IF NEW.payment_id IS NOT NULL THEN
  SELECT version INTO fact_version FROM payments WHERE tenant_id=NEW.tenant_id AND id=NEW.payment_id;
 ELSE
  SELECT version INTO fact_version FROM invoices WHERE tenant_id=NEW.tenant_id AND id=NEW.invoice_id;
 END IF;
 IF fact_version IS DISTINCT FROM NEW.resulting_version THEN RAISE EXCEPTION 'bad_debt_final_version_mismatch'; END IF;
 IF EXISTS(SELECT 1 FROM trips t WHERE t.tenant_id=NEW.tenant_id AND t.deleted_at IS NOT NULL AND t.id IN (SELECT id FROM sbm_bad_debt_related_trips(NEW.tenant_id,kind,coalesce(NEW.payment_id,NEW.invoice_id))) AND sbm_trip_bad_debt_locked(t.tenant_id,t.id)) THEN RAISE EXCEPTION 'trip_bad_debt_locked'; END IF;
 RETURN NEW;
END; $$;
CREATE CONSTRAINT TRIGGER bad_debt_final_effect AFTER INSERT ON fact_bad_debt_decisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION sbm_bad_debt_final_effect();

CREATE FUNCTION sbm_bad_debt_graph_final_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='trips' THEN
  IF EXISTS(SELECT 1 FROM trips t WHERE t.tenant_id=NEW.tenant_id AND t.id=NEW.id AND t.deleted_at IS NOT NULL AND sbm_trip_bad_debt_locked(t.tenant_id,t.id)) THEN RAISE EXCEPTION 'trip_bad_debt_locked'; END IF;
 ELSE
  IF EXISTS(SELECT 1 FROM trips t WHERE t.tenant_id=NEW.tenant_id AND t.deleted_at IS NOT NULL AND t.id IN (SELECT id FROM sbm_bad_debt_related_trips(NEW.tenant_id,'payment',NEW.payment_id)) AND sbm_trip_bad_debt_locked(t.tenant_id,t.id)) THEN RAISE EXCEPTION 'trip_bad_debt_locked'; END IF;
 END IF;
 RETURN NEW;
END; $$;
CREATE CONSTRAINT TRIGGER trip_bad_debt_final_guard AFTER UPDATE ON trips DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION sbm_bad_debt_graph_final_guard();
CREATE CONSTRAINT TRIGGER allocation_bad_debt_final_guard AFTER INSERT ON payment_invoice_links DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION sbm_bad_debt_graph_final_guard();
