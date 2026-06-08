-- 047: 平台虚拟库泛化为多维度（片商 / 番号前缀 / 演员）
-- 原 platform_libraries 只能按 items.studio 分组。这里加 dimension + match_value,
-- 让一个虚拟库可以按任意维度的某个值聚合;并加 cover_* 存生成的封面。
--
-- 语义:
--   platform_name = 显示名
--   match_value   = 实际匹配值(studio 维度回填 = platform_name)
--   dimension     = 'studio' | 'num_prefix' | 'actor'

ALTER TABLE platform_libraries ADD COLUMN IF NOT EXISTS dimension VARCHAR(20) NOT NULL DEFAULT 'studio';
ALTER TABLE platform_libraries ADD COLUMN IF NOT EXISTS match_value VARCHAR(255);
ALTER TABLE platform_libraries ADD COLUMN IF NOT EXISTS cover_image_path VARCHAR(1000);
ALTER TABLE platform_libraries ADD COLUMN IF NOT EXISTS cover_image_tag VARCHAR(64);

-- 现有行(studio 维度)回填 match_value
UPDATE platform_libraries SET match_value = platform_name WHERE match_value IS NULL;

-- 唯一键从 platform_name 改为 (dimension, match_value),允许不同维度同名值共存
ALTER TABLE platform_libraries DROP CONSTRAINT IF EXISTS platform_libraries_platform_name_key;
ALTER TABLE platform_libraries ADD CONSTRAINT platform_libraries_dim_value_key UNIQUE (dimension, match_value);

CREATE INDEX IF NOT EXISTS idx_platform_libraries_dimension ON platform_libraries(dimension);
