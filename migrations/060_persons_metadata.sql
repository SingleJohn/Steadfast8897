-- 060_persons_metadata.sql
-- 完整保存第三方刮削器(mdc-ng 等)回写的演员资料，使 fyms 的 /Persons、/Persons/{Name}、
-- /Items/{personId} 能像真实 Emby 一样把这些字段吐还给其它客户端。
-- 原 persons 表只有 overview / tmdb_person_id 两个可存字段，其余全被丢弃。

ALTER TABLE persons ADD COLUMN IF NOT EXISTS premiere_date TEXT;                                  -- 出生日期，存 mdc 原值 "YYYY-MM-DD"
ALTER TABLE persons ADD COLUMN IF NOT EXISTS production_year INT;                                 -- 出生年
ALTER TABLE persons ADD COLUMN IF NOT EXISTS production_locations JSONB NOT NULL DEFAULT '[]'::jsonb; -- 出身地
ALTER TABLE persons ADD COLUMN IF NOT EXISTS genres JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE persons ADD COLUMN IF NOT EXISTS tags JSONB NOT NULL DEFAULT '[]'::jsonb;             -- 罩杯/身高/三围/年龄/生涯/账号 等
ALTER TABLE persons ADD COLUMN IF NOT EXISTS taglines JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE persons ADD COLUMN IF NOT EXISTS provider_ids JSONB NOT NULL DEFAULT '{}'::jsonb;     -- 完整外部 id 映射(Imdb/Fanza/Twitter/...)
ALTER TABLE persons ADD COLUMN IF NOT EXISTS backdrop_path TEXT;                                  -- 背景图(独立于 image_path 头像)
