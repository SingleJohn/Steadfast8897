-- 046: 本地原图缓存开关(由 web 页面控制)
-- image_cache_copy_local = 'false'(默认):本地/挂载原图直读,不复制到 data/cache/sources;
--                          'true'        :复制一份到 cache/sources(LRU 上限可控)。
-- 仅影响本地/挂载原图;URL 源始终下载缓存。

INSERT INTO system_config (key, value) VALUES ('image_cache_copy_local', 'false')
ON CONFLICT (key) DO NOTHING;
