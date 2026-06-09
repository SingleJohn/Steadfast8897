-- 053: 统一媒体库展示顺序(实际库 + 虚拟库交错排序)
--
-- 现状: libraries.sort_order 与 platform_libraries.sort_order 两套独立顺序,
-- 再加全局 platform_libraries_position(before/after) 把整组虚拟库放普通库前/后,
-- 无法做到实际库与虚拟库交错混排。
--
-- 这里引入统一顺序表,entry_kind + entry_id 标识一个展示条目:
--   entry_kind = 'library'  -> entry_id = libraries.id (uuid)
--   entry_kind = 'platform' -> entry_id = 虚拟库 PlatformVirtualID
-- getUserViews 有此表记录时按 sort_order 合并排序; 无记录时回退 before/after。
CREATE TABLE IF NOT EXISTS library_display_order (
  entry_kind VARCHAR(16) NOT NULL,
  entry_id   VARCHAR(64) NOT NULL,
  sort_order INT NOT NULL DEFAULT 0,
  PRIMARY KEY (entry_kind, entry_id)
);

CREATE INDEX IF NOT EXISTS idx_library_display_order_sort ON library_display_order(sort_order);
