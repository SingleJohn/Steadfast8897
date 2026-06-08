-- 044: NFO 字段补全（原标题 / 预告片 / 标签）
-- 外部刮削好的 NFO 带的 originaltitle / trailer / tag 此前无处落库，这里补齐存储。

ALTER TABLE items ADD COLUMN IF NOT EXISTS original_title VARCHAR(500);
ALTER TABLE items ADD COLUMN IF NOT EXISTS trailer_url VARCHAR(1000);

-- tags 与 genres 是两套独立分类（对齐 Emby 的 Tags / Genres 字段）
CREATE TABLE IF NOT EXISTS tags (
  id SERIAL PRIMARY KEY,
  name VARCHAR(255) UNIQUE NOT NULL
);

CREATE TABLE IF NOT EXISTS item_tags (
  item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  tag_id  INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (item_id, tag_id)
);

CREATE INDEX IF NOT EXISTS idx_item_tags_item ON item_tags(item_id);
