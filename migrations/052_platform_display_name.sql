-- 052: 虚拟库(平台库)自定义显示名
--
-- 现状: platform_name 兼作显示名, 且内置平台会被 PlatformDisplayName() 二次本地化
-- 映射(腾讯/爱奇艺等), 用户无法自由命名虚拟库。
--
-- 这里加独立 display_name 列: 非空时直接作为展示名, 优先级:
--   display_name(用户自定义) > PlatformDisplayName(platform_name) > platform_name
--
-- logo / 渐变匹配仍走 platform_name / match_value, 改名不影响图标。
ALTER TABLE platform_libraries ADD COLUMN IF NOT EXISTS display_name VARCHAR(255);
