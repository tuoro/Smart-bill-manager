-- 已确认关联的支付与发票互相跟随行程归属。
--
-- 用户的判断：支付和发票的关联关系是审核时人工确认过的，这两张单据本来就是同一件
-- 事，其中一张归了行程，另一张不该再让人手动点一遍。规则 trip-link-attribution/1：
-- 处于自动模式的单据，取其所有活动关联对方当前所在的行程（支付再并上时间规则的
-- 唯一命中），恰好一个不同的行程才归属；零个保持未归属；多个不同行程视为冲突，
-- 留给人工，不按任何顺序挑选。人工归属与「保持无归属」仍然优先。
--
-- 发票因此也需要归属偏好列。已经人工归属过的发票标为 manual，规则不会动它们。

ALTER TABLE invoices ADD COLUMN trip_assignment_mode TEXT NOT NULL DEFAULT 'auto'
    CONSTRAINT invoices_trip_assignment_mode_check CHECK (trip_assignment_mode IN ('auto', 'manual', 'blocked'));

UPDATE invoices i SET trip_assignment_mode = 'manual'
WHERE EXISTS (SELECT 1 FROM trip_fact_assignments a
              WHERE a.tenant_id = i.tenant_id AND a.invoice_id = i.id AND a.ended_at IS NULL);

-- 来自 0014_two_roles.sql，放开发票的自动来源与关联规则版本。
CREATE OR REPLACE FUNCTION sbm_trip_assignment_version_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE fact_version BIGINT; preference TEXT;
BEGIN
    IF NEW.fact_type = 'payment' THEN
        SELECT version, trip_assignment_mode INTO fact_version, preference FROM payments
        WHERE tenant_id = NEW.tenant_id AND id = NEW.payment_id AND deleted_at IS NULL;
    ELSE
        SELECT version, trip_assignment_mode INTO fact_version, preference FROM invoices
        WHERE tenant_id = NEW.tenant_id AND id = NEW.invoice_id AND deleted_at IS NULL;
    END IF;
    IF fact_version IS NULL OR NEW.expected_fact_version <> fact_version
      OR NOT EXISTS (SELECT 1 FROM memberships m WHERE m.tenant_id = NEW.tenant_id AND m.user_id = NEW.actor_user_id
        AND m.status = 'active' AND (m.role IN ('owner', 'member')))
      OR (NEW.decision_source = 'automatic' AND (preference <> 'auto'
        OR NEW.rule_version NOT IN ('trip-time-attribution/1', 'trip-link-attribution/1')
        OR (NEW.fact_type = 'invoice' AND NEW.rule_version <> 'trip-link-attribution/1')))
      OR (NEW.decision_source = 'manual' AND NEW.rule_version IS NOT NULL) THEN
        RAISE EXCEPTION 'trip_assignment_stale' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
