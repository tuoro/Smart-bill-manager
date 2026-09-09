-- 钉钉进来的文件与网页上传走同一条管线：对象由本系统持有，因此
-- original_object_owner 仍是 'document'。邮件那条不同——它保留原始 RFC822 与
-- 不可变附件，对象归 email_attachment 所有，所以不能简单并进同一分支。
ALTER TABLE documents DROP CONSTRAINT documents_check1;

ALTER TABLE documents ADD CONSTRAINT documents_ingestion_owner_check CHECK (
    (ingestion_kind IN ('upload', 'dingtalk_message') AND original_object_owner = 'document')
    OR (ingestion_kind = 'email_attachment' AND original_object_owner = 'email_attachment')
);

-- 聊天账号到成员的映射。主键不含 tenant_id 是有意的：机器人收到一个文件时，
-- 必须能唯一确定它属于哪个租户的哪个成员。同一个钉钉号若能落到两个租户，
-- 投递就是歧义的，而歧义出现在财务收单路径上不可接受——宁可拒收。同时属于
-- 多个租户的人，往第二个租户投件请走网页。
--
-- 外键指向 memberships 而不是 users：成员被移出租户时映射必须一并失效，
-- 否则一个已经离开的人仍能通过聊天往里投文件。
CREATE TABLE chat_identities (
    platform TEXT NOT NULL,
    external_user_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL REFERENCES tenants(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    created_by_user_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (platform, external_user_id),
    FOREIGN KEY (tenant_id, user_id) REFERENCES memberships(tenant_id, user_id),
    FOREIGN KEY (tenant_id, created_by_user_id) REFERENCES memberships(tenant_id, user_id),
    CHECK (platform IN ('dingtalk')),
    CHECK (length(external_user_id) BETWEEN 1 AND 200)
);

CREATE INDEX chat_identities_member_index ON chat_identities (tenant_id, user_id);
