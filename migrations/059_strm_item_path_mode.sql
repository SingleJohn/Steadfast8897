-- strm 条目「item 级 Path」返回模式：
--   'strm'     = 返回 .strm 文件路径(对齐 Emby，默认)
--   'resolved' = 返回解析后的内层真实路径(FYMS 旧行为)
-- 与 Emby 一致：item.Path 永远是 .strm，解析后的真实地址只出现在 MediaSources.Path。
INSERT INTO system_config (key, value) VALUES ('strm_item_path_mode', 'strm')
ON CONFLICT (key) DO NOTHING;
