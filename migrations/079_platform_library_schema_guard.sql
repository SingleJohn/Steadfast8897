-- 079: 平台/虚拟库 schema 兜底修正
--
-- 平台库已泛化为按 studio / num_prefix / actor 等维度创建任意虚拟库，
-- 旧版 platform_name VARCHAR(100) 与 platform_name 唯一约束不再适合真实维度值。
-- 这里用幂等迁移放宽字段长度，并确保唯一语义落在 (dimension, match_value)。

ALTER TABLE platform_libraries ALTER COLUMN platform_name TYPE TEXT;
ALTER TABLE platform_libraries ALTER COLUMN match_value TYPE TEXT;
ALTER TABLE platform_libraries ALTER COLUMN display_name TYPE TEXT;

UPDATE platform_libraries
   SET match_value = platform_name
 WHERE match_value IS NULL OR match_value = '';

UPDATE platform_libraries
   SET match_values = ARRAY[match_value]
 WHERE match_values IS NULL OR cardinality(match_values) = 0;

ALTER TABLE platform_libraries DROP CONSTRAINT IF EXISTS platform_libraries_platform_name_key;
ALTER TABLE platform_libraries DROP CONSTRAINT IF EXISTS platform_libraries_dim_value_key;
ALTER TABLE platform_libraries ADD CONSTRAINT platform_libraries_dim_value_key UNIQUE (dimension, match_value);
