-- 聊天里逐项处理：改字段、判重复、挑关联，都要在多轮之间记住进度。
--
-- plan_json 是这段对话攒下的决定（待改字段、已判的重复、已选的候选与金额），
-- 攒齐了一次性交给与网页同一条确认用例。它仍然只是对话状态：删掉会话不改变
-- 任何账目，单据回到网页待审核队列。

ALTER TABLE chat_sessions
    ADD COLUMN plan_json JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE chat_sessions DROP CONSTRAINT chat_sessions_state_check;
ALTER TABLE chat_sessions ADD CONSTRAINT chat_sessions_state_check CHECK (state IN (
    -- 结果卡已推出，等「确认」「作废」「处理」或字段编号。
    'awaiting_decision',
    -- 等某个字段的新值。
    'awaiting_field_value',
    -- 等某笔疑似重复的判断。
    'awaiting_duplicate',
    -- 等挑选要关联的候选。
    'awaiting_candidates',
    -- 等某张候选分配多少金额。
    'awaiting_allocation_amount'
));
ALTER TABLE chat_sessions ADD CONSTRAINT chat_sessions_plan_object_check
    CHECK (jsonb_typeof(plan_json) = 'object');
