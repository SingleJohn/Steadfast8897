-- 045: 多图存储（extrafanart → 多张 Backdrop）
-- items 表每种图片类型只存一张。extrafanart/*.jpg 这类额外预览图需要多图存储，
-- 对齐 Emby：作为多张 Backdrop 返回（BackdropImageTags 数组）。
-- 约定：主 fanart.jpg 仍存 items.backdrop_image_path，视作 Backdrop/0；
--       extrafanart 按文件名排序存为 Backdrop/1..N。

CREATE TABLE IF NOT EXISTS item_images (
  id SERIAL PRIMARY KEY,
  item_id    UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  image_type VARCHAR(20) NOT NULL,   -- 'Backdrop'
  idx        INTEGER NOT NULL,       -- 1..N（0 = items.backdrop_image_path）
  path       VARCHAR(1000) NOT NULL,
  tag        VARCHAR(64)  NOT NULL,
  UNIQUE(item_id, image_type, idx)
);

CREATE INDEX IF NOT EXISTS idx_item_images_item ON item_images(item_id);
