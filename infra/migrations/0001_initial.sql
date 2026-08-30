PRAGMA foreign_keys = ON;

CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    applied_at TEXT NOT NULL
) STRICT;

CREATE TABLE users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL COLLATE NOCASE UNIQUE,
    password_hash TEXT NOT NULL,
    display_name TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (length(email) BETWEEN 3 AND 254),
    CHECK (length(display_name) BETWEEN 1 AND 100)
) STRICT;

CREATE TABLE tenants (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    default_currency TEXT NOT NULL,
    timezone TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (length(name) BETWEEN 1 AND 120),
    CHECK (default_currency IN ('CNY', 'USD', 'EUR', 'JPY')),
    UNIQUE (id, id)
) STRICT;

CREATE TABLE memberships (
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    role TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (tenant_id, user_id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CHECK (role IN ('owner', 'finance', 'reviewer', 'viewer')),
    CHECK (status IN ('active', 'suspended'))
) STRICT;

CREATE TRIGGER memberships_keep_active_owner_update
BEFORE UPDATE OF role, status ON memberships
WHEN OLD.role = 'owner'
 AND OLD.status = 'active'
 AND (NEW.role <> 'owner' OR NEW.status <> 'active')
 AND (SELECT count(*) FROM memberships
      WHERE tenant_id = OLD.tenant_id AND role = 'owner' AND status = 'active') = 1
BEGIN
    SELECT RAISE(ABORT, 'last_active_owner');
END;

CREATE TRIGGER memberships_keep_active_owner_delete
BEFORE DELETE ON memberships
WHEN OLD.role = 'owner'
 AND OLD.status = 'active'
 AND (SELECT count(*) FROM memberships
      WHERE tenant_id = OLD.tenant_id AND role = 'owner' AND status = 'active') = 1
BEGIN
    SELECT RAISE(ABORT, 'last_active_owner');
END;

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    csrf_token_hash TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    revoked_at TEXT,
    FOREIGN KEY (tenant_id, user_id) REFERENCES memberships(tenant_id, user_id) ON DELETE CASCADE,
    UNIQUE (tenant_id, id)
) STRICT;

CREATE INDEX sessions_principal_idx
ON sessions (tenant_id, user_id, expires_at)
WHERE revoked_at IS NULL;

CREATE TABLE provider_configs (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    base_url TEXT NOT NULL,
    encrypted_api_key BLOB,
    model TEXT NOT NULL,
    output_mode TEXT NOT NULL,
    capability_status TEXT NOT NULL,
    capability_checked_at TEXT,
    capability_safe_message TEXT,
    capability_schema_version TEXT,
    capability_schema_sha256 TEXT,
    active INTEGER NOT NULL DEFAULT 0,
    version INTEGER NOT NULL DEFAULT 1,
    safe_fingerprint TEXT NOT NULL,
    created_by_user_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT,
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, created_by_user_id) REFERENCES memberships(tenant_id, user_id),
    CHECK (capability_status IN ('pending', 'passed', 'failed')),
    CHECK (output_mode IN ('json_schema', 'json_object')),
    CHECK (active IN (0, 1)),
    CHECK (version >= 1),
    CHECK (capability_schema_sha256 IS NULL OR length(capability_schema_sha256) = 64),
    CHECK (deleted_at IS NULL OR (active = 0 AND encrypted_api_key IS NULL)),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE UNIQUE INDEX provider_configs_one_active_idx
ON provider_configs (tenant_id)
WHERE active = 1 AND deleted_at IS NULL;

CREATE TRIGGER provider_configs_require_capability_insert
BEFORE INSERT ON provider_configs
WHEN NEW.active = 1 AND (
    NEW.capability_status <> 'passed'
    OR NEW.capability_schema_version IS NULL
    OR NEW.capability_schema_sha256 IS NULL
    OR NEW.deleted_at IS NOT NULL
)
BEGIN
    SELECT RAISE(ABORT, 'provider_capability_required');
END;

CREATE TRIGGER provider_configs_require_capability_update
BEFORE UPDATE OF active, capability_status, capability_schema_version, capability_schema_sha256, deleted_at
ON provider_configs
WHEN NEW.active = 1 AND (
    NEW.capability_status <> 'passed'
    OR NEW.capability_schema_version IS NULL
    OR NEW.capability_schema_sha256 IS NULL
    OR NEW.deleted_at IS NOT NULL
)
BEGIN
    SELECT RAISE(ABORT, 'provider_capability_required');
END;

CREATE TABLE documents (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    storage_key TEXT NOT NULL,
    original_name TEXT NOT NULL,
    declared_mime TEXT NOT NULL,
    detected_mime TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    sha256 TEXT NOT NULL,
    page_count INTEGER NOT NULL,
    status TEXT NOT NULL,
    created_by_user_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, created_by_user_id) REFERENCES memberships(tenant_id, user_id),
    CHECK (declared_mime IN ('image/jpeg', 'image/png', 'image/webp', 'application/pdf')),
    CHECK (detected_mime IN ('image/jpeg', 'image/png', 'image/webp', 'application/pdf')),
    CHECK (declared_mime = detected_mime),
    CHECK (size_bytes BETWEEN 1 AND 20971520),
    CHECK (length(sha256) = 64),
    CHECK (page_count BETWEEN 1 AND 20),
    CHECK (status IN ('stored', 'processing', 'needs_review', 'blocked', 'failed', 'completed', 'cancelled', 'rejected')),
    UNIQUE (tenant_id, sha256),
    UNIQUE (tenant_id, storage_key),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE TABLE document_pages (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    page_number INTEGER NOT NULL,
    derived_image_storage_key TEXT NOT NULL,
    width INTEGER NOT NULL,
    height INTEGER NOT NULL,
    sha256 TEXT NOT NULL,
    processing_version TEXT NOT NULL,
    visual_fingerprint_version TEXT NOT NULL,
    dhash64 TEXT NOT NULL,
    ahash64 TEXT NOT NULL,
    dhash_band_0 INTEGER NOT NULL,
    dhash_band_1 INTEGER NOT NULL,
    dhash_band_2 INTEGER NOT NULL,
    dhash_band_3 INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (tenant_id, document_id) REFERENCES documents(tenant_id, id) ON DELETE CASCADE,
    CHECK (page_number BETWEEN 1 AND 20),
    CHECK (width BETWEEN 1 AND 8000),
    CHECK (height BETWEEN 1 AND 8000),
    CHECK (length(sha256) = 64),
    CHECK (visual_fingerprint_version = 'page-visual-dedup/1'),
    CHECK (length(dhash64) = 16 AND dhash64 NOT GLOB '*[^0-9a-f]*'),
    CHECK (length(ahash64) = 16 AND ahash64 NOT GLOB '*[^0-9a-f]*'),
    CHECK (dhash_band_0 BETWEEN 0 AND 65535),
    CHECK (dhash_band_1 BETWEEN 0 AND 65535),
    CHECK (dhash_band_2 BETWEEN 0 AND 65535),
    CHECK (dhash_band_3 BETWEEN 0 AND 65535),
    UNIQUE (tenant_id, document_id, page_number),
    UNIQUE (tenant_id, derived_image_storage_key),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE INDEX document_pages_visual_band_0_idx
ON document_pages (tenant_id, visual_fingerprint_version, dhash_band_0);

CREATE INDEX document_pages_visual_band_1_idx
ON document_pages (tenant_id, visual_fingerprint_version, dhash_band_1);

CREATE INDEX document_pages_visual_band_2_idx
ON document_pages (tenant_id, visual_fingerprint_version, dhash_band_2);

CREATE INDEX document_pages_visual_band_3_idx
ON document_pages (tenant_id, visual_fingerprint_version, dhash_band_3);

CREATE TABLE processing_jobs (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    lease_owner TEXT,
    lease_expires_at TEXT,
    cancel_requested_at TEXT,
    error_code TEXT,
    safe_error_message TEXT,
    created_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    version INTEGER NOT NULL DEFAULT 1,
    FOREIGN KEY (tenant_id, document_id) REFERENCES documents(tenant_id, id) ON DELETE CASCADE,
    CHECK (kind = 'document_process'),
    CHECK (status IN ('queued', 'processing', 'needs_review', 'blocked', 'failed', 'cancel_requested', 'cancelled', 'completed', 'rejected')),
    CHECK (attempt_count >= 0),
    CHECK (version >= 1),
    CHECK ((status = 'processing' AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL) OR status <> 'processing'),
    UNIQUE (tenant_id, document_id),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE INDEX processing_jobs_claim_idx
ON processing_jobs (status, lease_expires_at, created_at);

CREATE INDEX processing_jobs_tenant_created_idx
ON processing_jobs (tenant_id, created_at DESC, id DESC);

CREATE TABLE ai_runs (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    job_id TEXT NOT NULL,
    provider_config_id TEXT NOT NULL,
    provider_config_version INTEGER NOT NULL,
    provider_config_fingerprint TEXT NOT NULL,
    model TEXT NOT NULL,
    prompt_version TEXT NOT NULL,
    extraction_schema_version TEXT NOT NULL,
    provider_schema_version TEXT NOT NULL,
    provider_schema_sha256 TEXT NOT NULL,
    claim_schema_version TEXT NOT NULL,
    claim_mapper_version TEXT NOT NULL,
    input_processing_version TEXT NOT NULL,
    request_hash TEXT,
    response_hash TEXT,
    input_tokens INTEGER,
    output_tokens INTEGER,
    latency_ms INTEGER,
    outcome TEXT NOT NULL,
    error_code TEXT,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    FOREIGN KEY (tenant_id, job_id) REFERENCES processing_jobs(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, provider_config_id) REFERENCES provider_configs(tenant_id, id),
    CHECK (provider_config_version >= 1),
    CHECK (length(provider_schema_sha256) = 64),
    CHECK (outcome IN ('running', 'succeeded', 'failed', 'cancelled')),
    CHECK (input_tokens IS NULL OR input_tokens >= 0),
    CHECK (output_tokens IS NULL OR output_tokens >= 0),
    CHECK (latency_ms IS NULL OR latency_ms >= 0),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE INDEX ai_runs_job_idx ON ai_runs (tenant_id, job_id, started_at);

CREATE TABLE claim_sets (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    origin_ai_run_id TEXT NOT NULL,
    produced_by_ai_run_id TEXT,
    revised_by_user_id TEXT,
    document_type TEXT NOT NULL,
    status TEXT NOT NULL,
    revision INTEGER NOT NULL,
    supersedes_claim_set_id TEXT,
    optimistic_version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    FOREIGN KEY (tenant_id, document_id) REFERENCES documents(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, origin_ai_run_id) REFERENCES ai_runs(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, produced_by_ai_run_id) REFERENCES ai_runs(tenant_id, id),
    FOREIGN KEY (tenant_id, revised_by_user_id) REFERENCES memberships(tenant_id, user_id),
    FOREIGN KEY (tenant_id, supersedes_claim_set_id) REFERENCES claim_sets(tenant_id, id),
    CHECK (document_type IN ('payment', 'invoice', 'unknown')),
    CHECK (status IN ('draft', 'ready_for_review', 'blocked', 'superseded', 'confirmed', 'rejected', 'cancelled')),
    CHECK (revision >= 1),
    CHECK (optimistic_version >= 1),
    CHECK (
        (produced_by_ai_run_id IS NOT NULL AND revised_by_user_id IS NULL
         AND origin_ai_run_id = produced_by_ai_run_id AND revision = 1
         AND supersedes_claim_set_id IS NULL)
        OR
        (produced_by_ai_run_id IS NULL AND revised_by_user_id IS NOT NULL
         AND revision > 1 AND supersedes_claim_set_id IS NOT NULL)
    ),
    UNIQUE (tenant_id, document_id, revision),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE UNIQUE INDEX claim_sets_one_current_idx
ON claim_sets (tenant_id, document_id)
WHERE status IN ('draft', 'ready_for_review', 'blocked');

CREATE TABLE field_claims (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    claim_set_id TEXT NOT NULL,
    field_path TEXT NOT NULL,
    value_type TEXT NOT NULL,
    presence TEXT NOT NULL,
    typed_value_json TEXT,
    normalized_value TEXT,
    source TEXT NOT NULL,
    source_user_id TEXT,
    supersedes_field_claim_id TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (tenant_id, claim_set_id) REFERENCES claim_sets(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, source_user_id) REFERENCES memberships(tenant_id, user_id),
    FOREIGN KEY (tenant_id, supersedes_field_claim_id) REFERENCES field_claims(tenant_id, id),
    CHECK (value_type IN ('string', 'money_minor', 'date', 'instant', 'integer', 'decimal', 'document_type', 'supplementary')),
    CHECK (presence IN ('present', 'absent')),
    CHECK (source IN ('ai', 'user')),
    CHECK ((source = 'ai' AND source_user_id IS NULL) OR (source = 'user' AND source_user_id IS NOT NULL)),
    CHECK (
        (presence = 'absent' AND typed_value_json IS NULL AND normalized_value IS NULL)
        OR
        (presence = 'present' AND typed_value_json IS NOT NULL AND json_valid(typed_value_json))
    ),
    UNIQUE (tenant_id, claim_set_id, field_path),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE TABLE evidence (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    field_claim_id TEXT NOT NULL,
    document_page_id TEXT NOT NULL,
    quote TEXT,
    region_json TEXT,
    evidence_hash TEXT NOT NULL,
    copied_from_evidence_id TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (tenant_id, field_claim_id) REFERENCES field_claims(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, document_page_id) REFERENCES document_pages(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, copied_from_evidence_id) REFERENCES evidence(tenant_id, id),
    CHECK (quote IS NOT NULL OR region_json IS NOT NULL),
    CHECK (region_json IS NULL OR json_valid(region_json)),
    CHECK (length(evidence_hash) = 64),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE INDEX evidence_field_idx
ON evidence (tenant_id, field_claim_id);

CREATE TABLE validation_results (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    ai_run_id TEXT,
    claim_set_id TEXT,
    field_claim_id TEXT,
    rule_code TEXT NOT NULL,
    severity TEXT NOT NULL,
    status TEXT NOT NULL,
    safe_message TEXT NOT NULL,
    rule_version TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (tenant_id, ai_run_id) REFERENCES ai_runs(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, claim_set_id) REFERENCES claim_sets(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, field_claim_id) REFERENCES field_claims(tenant_id, id) ON DELETE CASCADE,
    CHECK ((ai_run_id IS NOT NULL) <> (claim_set_id IS NOT NULL)),
    CHECK (field_claim_id IS NULL OR claim_set_id IS NOT NULL),
    CHECK (severity IN ('info', 'warning', 'error', 'blocked')),
    CHECK (status IN ('passed', 'warning', 'error', 'blocked')),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE INDEX validation_results_claim_idx
ON validation_results (tenant_id, claim_set_id, status);

CREATE TABLE payment_invoice_link_candidates (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    claim_set_id TEXT NOT NULL,
    existing_payment_id TEXT,
    existing_invoice_id TEXT,
    candidate_key TEXT NOT NULL,
    rule_version TEXT NOT NULL,
    reason_codes_json TEXT NOT NULL,
    name_exact INTEGER NOT NULL,
    date_distance_days INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (tenant_id, claim_set_id) REFERENCES claim_sets(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, existing_payment_id) REFERENCES payments(tenant_id, id),
    FOREIGN KEY (tenant_id, existing_invoice_id) REFERENCES invoices(tenant_id, id),
    CHECK ((existing_payment_id IS NOT NULL) <> (existing_invoice_id IS NOT NULL)),
    CHECK (json_valid(reason_codes_json)),
    CHECK (name_exact IN (0, 1)),
    CHECK (date_distance_days BETWEEN 0 AND 30),
    UNIQUE (tenant_id, candidate_key),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE TABLE review_decisions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    claim_set_id TEXT NOT NULL,
    actor_user_id TEXT NOT NULL,
    action TEXT NOT NULL,
    association_mode TEXT,
    association_plan_hash TEXT,
    duplicate_plan_hash TEXT,
    idempotency_key TEXT NOT NULL,
    expected_revision INTEGER NOT NULL,
    reason TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (tenant_id, claim_set_id) REFERENCES claim_sets(tenant_id, id),
    FOREIGN KEY (tenant_id, actor_user_id) REFERENCES memberships(tenant_id, user_id),
    CHECK (action IN ('confirm', 'reject', 'cancel')),
    CHECK (association_mode IS NULL OR association_mode IN ('allocate_candidates', 'reject_all', 'no_candidate')),
    CHECK (
        (action = 'confirm' AND association_mode IS NOT NULL
         AND ((association_mode = 'allocate_candidates'
               AND association_plan_hash IS NOT NULL
               AND length(association_plan_hash) = 64
               AND association_plan_hash NOT GLOB '*[^0-9a-f]*')
              OR (association_mode IN ('reject_all', 'no_candidate') AND association_plan_hash IS NULL)))
        OR
        (action IN ('reject', 'cancel') AND association_mode IS NULL AND association_plan_hash IS NULL)
    ),
    CHECK (
        (action = 'confirm'
         AND duplicate_plan_hash IS NOT NULL
         AND length(duplicate_plan_hash) = 64
         AND duplicate_plan_hash NOT GLOB '*[^0-9a-f]*')
        OR
        (action IN ('reject', 'cancel') AND duplicate_plan_hash IS NULL)
    ),
    CHECK (expected_revision >= 1),
    UNIQUE (tenant_id, idempotency_key),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE TABLE payments (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    source_review_decision_id TEXT NOT NULL,
    amount_minor INTEGER NOT NULL,
    currency TEXT NOT NULL,
    merchant TEXT NOT NULL,
    transaction_time TEXT NOT NULL,
    source_timezone TEXT NOT NULL,
    payment_method TEXT,
    order_number TEXT,
    category TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    deleted_at TEXT,
    deleted_by_user_id TEXT,
    deletion_audit_event_id TEXT,
    FOREIGN KEY (tenant_id, source_review_decision_id) REFERENCES review_decisions(tenant_id, id),
    FOREIGN KEY (tenant_id, deleted_by_user_id) REFERENCES memberships(tenant_id, user_id),
    FOREIGN KEY (tenant_id, deletion_audit_event_id) REFERENCES audit_events(tenant_id, id),
    CHECK (amount_minor BETWEEN 0 AND 9007199254740991),
    CHECK (currency IN ('CNY', 'USD', 'EUR', 'JPY')),
    CHECK (version >= 1),
    CHECK ((deleted_at IS NULL AND deleted_by_user_id IS NULL AND deletion_audit_event_id IS NULL)
        OR (deleted_at IS NOT NULL AND deleted_by_user_id IS NOT NULL AND deletion_audit_event_id IS NOT NULL)),
    UNIQUE (tenant_id, source_review_decision_id),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE INDEX payments_tenant_time_active_idx
ON payments (tenant_id, transaction_time DESC, id DESC)
WHERE deleted_at IS NULL;

CREATE INDEX payments_duplicate_match_active_idx
ON payments (tenant_id, currency, amount_minor)
WHERE deleted_at IS NULL;

CREATE TABLE invoices (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    source_review_decision_id TEXT NOT NULL,
    invoice_number TEXT NOT NULL,
    normalized_invoice_number TEXT NOT NULL,
    invoice_date TEXT NOT NULL,
    total_minor INTEGER NOT NULL,
    tax_minor INTEGER,
    currency TEXT NOT NULL,
    seller_name TEXT NOT NULL,
    buyer_name TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    deleted_at TEXT,
    deleted_by_user_id TEXT,
    deletion_audit_event_id TEXT,
    FOREIGN KEY (tenant_id, source_review_decision_id) REFERENCES review_decisions(tenant_id, id),
    FOREIGN KEY (tenant_id, deleted_by_user_id) REFERENCES memberships(tenant_id, user_id),
    FOREIGN KEY (tenant_id, deletion_audit_event_id) REFERENCES audit_events(tenant_id, id),
    CHECK (total_minor BETWEEN 0 AND 9007199254740991),
    CHECK (tax_minor IS NULL OR tax_minor BETWEEN 0 AND 9007199254740991),
    CHECK (currency IN ('CNY', 'USD', 'EUR', 'JPY')),
    CHECK (version >= 1),
    CHECK ((deleted_at IS NULL AND deleted_by_user_id IS NULL AND deletion_audit_event_id IS NULL)
        OR (deleted_at IS NOT NULL AND deleted_by_user_id IS NOT NULL AND deletion_audit_event_id IS NOT NULL)),
    UNIQUE (tenant_id, source_review_decision_id),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE UNIQUE INDEX invoices_number_active_idx
ON invoices (tenant_id, normalized_invoice_number)
WHERE deleted_at IS NULL;

CREATE INDEX invoices_tenant_date_active_idx
ON invoices (tenant_id, invoice_date DESC, id DESC)
WHERE deleted_at IS NULL;

CREATE INDEX invoices_duplicate_match_active_idx
ON invoices (tenant_id, currency, total_minor, invoice_date)
WHERE deleted_at IS NULL;

CREATE TABLE invoice_items (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    invoice_id TEXT NOT NULL,
    item_key TEXT NOT NULL,
    name TEXT NOT NULL,
    quantity TEXT,
    unit TEXT,
    unit_price_minor INTEGER,
    amount_minor INTEGER NOT NULL,
    tax_minor INTEGER,
    sort_order INTEGER NOT NULL,
    FOREIGN KEY (tenant_id, invoice_id) REFERENCES invoices(tenant_id, id),
    CHECK (unit_price_minor IS NULL OR unit_price_minor BETWEEN 0 AND 9007199254740991),
    CHECK (amount_minor BETWEEN 0 AND 9007199254740991),
    CHECK (tax_minor IS NULL OR tax_minor BETWEEN 0 AND 9007199254740991),
    CHECK (sort_order >= 0),
    UNIQUE (tenant_id, invoice_id, item_key),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE TABLE duplicate_candidates (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    claim_set_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    existing_document_id TEXT,
    current_document_page_id TEXT,
    existing_document_page_id TEXT,
    existing_payment_id TEXT,
    existing_invoice_id TEXT,
    candidate_key TEXT NOT NULL,
    rule_version TEXT NOT NULL,
    reason_codes_json TEXT NOT NULL,
    dhash_distance INTEGER,
    ahash_distance INTEGER,
    created_at TEXT NOT NULL,
    FOREIGN KEY (tenant_id, claim_set_id) REFERENCES claim_sets(tenant_id, id) ON DELETE CASCADE,
    CHECK (kind IN ('near_file', 'cross_page', 'field_combination')),
    CHECK (rule_version = 'duplicate-detection/1'),
    CHECK (length(candidate_key) = 64 AND candidate_key NOT GLOB '*[^0-9a-f]*'),
    CHECK (json_valid(reason_codes_json) AND json_type(reason_codes_json) = 'array'),
    CHECK (
        (kind = 'near_file'
         AND existing_document_id IS NOT NULL
         AND current_document_page_id IS NULL
         AND existing_document_page_id IS NULL
         AND existing_payment_id IS NULL
         AND existing_invoice_id IS NULL
         AND dhash_distance BETWEEN 0 AND 3
         AND ahash_distance BETWEEN 0 AND 3)
        OR
        (kind = 'cross_page'
         AND existing_document_id IS NOT NULL
         AND current_document_page_id IS NOT NULL
         AND existing_document_page_id IS NOT NULL
         AND current_document_page_id <> existing_document_page_id
         AND existing_payment_id IS NULL
         AND existing_invoice_id IS NULL
         AND dhash_distance BETWEEN 0 AND 3
         AND ahash_distance BETWEEN 0 AND 3)
        OR
        (kind = 'field_combination'
         AND existing_document_id IS NULL
         AND current_document_page_id IS NULL
         AND existing_document_page_id IS NULL
         AND (existing_payment_id IS NOT NULL) <> (existing_invoice_id IS NOT NULL)
         AND dhash_distance IS NULL
         AND ahash_distance IS NULL)
    ),
    UNIQUE (tenant_id, candidate_key),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE INDEX duplicate_candidates_claim_idx
ON duplicate_candidates (tenant_id, claim_set_id, kind, candidate_key);

CREATE TRIGGER duplicate_candidates_limit
BEFORE INSERT ON duplicate_candidates
WHEN (SELECT count(*) FROM duplicate_candidates
      WHERE tenant_id = NEW.tenant_id AND claim_set_id = NEW.claim_set_id) >= 50
BEGIN
    SELECT RAISE(ABORT, 'duplicate_candidate_limit_exceeded');
END;

CREATE TRIGGER duplicate_candidates_require_draft_claim
BEFORE INSERT ON duplicate_candidates
WHEN NOT EXISTS (
    SELECT 1 FROM claim_sets claim
    WHERE claim.tenant_id = NEW.tenant_id
      AND claim.id = NEW.claim_set_id
      AND claim.status = 'draft'
)
BEGIN
    SELECT RAISE(ABORT, 'duplicate_candidate_draft_claim_required');
END;

CREATE TRIGGER duplicate_candidates_target_scope
BEFORE INSERT ON duplicate_candidates
WHEN NOT (
    (NEW.kind = 'near_file'
     AND EXISTS (
         SELECT 1
         FROM claim_sets claim
         JOIN documents target ON target.tenant_id = claim.tenant_id
         WHERE claim.tenant_id = NEW.tenant_id
           AND claim.id = NEW.claim_set_id
           AND target.id = NEW.existing_document_id
           AND target.id <> claim.document_id
     ))
    OR
    (NEW.kind = 'cross_page'
     AND EXISTS (
         SELECT 1
         FROM claim_sets claim
         JOIN document_pages current_page
           ON current_page.tenant_id = claim.tenant_id
          AND current_page.document_id = claim.document_id
         JOIN documents target
           ON target.tenant_id = claim.tenant_id
          AND target.id = NEW.existing_document_id
         JOIN document_pages existing_page
           ON existing_page.tenant_id = target.tenant_id
          AND existing_page.document_id = target.id
         WHERE claim.tenant_id = NEW.tenant_id
           AND claim.id = NEW.claim_set_id
           AND current_page.id = NEW.current_document_page_id
           AND existing_page.id = NEW.existing_document_page_id
           AND current_page.id <> existing_page.id
     ))
    OR
    (NEW.kind = 'field_combination'
     AND EXISTS (
         SELECT 1
         FROM claim_sets claim
         LEFT JOIN payments payment
           ON payment.tenant_id = claim.tenant_id
          AND payment.id = NEW.existing_payment_id
          AND payment.deleted_at IS NULL
         LEFT JOIN invoices invoice
           ON invoice.tenant_id = claim.tenant_id
          AND invoice.id = NEW.existing_invoice_id
          AND invoice.deleted_at IS NULL
         WHERE claim.tenant_id = NEW.tenant_id
           AND claim.id = NEW.claim_set_id
           AND ((claim.document_type = 'payment' AND payment.id IS NOT NULL)
             OR (claim.document_type = 'invoice' AND invoice.id IS NOT NULL))
     ))
)
BEGIN
    SELECT RAISE(ABORT, 'duplicate_candidate_scope_mismatch');
END;

CREATE TRIGGER duplicate_candidates_immutable
BEFORE UPDATE ON duplicate_candidates
BEGIN
    SELECT RAISE(ABORT, 'duplicate_candidate_immutable');
END;

CREATE TABLE duplicate_candidate_decisions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    candidate_id TEXT NOT NULL,
    review_decision_id TEXT NOT NULL,
    action TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (tenant_id, candidate_id) REFERENCES duplicate_candidates(tenant_id, id),
    FOREIGN KEY (tenant_id, review_decision_id) REFERENCES review_decisions(tenant_id, id),
    CHECK (action = 'keep_distinct'),
    UNIQUE (tenant_id, candidate_id),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE TRIGGER duplicate_candidate_decisions_same_claim
BEFORE INSERT ON duplicate_candidate_decisions
WHEN NOT EXISTS (
    SELECT 1
    FROM duplicate_candidates candidate
    JOIN review_decisions review
      ON review.tenant_id = candidate.tenant_id
     AND review.claim_set_id = candidate.claim_set_id
    WHERE candidate.tenant_id = NEW.tenant_id
      AND candidate.id = NEW.candidate_id
      AND review.id = NEW.review_decision_id
      AND review.action = 'confirm'
)
 OR EXISTS (
    SELECT 1
    FROM duplicate_candidate_decisions existing_decision
    JOIN duplicate_candidates existing_candidate
      ON existing_candidate.tenant_id = existing_decision.tenant_id
     AND existing_candidate.id = existing_decision.candidate_id
    JOIN duplicate_candidates new_candidate
      ON new_candidate.tenant_id = NEW.tenant_id
     AND new_candidate.id = NEW.candidate_id
    WHERE existing_decision.tenant_id = NEW.tenant_id
      AND existing_candidate.claim_set_id = new_candidate.claim_set_id
      AND existing_decision.review_decision_id <> NEW.review_decision_id
 )
BEGIN
    SELECT RAISE(ABORT, 'duplicate_decision_scope_mismatch');
END;

CREATE TRIGGER duplicate_candidate_decisions_immutable
BEFORE UPDATE ON duplicate_candidate_decisions
BEGIN
    SELECT RAISE(ABORT, 'duplicate_candidate_decision_immutable');
END;

CREATE TRIGGER duplicate_candidate_decisions_delete_forbidden
BEFORE DELETE ON duplicate_candidate_decisions
BEGIN
    SELECT RAISE(ABORT, 'duplicate_candidate_decision_immutable');
END;

CREATE TABLE audit_events (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    actor_user_id TEXT,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    safe_metadata_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, actor_user_id) REFERENCES memberships(tenant_id, user_id),
    CHECK (json_valid(safe_metadata_json)),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE TABLE payment_invoice_link_decisions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    candidate_id TEXT NOT NULL,
    review_decision_id TEXT NOT NULL,
    action TEXT NOT NULL,
    allocated_minor INTEGER,
    currency TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (tenant_id, candidate_id) REFERENCES payment_invoice_link_candidates(tenant_id, id),
    FOREIGN KEY (tenant_id, review_decision_id) REFERENCES review_decisions(tenant_id, id),
    CHECK (action IN ('accept', 'reject')),
    CHECK (
        (action = 'accept'
         AND allocated_minor IS NOT NULL
         AND allocated_minor BETWEEN 1 AND 9007199254740991
         AND currency IS NOT NULL
         AND currency IN ('CNY', 'USD', 'EUR', 'JPY'))
        OR
        (action = 'reject' AND allocated_minor IS NULL AND currency IS NULL)
    ),
    UNIQUE (tenant_id, candidate_id),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE TRIGGER link_decisions_same_claim
BEFORE INSERT ON payment_invoice_link_decisions
WHEN NOT EXISTS (
    SELECT 1
    FROM payment_invoice_link_candidates c
    JOIN review_decisions r
      ON r.tenant_id = c.tenant_id AND r.claim_set_id = c.claim_set_id
    WHERE c.tenant_id = NEW.tenant_id
      AND c.id = NEW.candidate_id
      AND r.id = NEW.review_decision_id
      AND r.action = 'confirm'
)
BEGIN
    SELECT RAISE(ABORT, 'link_decision_scope_mismatch');
END;

CREATE TABLE payment_invoice_allocation_adjustments (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    actor_user_id TEXT NOT NULL,
    anchor_fact_type TEXT NOT NULL,
    anchor_payment_id TEXT,
    anchor_invoice_id TEXT,
    mode TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    expected_plan_hash TEXT NOT NULL,
    resulting_plan_hash TEXT NOT NULL,
    reason TEXT NOT NULL,
    audit_event_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (tenant_id, actor_user_id) REFERENCES memberships(tenant_id, user_id),
    FOREIGN KEY (tenant_id, anchor_payment_id) REFERENCES payments(tenant_id, id),
    FOREIGN KEY (tenant_id, anchor_invoice_id) REFERENCES invoices(tenant_id, id),
    FOREIGN KEY (tenant_id, audit_event_id) REFERENCES audit_events(tenant_id, id),
    CHECK (anchor_fact_type IN ('payment', 'invoice')),
    CHECK ((anchor_fact_type = 'payment' AND anchor_payment_id IS NOT NULL AND anchor_invoice_id IS NULL)
        OR (anchor_fact_type = 'invoice' AND anchor_invoice_id IS NOT NULL AND anchor_payment_id IS NULL)),
    CHECK (mode IN ('supplement', 'withdraw', 'replace')),
    CHECK (length(idempotency_key) BETWEEN 8 AND 128),
    CHECK (instr(idempotency_key, ' ') = 0
        AND instr(idempotency_key, char(9)) = 0
        AND instr(idempotency_key, char(10)) = 0
        AND instr(idempotency_key, char(13)) = 0),
    CHECK (length(request_hash) = 64 AND request_hash NOT GLOB '*[^0-9a-f]*'),
    CHECK (length(expected_plan_hash) = 64 AND expected_plan_hash NOT GLOB '*[^0-9a-f]*'),
    CHECK (length(resulting_plan_hash) = 64 AND resulting_plan_hash NOT GLOB '*[^0-9a-f]*'),
    CHECK (reason = trim(reason) AND length(reason) BETWEEN 1 AND 500),
    UNIQUE (tenant_id, idempotency_key),
    UNIQUE (tenant_id, audit_event_id),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE TRIGGER payment_invoice_allocation_adjustments_anchor_available
BEFORE INSERT ON payment_invoice_allocation_adjustments
WHEN NOT EXISTS (
    SELECT 1
    FROM payments p
    JOIN review_decisions r ON r.tenant_id = p.tenant_id AND r.id = p.source_review_decision_id
    WHERE NEW.anchor_fact_type = 'payment'
      AND p.tenant_id = NEW.tenant_id
      AND p.id = NEW.anchor_payment_id
      AND p.deleted_at IS NULL
      AND r.action = 'confirm'
    UNION ALL
    SELECT 1
    FROM invoices i
    JOIN review_decisions r ON r.tenant_id = i.tenant_id AND r.id = i.source_review_decision_id
    WHERE NEW.anchor_fact_type = 'invoice'
      AND i.tenant_id = NEW.tenant_id
      AND i.id = NEW.anchor_invoice_id
      AND i.deleted_at IS NULL
      AND r.action = 'confirm'
)
BEGIN
    SELECT RAISE(ABORT, 'allocation_anchor_unavailable');
END;

CREATE TRIGGER payment_invoice_allocation_adjustments_immutable
BEFORE UPDATE ON payment_invoice_allocation_adjustments
BEGIN
    SELECT RAISE(ABORT, 'allocation_adjustment_immutable');
END;

CREATE TRIGGER payment_invoice_allocation_adjustments_delete_forbidden
BEFORE DELETE ON payment_invoice_allocation_adjustments
BEGIN
    SELECT RAISE(ABORT, 'allocation_adjustment_immutable');
END;

CREATE TABLE payment_invoice_links (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    payment_id TEXT NOT NULL,
    invoice_id TEXT NOT NULL,
    link_decision_id TEXT,
    created_by_adjustment_id TEXT,
    allocated_minor INTEGER NOT NULL,
    currency TEXT NOT NULL,
    created_at TEXT NOT NULL,
    ended_at TEXT,
    ended_by_audit_event_id TEXT,
    ended_by_adjustment_id TEXT,
    FOREIGN KEY (tenant_id, payment_id) REFERENCES payments(tenant_id, id),
    FOREIGN KEY (tenant_id, invoice_id) REFERENCES invoices(tenant_id, id),
    FOREIGN KEY (tenant_id, link_decision_id) REFERENCES payment_invoice_link_decisions(tenant_id, id),
    FOREIGN KEY (tenant_id, created_by_adjustment_id) REFERENCES payment_invoice_allocation_adjustments(tenant_id, id),
    FOREIGN KEY (tenant_id, ended_by_audit_event_id) REFERENCES audit_events(tenant_id, id),
    FOREIGN KEY (tenant_id, ended_by_adjustment_id) REFERENCES payment_invoice_allocation_adjustments(tenant_id, id),
    CHECK (allocated_minor BETWEEN 1 AND 9007199254740991),
    CHECK (currency IN ('CNY', 'USD', 'EUR', 'JPY')),
    CHECK ((link_decision_id IS NOT NULL) + (created_by_adjustment_id IS NOT NULL) = 1),
    CHECK ((ended_at IS NULL AND ended_by_audit_event_id IS NULL AND ended_by_adjustment_id IS NULL)
        OR (ended_at IS NOT NULL
            AND (ended_by_audit_event_id IS NOT NULL) + (ended_by_adjustment_id IS NOT NULL) = 1)),
    UNIQUE (tenant_id, link_decision_id),
    UNIQUE (tenant_id, created_by_adjustment_id, payment_id, invoice_id),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE UNIQUE INDEX payment_invoice_links_pair_active_idx
ON payment_invoice_links (tenant_id, payment_id, invoice_id)
WHERE ended_at IS NULL;

CREATE TRIGGER payment_invoice_links_immutable_fields
BEFORE UPDATE OF id, tenant_id, payment_id, invoice_id, link_decision_id, created_by_adjustment_id,
                 allocated_minor, currency, created_at
ON payment_invoice_links
WHEN NEW.id IS NOT OLD.id
  OR NEW.tenant_id IS NOT OLD.tenant_id
  OR NEW.payment_id IS NOT OLD.payment_id
  OR NEW.invoice_id IS NOT OLD.invoice_id
  OR NEW.link_decision_id IS NOT OLD.link_decision_id
  OR NEW.created_by_adjustment_id IS NOT OLD.created_by_adjustment_id
  OR NEW.allocated_minor IS NOT OLD.allocated_minor
  OR NEW.currency IS NOT OLD.currency
  OR NEW.created_at IS NOT OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'payment_invoice_link_immutable');
END;

CREATE TRIGGER payment_invoice_links_end_once
BEFORE UPDATE OF ended_at, ended_by_audit_event_id, ended_by_adjustment_id
ON payment_invoice_links
WHEN NOT (
    OLD.ended_at IS NULL
    AND OLD.ended_by_audit_event_id IS NULL
    AND OLD.ended_by_adjustment_id IS NULL
    AND NEW.ended_at IS NOT NULL
    AND (NEW.ended_by_audit_event_id IS NOT NULL) + (NEW.ended_by_adjustment_id IS NOT NULL) = 1
)
BEGIN
    SELECT RAISE(ABORT, 'payment_invoice_link_end_once');
END;

CREATE TRIGGER payment_invoice_links_history_required
BEFORE DELETE ON payment_invoice_links
BEGIN
    SELECT RAISE(ABORT, 'payment_invoice_link_history_required');
END;

CREATE TRIGGER payment_invoice_links_accept_only
BEFORE INSERT ON payment_invoice_links
WHEN NEW.link_decision_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM payment_invoice_link_decisions d
    WHERE d.tenant_id = NEW.tenant_id
      AND d.id = NEW.link_decision_id
      AND d.action = 'accept'
      AND d.allocated_minor = NEW.allocated_minor
      AND d.currency = NEW.currency
)
BEGIN
    SELECT RAISE(ABORT, 'accepted_link_decision_required');
END;

CREATE TRIGGER payment_invoice_links_candidate_scope
BEFORE INSERT ON payment_invoice_links
WHEN NEW.link_decision_id IS NOT NULL AND NOT EXISTS (
    SELECT 1
    FROM payment_invoice_link_decisions d
    JOIN payment_invoice_link_candidates c
      ON c.tenant_id = d.tenant_id AND c.id = d.candidate_id
    JOIN review_decisions r
      ON r.tenant_id = d.tenant_id AND r.id = d.review_decision_id
    WHERE d.tenant_id = NEW.tenant_id
      AND d.id = NEW.link_decision_id
      AND (
          (c.existing_payment_id = NEW.payment_id
           AND EXISTS (
               SELECT 1 FROM invoices i
               WHERE i.tenant_id = NEW.tenant_id
                 AND i.id = NEW.invoice_id
                 AND i.source_review_decision_id = r.id
           ))
          OR
          (c.existing_invoice_id = NEW.invoice_id
           AND EXISTS (
               SELECT 1 FROM payments p
               WHERE p.tenant_id = NEW.tenant_id
                 AND p.id = NEW.payment_id
                 AND p.source_review_decision_id = r.id
           ))
      )
)
BEGIN
    SELECT RAISE(ABORT, 'link_candidate_scope_mismatch');
END;

CREATE TRIGGER payment_invoice_links_adjustment_scope
BEFORE INSERT ON payment_invoice_links
WHEN NEW.created_by_adjustment_id IS NOT NULL AND NOT EXISTS (
    SELECT 1
    FROM payment_invoice_allocation_adjustments a
    WHERE a.tenant_id = NEW.tenant_id
      AND a.id = NEW.created_by_adjustment_id
      AND (
          (a.anchor_fact_type = 'payment' AND a.anchor_payment_id = NEW.payment_id)
          OR (a.anchor_fact_type = 'invoice' AND a.anchor_invoice_id = NEW.invoice_id)
      )
)
BEGIN
    SELECT RAISE(ABORT, 'allocation_adjustment_scope_mismatch');
END;

CREATE TRIGGER payment_invoice_links_fact_state
BEFORE INSERT ON payment_invoice_links
WHEN NOT EXISTS (
    SELECT 1
    FROM payments p
    JOIN invoices i ON i.tenant_id = p.tenant_id
    WHERE p.tenant_id = NEW.tenant_id
      AND p.id = NEW.payment_id
      AND i.id = NEW.invoice_id
      AND p.deleted_at IS NULL
      AND i.deleted_at IS NULL
      AND p.currency = NEW.currency
      AND i.currency = NEW.currency
)
BEGIN
    SELECT RAISE(ABORT, 'allocation_fact_unavailable');
END;

CREATE TRIGGER payment_invoice_links_payment_balance
BEFORE INSERT ON payment_invoice_links
WHEN coalesce((
         SELECT sum(l.allocated_minor)
         FROM payment_invoice_links l
         WHERE l.tenant_id = NEW.tenant_id
           AND l.payment_id = NEW.payment_id
           AND l.ended_at IS NULL
     ), 0) + NEW.allocated_minor > coalesce((
         SELECT p.amount_minor
         FROM payments p
         WHERE p.tenant_id = NEW.tenant_id AND p.id = NEW.payment_id
     ), -1)
BEGIN
    SELECT RAISE(ABORT, 'payment_allocation_exceeded');
END;

CREATE TRIGGER payment_invoice_links_invoice_balance
BEFORE INSERT ON payment_invoice_links
WHEN coalesce((
         SELECT sum(l.allocated_minor)
         FROM payment_invoice_links l
         WHERE l.tenant_id = NEW.tenant_id
           AND l.invoice_id = NEW.invoice_id
           AND l.ended_at IS NULL
     ), 0) + NEW.allocated_minor > coalesce((
         SELECT i.total_minor
         FROM invoices i
         WHERE i.tenant_id = NEW.tenant_id AND i.id = NEW.invoice_id
     ), -1)
BEGIN
    SELECT RAISE(ABORT, 'invoice_allocation_exceeded');
END;

CREATE TABLE fact_field_origins (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    payment_id TEXT,
    invoice_id TEXT,
    invoice_item_id TEXT,
    field_path TEXT NOT NULL,
    field_claim_id TEXT NOT NULL,
    review_decision_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (tenant_id, payment_id) REFERENCES payments(tenant_id, id),
    FOREIGN KEY (tenant_id, invoice_id) REFERENCES invoices(tenant_id, id),
    FOREIGN KEY (tenant_id, invoice_item_id) REFERENCES invoice_items(tenant_id, id),
    FOREIGN KEY (tenant_id, field_claim_id) REFERENCES field_claims(tenant_id, id),
    FOREIGN KEY (tenant_id, review_decision_id) REFERENCES review_decisions(tenant_id, id),
    CHECK ((payment_id IS NOT NULL) + (invoice_id IS NOT NULL) + (invoice_item_id IS NOT NULL) = 1),
    UNIQUE (tenant_id, payment_id, field_path),
    UNIQUE (tenant_id, invoice_id, field_path),
    UNIQUE (tenant_id, invoice_item_id, field_path),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE TRIGGER fact_field_origins_same_claim
BEFORE INSERT ON fact_field_origins
WHEN NOT EXISTS (
    SELECT 1
    FROM field_claims f
    JOIN review_decisions r
      ON r.tenant_id = f.tenant_id AND r.claim_set_id = f.claim_set_id
    WHERE f.tenant_id = NEW.tenant_id
      AND f.id = NEW.field_claim_id
      AND r.id = NEW.review_decision_id
      AND r.action = 'confirm'
)
BEGIN
    SELECT RAISE(ABORT, 'fact_origin_scope_mismatch');
END;

CREATE TABLE deletion_tombstones (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    actor_user_id TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id_hash TEXT NOT NULL,
    object_hashes_json TEXT NOT NULL,
    resource_counts_json TEXT NOT NULL,
    request_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, actor_user_id) REFERENCES memberships(tenant_id, user_id),
    CHECK (json_valid(object_hashes_json)),
    CHECK (json_valid(resource_counts_json)),
    UNIQUE (tenant_id, id)
) STRICT;

CREATE TRIGGER payments_require_confirmed_review
BEFORE INSERT ON payments
WHEN NOT EXISTS (
    SELECT 1 FROM review_decisions r
    JOIN claim_sets c ON c.tenant_id = r.tenant_id AND c.id = r.claim_set_id
    WHERE r.tenant_id = NEW.tenant_id
      AND r.id = NEW.source_review_decision_id
      AND r.action = 'confirm'
      AND c.document_type = 'payment'
)
BEGIN
    SELECT RAISE(ABORT, 'confirmed_payment_review_required');
END;

CREATE TRIGGER invoices_require_confirmed_review
BEFORE INSERT ON invoices
WHEN NOT EXISTS (
    SELECT 1 FROM review_decisions r
    JOIN claim_sets c ON c.tenant_id = r.tenant_id AND c.id = r.claim_set_id
    WHERE r.tenant_id = NEW.tenant_id
      AND r.id = NEW.source_review_decision_id
      AND r.action = 'confirm'
      AND c.document_type = 'invoice'
)
BEGIN
    SELECT RAISE(ABORT, 'confirmed_invoice_review_required');
END;

CREATE TRIGGER claim_confirmation_requires_duplicate_decisions
BEFORE UPDATE OF status ON claim_sets
WHEN NEW.status = 'confirmed'
 AND (
     (SELECT count(*)
      FROM duplicate_candidates candidate
      WHERE candidate.tenant_id = NEW.tenant_id
        AND candidate.claim_set_id = NEW.id)
     <>
     (SELECT count(*)
      FROM duplicate_candidate_decisions decision
      JOIN duplicate_candidates candidate
        ON candidate.tenant_id = decision.tenant_id
       AND candidate.id = decision.candidate_id
      JOIN review_decisions review
        ON review.tenant_id = decision.tenant_id
       AND review.id = decision.review_decision_id
      WHERE candidate.tenant_id = NEW.tenant_id
        AND candidate.claim_set_id = NEW.id
        AND decision.action = 'keep_distinct'
        AND review.claim_set_id = NEW.id
        AND review.action = 'confirm')
     OR
     (SELECT count(DISTINCT decision.review_decision_id)
      FROM duplicate_candidate_decisions decision
      JOIN duplicate_candidates candidate
        ON candidate.tenant_id = decision.tenant_id
       AND candidate.id = decision.candidate_id
      WHERE candidate.tenant_id = NEW.tenant_id
        AND candidate.claim_set_id = NEW.id) > 1
     OR EXISTS (
     SELECT 1
     FROM duplicate_candidates candidate
     WHERE candidate.tenant_id = NEW.tenant_id
       AND candidate.claim_set_id = NEW.id
       AND NOT EXISTS (
           SELECT 1
           FROM duplicate_candidate_decisions decision
           JOIN review_decisions review
             ON review.tenant_id = decision.tenant_id
            AND review.id = decision.review_decision_id
           WHERE decision.tenant_id = candidate.tenant_id
             AND decision.candidate_id = candidate.id
             AND decision.action = 'keep_distinct'
             AND review.claim_set_id = NEW.id
             AND review.action = 'confirm'
       )
     )
 )
BEGIN
    SELECT RAISE(ABORT, 'duplicate_decisions_incomplete');
END;

INSERT INTO schema_migrations (version, name, applied_at)
VALUES (1, 'initial', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
