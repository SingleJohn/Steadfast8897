-- 061_user_person_data.sql
-- Emby treats Person as an item-like entity for favorite state, but FYMS stores
-- persons outside the items table. Keep person favorites separate so the
-- existing user_item_data -> items foreign key stays intact.

CREATE TABLE IF NOT EXISTS user_person_data (
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  person_id UUID NOT NULL REFERENCES persons(id) ON DELETE CASCADE,
  is_favorite BOOLEAN NOT NULL DEFAULT FALSE,
  updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
  PRIMARY KEY (user_id, person_id)
);

CREATE INDEX IF NOT EXISTS idx_user_person_data_favorite
  ON user_person_data(user_id, is_favorite)
  WHERE is_favorite = TRUE;
