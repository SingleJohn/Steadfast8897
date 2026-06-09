-- migrations/055_persons_table.sql
-- 全局演员/人物表。对齐 Emby/Jellyfin 的 People 模型:以「姓名」为键归一,
-- 同名人物合并为同一实体(接受同名碰撞,概率极低,是行业标准做法)。
-- cast_members 仍按 item 保留 name/character/role/order_index(character 每片不同),
-- 通过 person_id 指向全局 persons;头像权威值落在 persons.image_path,
-- 上传/批量补一次即可让全库同名条目生效。

CREATE TABLE IF NOT EXISTS persons (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name           VARCHAR(255) NOT NULL,
  image_path     VARCHAR(1000),
  image_locked   BOOLEAN NOT NULL DEFAULT false, -- 用户手动上传/锁定后,批量补不覆盖
  tmdb_person_id INTEGER,                          -- TMDB 人物 ID(线索,不参与归一)
  overview       TEXT,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- name 唯一 = 按名归一。已存在的同名行靠唯一约束自动合并。
CREATE UNIQUE INDEX IF NOT EXISTS idx_persons_name ON persons(name);

-- 从现有 cast_members 回填 persons:每个唯一姓名一行,
-- image_path 取该姓名下任意一个非空 image_url 作为初始头像。
INSERT INTO persons (name, image_path)
SELECT
  name,
  (array_agg(image_url) FILTER (WHERE image_url IS NOT NULL AND image_url <> ''))[1]
FROM cast_members
WHERE name IS NOT NULL AND name <> ''
GROUP BY name
ON CONFLICT (name) DO NOTHING;

-- cast_members 增加 person_id 外链。删除 person 时置空,不级联删演职员行。
ALTER TABLE cast_members ADD COLUMN IF NOT EXISTS person_id UUID;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_cast_members_person'
  ) THEN
    ALTER TABLE cast_members
      ADD CONSTRAINT fk_cast_members_person
      FOREIGN KEY (person_id) REFERENCES persons(id) ON DELETE SET NULL;
  END IF;
END $$;

-- 按名回填 cast_members.person_id。
UPDATE cast_members cm
   SET person_id = p.id
  FROM persons p
 WHERE p.name = cm.name
   AND cm.person_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_cast_members_person_id ON cast_members(person_id);
