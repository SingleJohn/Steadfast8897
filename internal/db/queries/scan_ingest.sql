-- name: UpsertExternalSubtitle :exec
INSERT INTO external_subtitles (item_id, media_version_id, file_path, codec, language, title, is_default, is_forced, updated_at)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, NOW())
ON CONFLICT (media_version_id, file_path) DO UPDATE SET
  item_id = EXCLUDED.item_id,
  codec = EXCLUDED.codec,
  language = EXCLUDED.language,
  title = EXCLUDED.title,
  is_default = EXCLUDED.is_default,
  is_forced = EXCLUDED.is_forced,
  updated_at = NOW();

-- name: DeleteExternalSubtitlesForMediaVersion :exec
DELETE FROM external_subtitles WHERE media_version_id = $1::uuid;

-- name: PruneExternalSubtitlesForMediaVersion :exec
DELETE FROM external_subtitles
WHERE media_version_id = $1::uuid
  AND NOT (file_path = ANY($2::text[]));

-- name: ListMediaVersionsByPath :many
SELECT item_id, id, file_path
FROM media_versions
WHERE file_path = $1 OR file_path = $2;

-- name: DeleteItemsByExactPath :many
DELETE FROM items
WHERE file_path = $1
RETURNING id::text, name, type, file_path;

-- name: DeleteItemsByPathPrefix :many
DELETE FROM items
WHERE file_path = $1 OR file_path LIKE $2
RETURNING id::text, name, type, file_path;

-- name: ListItemsByPathPrefix :many
SELECT id, file_path
FROM items
WHERE file_path = $1 OR file_path LIKE $2;

-- name: UpdateItemFilePathByID :exec
UPDATE items SET file_path = $1, updated_at = NOW() WHERE id = $2;

-- name: UpdateItemFilePathByOldPath :execrows
UPDATE items SET file_path = $1, updated_at = NOW() WHERE file_path = $2;

-- name: UpdateMediaVersionFilePath :exec
UPDATE media_versions SET file_path = $1 WHERE file_path = $2;

-- name: RenameExternalSubtitlePaths :exec
UPDATE external_subtitles
SET file_path = $1 || substring(file_path from $3),
    updated_at = NOW()
WHERE file_path = $2 OR file_path LIKE $4;

-- name: ListSeriesSeasonIDs :many
SELECT id::text
FROM items
WHERE parent_id = $1::uuid AND type = 'Season'
ORDER BY index_number ASC NULLS FIRST, created_at ASC;

-- name: ListSeriesEpisodeIDs :many
SELECT id::text
FROM items
WHERE series_id = $1::uuid AND type = 'Episode'
ORDER BY parent_index_number ASC NULLS FIRST, index_number ASC NULLS FIRST, created_at ASC;

-- name: GetDominantEpisodeSeasonNumber :one
SELECT parent_index_number
FROM items
WHERE season_id = $1::uuid
  AND type = 'Episode'
  AND parent_index_number IS NOT NULL
GROUP BY parent_index_number
ORDER BY COUNT(*) DESC, parent_index_number ASC
LIMIT 1;

-- name: GetSeasonParentIndexNumber :one
SELECT parent_index_number
FROM items
WHERE id = $1::uuid AND type = 'Season';

-- name: GetSeasonIndexNumber :one
SELECT index_number
FROM items
WHERE id = $1::uuid AND type = 'Season';

-- name: GetRefreshItemType :one
SELECT type
FROM items
WHERE id = $1::uuid;

-- name: ListSeriesSubtreeTargetIDs :many
SELECT id::text
FROM items
WHERE id = $1::uuid
   OR parent_id = $1::uuid
   OR series_id = $1::uuid
ORDER BY CASE type WHEN 'Series' THEN 0 WHEN 'Season' THEN 1 WHEN 'Episode' THEN 2 ELSE 3 END,
         parent_index_number NULLS LAST,
         index_number NULLS LAST,
         name;

-- name: ListRefreshTargetIDsForLibrary :many
SELECT i.id::text
FROM items i
JOIN libraries l ON l.id = i.library_id
WHERE i.library_id = $1::uuid
  AND l.deleted_at IS NULL
  AND i.merged_to_id IS NULL
  AND i.type = ANY($2::text[])
ORDER BY i.created_at ASC;

-- name: ListRefreshTargetIDs :many
SELECT i.id::text
FROM items i
JOIN libraries l ON l.id = i.library_id
WHERE l.deleted_at IS NULL
  AND i.merged_to_id IS NULL
  AND i.type = ANY($1::text[])
ORDER BY i.created_at ASC;

-- name: GetLibraryByItemID :one
SELECT l.id, l.name, l.collection_type, l.paths, l.created_at, l.primary_image_path, l.primary_image_tag, l.sort_order,
       COALESCE(l.scrape_config::text, ''::text) AS scrape_config
FROM libraries l
JOIN items i ON i.library_id = l.id
WHERE i.id = $1::uuid AND l.deleted_at IS NULL;

-- name: GetItemFilePath :one
SELECT file_path FROM items WHERE id = $1::uuid;

-- name: GetFirstSeriesEpisodeFilePath :one
SELECT file_path
FROM items
WHERE series_id = $1::uuid AND type = 'Episode' AND file_path IS NOT NULL AND file_path NOT LIKE 'http%'
ORDER BY created_at ASC
LIMIT 1;

-- name: GetFirstSeasonEpisodeFilePath :one
SELECT file_path
FROM items
WHERE parent_id = $1::uuid AND type = 'Episode' AND file_path IS NOT NULL
ORDER BY created_at ASC
LIMIT 1;

-- name: GetEpisodeFilePath :one
SELECT file_path
FROM items
WHERE id = $1::uuid AND type = 'Episode';

-- name: UpdateItemTMDBAndIMDB :exec
UPDATE items
SET tmdb_id = $1, imdb_id = COALESCE(NULLIF($2, ''), imdb_id), updated_at = NOW()
WHERE id = $3::uuid;

-- name: UpdateItemTMDBID :exec
UPDATE items SET tmdb_id = $1, updated_at = NOW() WHERE id = $2::uuid;

-- name: UpdateItemPrimaryImage :exec
UPDATE items
SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW()
WHERE id = $3::uuid;

-- name: UpdateItemBackdropImage :exec
UPDATE items
SET backdrop_image_path = $1, backdrop_image_tag = $2, updated_at = NOW()
WHERE id = $3::uuid;

-- name: ListSeasonIDsAndNumbers :many
SELECT id, index_number
FROM items
WHERE parent_id = $1::uuid AND type = 'Season'
ORDER BY index_number;

-- name: GetItemPrimaryImageTag :one
SELECT primary_image_tag FROM items WHERE id = $1::uuid;
