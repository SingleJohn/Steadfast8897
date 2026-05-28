-- 045: 平台库手动排序 + studio 大小写碎片规范化
--
-- (1) platform_libraries 增加 sort_order, 供后台自定义平台库展示顺序;
--     默认 0, 排序按 (sort_order, platform_name)。
-- (2) 修正历史 NFO 扫描遗留的大小写碎片, 使其精确匹配平台库规范名。
--     注意: 'iQIYI Pictures' 是真实电影制作公司, 不在规范化范围内。

ALTER TABLE platform_libraries ADD COLUMN IF NOT EXISTS sort_order INT NOT NULL DEFAULT 0;

UPDATE items SET studio = 'YOUKU' WHERE studio = 'Youku';
UPDATE items SET studio = 'iQIYI' WHERE studio = 'iQiyi';
