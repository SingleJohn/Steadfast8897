-- name: ListMediaVersionsForItem :many
SELECT id::text, name, file_path, container, is_primary, runtime_ticks, bitrate, size, mediainfo,
       resolution, hdr_format, video_codec, audio_codec, source, quality_label, chapters
FROM media_versions
WHERE item_id = $1::uuid
ORDER BY is_primary DESC, created_at ASC;

-- name: UpsertMediaVersion :one
INSERT INTO media_versions (item_id, name, file_path, container, is_primary, mediainfo, runtime_ticks, bitrate, size, resolution, hdr_format, video_codec, audio_codec, source, quality_label)
VALUES ($1::uuid, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10, $11, $12, $13, $14, $15)
ON CONFLICT (item_id, file_path) DO UPDATE SET
    name = EXCLUDED.name,
    container = EXCLUDED.container,
    is_primary = EXCLUDED.is_primary,
    mediainfo = COALESCE(EXCLUDED.mediainfo, media_versions.mediainfo),
    runtime_ticks = COALESCE(EXCLUDED.runtime_ticks, media_versions.runtime_ticks),
    bitrate = COALESCE(EXCLUDED.bitrate, media_versions.bitrate),
    size = COALESCE(EXCLUDED.size, media_versions.size),
    resolution = COALESCE(EXCLUDED.resolution, media_versions.resolution),
    hdr_format = COALESCE(EXCLUDED.hdr_format, media_versions.hdr_format),
    video_codec = COALESCE(EXCLUDED.video_codec, media_versions.video_codec),
    audio_codec = COALESCE(EXCLUDED.audio_codec, media_versions.audio_codec),
    source = COALESCE(EXCLUDED.source, media_versions.source),
    quality_label = COALESCE(EXCLUDED.quality_label, media_versions.quality_label)
RETURNING id::text;

-- name: GetMergedPrimaryID :one
SELECT merged_to_id::text
FROM items
WHERE id = $1::uuid;

-- name: ListMergedSiblingItems :many
SELECT s.id::text, l.name AS lib_name
FROM items s
JOIN libraries l ON s.library_id = l.id
WHERE s.merged_to_id = $1::uuid AND l.deleted_at IS NULL;

-- name: GetMediaVersionFilePath :one
SELECT file_path
FROM media_versions
WHERE id = $1::uuid;

-- name: GetPrimaryMediaVersionFilePath :one
SELECT file_path
FROM media_versions
WHERE item_id = $1::uuid
ORDER BY is_primary DESC, created_at ASC
LIMIT 1;

-- name: GetLocalTrailerPath :one
SELECT local_trailer_path
FROM items
WHERE id = $1::uuid;

-- name: GetMediaVersionItemAndInfo :one
SELECT item_id::text, mediainfo
FROM media_versions
WHERE id = $1::uuid;

-- name: GetPrimaryMediaStreamsJSON :one
SELECT mediainfo->'MediaStreams'
FROM media_versions
WHERE item_id = $1::uuid AND mediainfo IS NOT NULL
ORDER BY is_primary DESC
LIMIT 1;

-- name: GetItemDetailExtras :one
SELECT original_title, trailer_url
FROM items
WHERE id = $1::uuid;

-- name: CountItemsByLibrary :one
SELECT COUNT(*)::bigint
FROM items
WHERE library_id = $1::uuid;

-- name: GetItemLibraryID :one
SELECT library_id::text
FROM items
WHERE id = $1::uuid;

-- name: ListSimilarItemIDsByLibrary :many
SELECT id::text
FROM items
WHERE library_id = $1::uuid
  AND id <> $2::uuid
  AND type IN ('Movie', 'Series', 'Episode', 'Video')
ORDER BY RANDOM()
LIMIT $3::bigint;

-- name: ListSeasonIDsForCompat :many
SELECT id::text
FROM items
WHERE parent_id = $1::uuid AND type = 'Season'
ORDER BY index_number NULLS LAST, sort_name ASC;

-- name: GetSeasonParentSeriesID :one
SELECT parent_id::text
FROM items
WHERE id = $1::uuid AND type = 'Season';

-- name: FindSeasonIDByNumber :one
SELECT id::text
FROM items
WHERE parent_id = $1::uuid AND type = 'Season' AND index_number = $2
LIMIT 1;

-- name: CountEpisodesBySeason :one
SELECT COUNT(*)::bigint
FROM items
WHERE parent_id = $1::uuid AND type = 'Episode';

-- name: ListEpisodeIDsBySeason :many
SELECT id::text
FROM items
WHERE parent_id = $1::uuid AND type = 'Episode'
ORDER BY index_number NULLS LAST, sort_name ASC, id ASC
LIMIT $2 OFFSET $3;

-- name: CountEpisodesBySeries :one
SELECT COUNT(*)::bigint
FROM items
WHERE series_id = $1::uuid AND type = 'Episode';

-- name: ListEpisodeIDsBySeries :many
SELECT id::text
FROM items
WHERE series_id = $1::uuid AND type = 'Episode'
ORDER BY parent_index_number NULLS LAST, index_number NULLS LAST, id ASC
LIMIT $2 OFFSET $3;

-- name: ListMediaStreamsForItem :many
SELECT codec, type, stream_index, language, title, is_default, is_forced,
       width, height, bit_rate, channels, sample_rate, bit_depth, pixel_format, display_title
FROM media_streams
WHERE item_id = $1::uuid
ORDER BY stream_index;
