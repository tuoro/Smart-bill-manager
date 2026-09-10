-- 理由字段改为可选。产品决定：操作者对自己的操作负责，审计记录里已经有谁、何时、
-- 改了什么；每次多敲一句「为什么」是负担，实际填进去的也多半是敷衍。
-- 唯一例外是坏账：把一笔钱认定为收不回来，理由仍然必填，本迁移不动它。
--
-- 只放宽约束，不删列：历史上填过的理由继续保留可读；新记录允许空串。
-- 用空串而非 NULL 作为「没填」，与既有 NOT NULL 列保持一致，调用方不必处理三态。
-- 约束名取自实际数据库（pg_constraint），不是猜的。

ALTER TABLE account_events DROP CONSTRAINT account_events_reason_check;
ALTER TABLE account_events ADD CONSTRAINT account_events_reason_check
    CHECK (length(trim(reason)) <= 500);
ALTER TABLE account_events ALTER COLUMN reason SET DEFAULT '';

ALTER TABLE invoice_material_decisions DROP CONSTRAINT invoice_material_decisions_reason_check;
ALTER TABLE invoice_material_decisions ADD CONSTRAINT invoice_material_decisions_reason_check
    CHECK (length(trim(reason)) <= 500);
ALTER TABLE invoice_material_decisions ALTER COLUMN reason SET DEFAULT '';

ALTER TABLE member_invitations DROP CONSTRAINT member_invitations_reason_check;
ALTER TABLE member_invitations ADD CONSTRAINT member_invitations_reason_check
    CHECK (length(trim(reason)) <= 500);
ALTER TABLE member_invitations ALTER COLUMN reason SET DEFAULT '';

-- 撤销理由原本嵌在版本状态机的复合约束里；撤销仍要求 revoked_at/revoked_by，
-- 只是不再要求 revoke_reason 非空。
ALTER TABLE member_invitations DROP CONSTRAINT member_invitations_check1;
ALTER TABLE member_invitations ADD CONSTRAINT member_invitations_lifecycle_check CHECK (
    (version = 1 AND num_nonnulls(consumed_at, consumed_by_user_id, revoked_at, revoked_by_user_id, revoke_reason) = 0)
    OR (version = 2 AND consumed_at IS NOT NULL AND consumed_by_user_id IS NOT NULL
        AND num_nonnulls(revoked_at, revoked_by_user_id, revoke_reason) = 0)
    OR (version = 2 AND consumed_at IS NULL AND consumed_by_user_id IS NULL
        AND revoked_at IS NOT NULL AND revoked_by_user_id IS NOT NULL
        AND (revoke_reason IS NULL OR length(trim(revoke_reason)) <= 500))
);

ALTER TABLE payment_invoice_allocation_adjustments
    DROP CONSTRAINT payment_invoice_allocation_adjustments_reason_check;
ALTER TABLE payment_invoice_allocation_adjustments
    ADD CONSTRAINT payment_invoice_allocation_adjustments_reason_check
    CHECK (reason = trim(reason) AND length(reason) <= 500);
ALTER TABLE payment_invoice_allocation_adjustments ALTER COLUMN reason SET DEFAULT '';

ALTER TABLE reimbursement_status_decisions DROP CONSTRAINT reimbursement_status_decisions_reason_check;
ALTER TABLE reimbursement_status_decisions ADD CONSTRAINT reimbursement_status_decisions_reason_check
    CHECK (reason = trim(reason) AND length(reason) <= 500);
ALTER TABLE reimbursement_status_decisions ALTER COLUMN reason SET DEFAULT '';

ALTER TABLE trip_fact_assignment_decisions DROP CONSTRAINT trip_fact_assignment_decisions_reason_check;
ALTER TABLE trip_fact_assignment_decisions ADD CONSTRAINT trip_fact_assignment_decisions_reason_check
    CHECK (reason = trim(reason) AND length(reason) <= 500);
ALTER TABLE trip_fact_assignment_decisions ALTER COLUMN reason SET DEFAULT '';

ALTER TABLE trip_management_decisions DROP CONSTRAINT trip_management_decisions_reason_check;
ALTER TABLE trip_management_decisions ADD CONSTRAINT trip_management_decisions_reason_check
    CHECK (length(trim(reason)) <= 500);
ALTER TABLE trip_management_decisions ALTER COLUMN reason SET DEFAULT '';

ALTER TABLE trip_material_decisions DROP CONSTRAINT trip_material_decisions_reason_check;
ALTER TABLE trip_material_decisions ADD CONSTRAINT trip_material_decisions_reason_check
    CHECK (length(trim(reason)) <= 500);
ALTER TABLE trip_material_decisions ALTER COLUMN reason SET DEFAULT '';

-- 转人工的理由嵌在「人工来源身份」复合约束里；人工来源仍要求幂等键与请求哈希，
-- 只是 manual_reason 允许为空串（保持非 NULL 以维持「人工 / AI」两态的判别）。
ALTER TABLE claim_sets DROP CONSTRAINT claim_sets_manual_identity_check;
ALTER TABLE claim_sets ADD CONSTRAINT claim_sets_manual_identity_check CHECK (
    (origin_ai_run_id IS NULL AND revision = 1
        AND manual_reason IS NOT NULL AND length(btrim(manual_reason)) <= 500
        AND manual_idempotency_key IS NOT NULL
        AND length(manual_idempotency_key) BETWEEN 8 AND 128
        AND manual_request_hash IS NOT NULL AND manual_request_hash ~ '^[0-9a-f]{64}$')
    OR ((origin_ai_run_id IS NOT NULL OR revision > 1)
        AND manual_reason IS NULL AND manual_idempotency_key IS NULL AND manual_request_hash IS NULL)
);
