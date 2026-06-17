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

-- name: DeleteEmptySeasons :exec
DELETE FROM items
WHERE type = 'Season'
  AND id NOT IN (
      SELECT DISTINCT parent_id FROM items WHERE parent_id IS NOT NULL AND type = 'Episode'
  );

-- name: DeleteEmptySeries :exec
DELETE FROM items
WHERE type = 'Series'
  AND id NOT IN (
      SELECT DISTINCT parent_id FROM items WHERE parent_id IS NOT NULL AND type = 'Season'
  );

-- name: SetLocalTrailerPath :exec
UPDATE items
SET local_trailer_path = $1
WHERE id = $2::uuid;

-- name: DeleteItemExtraBackdrops :exec
DELETE FROM item_images
WHERE item_id = $1::uuid AND image_type = 'Backdrop';

-- name: UpsertItemExtraBackdrop :exec
INSERT INTO item_images (item_id, image_type, idx, path, tag)
VALUES ($1::uuid, 'Backdrop', $2, $3, $4)
ON CONFLICT (item_id, image_type, idx) DO UPDATE SET
  path = EXCLUDED.path,
  tag = EXCLUDED.tag;

-- name: SyncItemArtwork :exec
UPDATE items
SET primary_image_path = CASE
                           WHEN sqlc.arg(clear_poster)::boolean THEN NULL
                           WHEN NULLIF(sqlc.arg(poster_path)::text, '') IS NOT NULL THEN sqlc.arg(poster_path)::text
                           ELSE primary_image_path
                         END,
    primary_image_tag = CASE
                          WHEN sqlc.arg(clear_poster)::boolean THEN NULL
                          WHEN NULLIF(sqlc.arg(poster_tag)::text, '') IS NOT NULL THEN sqlc.arg(poster_tag)::text
                          ELSE primary_image_tag
                        END,
    backdrop_image_path = CASE
                            WHEN sqlc.arg(clear_backdrop)::boolean THEN NULL
                            WHEN NULLIF(sqlc.arg(backdrop_path)::text, '') IS NOT NULL THEN sqlc.arg(backdrop_path)::text
                            ELSE backdrop_image_path
                          END,
    backdrop_image_tag = CASE
                           WHEN sqlc.arg(clear_backdrop)::boolean THEN NULL
                           WHEN NULLIF(sqlc.arg(backdrop_tag)::text, '') IS NOT NULL THEN sqlc.arg(backdrop_tag)::text
                           ELSE backdrop_image_tag
                         END,
    updated_at = NOW()
WHERE id = sqlc.arg(item_id)::uuid;

-- name: ListPruneCandidatePaths :many
SELECT id::text, file_path
FROM items
WHERE library_id = $1::uuid AND type = $2 AND file_path IS NOT NULL;

-- name: ListCatalogNumberBackfillCandidates :many
SELECT id, name, COALESCE(file_path, '') AS file_path
FROM items
WHERE type IN ('Movie', 'Series') AND (catalog_number IS NULL OR catalog_number = '');

-- name: FillCatalogNumberIfEmpty :execrows
UPDATE items
SET catalog_number = $1
WHERE id = $2::uuid AND (catalog_number IS NULL OR catalog_number = '');

-- name: ListMediaVersionBackfillCandidates :many
SELECT i.id, i.file_path, i.container
FROM items i
WHERE i.type IN ('Movie', 'Episode')
  AND i.file_path IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM media_versions mv WHERE mv.item_id = i.id)
ORDER BY i.created_at DESC;

-- name: InsertMixedFolder :one
INSERT INTO items (library_id, parent_id, type, name, sort_name, file_path, created_at)
VALUES ($1::uuid, $2::uuid, 'Folder', $3, $4, $5, COALESCE($6, NOW()))
ON CONFLICT DO NOTHING
RETURNING id::text;

-- name: FindMixedFolderByPath :one
SELECT id::text
FROM items
WHERE library_id = $1::uuid AND type = 'Folder' AND file_path = $2
LIMIT 1;

-- name: UpdateMixedFolder :exec
UPDATE items
SET parent_id = $1::uuid,
    name = $2,
    sort_name = $3,
    updated_at = NOW()
WHERE id = $4::uuid;

-- name: SetMixedItemParent :exec
UPDATE items
SET parent_id = $1::uuid,
    updated_at = NOW()
WHERE library_id = $2::uuid AND type = $3 AND file_path = $4;

-- name: GetItemTMDBIDByType :one
SELECT tmdb_id
FROM items
WHERE id = $1::uuid AND type = $2;

-- name: GetItemTypeForNFO :one
SELECT type
FROM items
WHERE id = $1::uuid;

-- name: GetItemProviderIDsForNFO :one
SELECT provider_ids
FROM items
WHERE id = $1::uuid;

-- name: UpdateItemProviderIDsForNFO :exec
UPDATE items
SET provider_ids = $1::jsonb,
    updated_at = NOW()
WHERE id = $2::uuid;

-- name: MarkItemPlatformScanError :exec
UPDATE items
SET platform_scan_status = 'error',
    platform_scan_error = $1,
    platform_scanned_at = NOW(),
    updated_at = NOW()
WHERE id = $2::uuid;

-- name: ReplaceItemGenresForNFO :exec
WITH deleted AS (
    DELETE FROM item_genres WHERE item_id = $1::uuid
),
inserted_genres AS (
    INSERT INTO genres (name)
    SELECT unnest($2::text[])
    ON CONFLICT (name) DO NOTHING
)
INSERT INTO item_genres (item_id, genre_id)
SELECT $1::uuid, id
FROM genres
WHERE name = ANY($2::text[])
ON CONFLICT DO NOTHING;

-- name: ReplaceItemTagsForNFO :exec
WITH deleted AS (
    DELETE FROM item_tags WHERE item_id = $1::uuid
),
inserted_tags AS (
    INSERT INTO tags (name)
    SELECT unnest($2::text[])
    ON CONFLICT (name) DO NOTHING
)
INSERT INTO item_tags (item_id, tag_id)
SELECT $1::uuid, id
FROM tags
WHERE name = ANY($2::text[])
ON CONFLICT DO NOTHING;

-- name: ListCastImagesForNFO :many
SELECT name, role, image_url
FROM cast_members
WHERE item_id = $1::uuid
  AND image_url IS NOT NULL
  AND image_url <> '';

-- name: DeleteCastMembersForNFO :exec
DELETE FROM cast_members
WHERE item_id = $1::uuid;

-- name: CopyEpisodeMediaVersionsToCanonical :exec
INSERT INTO media_versions (item_id, name, file_path, container, is_primary, mediainfo, runtime_ticks, bitrate, size, resolution, hdr_format, video_codec, audio_codec, source, quality_label)
SELECT $1::uuid, name, file_path, container, is_primary, mediainfo, runtime_ticks, bitrate, size, resolution, hdr_format, video_codec, audio_codec, source, quality_label
FROM media_versions
WHERE item_id = $2::uuid
ON CONFLICT (item_id, file_path) DO NOTHING;

-- name: MergeEpisodeUserDataToCanonical :exec
INSERT INTO user_item_data (user_id, item_id, playback_position_ticks, play_count, is_favorite, played, last_played_date)
SELECT user_id, $1::uuid, playback_position_ticks, play_count, is_favorite, played, last_played_date
FROM user_item_data
WHERE item_id = $2::uuid
ON CONFLICT (user_id, item_id) DO UPDATE SET
    playback_position_ticks = GREATEST(user_item_data.playback_position_ticks, EXCLUDED.playback_position_ticks),
    play_count = GREATEST(user_item_data.play_count, EXCLUDED.play_count),
    is_favorite = user_item_data.is_favorite OR EXCLUDED.is_favorite,
    played = user_item_data.played OR EXCLUDED.played,
    last_played_date = GREATEST(
        COALESCE(user_item_data.last_played_date, TIMESTAMP 'epoch'),
        COALESCE(EXCLUDED.last_played_date, TIMESTAMP 'epoch')
    );

-- name: DeleteItemByIDForScan :exec
DELETE FROM items
WHERE id = $1::uuid;
