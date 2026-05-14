-- HideFromResume 端点支持: 允许客户端从"继续观看"列表移除条目,
-- 但不丢失 playback_position_ticks (位置数据保留,仅打上隐藏标记)。
-- 配套 IsResumable 过滤条件追加 AND is_hidden_from_resume = FALSE。

ALTER TABLE user_item_data ADD COLUMN IF NOT EXISTS is_hidden_from_resume BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_uid_hidden_resume
    ON user_item_data(user_id) WHERE is_hidden_from_resume = TRUE;
