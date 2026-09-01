-- PostgreSQL 17 Clean Slate schema. No legacy database has a migration path.

CREATE TABLE users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK (length(email) BETWEEN 3 AND 254),
    CHECK (length(display_name) BETWEEN 1 AND 100)
);

CREATE TABLE tenants (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    default_currency TEXT NOT NULL,
    timezone TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK (length(name) BETWEEN 1 AND 120),
    CHECK (default_currency IN ('CNY', 'USD', 'EUR', 'JPY'))
);

CREATE TABLE memberships (
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    role TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, user_id),
    CHECK (role IN ('owner', 'finance', 'reviewer', 'viewer')),
    CHECK (status IN ('active', 'suspended'))
);

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    csrf_token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    UNIQUE (tenant_id, id)
);

CREATE TABLE provider_configs (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    base_url TEXT NOT NULL,
    encrypted_api_key BYTEA,
    model TEXT NOT NULL,
    output_mode TEXT NOT NULL,
    capability_status TEXT NOT NULL,
    capability_checked_at TIMESTAMPTZ,
    capability_safe_message TEXT,
    capability_schema_version TEXT,
    capability_schema_sha256 TEXT,
    active BOOLEAN NOT NULL DEFAULT FALSE,
    version BIGINT NOT NULL DEFAULT 1,
    safe_fingerprint TEXT NOT NULL,
    created_by_user_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ,
    CHECK (capability_status IN ('pending', 'passed', 'failed')),
    CHECK (output_mode IN ('json_schema', 'json_object')),
    CHECK (version >= 1),
    CHECK (capability_schema_sha256 IS NULL OR length(capability_schema_sha256) = 64),
    CHECK (deleted_at IS NULL OR (active = FALSE AND encrypted_api_key IS NULL)),
    UNIQUE (tenant_id, id)
);

CREATE TABLE documents (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    storage_key TEXT NOT NULL,
    original_name TEXT NOT NULL,
    declared_mime TEXT NOT NULL,
    detected_mime TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    sha256 TEXT NOT NULL,
    page_count BIGINT NOT NULL,
    status TEXT NOT NULL,
    ingestion_kind TEXT NOT NULL DEFAULT 'upload',
    original_object_owner TEXT NOT NULL DEFAULT 'document',
    created_by_user_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (declared_mime IN ('image/jpeg', 'image/png', 'image/webp', 'application/pdf')),
    CHECK (detected_mime IN ('image/jpeg', 'image/png', 'image/webp', 'application/pdf')),
    CHECK (declared_mime = detected_mime),
    CHECK (size_bytes BETWEEN 1 AND 20971520),
    CHECK (length(sha256) = 64),
    CHECK (page_count BETWEEN 1 AND 20),
    CHECK (status IN ('stored', 'processing', 'needs_review', 'blocked', 'failed', 'completed', 'cancelled', 'rejected')),
    CHECK ((ingestion_kind = 'upload' AND original_object_owner = 'document')
        OR (ingestion_kind = 'email_attachment' AND original_object_owner = 'email_attachment')),
    UNIQUE (tenant_id, sha256),
    UNIQUE (tenant_id, storage_key),
    UNIQUE (tenant_id, id)
);

CREATE TABLE email_sources (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    display_name TEXT NOT NULL,
    mailbox_address_normalized TEXT NOT NULL,
    imap_host_normalized TEXT NOT NULL,
    imap_port BIGINT NOT NULL,
    transport_security TEXT NOT NULL,
    status TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    created_by_user_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    last_archived_at TIMESTAMPTZ,
    version BIGINT NOT NULL,
    CHECK (length(display_name) BETWEEN 1 AND 100),
    CHECK (length(mailbox_address_normalized) BETWEEN 3 AND 254),
    CHECK (length(imap_host_normalized) BETWEEN 1 AND 253),
    CHECK (imap_port BETWEEN 1 AND 65535),
    CHECK (transport_security IN ('implicit_tls', 'starttls')),
    CHECK (status IN ('pending_connection', 'active')),
    CHECK (length(idempotency_key) BETWEEN 8 AND 128),
    CHECK (position(' ' in idempotency_key) = 0
        AND position(chr(9) in idempotency_key) = 0
        AND position(chr(10) in idempotency_key) = 0
        AND position(chr(13) in idempotency_key) = 0),
    CHECK (length(request_hash) = 64 AND request_hash ~ '^[0-9a-f]+$'),
    CHECK ((status = 'pending_connection' AND last_archived_at IS NULL)
        OR (status = 'active' AND last_archived_at IS NOT NULL)),
    CHECK (version >= 1),
    UNIQUE (tenant_id, idempotency_key),
    UNIQUE (tenant_id, mailbox_address_normalized, imap_host_normalized, imap_port, transport_security),
    UNIQUE (tenant_id, id)
);

CREATE TABLE email_messages (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    email_source_id TEXT NOT NULL,
    external_message_key TEXT NOT NULL,
    raw_storage_key TEXT NOT NULL,
    raw_sha256 TEXT NOT NULL,
    raw_size_bytes BIGINT NOT NULL,
    subject TEXT NOT NULL,
    sender_address TEXT NOT NULL,
    sent_at TIMESTAMPTZ,
    received_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    safe_error_code TEXT,
    safe_error_text TEXT,
    audit_event_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (length(external_message_key) = 64 AND external_message_key ~ '^[0-9a-f]+$'),
    CHECK (length(raw_sha256) = 64 AND raw_sha256 ~ '^[0-9a-f]+$'),
    CHECK (raw_size_bytes BETWEEN 1 AND 33554432),
    CHECK (length(subject) <= 500),
    CHECK (length(sender_address) <= 254),
    CHECK (status IN ('archived', 'blocked')),
    CHECK ((status = 'archived' AND safe_error_code IS NULL AND safe_error_text IS NULL)
        OR (status = 'blocked' AND length(safe_error_code) BETWEEN 1 AND 100
            AND length(safe_error_text) BETWEEN 1 AND 200)),
    UNIQUE (tenant_id, email_source_id, external_message_key),
    UNIQUE (tenant_id, raw_storage_key),
    UNIQUE (tenant_id, audit_event_id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE email_attachments (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    email_message_id TEXT NOT NULL,
    part_index BIGINT NOT NULL,
    storage_key TEXT,
    original_name TEXT NOT NULL,
    declared_mime TEXT NOT NULL,
    disposition TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    sha256 TEXT NOT NULL,
    processing_status TEXT NOT NULL,
    safe_reason_code TEXT,
    document_id TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (part_index BETWEEN 1 AND 50),
    CHECK (length(original_name) BETWEEN 1 AND 200),
    CHECK (length(declared_mime) BETWEEN 1 AND 200),
    CHECK (disposition IN ('attachment', 'inline')),
    CHECK (size_bytes BETWEEN 0 AND 33554432),
    CHECK (length(sha256) = 64 AND sha256 ~ '^[0-9a-f]+$'),
    CHECK ((size_bytes = 0 AND storage_key IS NULL) OR (size_bytes > 0 AND storage_key IS NOT NULL)),
    CHECK (processing_status IN ('queued', 'existing_document', 'archived_only')),
    CHECK ((processing_status = 'archived_only' AND length(safe_reason_code) BETWEEN 1 AND 100 AND document_id IS NULL)
        OR (processing_status IN ('queued', 'existing_document') AND safe_reason_code IS NULL)),
    UNIQUE (tenant_id, email_message_id, part_index),
    UNIQUE (tenant_id, storage_key),
    UNIQUE (tenant_id, id)
);

CREATE TABLE document_pages (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    page_number BIGINT NOT NULL,
    derived_image_storage_key TEXT NOT NULL,
    width BIGINT NOT NULL,
    height BIGINT NOT NULL,
    sha256 TEXT NOT NULL,
    processing_version TEXT NOT NULL,
    visual_fingerprint_version TEXT NOT NULL,
    dhash64 TEXT NOT NULL,
    ahash64 TEXT NOT NULL,
    dhash_band_0 BIGINT NOT NULL,
    dhash_band_1 BIGINT NOT NULL,
    dhash_band_2 BIGINT NOT NULL,
    dhash_band_3 BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (page_number BETWEEN 1 AND 20),
    CHECK (width BETWEEN 1 AND 8000),
    CHECK (height BETWEEN 1 AND 8000),
    CHECK (length(sha256) = 64),
    CHECK (visual_fingerprint_version = 'page-visual-dedup/1'),
    CHECK (length(dhash64) = 16 AND dhash64 ~ '^[0-9a-f]+$'),
    CHECK (length(ahash64) = 16 AND ahash64 ~ '^[0-9a-f]+$'),
    CHECK (dhash_band_0 BETWEEN 0 AND 65535),
    CHECK (dhash_band_1 BETWEEN 0 AND 65535),
    CHECK (dhash_band_2 BETWEEN 0 AND 65535),
    CHECK (dhash_band_3 BETWEEN 0 AND 65535),
    UNIQUE (tenant_id, document_id, page_number),
    UNIQUE (tenant_id, derived_image_storage_key),
    UNIQUE (tenant_id, id)
);

CREATE TABLE processing_jobs (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    attempt_count BIGINT NOT NULL DEFAULT 0,
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    cancel_requested_at TIMESTAMPTZ,
    error_code TEXT,
    safe_error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1,
    CHECK (kind = 'document_process'),
    CHECK (status IN ('queued', 'processing', 'needs_review', 'blocked', 'failed', 'cancel_requested', 'cancelled', 'completed', 'rejected')),
    CHECK (attempt_count >= 0),
    CHECK (version >= 1),
    CHECK ((status = 'processing' AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL) OR status <> 'processing'),
    UNIQUE (tenant_id, document_id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE ai_runs (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    job_id TEXT NOT NULL,
    provider_config_id TEXT NOT NULL,
    provider_config_version BIGINT NOT NULL,
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
    input_tokens BIGINT,
    output_tokens BIGINT,
    latency_ms BIGINT,
    outcome TEXT NOT NULL,
    error_code TEXT,
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    CHECK (provider_config_version >= 1),
    CHECK (length(provider_schema_sha256) = 64),
    CHECK (outcome IN ('running', 'succeeded', 'failed', 'cancelled')),
    CHECK (input_tokens IS NULL OR input_tokens >= 0),
    CHECK (output_tokens IS NULL OR output_tokens >= 0),
    CHECK (latency_ms IS NULL OR latency_ms >= 0),
    UNIQUE (tenant_id, id)
);

CREATE TABLE claim_sets (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    origin_ai_run_id TEXT NOT NULL,
    produced_by_ai_run_id TEXT,
    revised_by_user_id TEXT,
    document_type TEXT NOT NULL,
    status TEXT NOT NULL,
    revision BIGINT NOT NULL,
    supersedes_claim_set_id TEXT,
    optimistic_version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (document_type IN ('payment', 'invoice', 'trip', 'unknown')),
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
);

CREATE TABLE field_claims (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    claim_set_id TEXT NOT NULL,
    field_path TEXT NOT NULL,
    value_type TEXT NOT NULL,
    presence TEXT NOT NULL,
    typed_value_json JSONB,
    normalized_value TEXT,
    source TEXT NOT NULL,
    source_user_id TEXT,
    supersedes_field_claim_id TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (value_type IN ('string', 'money_minor', 'date', 'instant', 'integer', 'decimal', 'document_type', 'supplementary')),
    CHECK (presence IN ('present', 'absent')),
    CHECK (source IN ('ai', 'user')),
    CHECK ((source = 'ai' AND source_user_id IS NULL) OR (source = 'user' AND source_user_id IS NOT NULL)),
    CHECK (
        (presence = 'absent' AND typed_value_json IS NULL AND normalized_value IS NULL)
        OR
        (presence = 'present' AND typed_value_json IS NOT NULL AND jsonb_typeof(typed_value_json) IS NOT NULL)
    ),
    UNIQUE (tenant_id, claim_set_id, field_path),
    UNIQUE (tenant_id, id)
);

CREATE TABLE evidence (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    field_claim_id TEXT NOT NULL,
    document_page_id TEXT NOT NULL,
    quote TEXT,
    region_json JSONB,
    evidence_hash TEXT NOT NULL,
    copied_from_evidence_id TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (quote IS NOT NULL OR region_json IS NOT NULL),
    CHECK (region_json IS NULL OR jsonb_typeof(region_json) IS NOT NULL),
    CHECK (length(evidence_hash) = 64),
    UNIQUE (tenant_id, id)
);

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
    created_at TIMESTAMPTZ NOT NULL,
    CHECK ((ai_run_id IS NOT NULL) <> (claim_set_id IS NOT NULL)),
    CHECK (field_claim_id IS NULL OR claim_set_id IS NOT NULL),
    CHECK (severity IN ('info', 'warning', 'error', 'blocked')),
    CHECK (status IN ('passed', 'warning', 'error', 'blocked')),
    UNIQUE (tenant_id, id)
);

CREATE TABLE payment_invoice_link_candidates (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    claim_set_id TEXT NOT NULL,
    existing_payment_id TEXT,
    existing_invoice_id TEXT,
    candidate_key TEXT NOT NULL,
    rule_version TEXT NOT NULL,
    reason_codes_json JSONB NOT NULL,
    name_exact BOOLEAN NOT NULL,
    date_distance_days BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK ((existing_payment_id IS NOT NULL) <> (existing_invoice_id IS NOT NULL)),
    CHECK (jsonb_typeof(reason_codes_json) IS NOT NULL),
    CHECK (date_distance_days BETWEEN 0 AND 30),
    UNIQUE (tenant_id, candidate_key),
    UNIQUE (tenant_id, id)
);

CREATE TABLE review_decisions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    claim_set_id TEXT NOT NULL,
    actor_user_id TEXT NOT NULL,
    action TEXT NOT NULL,
    fact_type TEXT,
    association_mode TEXT,
    association_plan_hash TEXT,
    duplicate_plan_hash TEXT,
    idempotency_key TEXT NOT NULL,
    expected_revision BIGINT NOT NULL,
    reason TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (action IN ('confirm', 'reject', 'cancel')),
    CHECK (fact_type IS NULL OR fact_type IN ('payment', 'invoice', 'trip')),
    CHECK (association_mode IS NULL OR association_mode IN ('allocate_candidates', 'reject_all', 'no_candidate')),
    CHECK (
        (action = 'confirm' AND fact_type IN ('payment', 'invoice') AND association_mode IS NOT NULL
         AND ((association_mode = 'allocate_candidates'
               AND association_plan_hash IS NOT NULL
               AND length(association_plan_hash) = 64
               AND association_plan_hash ~ '^[0-9a-f]+$')
              OR (association_mode IN ('reject_all', 'no_candidate') AND association_plan_hash IS NULL)))
        OR
        (action = 'confirm' AND fact_type = 'trip'
         AND association_mode IS NULL AND association_plan_hash IS NULL)
        OR
        (action IN ('reject', 'cancel') AND fact_type IS NULL
         AND association_mode IS NULL AND association_plan_hash IS NULL)
    ),
    CHECK (
        (action = 'confirm'
         AND duplicate_plan_hash IS NOT NULL
         AND length(duplicate_plan_hash) = 64
         AND duplicate_plan_hash ~ '^[0-9a-f]+$')
        OR
        (action IN ('reject', 'cancel') AND duplicate_plan_hash IS NULL)
    ),
    CHECK (expected_revision >= 1),
    UNIQUE (tenant_id, idempotency_key),
    UNIQUE (tenant_id, id)
);

CREATE TABLE payments (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    source_review_decision_id TEXT NOT NULL,
    amount_minor BIGINT NOT NULL,
    currency TEXT NOT NULL,
    merchant TEXT NOT NULL,
    transaction_time TIMESTAMPTZ NOT NULL,
    source_timezone TEXT NOT NULL,
    business_date DATE NOT NULL,
    payment_method TEXT,
    order_number TEXT,
    category TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    deleted_at TIMESTAMPTZ,
    deleted_by_user_id TEXT,
    deletion_audit_event_id TEXT,
    CHECK (amount_minor BETWEEN 0 AND 9007199254740991),
    CHECK (currency IN ('CNY', 'USD', 'EUR', 'JPY')),
    CHECK (business_date IS NOT NULL),
    CHECK (version >= 1),
    CHECK ((deleted_at IS NULL AND deleted_by_user_id IS NULL AND deletion_audit_event_id IS NULL)
        OR (deleted_at IS NOT NULL AND deleted_by_user_id IS NOT NULL AND deletion_audit_event_id IS NOT NULL)),
    UNIQUE (tenant_id, source_review_decision_id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE invoices (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    source_review_decision_id TEXT NOT NULL,
    invoice_number TEXT NOT NULL,
    normalized_invoice_number TEXT NOT NULL,
    invoice_date DATE NOT NULL,
    total_minor BIGINT NOT NULL,
    tax_minor BIGINT,
    currency TEXT NOT NULL,
    seller_name TEXT NOT NULL,
    buyer_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    deleted_at TIMESTAMPTZ,
    deleted_by_user_id TEXT,
    deletion_audit_event_id TEXT,
    CHECK (total_minor BETWEEN 0 AND 9007199254740991),
    CHECK (tax_minor IS NULL OR tax_minor BETWEEN 0 AND 9007199254740991),
    CHECK (currency IN ('CNY', 'USD', 'EUR', 'JPY')),
    CHECK (version >= 1),
    CHECK ((deleted_at IS NULL AND deleted_by_user_id IS NULL AND deletion_audit_event_id IS NULL)
        OR (deleted_at IS NOT NULL AND deleted_by_user_id IS NOT NULL AND deletion_audit_event_id IS NOT NULL)),
    UNIQUE (tenant_id, source_review_decision_id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE invoice_items (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    invoice_id TEXT NOT NULL,
    item_key TEXT NOT NULL,
    name TEXT NOT NULL,
    quantity TEXT,
    unit TEXT,
    unit_price_minor BIGINT,
    amount_minor BIGINT NOT NULL,
    tax_minor BIGINT,
    sort_order BIGINT NOT NULL,
    CHECK (unit_price_minor IS NULL OR unit_price_minor BETWEEN 0 AND 9007199254740991),
    CHECK (amount_minor BETWEEN 0 AND 9007199254740991),
    CHECK (tax_minor IS NULL OR tax_minor BETWEEN 0 AND 9007199254740991),
    CHECK (sort_order >= 0),
    UNIQUE (tenant_id, invoice_id, item_key),
    UNIQUE (tenant_id, id)
);

CREATE TABLE trips (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    source_review_decision_id TEXT NOT NULL,
    origin TEXT,
    destination TEXT NOT NULL,
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    traveler_name TEXT,
    transport_type TEXT,
    booking_reference TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    deleted_at TIMESTAMPTZ,
    deleted_by_user_id TEXT,
    deletion_audit_event_id TEXT,
    CHECK (length(trim(destination)) BETWEEN 1 AND 500),
    CHECK (start_date IS NOT NULL AND end_date IS NOT NULL AND end_date >= start_date),
    CHECK (version >= 1),
    CHECK ((deleted_at IS NULL AND deleted_by_user_id IS NULL AND deletion_audit_event_id IS NULL)
        OR (deleted_at IS NOT NULL AND deleted_by_user_id IS NOT NULL AND deletion_audit_event_id IS NOT NULL)),
    UNIQUE (tenant_id, source_review_decision_id),
    UNIQUE (tenant_id, id)
);

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
    reason_codes_json JSONB NOT NULL,
    dhash_distance BIGINT,
    ahash_distance BIGINT,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (kind IN ('near_file', 'cross_page', 'field_combination')),
    CHECK (rule_version = 'duplicate-detection/1'),
    CHECK (length(candidate_key) = 64 AND candidate_key ~ '^[0-9a-f]+$'),
    CHECK (jsonb_typeof(reason_codes_json) IS NOT NULL AND jsonb_typeof(reason_codes_json) = 'array'),
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
);

CREATE TABLE duplicate_candidate_decisions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    candidate_id TEXT NOT NULL,
    review_decision_id TEXT NOT NULL,
    action TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (action = 'keep_distinct'),
    UNIQUE (tenant_id, candidate_id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE audit_events (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    actor_user_id TEXT,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    safe_metadata_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (jsonb_typeof(safe_metadata_json) IS NOT NULL),
    UNIQUE (tenant_id, id)
);

CREATE TABLE payment_invoice_link_decisions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    candidate_id TEXT NOT NULL,
    review_decision_id TEXT NOT NULL,
    action TEXT NOT NULL,
    allocated_minor BIGINT,
    currency TEXT,
    created_at TIMESTAMPTZ NOT NULL,
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
);

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
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (anchor_fact_type IN ('payment', 'invoice')),
    CHECK ((anchor_fact_type = 'payment' AND anchor_payment_id IS NOT NULL AND anchor_invoice_id IS NULL)
        OR (anchor_fact_type = 'invoice' AND anchor_invoice_id IS NOT NULL AND anchor_payment_id IS NULL)),
    CHECK (mode IN ('supplement', 'withdraw', 'replace')),
    CHECK (length(idempotency_key) BETWEEN 8 AND 128),
    CHECK (position(' ' in idempotency_key) = 0
        AND position(chr(9) in idempotency_key) = 0
        AND position(chr(10) in idempotency_key) = 0
        AND position(chr(13) in idempotency_key) = 0),
    CHECK (length(request_hash) = 64 AND request_hash ~ '^[0-9a-f]+$'),
    CHECK (length(expected_plan_hash) = 64 AND expected_plan_hash ~ '^[0-9a-f]+$'),
    CHECK (length(resulting_plan_hash) = 64 AND resulting_plan_hash ~ '^[0-9a-f]+$'),
    CHECK (reason = trim(reason) AND length(reason) BETWEEN 1 AND 500),
    UNIQUE (tenant_id, idempotency_key),
    UNIQUE (tenant_id, audit_event_id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE payment_invoice_links (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    payment_id TEXT NOT NULL,
    invoice_id TEXT NOT NULL,
    link_decision_id TEXT,
    created_by_adjustment_id TEXT,
    allocated_minor BIGINT NOT NULL,
    currency TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
    ended_by_audit_event_id TEXT,
    ended_by_adjustment_id TEXT,
    CHECK (allocated_minor BETWEEN 1 AND 9007199254740991),
    CHECK (currency IN ('CNY', 'USD', 'EUR', 'JPY')),
    CHECK (num_nonnulls(link_decision_id, created_by_adjustment_id) = 1),
    CHECK ((ended_at IS NULL AND ended_by_audit_event_id IS NULL AND ended_by_adjustment_id IS NULL)
        OR (ended_at IS NOT NULL
            AND num_nonnulls(ended_by_audit_event_id, ended_by_adjustment_id) = 1)),
    UNIQUE (tenant_id, link_decision_id),
    UNIQUE (tenant_id, created_by_adjustment_id, payment_id, invoice_id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE fact_field_origins (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    payment_id TEXT,
    invoice_id TEXT,
    invoice_item_id TEXT,
    trip_id TEXT,
    field_path TEXT NOT NULL,
    field_claim_id TEXT NOT NULL,
    review_decision_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (num_nonnulls(payment_id, invoice_id, invoice_item_id, trip_id) = 1),
    UNIQUE (tenant_id, payment_id, field_path),
    UNIQUE (tenant_id, invoice_id, field_path),
    UNIQUE (tenant_id, invoice_item_id, field_path),
    UNIQUE (tenant_id, trip_id, field_path),
    UNIQUE (tenant_id, id)
);

CREATE TABLE trip_fact_assignment_decisions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    actor_user_id TEXT NOT NULL,
    fact_type TEXT NOT NULL,
    payment_id TEXT,
    invoice_id TEXT,
    previous_assignment_id TEXT,
    desired_trip_id TEXT,
    action TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    reason TEXT NOT NULL,
    audit_event_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (fact_type IN ('payment', 'invoice')),
    CHECK ((fact_type = 'payment' AND payment_id IS NOT NULL AND invoice_id IS NULL)
        OR (fact_type = 'invoice' AND invoice_id IS NOT NULL AND payment_id IS NULL)),
    CHECK (action IN ('assign', 'move', 'unassign')),
    CHECK ((action = 'assign' AND previous_assignment_id IS NULL AND desired_trip_id IS NOT NULL)
        OR (action = 'move' AND previous_assignment_id IS NOT NULL AND desired_trip_id IS NOT NULL)
        OR (action = 'unassign' AND previous_assignment_id IS NOT NULL AND desired_trip_id IS NULL)),
    CHECK (length(idempotency_key) BETWEEN 8 AND 128),
    CHECK (position(' ' in idempotency_key) = 0
        AND position(chr(9) in idempotency_key) = 0
        AND position(chr(10) in idempotency_key) = 0
        AND position(chr(13) in idempotency_key) = 0),
    CHECK (length(request_hash) = 64 AND request_hash ~ '^[0-9a-f]+$'),
    CHECK (reason = trim(reason) AND length(reason) BETWEEN 1 AND 500),
    UNIQUE (tenant_id, idempotency_key),
    UNIQUE (tenant_id, audit_event_id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE trip_fact_assignments (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    trip_id TEXT NOT NULL,
    payment_id TEXT,
    invoice_id TEXT,
    created_by_decision_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
    ended_by_decision_id TEXT,
    ended_by_audit_event_id TEXT,
    CHECK ((payment_id IS NOT NULL) <> (invoice_id IS NOT NULL)),
    CHECK ((ended_at IS NULL AND ended_by_decision_id IS NULL AND ended_by_audit_event_id IS NULL)
        OR (ended_at IS NOT NULL AND (ended_by_decision_id IS NOT NULL) <> (ended_by_audit_event_id IS NOT NULL))),
    UNIQUE (tenant_id, created_by_decision_id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE reimbursements (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    trip_id TEXT NOT NULL,
    trip_destination TEXT NOT NULL,
    trip_start_date DATE NOT NULL,
    trip_end_date DATE NOT NULL,
    status TEXT NOT NULL,
    policy_rule_version TEXT NOT NULL,
    snapshot_hash TEXT NOT NULL,
    created_by_user_id TEXT NOT NULL,
    created_by_decision_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    version BIGINT NOT NULL,
    CHECK (length(trim(trip_destination)) BETWEEN 1 AND 500),
    CHECK (trip_start_date IS NOT NULL AND trip_end_date IS NOT NULL
        AND trip_end_date >= trip_start_date),
    CHECK (status IN ('submitted', 'reimbursed', 'rejected')),
    CHECK (policy_rule_version = 'reimbursement-policy/1'),
    CHECK (length(snapshot_hash) = 64 AND snapshot_hash ~ '^[0-9a-f]+$'),
    CHECK (version >= 1),
    UNIQUE (tenant_id, id)
);

CREATE TABLE reimbursement_items (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    reimbursement_id TEXT NOT NULL,
    trip_fact_assignment_id TEXT NOT NULL,
    fact_type TEXT NOT NULL,
    payment_id TEXT,
    invoice_id TEXT,
    display_name TEXT NOT NULL,
    business_date DATE NOT NULL,
    amount_minor BIGINT NOT NULL,
    currency TEXT NOT NULL,
    sort_order BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (fact_type IN ('payment', 'invoice')),
    CHECK ((fact_type = 'payment' AND payment_id IS NOT NULL AND invoice_id IS NULL)
        OR (fact_type = 'invoice' AND invoice_id IS NOT NULL AND payment_id IS NULL)),
    CHECK (length(trim(display_name)) BETWEEN 1 AND 500),
    CHECK (business_date IS NOT NULL),
    CHECK (amount_minor BETWEEN 0 AND 9007199254740991),
    CHECK (currency IN ('CNY', 'USD', 'EUR', 'JPY')),
    CHECK (sort_order BETWEEN 0 AND 199),
    UNIQUE (tenant_id, reimbursement_id, trip_fact_assignment_id),
    UNIQUE (tenant_id, reimbursement_id, payment_id),
    UNIQUE (tenant_id, reimbursement_id, invoice_id),
    UNIQUE (tenant_id, reimbursement_id, sort_order),
    UNIQUE (tenant_id, id)
);

CREATE TABLE reimbursement_policy_findings (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    reimbursement_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    finding_key TEXT NOT NULL,
    rule_version TEXT NOT NULL,
    code TEXT NOT NULL,
    expected_minor BIGINT,
    actual_minor BIGINT,
    currency TEXT,
    related_reimbursement_id TEXT,
    related_reimbursement_status TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (length(finding_key) = 64 AND finding_key ~ '^[0-9a-f]+$'),
    CHECK (rule_version = 'reimbursement-policy/1'),
    CHECK (code IN ('missing_invoice', 'amount_conflict', 'duplicate_reimbursement')),
    CHECK (expected_minor IS NULL OR expected_minor BETWEEN 0 AND 9007199254740991),
    CHECK (actual_minor IS NULL OR actual_minor BETWEEN 0 AND 9007199254740991),
    CHECK (currency IS NULL OR currency IN ('CNY', 'USD', 'EUR', 'JPY')),
    CHECK (
        (code IN ('missing_invoice', 'amount_conflict')
         AND expected_minor IS NOT NULL AND actual_minor IS NOT NULL AND currency IS NOT NULL
         AND related_reimbursement_id IS NULL AND related_reimbursement_status IS NULL)
        OR
        (code = 'duplicate_reimbursement'
         AND expected_minor IS NULL AND actual_minor IS NULL AND currency IS NULL
         AND related_reimbursement_id IS NOT NULL
         AND related_reimbursement_status IN ('submitted', 'reimbursed'))
    ),
    UNIQUE (tenant_id, reimbursement_id, finding_key),
    UNIQUE (tenant_id, id)
);

CREATE TABLE reimbursement_status_decisions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    reimbursement_id TEXT NOT NULL,
    actor_user_id TEXT NOT NULL,
    previous_status TEXT,
    desired_status TEXT NOT NULL,
    expected_version BIGINT NOT NULL,
    result_version BIGINT NOT NULL,
    action TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    reason TEXT NOT NULL,
    audit_event_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (previous_status IS NULL OR previous_status IN ('submitted', 'reimbursed', 'rejected')),
    CHECK (desired_status IN ('submitted', 'reimbursed', 'rejected')),
    CHECK (expected_version >= 0 AND result_version = expected_version + 1),
    CHECK (action IN ('submit', 'mark_reimbursed', 'reject', 'reopen')),
    CHECK (
        (action = 'submit' AND previous_status IS NULL AND desired_status = 'submitted'
         AND expected_version = 0 AND result_version = 1)
        OR
        (action = 'mark_reimbursed' AND previous_status = 'submitted' AND desired_status = 'reimbursed'
         AND expected_version >= 1)
        OR
        (action = 'reject' AND previous_status = 'submitted' AND desired_status = 'rejected'
         AND expected_version >= 1)
        OR
        (action = 'reopen' AND previous_status IN ('reimbursed', 'rejected') AND desired_status = 'submitted'
         AND expected_version >= 1)
    ),
    CHECK (length(idempotency_key) BETWEEN 8 AND 128),
    CHECK (position(' ' in idempotency_key) = 0
        AND position(chr(9) in idempotency_key) = 0
        AND position(chr(10) in idempotency_key) = 0
        AND position(chr(13) in idempotency_key) = 0),
    CHECK (length(request_hash) = 64 AND request_hash ~ '^[0-9a-f]+$'),
    CHECK (reason = trim(reason) AND length(reason) BETWEEN 1 AND 500),
    UNIQUE (tenant_id, idempotency_key),
    UNIQUE (tenant_id, reimbursement_id, result_version),
    UNIQUE (tenant_id, audit_event_id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE deletion_tombstones (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    actor_user_id TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id_hash TEXT NOT NULL,
    object_hashes_json JSONB NOT NULL,
    resource_counts_json JSONB NOT NULL,
    request_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (jsonb_typeof(object_hashes_json) IS NOT NULL),
    CHECK (jsonb_typeof(resource_counts_json) IS NOT NULL),
    UNIQUE (tenant_id, id)
);

CREATE UNIQUE INDEX users_email_normalized_idx ON users (lower(email));

CREATE INDEX sessions_principal_idx
ON sessions (tenant_id, user_id, expires_at)
WHERE revoked_at IS NULL;

CREATE UNIQUE INDEX provider_configs_one_active_idx
ON provider_configs (tenant_id)
WHERE active = TRUE AND deleted_at IS NULL;

CREATE INDEX email_sources_tenant_created_idx
ON email_sources (tenant_id, created_at, id);

CREATE INDEX email_messages_source_received_idx
ON email_messages (tenant_id, email_source_id, received_at DESC, id DESC);

CREATE INDEX document_pages_visual_band_0_idx
ON document_pages (tenant_id, visual_fingerprint_version, dhash_band_0);

CREATE INDEX document_pages_visual_band_1_idx
ON document_pages (tenant_id, visual_fingerprint_version, dhash_band_1);

CREATE INDEX document_pages_visual_band_2_idx
ON document_pages (tenant_id, visual_fingerprint_version, dhash_band_2);

CREATE INDEX document_pages_visual_band_3_idx
ON document_pages (tenant_id, visual_fingerprint_version, dhash_band_3);

CREATE INDEX processing_jobs_claim_idx
ON processing_jobs (status, lease_expires_at, created_at);

CREATE INDEX processing_jobs_tenant_created_idx
ON processing_jobs (tenant_id, created_at DESC, id DESC);

CREATE INDEX ai_runs_job_idx ON ai_runs (tenant_id, job_id, started_at);

CREATE UNIQUE INDEX claim_sets_one_current_idx
ON claim_sets (tenant_id, document_id)
WHERE status IN ('draft', 'ready_for_review', 'blocked');

CREATE INDEX evidence_field_idx
ON evidence (tenant_id, field_claim_id);

CREATE INDEX validation_results_claim_idx
ON validation_results (tenant_id, claim_set_id, status);

CREATE INDEX payments_tenant_time_active_idx
ON payments (tenant_id, transaction_time DESC, id DESC)
WHERE deleted_at IS NULL;

CREATE INDEX payments_duplicate_match_active_idx
ON payments (tenant_id, currency, amount_minor)
WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX invoices_number_active_idx
ON invoices (tenant_id, normalized_invoice_number)
WHERE deleted_at IS NULL;

CREATE INDEX invoices_tenant_date_active_idx
ON invoices (tenant_id, invoice_date DESC, id DESC)
WHERE deleted_at IS NULL;

CREATE INDEX invoices_duplicate_match_active_idx
ON invoices (tenant_id, currency, total_minor, invoice_date)
WHERE deleted_at IS NULL;

CREATE INDEX trips_tenant_dates_active_idx
ON trips (tenant_id, start_date DESC, end_date DESC, id DESC)
WHERE deleted_at IS NULL;

CREATE INDEX duplicate_candidates_claim_idx
ON duplicate_candidates (tenant_id, claim_set_id, kind, candidate_key);

CREATE UNIQUE INDEX payment_invoice_links_pair_active_idx
ON payment_invoice_links (tenant_id, payment_id, invoice_id)
WHERE ended_at IS NULL;

CREATE UNIQUE INDEX trip_fact_assignments_payment_active_idx
ON trip_fact_assignments (tenant_id, payment_id)
WHERE ended_at IS NULL AND payment_id IS NOT NULL;

CREATE UNIQUE INDEX trip_fact_assignments_invoice_active_idx
ON trip_fact_assignments (tenant_id, invoice_id)
WHERE ended_at IS NULL AND invoice_id IS NOT NULL;

CREATE INDEX trip_fact_assignments_trip_active_idx
ON trip_fact_assignments (tenant_id, trip_id, created_at, id)
WHERE ended_at IS NULL;

CREATE UNIQUE INDEX reimbursements_trip_submitted_idx
ON reimbursements (tenant_id, trip_id)
WHERE status = 'submitted';

CREATE INDEX reimbursements_tenant_created_idx
ON reimbursements (tenant_id, created_at DESC, id DESC);

CREATE INDEX reimbursement_items_fact_idx
ON reimbursement_items (tenant_id, fact_type, payment_id, invoice_id, reimbursement_id);

CREATE INDEX reimbursement_findings_reimbursement_idx
ON reimbursement_policy_findings (tenant_id, reimbursement_id, code, finding_key);

CREATE INDEX payments_tenant_business_date_active_idx
ON payments (tenant_id, business_date DESC, id DESC) INCLUDE (currency)
WHERE deleted_at IS NULL;

CREATE INDEX invoices_tenant_insight_active_idx
ON invoices (tenant_id, invoice_date DESC, currency, id DESC)
WHERE deleted_at IS NULL;

ALTER TABLE memberships ADD CONSTRAINT fk_memberships_1 FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE;

ALTER TABLE memberships ADD CONSTRAINT fk_memberships_2 FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE sessions ADD CONSTRAINT fk_sessions_1 FOREIGN KEY (tenant_id, user_id) REFERENCES memberships(tenant_id, user_id) ON DELETE CASCADE;

ALTER TABLE provider_configs ADD CONSTRAINT fk_provider_configs_1 FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE;

ALTER TABLE provider_configs ADD CONSTRAINT fk_provider_configs_2 FOREIGN KEY (tenant_id, created_by_user_id) REFERENCES memberships(tenant_id, user_id);

ALTER TABLE documents ADD CONSTRAINT fk_documents_1 FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE;

ALTER TABLE documents ADD CONSTRAINT fk_documents_2 FOREIGN KEY (tenant_id, created_by_user_id) REFERENCES memberships(tenant_id, user_id);

ALTER TABLE email_sources ADD CONSTRAINT fk_email_sources_1 FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE;

ALTER TABLE email_sources ADD CONSTRAINT fk_email_sources_2 FOREIGN KEY (tenant_id, created_by_user_id) REFERENCES memberships(tenant_id, user_id);

ALTER TABLE email_messages ADD CONSTRAINT fk_email_messages_1 FOREIGN KEY (tenant_id, email_source_id) REFERENCES email_sources(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE email_messages ADD CONSTRAINT fk_email_messages_2 FOREIGN KEY (tenant_id, audit_event_id) REFERENCES audit_events(tenant_id, id);

ALTER TABLE email_attachments ADD CONSTRAINT fk_email_attachments_1 FOREIGN KEY (tenant_id, email_message_id) REFERENCES email_messages(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE email_attachments ADD CONSTRAINT fk_email_attachments_2 FOREIGN KEY (tenant_id, document_id) REFERENCES documents(tenant_id, id) ON DELETE RESTRICT;

ALTER TABLE document_pages ADD CONSTRAINT fk_document_pages_1 FOREIGN KEY (tenant_id, document_id) REFERENCES documents(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE processing_jobs ADD CONSTRAINT fk_processing_jobs_1 FOREIGN KEY (tenant_id, document_id) REFERENCES documents(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE ai_runs ADD CONSTRAINT fk_ai_runs_1 FOREIGN KEY (tenant_id, job_id) REFERENCES processing_jobs(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE ai_runs ADD CONSTRAINT fk_ai_runs_2 FOREIGN KEY (tenant_id, provider_config_id) REFERENCES provider_configs(tenant_id, id);

ALTER TABLE claim_sets ADD CONSTRAINT fk_claim_sets_1 FOREIGN KEY (tenant_id, document_id) REFERENCES documents(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE claim_sets ADD CONSTRAINT fk_claim_sets_2 FOREIGN KEY (tenant_id, origin_ai_run_id) REFERENCES ai_runs(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE claim_sets ADD CONSTRAINT fk_claim_sets_3 FOREIGN KEY (tenant_id, produced_by_ai_run_id) REFERENCES ai_runs(tenant_id, id);

ALTER TABLE claim_sets ADD CONSTRAINT fk_claim_sets_4 FOREIGN KEY (tenant_id, revised_by_user_id) REFERENCES memberships(tenant_id, user_id);

ALTER TABLE claim_sets ADD CONSTRAINT fk_claim_sets_5 FOREIGN KEY (tenant_id, supersedes_claim_set_id) REFERENCES claim_sets(tenant_id, id);

ALTER TABLE field_claims ADD CONSTRAINT fk_field_claims_1 FOREIGN KEY (tenant_id, claim_set_id) REFERENCES claim_sets(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE field_claims ADD CONSTRAINT fk_field_claims_2 FOREIGN KEY (tenant_id, source_user_id) REFERENCES memberships(tenant_id, user_id);

ALTER TABLE field_claims ADD CONSTRAINT fk_field_claims_3 FOREIGN KEY (tenant_id, supersedes_field_claim_id) REFERENCES field_claims(tenant_id, id);

ALTER TABLE evidence ADD CONSTRAINT fk_evidence_1 FOREIGN KEY (tenant_id, field_claim_id) REFERENCES field_claims(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE evidence ADD CONSTRAINT fk_evidence_2 FOREIGN KEY (tenant_id, document_page_id) REFERENCES document_pages(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE evidence ADD CONSTRAINT fk_evidence_3 FOREIGN KEY (tenant_id, copied_from_evidence_id) REFERENCES evidence(tenant_id, id);

ALTER TABLE validation_results ADD CONSTRAINT fk_validation_results_1 FOREIGN KEY (tenant_id, ai_run_id) REFERENCES ai_runs(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE validation_results ADD CONSTRAINT fk_validation_results_2 FOREIGN KEY (tenant_id, claim_set_id) REFERENCES claim_sets(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE validation_results ADD CONSTRAINT fk_validation_results_3 FOREIGN KEY (tenant_id, field_claim_id) REFERENCES field_claims(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE payment_invoice_link_candidates ADD CONSTRAINT fk_payment_invoice_link_candidates_1 FOREIGN KEY (tenant_id, claim_set_id) REFERENCES claim_sets(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE payment_invoice_link_candidates ADD CONSTRAINT fk_payment_invoice_link_candidates_2 FOREIGN KEY (tenant_id, existing_payment_id) REFERENCES payments(tenant_id, id);

ALTER TABLE payment_invoice_link_candidates ADD CONSTRAINT fk_payment_invoice_link_candidates_3 FOREIGN KEY (tenant_id, existing_invoice_id) REFERENCES invoices(tenant_id, id);

ALTER TABLE review_decisions ADD CONSTRAINT fk_review_decisions_1 FOREIGN KEY (tenant_id, claim_set_id) REFERENCES claim_sets(tenant_id, id);

ALTER TABLE review_decisions ADD CONSTRAINT fk_review_decisions_2 FOREIGN KEY (tenant_id, actor_user_id) REFERENCES memberships(tenant_id, user_id);

ALTER TABLE payments ADD CONSTRAINT fk_payments_1 FOREIGN KEY (tenant_id, source_review_decision_id) REFERENCES review_decisions(tenant_id, id);

ALTER TABLE payments ADD CONSTRAINT fk_payments_2 FOREIGN KEY (tenant_id, deleted_by_user_id) REFERENCES memberships(tenant_id, user_id);

ALTER TABLE payments ADD CONSTRAINT fk_payments_3 FOREIGN KEY (tenant_id, deletion_audit_event_id) REFERENCES audit_events(tenant_id, id);

ALTER TABLE invoices ADD CONSTRAINT fk_invoices_1 FOREIGN KEY (tenant_id, source_review_decision_id) REFERENCES review_decisions(tenant_id, id);

ALTER TABLE invoices ADD CONSTRAINT fk_invoices_2 FOREIGN KEY (tenant_id, deleted_by_user_id) REFERENCES memberships(tenant_id, user_id);

ALTER TABLE invoices ADD CONSTRAINT fk_invoices_3 FOREIGN KEY (tenant_id, deletion_audit_event_id) REFERENCES audit_events(tenant_id, id);

ALTER TABLE invoice_items ADD CONSTRAINT fk_invoice_items_1 FOREIGN KEY (tenant_id, invoice_id) REFERENCES invoices(tenant_id, id);

ALTER TABLE trips ADD CONSTRAINT fk_trips_1 FOREIGN KEY (tenant_id, source_review_decision_id) REFERENCES review_decisions(tenant_id, id);

ALTER TABLE trips ADD CONSTRAINT fk_trips_2 FOREIGN KEY (tenant_id, deleted_by_user_id) REFERENCES memberships(tenant_id, user_id);

ALTER TABLE trips ADD CONSTRAINT fk_trips_3 FOREIGN KEY (tenant_id, deletion_audit_event_id) REFERENCES audit_events(tenant_id, id);

ALTER TABLE duplicate_candidates ADD CONSTRAINT fk_duplicate_candidates_1 FOREIGN KEY (tenant_id, claim_set_id) REFERENCES claim_sets(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE duplicate_candidate_decisions ADD CONSTRAINT fk_duplicate_candidate_decisions_1 FOREIGN KEY (tenant_id, candidate_id) REFERENCES duplicate_candidates(tenant_id, id);

ALTER TABLE duplicate_candidate_decisions ADD CONSTRAINT fk_duplicate_candidate_decisions_2 FOREIGN KEY (tenant_id, review_decision_id) REFERENCES review_decisions(tenant_id, id);

ALTER TABLE audit_events ADD CONSTRAINT fk_audit_events_1 FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE;

ALTER TABLE audit_events ADD CONSTRAINT fk_audit_events_2 FOREIGN KEY (tenant_id, actor_user_id) REFERENCES memberships(tenant_id, user_id);

ALTER TABLE payment_invoice_link_decisions ADD CONSTRAINT fk_payment_invoice_link_decisions_1 FOREIGN KEY (tenant_id, candidate_id) REFERENCES payment_invoice_link_candidates(tenant_id, id);

ALTER TABLE payment_invoice_link_decisions ADD CONSTRAINT fk_payment_invoice_link_decisions_2 FOREIGN KEY (tenant_id, review_decision_id) REFERENCES review_decisions(tenant_id, id);

ALTER TABLE payment_invoice_allocation_adjustments ADD CONSTRAINT fk_payment_invoice_allocation_adjustments_1 FOREIGN KEY (tenant_id, actor_user_id) REFERENCES memberships(tenant_id, user_id);

ALTER TABLE payment_invoice_allocation_adjustments ADD CONSTRAINT fk_payment_invoice_allocation_adjustments_2 FOREIGN KEY (tenant_id, anchor_payment_id) REFERENCES payments(tenant_id, id);

ALTER TABLE payment_invoice_allocation_adjustments ADD CONSTRAINT fk_payment_invoice_allocation_adjustments_3 FOREIGN KEY (tenant_id, anchor_invoice_id) REFERENCES invoices(tenant_id, id);

ALTER TABLE payment_invoice_allocation_adjustments ADD CONSTRAINT fk_payment_invoice_allocation_adjustments_4 FOREIGN KEY (tenant_id, audit_event_id) REFERENCES audit_events(tenant_id, id);

ALTER TABLE payment_invoice_links ADD CONSTRAINT fk_payment_invoice_links_1 FOREIGN KEY (tenant_id, payment_id) REFERENCES payments(tenant_id, id);

ALTER TABLE payment_invoice_links ADD CONSTRAINT fk_payment_invoice_links_2 FOREIGN KEY (tenant_id, invoice_id) REFERENCES invoices(tenant_id, id);

ALTER TABLE payment_invoice_links ADD CONSTRAINT fk_payment_invoice_links_3 FOREIGN KEY (tenant_id, link_decision_id) REFERENCES payment_invoice_link_decisions(tenant_id, id);

ALTER TABLE payment_invoice_links ADD CONSTRAINT fk_payment_invoice_links_4 FOREIGN KEY (tenant_id, created_by_adjustment_id) REFERENCES payment_invoice_allocation_adjustments(tenant_id, id);

ALTER TABLE payment_invoice_links ADD CONSTRAINT fk_payment_invoice_links_5 FOREIGN KEY (tenant_id, ended_by_audit_event_id) REFERENCES audit_events(tenant_id, id);

ALTER TABLE payment_invoice_links ADD CONSTRAINT fk_payment_invoice_links_6 FOREIGN KEY (tenant_id, ended_by_adjustment_id) REFERENCES payment_invoice_allocation_adjustments(tenant_id, id);

ALTER TABLE fact_field_origins ADD CONSTRAINT fk_fact_field_origins_1 FOREIGN KEY (tenant_id, payment_id) REFERENCES payments(tenant_id, id);

ALTER TABLE fact_field_origins ADD CONSTRAINT fk_fact_field_origins_2 FOREIGN KEY (tenant_id, invoice_id) REFERENCES invoices(tenant_id, id);

ALTER TABLE fact_field_origins ADD CONSTRAINT fk_fact_field_origins_3 FOREIGN KEY (tenant_id, invoice_item_id) REFERENCES invoice_items(tenant_id, id);

ALTER TABLE fact_field_origins ADD CONSTRAINT fk_fact_field_origins_4 FOREIGN KEY (tenant_id, trip_id) REFERENCES trips(tenant_id, id);

ALTER TABLE fact_field_origins ADD CONSTRAINT fk_fact_field_origins_5 FOREIGN KEY (tenant_id, field_claim_id) REFERENCES field_claims(tenant_id, id);

ALTER TABLE fact_field_origins ADD CONSTRAINT fk_fact_field_origins_6 FOREIGN KEY (tenant_id, review_decision_id) REFERENCES review_decisions(tenant_id, id);

ALTER TABLE trip_fact_assignment_decisions ADD CONSTRAINT fk_trip_fact_assignment_decisions_1 FOREIGN KEY (tenant_id, actor_user_id) REFERENCES memberships(tenant_id, user_id);

ALTER TABLE trip_fact_assignment_decisions ADD CONSTRAINT fk_trip_fact_assignment_decisions_2 FOREIGN KEY (tenant_id, payment_id) REFERENCES payments(tenant_id, id);

ALTER TABLE trip_fact_assignment_decisions ADD CONSTRAINT fk_trip_fact_assignment_decisions_3 FOREIGN KEY (tenant_id, invoice_id) REFERENCES invoices(tenant_id, id);

ALTER TABLE trip_fact_assignment_decisions ADD CONSTRAINT fk_trip_fact_assignment_decisions_4 FOREIGN KEY (tenant_id, previous_assignment_id) REFERENCES trip_fact_assignments(tenant_id, id);

ALTER TABLE trip_fact_assignment_decisions ADD CONSTRAINT fk_trip_fact_assignment_decisions_5 FOREIGN KEY (tenant_id, desired_trip_id) REFERENCES trips(tenant_id, id);

ALTER TABLE trip_fact_assignment_decisions ADD CONSTRAINT fk_trip_fact_assignment_decisions_6 FOREIGN KEY (tenant_id, audit_event_id) REFERENCES audit_events(tenant_id, id);

ALTER TABLE trip_fact_assignments ADD CONSTRAINT fk_trip_fact_assignments_1 FOREIGN KEY (tenant_id, trip_id) REFERENCES trips(tenant_id, id);

ALTER TABLE trip_fact_assignments ADD CONSTRAINT fk_trip_fact_assignments_2 FOREIGN KEY (tenant_id, payment_id) REFERENCES payments(tenant_id, id);

ALTER TABLE trip_fact_assignments ADD CONSTRAINT fk_trip_fact_assignments_3 FOREIGN KEY (tenant_id, invoice_id) REFERENCES invoices(tenant_id, id);

ALTER TABLE trip_fact_assignments ADD CONSTRAINT fk_trip_fact_assignments_4 FOREIGN KEY (tenant_id, created_by_decision_id) REFERENCES trip_fact_assignment_decisions(tenant_id, id);

ALTER TABLE trip_fact_assignments ADD CONSTRAINT fk_trip_fact_assignments_5 FOREIGN KEY (tenant_id, ended_by_decision_id) REFERENCES trip_fact_assignment_decisions(tenant_id, id);

ALTER TABLE trip_fact_assignments ADD CONSTRAINT fk_trip_fact_assignments_6 FOREIGN KEY (tenant_id, ended_by_audit_event_id) REFERENCES audit_events(tenant_id, id);

ALTER TABLE reimbursements ADD CONSTRAINT fk_reimbursements_1 FOREIGN KEY (tenant_id, trip_id) REFERENCES trips(tenant_id, id);

ALTER TABLE reimbursements ADD CONSTRAINT fk_reimbursements_2 FOREIGN KEY (tenant_id, created_by_user_id) REFERENCES memberships(tenant_id, user_id);

ALTER TABLE reimbursements ADD CONSTRAINT fk_reimbursements_3 FOREIGN KEY (tenant_id, created_by_decision_id)
        REFERENCES reimbursement_status_decisions(tenant_id, id)
        DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE reimbursement_items ADD CONSTRAINT fk_reimbursement_items_1 FOREIGN KEY (tenant_id, reimbursement_id) REFERENCES reimbursements(tenant_id, id);

ALTER TABLE reimbursement_items ADD CONSTRAINT fk_reimbursement_items_2 FOREIGN KEY (tenant_id, trip_fact_assignment_id) REFERENCES trip_fact_assignments(tenant_id, id);

ALTER TABLE reimbursement_items ADD CONSTRAINT fk_reimbursement_items_3 FOREIGN KEY (tenant_id, payment_id) REFERENCES payments(tenant_id, id);

ALTER TABLE reimbursement_items ADD CONSTRAINT fk_reimbursement_items_4 FOREIGN KEY (tenant_id, invoice_id) REFERENCES invoices(tenant_id, id);

ALTER TABLE reimbursement_policy_findings ADD CONSTRAINT fk_reimbursement_policy_findings_1 FOREIGN KEY (tenant_id, reimbursement_id) REFERENCES reimbursements(tenant_id, id);

ALTER TABLE reimbursement_policy_findings ADD CONSTRAINT fk_reimbursement_policy_findings_2 FOREIGN KEY (tenant_id, item_id) REFERENCES reimbursement_items(tenant_id, id);

ALTER TABLE reimbursement_policy_findings ADD CONSTRAINT fk_reimbursement_policy_findings_3 FOREIGN KEY (tenant_id, related_reimbursement_id) REFERENCES reimbursements(tenant_id, id);

ALTER TABLE reimbursement_status_decisions ADD CONSTRAINT fk_reimbursement_status_decisions_1 FOREIGN KEY (tenant_id, reimbursement_id)
        REFERENCES reimbursements(tenant_id, id)
        DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE reimbursement_status_decisions ADD CONSTRAINT fk_reimbursement_status_decisions_2 FOREIGN KEY (tenant_id, actor_user_id) REFERENCES memberships(tenant_id, user_id);

ALTER TABLE reimbursement_status_decisions ADD CONSTRAINT fk_reimbursement_status_decisions_3 FOREIGN KEY (tenant_id, audit_event_id) REFERENCES audit_events(tenant_id, id);

ALTER TABLE deletion_tombstones ADD CONSTRAINT fk_deletion_tombstones_1 FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE;

ALTER TABLE deletion_tombstones ADD CONSTRAINT fk_deletion_tombstones_2 FOREIGN KEY (tenant_id, actor_user_id) REFERENCES memberships(tenant_id, user_id);

CREATE FUNCTION sbm_t_1_memberships_keep_active_owner_() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.role = 'owner'
 AND OLD.status = 'active'
 AND (NEW.role <> 'owner' OR NEW.status <> 'active')
 AND (SELECT count(*) FROM memberships
      WHERE tenant_id = OLD.tenant_id AND role = 'owner' AND status = 'active') = 1 THEN
        RAISE EXCEPTION 'last_active_owner' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER memberships_keep_active_owner_update
BEFORE UPDATE OF role, status ON memberships
FOR EACH ROW EXECUTE FUNCTION sbm_t_1_memberships_keep_active_owner_();

CREATE FUNCTION sbm_t_2_memberships_keep_active_owner_() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.role = 'owner'
 AND OLD.status = 'active'
 AND (SELECT count(*) FROM memberships
      WHERE tenant_id = OLD.tenant_id AND role = 'owner' AND status = 'active') = 1 THEN
        RAISE EXCEPTION 'last_active_owner' USING ERRCODE = 'P0001';
    END IF;
    RETURN OLD;
END;
$$;

CREATE TRIGGER memberships_keep_active_owner_delete
BEFORE DELETE ON memberships
FOR EACH ROW EXECUTE FUNCTION sbm_t_2_memberships_keep_active_owner_();

CREATE FUNCTION sbm_t_3_provider_configs_require_capab() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.active = TRUE AND (
    NEW.capability_status <> 'passed'
    OR NEW.capability_schema_version IS NULL
    OR NEW.capability_schema_sha256 IS NULL
    OR NEW.deleted_at IS NOT NULL
) THEN
        RAISE EXCEPTION 'provider_capability_required' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER provider_configs_require_capability_insert
BEFORE INSERT ON provider_configs
FOR EACH ROW EXECUTE FUNCTION sbm_t_3_provider_configs_require_capab();

CREATE FUNCTION sbm_t_4_provider_configs_require_capab() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.active = TRUE AND (
    NEW.capability_status <> 'passed'
    OR NEW.capability_schema_version IS NULL
    OR NEW.capability_schema_sha256 IS NULL
    OR NEW.deleted_at IS NOT NULL
) THEN
        RAISE EXCEPTION 'provider_capability_required' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER provider_configs_require_capability_update
BEFORE UPDATE OF active, capability_status, capability_schema_version, capability_schema_sha256, deleted_at
ON provider_configs
FOR EACH ROW EXECUTE FUNCTION sbm_t_4_provider_configs_require_capab();

CREATE FUNCTION sbm_t_5_email_sources_configuration_im() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
  OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
  OR NEW.display_name IS DISTINCT FROM OLD.display_name
  OR NEW.mailbox_address_normalized IS DISTINCT FROM OLD.mailbox_address_normalized
  OR NEW.imap_host_normalized IS DISTINCT FROM OLD.imap_host_normalized
  OR NEW.imap_port IS DISTINCT FROM OLD.imap_port
  OR NEW.transport_security IS DISTINCT FROM OLD.transport_security
  OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
  OR NEW.request_hash IS DISTINCT FROM OLD.request_hash
  OR NEW.created_by_user_id IS DISTINCT FROM OLD.created_by_user_id
  OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'email_source_configuration_immutable' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER email_sources_configuration_immutable
BEFORE UPDATE ON email_sources
FOR EACH ROW EXECUTE FUNCTION sbm_t_5_email_sources_configuration_im();

CREATE FUNCTION sbm_t_6_email_sources_state_transition() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT (
    NEW.version = OLD.version + 1
    AND (
        (NEW.status = 'active' AND NEW.last_archived_at IS NOT NULL
            AND (
                OLD.last_archived_at IS NULL
                OR NEW.last_archived_at >= OLD.last_archived_at
                OR NEW.last_archived_at = (
                    SELECT max(m.created_at) FROM email_messages m
                    WHERE m.tenant_id = OLD.tenant_id AND m.email_source_id = OLD.id
                )
            ))
        OR (NEW.status = 'pending_connection' AND NEW.last_archived_at IS NULL
            AND NOT EXISTS (
                SELECT 1 FROM email_messages m
                WHERE m.tenant_id = OLD.tenant_id AND m.email_source_id = OLD.id
            ))
    )
) THEN
        RAISE EXCEPTION 'invalid_email_source_state_transition' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER email_sources_state_transition
BEFORE UPDATE OF status, last_archived_at, version ON email_sources
FOR EACH ROW EXECUTE FUNCTION sbm_t_6_email_sources_state_transition();

CREATE FUNCTION sbm_t_7_email_messages_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'email_message_immutable' USING ERRCODE = 'P0001';
    RETURN NEW;
END;
$$;

CREATE TRIGGER email_messages_immutable
BEFORE UPDATE ON email_messages
FOR EACH ROW EXECUTE FUNCTION sbm_t_7_email_messages_immutable();

CREATE FUNCTION sbm_t_8_email_attachments_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'email_attachment_immutable' USING ERRCODE = 'P0001';
    RETURN NEW;
END;
$$;

CREATE TRIGGER email_attachments_immutable
BEFORE UPDATE OF
    id, tenant_id, email_message_id, part_index, storage_key, original_name,
    declared_mime, disposition, size_bytes, sha256, processing_status,
    safe_reason_code, created_at
ON email_attachments
FOR EACH ROW EXECUTE FUNCTION sbm_t_8_email_attachments_immutable();

CREATE FUNCTION sbm_t_9_email_attachments_document_det() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.document_id IS NULL OR NEW.document_id IS NOT NULL THEN
        RAISE EXCEPTION 'email_attachment_document_link_immutable' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER email_attachments_document_detach_only
BEFORE UPDATE OF document_id ON email_attachments
FOR EACH ROW EXECUTE FUNCTION sbm_t_9_email_attachments_document_det();

CREATE FUNCTION sbm_t_10_email_attachments_document_sco() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.processing_status IN ('queued', 'existing_document') AND NOT EXISTS (
    SELECT 1 FROM documents d
    WHERE d.tenant_id = NEW.tenant_id
      AND d.id = NEW.document_id
      AND d.sha256 = NEW.sha256
) THEN
        RAISE EXCEPTION 'email_attachment_document_scope_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER email_attachments_document_scope
BEFORE INSERT ON email_attachments
FOR EACH ROW EXECUTE FUNCTION sbm_t_10_email_attachments_document_sco();

CREATE FUNCTION sbm_t_11_duplicate_candidates_limit() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF (SELECT count(*) FROM duplicate_candidates
      WHERE tenant_id = NEW.tenant_id AND claim_set_id = NEW.claim_set_id) >= 50 THEN
        RAISE EXCEPTION 'duplicate_candidate_limit_exceeded' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER duplicate_candidates_limit
BEFORE INSERT ON duplicate_candidates
FOR EACH ROW EXECUTE FUNCTION sbm_t_11_duplicate_candidates_limit();

CREATE FUNCTION sbm_t_12_duplicate_candidates_require_d() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
    SELECT 1 FROM claim_sets claim
    WHERE claim.tenant_id = NEW.tenant_id
      AND claim.id = NEW.claim_set_id
      AND claim.status = 'draft'
) THEN
        RAISE EXCEPTION 'duplicate_candidate_draft_claim_required' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER duplicate_candidates_require_draft_claim
BEFORE INSERT ON duplicate_candidates
FOR EACH ROW EXECUTE FUNCTION sbm_t_12_duplicate_candidates_require_d();

CREATE FUNCTION sbm_t_13_duplicate_candidates_target_sc() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT (
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
) THEN
        RAISE EXCEPTION 'duplicate_candidate_scope_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER duplicate_candidates_target_scope
BEFORE INSERT ON duplicate_candidates
FOR EACH ROW EXECUTE FUNCTION sbm_t_13_duplicate_candidates_target_sc();

CREATE FUNCTION sbm_t_14_duplicate_candidates_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'duplicate_candidate_immutable' USING ERRCODE = 'P0001';
    RETURN NEW;
END;
$$;

CREATE TRIGGER duplicate_candidates_immutable
BEFORE UPDATE ON duplicate_candidates
FOR EACH ROW EXECUTE FUNCTION sbm_t_14_duplicate_candidates_immutable();

CREATE FUNCTION sbm_t_15_duplicate_candidate_decisions_() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
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
 ) THEN
        RAISE EXCEPTION 'duplicate_decision_scope_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER duplicate_candidate_decisions_same_claim
BEFORE INSERT ON duplicate_candidate_decisions
FOR EACH ROW EXECUTE FUNCTION sbm_t_15_duplicate_candidate_decisions_();

CREATE FUNCTION sbm_t_16_duplicate_candidate_decisions_() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'duplicate_candidate_decision_immutable' USING ERRCODE = 'P0001';
    RETURN NEW;
END;
$$;

CREATE TRIGGER duplicate_candidate_decisions_immutable
BEFORE UPDATE ON duplicate_candidate_decisions
FOR EACH ROW EXECUTE FUNCTION sbm_t_16_duplicate_candidate_decisions_();

CREATE FUNCTION sbm_t_17_duplicate_candidate_decisions_() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'duplicate_candidate_decision_immutable' USING ERRCODE = 'P0001';
    RETURN OLD;
END;
$$;

CREATE TRIGGER duplicate_candidate_decisions_delete_forbidden
BEFORE DELETE ON duplicate_candidate_decisions
FOR EACH ROW EXECUTE FUNCTION sbm_t_17_duplicate_candidate_decisions_();

CREATE FUNCTION sbm_t_18_link_decisions_same_claim() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
    SELECT 1
    FROM payment_invoice_link_candidates c
    JOIN review_decisions r
      ON r.tenant_id = c.tenant_id AND r.claim_set_id = c.claim_set_id
    WHERE c.tenant_id = NEW.tenant_id
      AND c.id = NEW.candidate_id
      AND r.id = NEW.review_decision_id
      AND r.action = 'confirm'
) THEN
        RAISE EXCEPTION 'link_decision_scope_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER link_decisions_same_claim
BEFORE INSERT ON payment_invoice_link_decisions
FOR EACH ROW EXECUTE FUNCTION sbm_t_18_link_decisions_same_claim();

CREATE FUNCTION sbm_t_19_payment_invoice_allocation_adj() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
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
) THEN
        RAISE EXCEPTION 'allocation_anchor_unavailable' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER payment_invoice_allocation_adjustments_anchor_available
BEFORE INSERT ON payment_invoice_allocation_adjustments
FOR EACH ROW EXECUTE FUNCTION sbm_t_19_payment_invoice_allocation_adj();

CREATE FUNCTION sbm_t_20_payment_invoice_allocation_adj() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'allocation_adjustment_immutable' USING ERRCODE = 'P0001';
    RETURN NEW;
END;
$$;

CREATE TRIGGER payment_invoice_allocation_adjustments_immutable
BEFORE UPDATE ON payment_invoice_allocation_adjustments
FOR EACH ROW EXECUTE FUNCTION sbm_t_20_payment_invoice_allocation_adj();

CREATE FUNCTION sbm_t_21_payment_invoice_allocation_adj() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'allocation_adjustment_immutable' USING ERRCODE = 'P0001';
    RETURN OLD;
END;
$$;

CREATE TRIGGER payment_invoice_allocation_adjustments_delete_forbidden
BEFORE DELETE ON payment_invoice_allocation_adjustments
FOR EACH ROW EXECUTE FUNCTION sbm_t_21_payment_invoice_allocation_adj();

CREATE FUNCTION sbm_t_22_payment_invoice_links_immutabl() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
  OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
  OR NEW.payment_id IS DISTINCT FROM OLD.payment_id
  OR NEW.invoice_id IS DISTINCT FROM OLD.invoice_id
  OR NEW.link_decision_id IS DISTINCT FROM OLD.link_decision_id
  OR NEW.created_by_adjustment_id IS DISTINCT FROM OLD.created_by_adjustment_id
  OR NEW.allocated_minor IS DISTINCT FROM OLD.allocated_minor
  OR NEW.currency IS DISTINCT FROM OLD.currency
  OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'payment_invoice_link_immutable' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER payment_invoice_links_immutable_fields
BEFORE UPDATE OF id, tenant_id, payment_id, invoice_id, link_decision_id, created_by_adjustment_id,
                 allocated_minor, currency, created_at
ON payment_invoice_links
FOR EACH ROW EXECUTE FUNCTION sbm_t_22_payment_invoice_links_immutabl();

CREATE FUNCTION sbm_t_23_payment_invoice_links_end_once() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT (
    OLD.ended_at IS NULL
    AND OLD.ended_by_audit_event_id IS NULL
    AND OLD.ended_by_adjustment_id IS NULL
    AND NEW.ended_at IS NOT NULL
    AND num_nonnulls(NEW.ended_by_audit_event_id, NEW.ended_by_adjustment_id) = 1
) THEN
        RAISE EXCEPTION 'payment_invoice_link_end_once' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER payment_invoice_links_end_once
BEFORE UPDATE OF ended_at, ended_by_audit_event_id, ended_by_adjustment_id
ON payment_invoice_links
FOR EACH ROW EXECUTE FUNCTION sbm_t_23_payment_invoice_links_end_once();

CREATE FUNCTION sbm_t_24_payment_invoice_links_history_() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'payment_invoice_link_history_required' USING ERRCODE = 'P0001';
    RETURN OLD;
END;
$$;

CREATE TRIGGER payment_invoice_links_history_required
BEFORE DELETE ON payment_invoice_links
FOR EACH ROW EXECUTE FUNCTION sbm_t_24_payment_invoice_links_history_();

CREATE FUNCTION sbm_t_25_payment_invoice_links_accept_o() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.link_decision_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM payment_invoice_link_decisions d
    WHERE d.tenant_id = NEW.tenant_id
      AND d.id = NEW.link_decision_id
      AND d.action = 'accept'
      AND d.allocated_minor = NEW.allocated_minor
      AND d.currency = NEW.currency
) THEN
        RAISE EXCEPTION 'accepted_link_decision_required' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER payment_invoice_links_accept_only
BEFORE INSERT ON payment_invoice_links
FOR EACH ROW EXECUTE FUNCTION sbm_t_25_payment_invoice_links_accept_o();

CREATE FUNCTION sbm_t_26_payment_invoice_links_candidat() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.link_decision_id IS NOT NULL AND NOT EXISTS (
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
) THEN
        RAISE EXCEPTION 'link_candidate_scope_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER payment_invoice_links_candidate_scope
BEFORE INSERT ON payment_invoice_links
FOR EACH ROW EXECUTE FUNCTION sbm_t_26_payment_invoice_links_candidat();

CREATE FUNCTION sbm_t_27_payment_invoice_links_adjustme() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.created_by_adjustment_id IS NOT NULL AND NOT EXISTS (
    SELECT 1
    FROM payment_invoice_allocation_adjustments a
    WHERE a.tenant_id = NEW.tenant_id
      AND a.id = NEW.created_by_adjustment_id
      AND (
          (a.anchor_fact_type = 'payment' AND a.anchor_payment_id = NEW.payment_id)
          OR (a.anchor_fact_type = 'invoice' AND a.anchor_invoice_id = NEW.invoice_id)
      )
) THEN
        RAISE EXCEPTION 'allocation_adjustment_scope_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER payment_invoice_links_adjustment_scope
BEFORE INSERT ON payment_invoice_links
FOR EACH ROW EXECUTE FUNCTION sbm_t_27_payment_invoice_links_adjustme();

CREATE FUNCTION sbm_t_28_payment_invoice_links_fact_sta() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
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
) THEN
        RAISE EXCEPTION 'allocation_fact_unavailable' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER payment_invoice_links_fact_state
BEFORE INSERT ON payment_invoice_links
FOR EACH ROW EXECUTE FUNCTION sbm_t_28_payment_invoice_links_fact_sta();

CREATE FUNCTION sbm_t_29_payment_invoice_links_payment_() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF coalesce((
         SELECT sum(l.allocated_minor)
         FROM payment_invoice_links l
         WHERE l.tenant_id = NEW.tenant_id
           AND l.payment_id = NEW.payment_id
           AND l.ended_at IS NULL
     ), 0) + NEW.allocated_minor > coalesce((
         SELECT p.amount_minor
         FROM payments p
         WHERE p.tenant_id = NEW.tenant_id AND p.id = NEW.payment_id
     ), -1) THEN
        RAISE EXCEPTION 'payment_allocation_exceeded' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER payment_invoice_links_payment_balance
BEFORE INSERT ON payment_invoice_links
FOR EACH ROW EXECUTE FUNCTION sbm_t_29_payment_invoice_links_payment_();

CREATE FUNCTION sbm_t_30_payment_invoice_links_invoice_() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF coalesce((
         SELECT sum(l.allocated_minor)
         FROM payment_invoice_links l
         WHERE l.tenant_id = NEW.tenant_id
           AND l.invoice_id = NEW.invoice_id
           AND l.ended_at IS NULL
     ), 0) + NEW.allocated_minor > coalesce((
         SELECT i.total_minor
         FROM invoices i
         WHERE i.tenant_id = NEW.tenant_id AND i.id = NEW.invoice_id
     ), -1) THEN
        RAISE EXCEPTION 'invoice_allocation_exceeded' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER payment_invoice_links_invoice_balance
BEFORE INSERT ON payment_invoice_links
FOR EACH ROW EXECUTE FUNCTION sbm_t_30_payment_invoice_links_invoice_();

CREATE FUNCTION sbm_t_31_trip_assignment_decisions_scop() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
    SELECT 1
    FROM memberships membership
    LEFT JOIN payments payment
      ON payment.tenant_id = membership.tenant_id
     AND payment.id = NEW.payment_id
     AND payment.deleted_at IS NULL
    LEFT JOIN invoices invoice
      ON invoice.tenant_id = membership.tenant_id
     AND invoice.id = NEW.invoice_id
     AND invoice.deleted_at IS NULL
    LEFT JOIN trips desired
      ON desired.tenant_id = membership.tenant_id
     AND desired.id = NEW.desired_trip_id
     AND desired.deleted_at IS NULL
    LEFT JOIN trip_fact_assignments previous
      ON previous.tenant_id = membership.tenant_id
     AND previous.id = NEW.previous_assignment_id
     AND previous.ended_at IS NULL
    WHERE membership.tenant_id = NEW.tenant_id
      AND membership.user_id = NEW.actor_user_id
      AND membership.status = 'active'
      AND ((NEW.fact_type = 'payment' AND payment.id IS NOT NULL)
        OR (NEW.fact_type = 'invoice' AND invoice.id IS NOT NULL))
      AND (NEW.desired_trip_id IS NULL OR desired.id IS NOT NULL)
      AND (
          (NEW.action = 'assign' AND previous.id IS NULL
           AND NOT EXISTS (
               SELECT 1 FROM trip_fact_assignments active
               WHERE active.tenant_id = NEW.tenant_id
                 AND active.ended_at IS NULL
                 AND ((NEW.fact_type = 'payment' AND active.payment_id = NEW.payment_id)
                   OR (NEW.fact_type = 'invoice' AND active.invoice_id = NEW.invoice_id))
           ))
          OR
          (NEW.action IN ('move', 'unassign')
           AND ((NEW.fact_type = 'payment' AND previous.payment_id = NEW.payment_id)
             OR (NEW.fact_type = 'invoice' AND previous.invoice_id = NEW.invoice_id))
           AND (NEW.action = 'unassign' OR previous.trip_id <> NEW.desired_trip_id))
      )
) THEN
        RAISE EXCEPTION 'trip_assignment_decision_scope_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trip_assignment_decisions_scope
BEFORE INSERT ON trip_fact_assignment_decisions
FOR EACH ROW EXECUTE FUNCTION sbm_t_31_trip_assignment_decisions_scop();

CREATE FUNCTION sbm_t_32_trip_assignment_decisions_immu() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'trip_assignment_decision_immutable' USING ERRCODE = 'P0001';
    RETURN NEW;
END;
$$;

CREATE TRIGGER trip_assignment_decisions_immutable
BEFORE UPDATE ON trip_fact_assignment_decisions
FOR EACH ROW EXECUTE FUNCTION sbm_t_32_trip_assignment_decisions_immu();

CREATE FUNCTION sbm_t_33_trip_assignment_decisions_dele() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'trip_assignment_decision_immutable' USING ERRCODE = 'P0001';
    RETURN OLD;
END;
$$;

CREATE TRIGGER trip_assignment_decisions_delete_forbidden
BEFORE DELETE ON trip_fact_assignment_decisions
FOR EACH ROW EXECUTE FUNCTION sbm_t_33_trip_assignment_decisions_dele();

CREATE FUNCTION sbm_t_34_trip_fact_assignments_creation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
    SELECT 1
    FROM trip_fact_assignment_decisions decision
    JOIN trips trip
      ON trip.tenant_id = decision.tenant_id
     AND trip.id = NEW.trip_id
     AND trip.deleted_at IS NULL
    LEFT JOIN payments payment
      ON payment.tenant_id = decision.tenant_id
     AND payment.id = NEW.payment_id
     AND payment.deleted_at IS NULL
    LEFT JOIN invoices invoice
      ON invoice.tenant_id = decision.tenant_id
     AND invoice.id = NEW.invoice_id
     AND invoice.deleted_at IS NULL
    LEFT JOIN trip_fact_assignments previous
      ON previous.tenant_id = decision.tenant_id
     AND previous.id = decision.previous_assignment_id
     AND previous.ended_by_decision_id = decision.id
    WHERE decision.tenant_id = NEW.tenant_id
      AND decision.id = NEW.created_by_decision_id
      AND decision.action IN ('assign', 'move')
      AND decision.desired_trip_id = NEW.trip_id
      AND ((decision.fact_type = 'payment' AND decision.payment_id = NEW.payment_id AND payment.id IS NOT NULL)
        OR (decision.fact_type = 'invoice' AND decision.invoice_id = NEW.invoice_id AND invoice.id IS NOT NULL))
      AND (decision.action = 'assign' OR previous.id IS NOT NULL)
) THEN
        RAISE EXCEPTION 'trip_assignment_creation_scope_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trip_fact_assignments_creation_scope
BEFORE INSERT ON trip_fact_assignments
FOR EACH ROW EXECUTE FUNCTION sbm_t_34_trip_fact_assignments_creation();

CREATE FUNCTION sbm_t_35_trip_fact_assignments_immutabl() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
  OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
  OR NEW.trip_id IS DISTINCT FROM OLD.trip_id
  OR NEW.payment_id IS DISTINCT FROM OLD.payment_id
  OR NEW.invoice_id IS DISTINCT FROM OLD.invoice_id
  OR NEW.created_by_decision_id IS DISTINCT FROM OLD.created_by_decision_id
  OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'trip_fact_assignment_immutable' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trip_fact_assignments_immutable_fields
BEFORE UPDATE OF id, tenant_id, trip_id, payment_id, invoice_id, created_by_decision_id, created_at
ON trip_fact_assignments
FOR EACH ROW EXECUTE FUNCTION sbm_t_35_trip_fact_assignments_immutabl();

CREATE FUNCTION sbm_t_36_trip_fact_assignments_end_once() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT (
    OLD.ended_at IS NULL
    AND OLD.ended_by_decision_id IS NULL
    AND OLD.ended_by_audit_event_id IS NULL
    AND NEW.ended_at IS NOT NULL
    AND (NEW.ended_by_decision_id IS NOT NULL) <> (NEW.ended_by_audit_event_id IS NOT NULL)
) THEN
        RAISE EXCEPTION 'trip_fact_assignment_end_once' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trip_fact_assignments_end_once
BEFORE UPDATE OF ended_at, ended_by_decision_id, ended_by_audit_event_id
ON trip_fact_assignments
FOR EACH ROW EXECUTE FUNCTION sbm_t_36_trip_fact_assignments_end_once();

CREATE FUNCTION sbm_t_37_trip_fact_assignments_end_scop() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT (
    (NEW.ended_by_decision_id IS NOT NULL AND EXISTS (
        SELECT 1 FROM trip_fact_assignment_decisions decision
        WHERE decision.tenant_id = OLD.tenant_id
          AND decision.id = NEW.ended_by_decision_id
          AND decision.previous_assignment_id = OLD.id
          AND decision.action IN ('move', 'unassign')
    ))
    OR
    (NEW.ended_by_audit_event_id IS NOT NULL AND EXISTS (
        SELECT 1 FROM audit_events audit
        WHERE audit.tenant_id = OLD.tenant_id
          AND audit.id = NEW.ended_by_audit_event_id
          AND audit.action = 'fact_deleted'
          AND ((audit.resource_type = 'trip' AND audit.resource_id = OLD.trip_id)
            OR (audit.resource_type = 'payment' AND audit.resource_id = OLD.payment_id)
            OR (audit.resource_type = 'invoice' AND audit.resource_id = OLD.invoice_id))
    ))
) THEN
        RAISE EXCEPTION 'trip_assignment_end_scope_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trip_fact_assignments_end_scope
BEFORE UPDATE OF ended_at, ended_by_decision_id, ended_by_audit_event_id
ON trip_fact_assignments
FOR EACH ROW EXECUTE FUNCTION sbm_t_37_trip_fact_assignments_end_scop();

CREATE FUNCTION sbm_t_38_trip_fact_assignments_delete_f() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'trip_fact_assignment_history_required' USING ERRCODE = 'P0001';
    RETURN OLD;
END;
$$;

CREATE TRIGGER trip_fact_assignments_delete_forbidden
BEFORE DELETE ON trip_fact_assignments
FOR EACH ROW EXECUTE FUNCTION sbm_t_38_trip_fact_assignments_delete_f();

CREATE FUNCTION sbm_t_39_reimbursements_creation_scope() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
    SELECT 1
    FROM trips trip
    JOIN memberships membership
      ON membership.tenant_id = trip.tenant_id
     AND membership.user_id = NEW.created_by_user_id
     AND membership.status = 'active'
    WHERE trip.tenant_id = NEW.tenant_id
      AND trip.id = NEW.trip_id
      AND trip.deleted_at IS NULL
      AND NEW.trip_destination = trip.destination
      AND NEW.trip_start_date = trip.start_date
      AND NEW.trip_end_date = trip.end_date
      AND NEW.status = 'submitted'
      AND NEW.version = 1
) THEN
        RAISE EXCEPTION 'reimbursement_creation_scope_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER reimbursements_creation_scope
BEFORE INSERT ON reimbursements
FOR EACH ROW EXECUTE FUNCTION sbm_t_39_reimbursements_creation_scope();

CREATE FUNCTION sbm_t_40_reimbursements_immutable_field() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
  OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
  OR NEW.trip_id IS DISTINCT FROM OLD.trip_id
  OR NEW.trip_destination IS DISTINCT FROM OLD.trip_destination
  OR NEW.trip_start_date IS DISTINCT FROM OLD.trip_start_date
  OR NEW.trip_end_date IS DISTINCT FROM OLD.trip_end_date
  OR NEW.policy_rule_version IS DISTINCT FROM OLD.policy_rule_version
  OR NEW.snapshot_hash IS DISTINCT FROM OLD.snapshot_hash
  OR NEW.created_by_user_id IS DISTINCT FROM OLD.created_by_user_id
  OR NEW.created_by_decision_id IS DISTINCT FROM OLD.created_by_decision_id
  OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'reimbursement_immutable' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER reimbursements_immutable_fields
BEFORE UPDATE OF id, tenant_id, trip_id, trip_destination, trip_start_date, trip_end_date,
                 policy_rule_version, snapshot_hash, created_by_user_id,
                 created_by_decision_id, created_at
ON reimbursements
FOR EACH ROW EXECUTE FUNCTION sbm_t_40_reimbursements_immutable_field();

CREATE FUNCTION sbm_t_41_reimbursements_status_update_s() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT (
    NEW.version = OLD.version + 1
    AND NEW.status <> OLD.status
    AND EXISTS (
        SELECT 1
        FROM reimbursement_status_decisions decision
        WHERE decision.tenant_id = OLD.tenant_id
          AND decision.reimbursement_id = OLD.id
          AND decision.previous_status = OLD.status
          AND decision.desired_status = NEW.status
          AND decision.expected_version = OLD.version
          AND decision.result_version = NEW.version
          AND decision.created_at = NEW.updated_at
    )
) THEN
        RAISE EXCEPTION 'reimbursement_status_decision_required' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER reimbursements_status_update_scope
BEFORE UPDATE OF status, version, updated_at ON reimbursements
FOR EACH ROW EXECUTE FUNCTION sbm_t_41_reimbursements_status_update_s();

CREATE FUNCTION sbm_t_42_reimbursements_delete_forbidde() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'reimbursement_history_required' USING ERRCODE = 'P0001';
    RETURN OLD;
END;
$$;

CREATE TRIGGER reimbursements_delete_forbidden
BEFORE DELETE ON reimbursements
FOR EACH ROW EXECUTE FUNCTION sbm_t_42_reimbursements_delete_forbidde();

CREATE FUNCTION sbm_t_43_reimbursement_items_creation_s() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
    SELECT 1
    FROM reimbursements reimbursement
    JOIN trip_fact_assignments assignment
      ON assignment.tenant_id = reimbursement.tenant_id
     AND assignment.id = NEW.trip_fact_assignment_id
     AND assignment.trip_id = reimbursement.trip_id
     AND assignment.ended_at IS NULL
    LEFT JOIN payments payment
      ON payment.tenant_id = assignment.tenant_id
     AND payment.id = assignment.payment_id
     AND payment.deleted_at IS NULL
    LEFT JOIN invoices invoice
      ON invoice.tenant_id = assignment.tenant_id
     AND invoice.id = assignment.invoice_id
     AND invoice.deleted_at IS NULL
    WHERE reimbursement.tenant_id = NEW.tenant_id
      AND reimbursement.id = NEW.reimbursement_id
      AND reimbursement.status = 'submitted'
      AND (
          (NEW.fact_type = 'payment'
           AND NEW.payment_id = assignment.payment_id AND NEW.invoice_id IS NULL
           AND payment.id IS NOT NULL
           AND NEW.display_name = payment.merchant
           AND NEW.business_date = payment.business_date
           AND NEW.amount_minor = payment.amount_minor AND NEW.currency = payment.currency)
          OR
          (NEW.fact_type = 'invoice'
           AND NEW.invoice_id = assignment.invoice_id AND NEW.payment_id IS NULL
           AND invoice.id IS NOT NULL
           AND NEW.display_name = invoice.seller_name
           AND NEW.business_date = invoice.invoice_date
           AND NEW.amount_minor = invoice.total_minor AND NEW.currency = invoice.currency)
      )
) THEN
        RAISE EXCEPTION 'reimbursement_item_scope_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER reimbursement_items_creation_scope
BEFORE INSERT ON reimbursement_items
FOR EACH ROW EXECUTE FUNCTION sbm_t_43_reimbursement_items_creation_s();

CREATE FUNCTION sbm_t_44_reimbursement_items_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'reimbursement_item_immutable' USING ERRCODE = 'P0001';
    RETURN NEW;
END;
$$;

CREATE TRIGGER reimbursement_items_immutable
BEFORE UPDATE ON reimbursement_items
FOR EACH ROW EXECUTE FUNCTION sbm_t_44_reimbursement_items_immutable();

CREATE FUNCTION sbm_t_45_reimbursement_items_delete_for() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'reimbursement_item_immutable' USING ERRCODE = 'P0001';
    RETURN OLD;
END;
$$;

CREATE TRIGGER reimbursement_items_delete_forbidden
BEFORE DELETE ON reimbursement_items
FOR EACH ROW EXECUTE FUNCTION sbm_t_45_reimbursement_items_delete_for();

CREATE FUNCTION sbm_t_46_reimbursement_findings_limit() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF (SELECT count(*) FROM reimbursement_policy_findings
      WHERE tenant_id = NEW.tenant_id AND reimbursement_id = NEW.reimbursement_id) >= 1000 THEN
        RAISE EXCEPTION 'reimbursement_finding_limit_exceeded' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER reimbursement_findings_limit
BEFORE INSERT ON reimbursement_policy_findings
FOR EACH ROW EXECUTE FUNCTION sbm_t_46_reimbursement_findings_limit();

CREATE FUNCTION sbm_t_47_reimbursement_findings_creatio() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
    SELECT 1
    FROM reimbursement_items item
    JOIN reimbursements reimbursement
      ON reimbursement.tenant_id = item.tenant_id
     AND reimbursement.id = item.reimbursement_id
    LEFT JOIN reimbursements related
      ON related.tenant_id = reimbursement.tenant_id
     AND related.id = NEW.related_reimbursement_id
    WHERE item.tenant_id = NEW.tenant_id
      AND item.id = NEW.item_id
      AND item.reimbursement_id = NEW.reimbursement_id
      AND reimbursement.policy_rule_version = NEW.rule_version
      AND (
          (NEW.code IN ('missing_invoice', 'amount_conflict') AND related.id IS NULL)
          OR
          (NEW.code = 'duplicate_reimbursement'
           AND related.id IS NOT NULL
           AND related.id <> reimbursement.id
           AND NEW.related_reimbursement_status = related.status)
      )
) THEN
        RAISE EXCEPTION 'reimbursement_finding_scope_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER reimbursement_findings_creation_scope
BEFORE INSERT ON reimbursement_policy_findings
FOR EACH ROW EXECUTE FUNCTION sbm_t_47_reimbursement_findings_creatio();

CREATE FUNCTION sbm_t_48_reimbursement_findings_immutab() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'reimbursement_finding_immutable' USING ERRCODE = 'P0001';
    RETURN NEW;
END;
$$;

CREATE TRIGGER reimbursement_findings_immutable
BEFORE UPDATE ON reimbursement_policy_findings
FOR EACH ROW EXECUTE FUNCTION sbm_t_48_reimbursement_findings_immutab();

CREATE FUNCTION sbm_t_49_reimbursement_findings_delete_() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'reimbursement_finding_immutable' USING ERRCODE = 'P0001';
    RETURN OLD;
END;
$$;

CREATE TRIGGER reimbursement_findings_delete_forbidden
BEFORE DELETE ON reimbursement_policy_findings
FOR EACH ROW EXECUTE FUNCTION sbm_t_49_reimbursement_findings_delete_();

CREATE FUNCTION sbm_t_50_reimbursement_decisions_scope() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
    SELECT 1
    FROM reimbursements reimbursement
    JOIN memberships membership
      ON membership.tenant_id = reimbursement.tenant_id
     AND membership.user_id = NEW.actor_user_id
     AND membership.status = 'active'
    JOIN audit_events audit
      ON audit.tenant_id = reimbursement.tenant_id
     AND audit.id = NEW.audit_event_id
     AND audit.resource_type = 'reimbursement'
     AND audit.resource_id = reimbursement.id
    WHERE reimbursement.tenant_id = NEW.tenant_id
      AND reimbursement.id = NEW.reimbursement_id
      AND (
          (NEW.action = 'submit'
           AND reimbursement.created_by_user_id = NEW.actor_user_id
           AND reimbursement.created_by_decision_id = NEW.id
           AND reimbursement.status = 'submitted' AND reimbursement.version = 1
           AND NEW.previous_status IS NULL AND NEW.desired_status = 'submitted'
           AND NEW.expected_version = 0 AND NEW.result_version = 1
           AND audit.action = 'reimbursement_submitted'
           AND (SELECT count(*) FROM reimbursement_items item
                WHERE item.tenant_id = reimbursement.tenant_id
                  AND item.reimbursement_id = reimbursement.id) BETWEEN 1 AND 200)
          OR
          (NEW.action <> 'submit'
           AND NEW.previous_status = reimbursement.status
           AND NEW.expected_version = reimbursement.version
           AND NEW.result_version = reimbursement.version + 1
           AND audit.action = 'reimbursement_status_changed')
      )
) THEN
        RAISE EXCEPTION 'reimbursement_decision_scope_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER reimbursement_decisions_scope
BEFORE INSERT ON reimbursement_status_decisions
FOR EACH ROW EXECUTE FUNCTION sbm_t_50_reimbursement_decisions_scope();

CREATE FUNCTION sbm_t_51_reimbursement_decisions_apply_() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
	IF NEW.action = 'submit' THEN
		RETURN NEW;
	END IF;
	UPDATE reimbursements
    SET status = NEW.desired_status,
        version = NEW.result_version,
        updated_at = NEW.created_at
    WHERE tenant_id = NEW.tenant_id
      AND id = NEW.reimbursement_id
      AND status = NEW.previous_status
      AND version = NEW.expected_version;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'reimbursement_status_stale' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER reimbursement_decisions_apply_status
AFTER INSERT ON reimbursement_status_decisions
FOR EACH ROW EXECUTE FUNCTION sbm_t_51_reimbursement_decisions_apply_();

CREATE FUNCTION sbm_t_52_reimbursement_decisions_immuta() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'reimbursement_decision_immutable' USING ERRCODE = 'P0001';
    RETURN NEW;
END;
$$;

CREATE TRIGGER reimbursement_decisions_immutable
BEFORE UPDATE ON reimbursement_status_decisions
FOR EACH ROW EXECUTE FUNCTION sbm_t_52_reimbursement_decisions_immuta();

CREATE FUNCTION sbm_t_53_reimbursement_decisions_delete() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'reimbursement_decision_immutable' USING ERRCODE = 'P0001';
    RETURN OLD;
END;
$$;

CREATE TRIGGER reimbursement_decisions_delete_forbidden
BEFORE DELETE ON reimbursement_status_decisions
FOR EACH ROW EXECUTE FUNCTION sbm_t_53_reimbursement_decisions_delete();

CREATE FUNCTION sbm_t_54_fact_field_origins_same_claim() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
    SELECT 1
    FROM field_claims f
    JOIN review_decisions r
      ON r.tenant_id = f.tenant_id AND r.claim_set_id = f.claim_set_id
    WHERE f.tenant_id = NEW.tenant_id
      AND f.id = NEW.field_claim_id
      AND r.id = NEW.review_decision_id
      AND r.action = 'confirm'
) THEN
        RAISE EXCEPTION 'fact_origin_scope_mismatch' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER fact_field_origins_same_claim
BEFORE INSERT ON fact_field_origins
FOR EACH ROW EXECUTE FUNCTION sbm_t_54_fact_field_origins_same_claim();

CREATE FUNCTION sbm_t_55_payments_require_confirmed_rev() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
    SELECT 1 FROM review_decisions r
    JOIN claim_sets c ON c.tenant_id = r.tenant_id AND c.id = r.claim_set_id
    WHERE r.tenant_id = NEW.tenant_id
      AND r.id = NEW.source_review_decision_id
      AND r.action = 'confirm'
      AND c.document_type = 'payment'
) THEN
        RAISE EXCEPTION 'confirmed_payment_review_required' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER payments_require_confirmed_review
BEFORE INSERT ON payments
FOR EACH ROW EXECUTE FUNCTION sbm_t_55_payments_require_confirmed_rev();

CREATE FUNCTION sbm_t_56_invoices_require_confirmed_rev() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
    SELECT 1 FROM review_decisions r
    JOIN claim_sets c ON c.tenant_id = r.tenant_id AND c.id = r.claim_set_id
    WHERE r.tenant_id = NEW.tenant_id
      AND r.id = NEW.source_review_decision_id
      AND r.action = 'confirm'
      AND c.document_type = 'invoice'
) THEN
        RAISE EXCEPTION 'confirmed_invoice_review_required' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER invoices_require_confirmed_review
BEFORE INSERT ON invoices
FOR EACH ROW EXECUTE FUNCTION sbm_t_56_invoices_require_confirmed_rev();

CREATE FUNCTION sbm_t_57_trips_require_confirmed_review() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
    SELECT 1 FROM review_decisions r
    JOIN claim_sets c ON c.tenant_id = r.tenant_id AND c.id = r.claim_set_id
    WHERE r.tenant_id = NEW.tenant_id
      AND r.id = NEW.source_review_decision_id
      AND r.action = 'confirm'
      AND r.fact_type = 'trip'
      AND c.document_type = 'trip'
) THEN
        RAISE EXCEPTION 'confirmed_trip_review_required' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trips_require_confirmed_review
BEFORE INSERT ON trips
FOR EACH ROW EXECUTE FUNCTION sbm_t_57_trips_require_confirmed_review();

CREATE FUNCTION sbm_t_58_claim_confirmation_requires_du() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status = 'confirmed'
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
 ) THEN
        RAISE EXCEPTION 'duplicate_decisions_incomplete' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER claim_confirmation_requires_duplicate_decisions
BEFORE UPDATE OF status ON claim_sets
FOR EACH ROW EXECUTE FUNCTION sbm_t_58_claim_confirmation_requires_du();
