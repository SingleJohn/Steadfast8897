-- 041: 合并豆瓣开关,删 system_config.douban_enabled
-- 取而代之:是否启用豆瓣源由 scrape_providers_enabled 列表控制(语义合一)
-- 若历史上关闭过豆瓣补全(douban_enabled=false),对应把豆瓣从启用列表剔除,
-- 保持行为等价。

DO $$
DECLARE
    enabled_val text;
    providers_raw text;
    providers_json jsonb;
BEGIN
    SELECT value INTO enabled_val
      FROM system_config
     WHERE key = 'douban_enabled'
     LIMIT 1;

    IF enabled_val IS NOT NULL AND lower(trim(enabled_val)) IN ('false', '0', 'no', 'off') THEN
        SELECT value INTO providers_raw
          FROM system_config
         WHERE key = 'scrape_providers_enabled'
         LIMIT 1;

        IF providers_raw IS NULL OR trim(providers_raw) = '' THEN
            -- 无显式配置时用默认列表剔除豆瓣后写回
            INSERT INTO system_config(key, value)
            VALUES ('scrape_providers_enabled', '["tmdb","bangumi","tvdb","fanart"]')
            ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
        ELSE
            BEGIN
                providers_json := providers_raw::jsonb;
                IF jsonb_typeof(providers_json) = 'array' THEN
                    UPDATE system_config
                       SET value = (
                           SELECT jsonb_agg(x)::text
                             FROM jsonb_array_elements_text(providers_json) AS x
                            WHERE x <> 'douban'
                       )
                     WHERE key = 'scrape_providers_enabled';
                END IF;
            EXCEPTION WHEN others THEN
                -- JSON 损坏时不处理,让运行时兜底
                NULL;
            END;
        END IF;
    END IF;
END $$;

DELETE FROM system_config WHERE key = 'douban_enabled';
