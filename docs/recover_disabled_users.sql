-- =============================================================================
-- 恢复被 Sakura_embyboss 活跃检测误禁的用户
-- =============================================================================
-- 背景：bot 只读 FYMS 的 LastActivityDate（登录时才更新），不看播放记录，
--       导致长期 token 播放用户被当成未活跃禁用。
--
-- 建议顺序：
--   1) 先在 bot MySQL 执行「二、bot 库」
--   2) 再在 FYMS PostgreSQL 执行「一、FYMS 库」
--   3) 部署已修复 last_activity 刷新的 FYMS 版本
--
-- 字段说明：
--   FYMS users.is_disabled = true  → 账号在媒体服务器侧禁用
--   bot  emby.lv = 'c'             → 已禁用；'b' = 正常；'a' = 白名单；'d' = 未注册
-- =============================================================================


-- #############################################################################
-- 一、FYMS 库（PostgreSQL）
-- #############################################################################

-- ---------- 预览：当前被禁用的用户 ----------
SELECT id, name, is_disabled, last_login_date, last_activity_date
FROM users
WHERE is_disabled = true
ORDER BY name;

-- ---------- 1. 全部解禁 ----------
UPDATE users
SET is_disabled = false
WHERE is_disabled = true;

-- ---------- 2. 用播放记录回填 LastActivityDate（防 bot 再次误杀）----------
UPDATE users u
SET last_activity_date = p.last_play
FROM (
  SELECT user_id, MAX(date_created) AS last_play
  FROM playback_activity
  GROUP BY user_id
) p
WHERE u.id = p.user_id
  AND (u.last_activity_date IS NULL OR u.last_activity_date < p.last_play);

-- ---------- 3. 无播放记录时，用登录时间兜底 ----------
UPDATE users
SET last_activity_date = last_login_date
WHERE last_activity_date IS NULL
  AND last_login_date IS NOT NULL;

-- ---------- 4. 仍为空的设为当前时间 ----------
-- 避免 API 省略 LastActivityDate，被 bot 当「注册后未活跃」直接禁用。
-- 若不想放开从未登录也无播放的账号，可跳过本条。
UPDATE users
SET last_activity_date = NOW()
WHERE last_activity_date IS NULL;

-- ---------- 复查 ----------
SELECT name, is_disabled, last_login_date, last_activity_date
FROM users
ORDER BY last_activity_date NULLS FIRST, name
LIMIT 50;

SELECT is_disabled, COUNT(*) AS cnt
FROM users
GROUP BY is_disabled;


-- #############################################################################
-- 二、Sakura bot 库（MySQL）
-- #############################################################################

-- ---------- 预览 ----------
SELECT tg, name, embyid, lv, cr, ex
FROM emby
WHERE lv = 'c'
ORDER BY name;

-- ---------- 全部从「已禁用」改回「正常」----------
UPDATE emby
SET lv = 'b'
WHERE lv = 'c';

-- ---------- 复查 ----------
SELECT lv, COUNT(*) AS cnt
FROM emby
GROUP BY lv;


-- #############################################################################
-- 三、可选：只恢复指定用户（不要全量时用）
-- #############################################################################

-- FYMS（PostgreSQL）示例：
-- UPDATE users SET is_disabled = false WHERE name IN ('user1', 'user2');

-- bot（MySQL）示例：
-- UPDATE emby SET lv = 'b' WHERE lv = 'c' AND name IN ('user1', 'user2');
