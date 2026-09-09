-- 一个成员在一个平台上只保留一个绑定账号。换手机、换钉钉号时重新绑定即替换，
-- 旧账号随即失去投件能力——否则一个已经不在手上的账号会长期保有往账目里投
-- 单据的权力，而没有任何地方会提示它还在。
CREATE UNIQUE INDEX chat_identities_one_account_per_member
    ON chat_identities (platform, tenant_id, user_id);

-- 绑定码证明持有者能登录网页、因而是在册成员。它是一次性凭据：只存哈希，
-- 与会话令牌和邀请码同一套处理，库被读到也无法据此绑定。
-- 消费后保留行而不是删除，绑定这件事需要留痕。
CREATE TABLE chat_binding_codes (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    platform TEXT NOT NULL,
    code_hash TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    consumed_external_user_id TEXT,
    FOREIGN KEY (tenant_id, user_id) REFERENCES memberships(tenant_id, user_id),
    CHECK (platform IN ('dingtalk')),
    CHECK (code_hash ~ '^[0-9a-f]{64}$'),
    -- 有效期短是这条通道的主要防线：码要经由聊天软件传递，暴露面比会话大。
    CHECK (expires_at > created_at AND expires_at <= created_at + INTERVAL '30 minutes'),
    CHECK ((consumed_at IS NULL AND consumed_external_user_id IS NULL)
        OR (consumed_at IS NOT NULL AND consumed_external_user_id IS NOT NULL))
);

CREATE INDEX chat_binding_codes_member ON chat_binding_codes (tenant_id, user_id, created_at DESC);
