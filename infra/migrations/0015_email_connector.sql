-- 邮箱来源接上真正的 IMAP 连接。
--
-- 之前只登记描述信息、不收密码、不拉邮件（ADR-0014 把真实连接留给「未来的连接器」）。
-- 现在密码用主密钥加密落库（与 Provider API Key、钉钉 AppSecret 同一套），服务端
-- 定时 IMAP 拉取新邮件，附件自动进识别队列。
--
-- 邮箱是每个成员自己的：created_by_user_id 即归属人；成员只看得到、删得掉自己的。
-- 删除是软删除：已归档的邮件与由此生成的单据是不可变 Source，保留；只是不再同步、
-- 不再出现在列表里，同一邮箱身份允许重新登记。

ALTER TABLE email_sources
    ADD COLUMN imap_username TEXT NOT NULL DEFAULT '',
    ADD COLUMN encrypted_password BYTEA,
    ADD COLUMN connection_status TEXT NOT NULL DEFAULT 'pending',
    ADD COLUMN connection_checked_at TIMESTAMPTZ,
    ADD COLUMN connection_safe_message TEXT NOT NULL DEFAULT '',
    ADD COLUMN sync_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN sync_uid_validity BIGINT,
    ADD COLUMN sync_last_uid BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN last_sync_at TIMESTAMPTZ,
    ADD COLUMN last_sync_safe_message TEXT NOT NULL DEFAULT '',
    ADD COLUMN deleted_at TIMESTAMPTZ;

ALTER TABLE email_sources
    ADD CONSTRAINT email_sources_imap_username_check CHECK (length(imap_username) <= 254),
    ADD CONSTRAINT email_sources_connection_status_check
        CHECK (connection_status IN ('pending', 'passed', 'failed')),
    ADD CONSTRAINT email_sources_connection_message_check CHECK (length(connection_safe_message) <= 200),
    ADD CONSTRAINT email_sources_sync_message_check CHECK (length(last_sync_safe_message) <= 200),
    ADD CONSTRAINT email_sources_sync_progress_check
        CHECK (sync_last_uid >= 0 AND (sync_uid_validity IS NULL OR sync_uid_validity >= 0)),
    -- 没验过的密码不能被拿去同步；删掉的邮箱不能还在同步。
    ADD CONSTRAINT email_sources_sync_requires_connection_check
        CHECK (NOT sync_enabled OR (connection_status = 'passed' AND encrypted_password IS NOT NULL AND deleted_at IS NULL));

-- 身份唯一只对未删除的来源生效，删掉之后同一邮箱可以重新登记。
ALTER TABLE email_sources DROP CONSTRAINT email_sources_tenant_id_mailbox_address_normalized_imap_hos_key;
CREATE UNIQUE INDEX email_sources_live_identity_key
    ON email_sources (tenant_id, mailbox_address_normalized, imap_host_normalized, imap_port, transport_security)
    WHERE deleted_at IS NULL;

CREATE INDEX email_sources_sync_enabled_idx ON email_sources (sync_enabled) WHERE sync_enabled;
