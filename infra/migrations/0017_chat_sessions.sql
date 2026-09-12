-- 钉钉对话会话：识别完推回执，用户回「确认」或「作废」。
--
-- 主键就是产品规则「一个人同时只有一条进行中的单据」：新文件到达即覆盖旧会话，
-- 不排队。20 分钟提醒一次（reminded 记住只提醒这一次），30 分钟超时删除会话，
-- 单据留在网页待审核队列——超时不做任何业务决定。
--
-- 会话是纯粹的对话状态，不是业务事实：删除它不影响 Job、Claim 或任何账目。
-- 因此没有版本列、没有不可变历史，job 或绑定消失时随之级联删除。

CREATE TABLE chat_sessions (
    platform TEXT NOT NULL,
    external_user_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    job_id TEXT NOT NULL,
    document_name TEXT NOT NULL,
    state TEXT NOT NULL,
    expected_revision BIGINT NOT NULL,
    reminded BOOLEAN NOT NULL DEFAULT FALSE,
    started_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (platform, external_user_id),
    CHECK (platform IN ('dingtalk')),
    -- awaiting_decision：可以在聊天里确认/作废。
    -- needs_web：有候选或疑似重复，聊天里只能作废，确认必须回网页。
    CHECK (state IN ('awaiting_decision', 'needs_web')),
    CHECK (expected_revision >= 1),
    CHECK (length(document_name) BETWEEN 1 AND 200),
    FOREIGN KEY (platform, external_user_id)
        REFERENCES chat_identities(platform, external_user_id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, user_id) REFERENCES memberships(tenant_id, user_id),
    FOREIGN KEY (tenant_id, job_id) REFERENCES processing_jobs(tenant_id, id) ON DELETE CASCADE
);

-- 扫描到期会话按时间取，不做全表扫描。
CREATE INDEX chat_sessions_started_idx ON chat_sessions (started_at);
