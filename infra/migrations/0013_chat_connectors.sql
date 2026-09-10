-- 钉钉机器人凭据放进面板、按工作区保存，与 AI 供应商配置同一套做法：
-- AppSecret 用主密钥加密落库，界面只回显「已设置」；不再走环境变量与密钥文件。
--
-- 一个工作区一个平台最多一条；同一个 AppKey 不能被两个工作区各绑一次——机器人
-- 收到的消息必须能唯一确定归属，两边都认领等于谁都不该收。
CREATE TABLE chat_connectors (
    tenant_id TEXT NOT NULL REFERENCES tenants(id),
    platform TEXT NOT NULL,
    app_key TEXT NOT NULL,
    encrypted_app_secret BYTEA NOT NULL,
    detection_status TEXT NOT NULL DEFAULT 'pending',
    detection_checked_at TIMESTAMPTZ,
    detection_safe_message TEXT NOT NULL DEFAULT '',
    active BOOLEAN NOT NULL DEFAULT FALSE,
    version BIGINT NOT NULL DEFAULT 1,
    updated_by_user_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, platform),
    UNIQUE (platform, app_key),
    FOREIGN KEY (tenant_id, updated_by_user_id) REFERENCES memberships(tenant_id, user_id),
    CHECK (platform IN ('dingtalk')),
    CHECK (length(app_key) BETWEEN 1 AND 200),
    CHECK (detection_status IN ('pending', 'passed', 'failed')),
    CHECK (length(detection_safe_message) <= 500),
    -- 只有检测通过的凭据才能启用；重新保存凭据会把检测打回 pending，启用随之失效。
    CHECK (NOT active OR detection_status = 'passed'),
    CHECK (version >= 1)
);
