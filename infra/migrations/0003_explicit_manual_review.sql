-- 人工根 Claim 不伪造 AiRun；现存 AI 根和修订通过前向迁移保留。
ALTER TABLE claim_sets ALTER COLUMN origin_ai_run_id DROP NOT NULL;
ALTER TABLE claim_sets ADD COLUMN manual_reason TEXT;
ALTER TABLE claim_sets ADD COLUMN manual_idempotency_key TEXT;
ALTER TABLE claim_sets ADD COLUMN manual_request_hash TEXT;
ALTER TABLE claim_sets DROP CONSTRAINT claim_sets_check;
ALTER TABLE claim_sets ADD CONSTRAINT claim_sets_origin_check CHECK (
    (produced_by_ai_run_id IS NOT NULL AND revised_by_user_id IS NULL
     AND origin_ai_run_id IS NOT NULL AND origin_ai_run_id = produced_by_ai_run_id
     AND revision = 1 AND supersedes_claim_set_id IS NULL)
    OR
    (produced_by_ai_run_id IS NULL AND revised_by_user_id IS NOT NULL
     AND revision > 1 AND supersedes_claim_set_id IS NOT NULL)
    OR
    (produced_by_ai_run_id IS NULL AND revised_by_user_id IS NOT NULL
     AND origin_ai_run_id IS NULL AND revision = 1 AND supersedes_claim_set_id IS NULL)
);
ALTER TABLE claim_sets ADD CONSTRAINT claim_sets_manual_identity_check CHECK (
    (origin_ai_run_id IS NULL AND revision = 1
     AND manual_reason IS NOT NULL AND length(btrim(manual_reason)) BETWEEN 1 AND 500
     AND manual_idempotency_key IS NOT NULL AND length(manual_idempotency_key) BETWEEN 8 AND 128
     AND manual_request_hash IS NOT NULL AND manual_request_hash ~ '^[0-9a-f]{64}$')
    OR
    ((origin_ai_run_id IS NOT NULL OR revision > 1)
     AND manual_reason IS NULL AND manual_idempotency_key IS NULL AND manual_request_hash IS NULL)
);
CREATE UNIQUE INDEX claim_sets_manual_key_idx ON claim_sets (tenant_id, manual_idempotency_key)
WHERE manual_idempotency_key IS NOT NULL;

CREATE FUNCTION sbm_claim_source_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF (NEW.tenant_id, NEW.document_id, NEW.origin_ai_run_id, NEW.produced_by_ai_run_id,
            NEW.revised_by_user_id, NEW.revision, NEW.supersedes_claim_set_id,
            NEW.manual_reason, NEW.manual_idempotency_key, NEW.manual_request_hash)
           IS DISTINCT FROM
           (OLD.tenant_id, OLD.document_id, OLD.origin_ai_run_id, OLD.produced_by_ai_run_id,
            OLD.revised_by_user_id, OLD.revision, OLD.supersedes_claim_set_id,
            OLD.manual_reason, OLD.manual_idempotency_key, OLD.manual_request_hash) THEN
            RAISE EXCEPTION 'claim_source_immutable' USING ERRCODE = 'P0001';
        END IF;
    ELSIF NEW.revision > 1 AND NOT EXISTS (
        SELECT 1 FROM claim_sets parent WHERE parent.tenant_id = NEW.tenant_id
          AND parent.id = NEW.supersedes_claim_set_id AND parent.document_id = NEW.document_id
          AND parent.revision + 1 = NEW.revision
          AND parent.origin_ai_run_id IS NOT DISTINCT FROM NEW.origin_ai_run_id
    ) THEN
        RAISE EXCEPTION 'claim_source_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER claim_source_identity BEFORE INSERT OR UPDATE ON claim_sets
FOR EACH ROW EXECUTE FUNCTION sbm_claim_source_identity();
