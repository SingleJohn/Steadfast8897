-- 056: metadata browse indexes for web actor / genre / tag entry points.

CREATE INDEX IF NOT EXISTS idx_item_genres_genre ON item_genres(genre_id);
CREATE INDEX IF NOT EXISTS idx_item_tags_tag ON item_tags(tag_id);
CREATE INDEX IF NOT EXISTS idx_cast_members_name_role ON cast_members(name, role);
CREATE INDEX IF NOT EXISTS idx_cast_members_person_role ON cast_members(person_id, role);
