-- name: GetItemImageInfo :one
SELECT primary_image_path, backdrop_image_path, type
FROM items
WHERE id = $1::uuid;

-- name: GetLibraryPrimaryImagePath :one
SELECT primary_image_path
FROM libraries
WHERE id = $1::uuid AND deleted_at IS NULL;

-- name: GetCastImageURL :one
SELECT image_url
FROM cast_members
WHERE id = $1::uuid AND image_url IS NOT NULL
LIMIT 1;

-- name: GetCastImageURLByTagAndItem :one
SELECT image_url
FROM cast_members
WHERE id = sqlc.arg(tag_id)::uuid AND item_id = sqlc.arg(item_id)::uuid;

-- name: GetItemExtraImagePath :one
SELECT path
FROM item_images
WHERE item_id = sqlc.arg(item_id)::uuid AND image_type = 'Backdrop' AND idx = sqlc.arg(idx);

-- name: GetMergedSecondaryImagePaths :one
SELECT primary_image_path, backdrop_image_path
FROM items
WHERE merged_to_id = $1::uuid
  AND primary_image_path IS NOT NULL
LIMIT 1;

-- name: GetMergedPrimaryImagePaths :one
SELECT p.primary_image_path, p.backdrop_image_path
FROM items s
JOIN items p ON p.id = s.merged_to_id
WHERE s.id = $1::uuid AND p.primary_image_path IS NOT NULL;

-- name: GetEpisodeSeriesImageParentID :one
SELECT COALESCE(series_id::text, parent_id::text)::text AS parent_id
FROM items
WHERE id = $1::uuid;

-- name: GetItemPrimaryImagePath :one
SELECT primary_image_path
FROM items
WHERE id = $1::uuid;

-- name: GetItemBackdropImagePath :one
SELECT backdrop_image_path
FROM items
WHERE id = $1::uuid;
