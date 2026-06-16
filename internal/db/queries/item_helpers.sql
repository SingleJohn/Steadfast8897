-- name: ListItemGenres :many
SELECT g.id::text, g.name
FROM genres g
JOIN item_genres ig ON g.id = ig.genre_id
WHERE ig.item_id = $1::uuid
ORDER BY g.name;

-- name: ListItemTags :many
SELECT t.name
FROM tags t
JOIN item_tags it ON t.id = it.tag_id
WHERE it.item_id = $1::uuid
ORDER BY t.name;

-- name: ListAllTagsWithCounts :many
SELECT t.id, t.name, COUNT(it.item_id)::bigint AS item_count
FROM tags t
LEFT JOIN item_tags it ON t.id = it.tag_id
GROUP BY t.id, t.name
ORDER BY t.name;

-- name: ListItemExtraBackdropTags :many
SELECT tag
FROM item_images
WHERE item_id = $1::uuid AND image_type = 'Backdrop'
ORDER BY idx;

-- name: ListItemCast :many
SELECT cm.id::text AS cast_id,
       COALESCE(cm.person_id::text, cm.id::text) AS person_id,
       cm.name,
       cm.character,
       cm.role,
       cm.order_index,
       COALESCE(NULLIF(p.image_path, ''), NULLIF(cm.image_url, '')) AS image,
       COALESCE(EXTRACT(EPOCH FROM p.updated_at)::bigint::text, cm.id::text) AS image_tag
FROM cast_members cm
LEFT JOIN persons p ON p.id = cm.person_id
WHERE cm.item_id = $1::uuid
ORDER BY cm.role, cm.order_index;

-- name: ListAllGenresWithCounts :many
SELECT g.id::text, g.name, COUNT(ig.item_id)::bigint AS item_count
FROM genres g
LEFT JOIN item_genres ig ON g.id = ig.genre_id
GROUP BY g.id, g.name
ORDER BY g.name;

-- name: ListExternalSubtitlesForMediaVersion :many
SELECT id::text, item_id::text, media_version_id::text, file_path, codec, language, title, is_default, is_forced
FROM external_subtitles
WHERE media_version_id = $1::uuid
ORDER BY language NULLS LAST, title NULLS LAST, file_path;

-- name: GetUserItemData :one
SELECT playback_position_ticks, play_count, is_favorite, played, last_played_date
FROM user_item_data
WHERE user_id = $1::uuid AND item_id = $2::uuid;

-- name: UpsertUserItemData :exec
INSERT INTO user_item_data (user_id, item_id, playback_position_ticks, play_count, is_favorite, played, last_played_date)
VALUES ($1::uuid, $2::uuid,
        COALESCE(sqlc.narg('position')::bigint, 0),
        COALESCE(sqlc.narg('play_count')::int, 0),
        COALESCE(sqlc.narg('is_favorite')::boolean, false),
        COALESCE(sqlc.narg('played')::boolean, false),
        NOW())
ON CONFLICT (user_id, item_id) DO UPDATE SET
  playback_position_ticks = COALESCE(sqlc.narg('position')::bigint, user_item_data.playback_position_ticks),
  play_count = COALESCE(sqlc.narg('play_count')::int, user_item_data.play_count),
  is_favorite = COALESCE(sqlc.narg('is_favorite')::boolean, user_item_data.is_favorite),
  played = COALESCE(sqlc.narg('played')::boolean, user_item_data.played),
  last_played_date = NOW();

-- name: SetHiddenFromResume :exec
INSERT INTO user_item_data (user_id, item_id, is_hidden_from_resume)
VALUES ($1::uuid, $2::uuid, $3)
ON CONFLICT (user_id, item_id) DO UPDATE SET is_hidden_from_resume = $3;

-- name: GetChildCount :one
SELECT COUNT(*)::bigint
FROM items
WHERE parent_id = $1::uuid;

-- name: GetRecursiveItemCount :one
WITH RECURSIVE children AS (
  SELECT id FROM items WHERE parent_id = $1::uuid
  UNION ALL
  SELECT i.id FROM items i JOIN children c ON i.parent_id = c.id
)
SELECT COUNT(*)::bigint FROM children;

-- name: GetCollectionTypeByLibraryID :one
SELECT collection_type
FROM libraries
WHERE id = $1::uuid AND deleted_at IS NULL;

-- name: GetItemEmbyID :one
SELECT emby_id
FROM items
WHERE id = $1::uuid;

-- name: ResolveItemUUIDByEmbyID :one
SELECT id::text
FROM items
WHERE emby_id = $1;

-- name: GetPersonImagePath :one
SELECT COALESCE(NULLIF(p.image_path, ''),
       (SELECT image_url FROM cast_members
        WHERE person_id = p.id AND image_url IS NOT NULL AND image_url <> ''
        LIMIT 1))
FROM persons p
WHERE p.id = $1::uuid;

-- name: SetPersonImage :exec
UPDATE persons
SET image_path = $1, image_locked = $2, updated_at = NOW()
WHERE id = $3::uuid;

-- name: ClearPersonImage :exec
UPDATE persons
SET image_path = NULL, image_locked = false, updated_at = NOW()
WHERE id = $1::uuid;

-- name: FillPersonImageIfUnlocked :execrows
UPDATE persons
SET image_path = $1, updated_at = NOW()
WHERE id = $2::uuid
  AND image_locked = false
  AND (image_path IS NULL OR image_path = '');

-- name: ListItemsForActorImageBackfill :many
SELECT DISTINCT i.id::text
FROM items i
JOIN cast_members cm ON cm.item_id = i.id
LEFT JOIN persons p ON p.id = cm.person_id
WHERE i.type IN ('Movie','Series')
  AND i.tmdb_id IS NOT NULL AND i.tmdb_id > 0
  AND COALESCE(NULLIF(p.image_path,''), NULLIF(cm.image_url,'')) IS NULL;

-- name: GetActorImageStats :one
SELECT COUNT(*)::bigint AS total,
       COUNT(*) FILTER (WHERE image_path IS NOT NULL AND image_path <> '')::bigint AS with_image,
       COUNT(*) FILTER (WHERE image_locked)::bigint AS locked
FROM persons;

-- name: PersonExists :one
SELECT EXISTS(SELECT 1 FROM persons WHERE id = $1::uuid);

-- name: GetPersonBackdropPath :one
SELECT backdrop_path
FROM persons
WHERE id = $1::uuid;

-- name: SetPersonBackdrop :exec
UPDATE persons
SET backdrop_path = $1, updated_at = NOW()
WHERE id = $2::uuid;

-- name: ClearPersonBackdrop :exec
UPDATE persons
SET backdrop_path = NULL, updated_at = NOW()
WHERE id = $1::uuid;
