-- 080: 用历史播放回填 last_activity_date，对齐 Emby「有活动就算活跃」语义。
-- Sakura_embyboss 活跃保号只读 users.LastActivityDate，不读 playback_activity。

UPDATE users u
SET last_activity_date = p.last_play
FROM (
  SELECT user_id, MAX(date_created) AS last_play
  FROM playback_activity
  GROUP BY user_id
) p
WHERE u.id = p.user_id
  AND (u.last_activity_date IS NULL OR u.last_activity_date < p.last_play);

-- 无播放记录时，至少用 last_login_date 兜底，避免 API 省略 LastActivityDate 被 bot 当「从未活跃」
UPDATE users
SET last_activity_date = last_login_date
WHERE last_activity_date IS NULL
  AND last_login_date IS NOT NULL;
