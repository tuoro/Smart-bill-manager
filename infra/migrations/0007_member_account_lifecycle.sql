-- 全局身份不拆分，成员版本保护编辑；已有规范账号和业务记录全部保留。
CREATE UNIQUE INDEX users_normalized_email ON users(lower(email));
ALTER TABLE memberships ADD COLUMN version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1);
CREATE INDEX memberships_page ON memberships(tenant_id, created_at DESC, user_id DESC);

CREATE OR REPLACE FUNCTION sbm_t_1_memberships_keep_active_owner_() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM id FROM tenants WHERE id = OLD.tenant_id FOR UPDATE;
    IF OLD.role = 'owner' AND OLD.status = 'active' AND (NEW.role <> 'owner' OR NEW.status <> 'active')
       AND NOT EXISTS (SELECT 1 FROM memberships WHERE tenant_id = OLD.tenant_id
           AND user_id <> OLD.user_id AND role = 'owner' AND status = 'active') THEN
        RAISE EXCEPTION 'last_active_owner' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
CREATE OR REPLACE FUNCTION sbm_t_2_memberships_keep_active_owner_() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM id FROM tenants WHERE id = OLD.tenant_id FOR UPDATE;
    IF OLD.role = 'owner' AND OLD.status = 'active'
       AND NOT EXISTS (SELECT 1 FROM memberships WHERE tenant_id = OLD.tenant_id
           AND user_id <> OLD.user_id AND role = 'owner' AND status = 'active') THEN
        RAISE EXCEPTION 'last_active_owner' USING ERRCODE = 'P0001';
    END IF;
    RETURN OLD;
END;
$$;

CREATE FUNCTION sbm_membership_session_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id OR NEW.user_id IS DISTINCT FROM OLD.user_id
        OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'membership_identity_immutable' USING ERRCODE = 'P0001';
    END IF;
    IF NEW.role IS DISTINCT FROM OLD.role OR NEW.status IS DISTINCT FROM OLD.status THEN
        NEW.version := OLD.version + 1;
        UPDATE sessions SET revoked_at = CURRENT_TIMESTAMP
            WHERE tenant_id = OLD.tenant_id AND user_id = OLD.user_id AND revoked_at IS NULL;
    ELSIF NEW.version IS DISTINCT FROM OLD.version THEN
        RAISE EXCEPTION 'membership_version_without_change' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER memberships_session_revision BEFORE UPDATE ON memberships
    FOR EACH ROW EXECUTE FUNCTION sbm_membership_session_revision();

CREATE FUNCTION sbm_password_revokes_sessions() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.password_hash IS DISTINCT FROM OLD.password_hash THEN
        UPDATE sessions SET revoked_at = CURRENT_TIMESTAMP WHERE user_id = OLD.id AND revoked_at IS NULL;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER users_password_revokes_sessions BEFORE UPDATE OF password_hash ON users
    FOR EACH ROW EXECUTE FUNCTION sbm_password_revokes_sessions();

CREATE TABLE member_invitations (
    id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL REFERENCES tenants(id),
    email TEXT NOT NULL CHECK (length(email) BETWEEN 3 AND 254 AND email = lower(email)),
    role TEXT NOT NULL CHECK (role IN ('owner','finance','reviewer','viewer')),
    token_hash TEXT NOT NULL UNIQUE CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    created_by_user_id TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL, expires_at TIMESTAMPTZ NOT NULL,
    reason TEXT NOT NULL CHECK (length(trim(reason)) BETWEEN 1 AND 500),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 8 AND 128),
    request_hash TEXT NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    audit_event_id TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1 CHECK (version IN (1,2)),
    consumed_at TIMESTAMPTZ, consumed_by_user_id TEXT REFERENCES users(id),
    revoked_at TIMESTAMPTZ, revoked_by_user_id TEXT, revoke_reason TEXT,
    CHECK (expires_at > created_at AND expires_at <= created_at + INTERVAL '48 hours'),
    CHECK ((version = 1 AND num_nonnulls(consumed_at, consumed_by_user_id, revoked_at, revoked_by_user_id, revoke_reason) = 0)
        OR (version = 2 AND consumed_at IS NOT NULL AND consumed_by_user_id IS NOT NULL AND num_nonnulls(revoked_at, revoked_by_user_id, revoke_reason) = 0)
        OR (version = 2 AND consumed_at IS NULL AND consumed_by_user_id IS NULL AND revoked_at IS NOT NULL AND revoke_reason IS NOT NULL AND length(trim(revoke_reason)) BETWEEN 1 AND 500)),
    UNIQUE (tenant_id, id), UNIQUE (tenant_id, idempotency_key),
    FOREIGN KEY (tenant_id, created_by_user_id) REFERENCES memberships(tenant_id, user_id),
    FOREIGN KEY (tenant_id, revoked_by_user_id) REFERENCES memberships(tenant_id, user_id),
    FOREIGN KEY (tenant_id, audit_event_id) REFERENCES audit_events(tenant_id, id)
);
CREATE INDEX member_invitations_page ON member_invitations(tenant_id, created_at DESC, id DESC);
CREATE INDEX member_invitations_pending ON member_invitations(tenant_id, email, expires_at) WHERE version = 1;

CREATE FUNCTION sbm_invitation_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'invitation_history_immutable' USING ERRCODE = 'P0001'; END IF;
    IF OLD.version <> 1 OR NEW.version <> 2 OR
        (to_jsonb(NEW) - ARRAY['version','consumed_at','consumed_by_user_id','revoked_at','revoked_by_user_id','revoke_reason']) IS DISTINCT FROM
        (to_jsonb(OLD) - ARRAY['version','consumed_at','consumed_by_user_id','revoked_at','revoked_by_user_id','revoke_reason']) THEN
        RAISE EXCEPTION 'invitation_history_immutable' USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER member_invitations_immutable BEFORE UPDATE OR DELETE ON member_invitations
    FOR EACH ROW EXECUTE FUNCTION sbm_invitation_immutable();

-- 账号密码是全局身份行为，不假扮任何租户的 Owner。
CREATE TABLE account_events (
    id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id),
    actor_kind TEXT NOT NULL CHECK (actor_kind IN ('self','local_operator')),
    action TEXT NOT NULL CHECK (action IN ('password_changed','password_recovered')),
    reason TEXT NOT NULL CHECK (length(trim(reason)) BETWEEN 1 AND 500),
    created_at TIMESTAMPTZ NOT NULL,
    CHECK ((actor_kind = 'self' AND action = 'password_changed') OR (actor_kind = 'local_operator' AND action = 'password_recovered'))
);
CREATE FUNCTION sbm_account_event_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'account_event_immutable' USING ERRCODE = 'P0001';
END;
$$;
CREATE TRIGGER account_events_immutable BEFORE UPDATE OR DELETE ON account_events
    FOR EACH ROW EXECUTE FUNCTION sbm_account_event_immutable();
