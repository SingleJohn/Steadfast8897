-- 054: 虚拟库支持聚合多个匹配值(解决簡繁/译名把同一片商或演员拆成多个库)
--
-- 现状: 一个 platform_libraries 行只绑定单个 match_value, 同一实体的不同写法
-- (如 "腾讯视频" / "騰訊視頻" / "Tencent Video") 会被拆成多个虚拟库。
--
-- 这里加 match_values TEXT[]: 一个虚拟库可绑定多个值, 查询时按 = ANY(match_values) 聚合。
-- 保留 match_value 作为"主值"(唯一键 (dimension, match_value) 与 PlatformVirtualID 稳定性不变)。
ALTER TABLE platform_libraries ADD COLUMN IF NOT EXISTS match_values TEXT[];

-- 现有行回填: match_values = [主值]
UPDATE platform_libraries
   SET match_values = ARRAY[COALESCE(match_value, platform_name)]
 WHERE match_values IS NULL;
