-- name: ListLibraries :many
SELECT id, name, collection_type, paths, created_at, primary_image_path, primary_image_tag, sort_order,
       COALESCE(scrape_config::text, ''::text) AS scrape_config
FROM libraries
WHERE deleted_at IS NULL
ORDER BY sort_order ASC, name ASC;

-- name: ListLibrariesForWatcher :many
SELECT id, name, paths
FROM libraries
WHERE deleted_at IS NULL
ORDER BY name;

-- name: ListLibrariesForIngestMatch :many
SELECT id, collection_type, paths
FROM libraries
WHERE deleted_at IS NULL
ORDER BY name;

-- name: GetLibraryByID :one
SELECT id, name, collection_type, paths, created_at, primary_image_path, primary_image_tag, sort_order,
       COALESCE(scrape_config::text, ''::text) AS scrape_config
FROM libraries
WHERE id = $1 AND deleted_at IS NULL;

-- name: CreateLibrary :one
INSERT INTO libraries (name, collection_type, paths)
VALUES ($1, $2, $3)
RETURNING id, name, collection_type, paths, created_at, primary_image_path, primary_image_tag, sort_order,
          COALESCE(scrape_config::text, ''::text) AS scrape_config;

-- name: UpdateLibraryName :exec
UPDATE libraries SET name = $1 WHERE id = $2;

-- name: UpdateLibrarySortOrder :exec
UPDATE libraries SET sort_order = $1 WHERE id = $2;

-- name: UpdateLibraryScrapeConfigNull :exec
UPDATE libraries SET scrape_config = NULL WHERE id = $1;

-- name: UpdateLibraryScrapeConfig :exec
UPDATE libraries SET scrape_config = $1::jsonb WHERE id = $2;

-- name: MarkLibraryDeleted :execrows
UPDATE libraries SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL;

-- name: CountLibraryItems :one
SELECT COUNT(*) FROM items WHERE library_id = $1;

-- name: FinalizeLibraryDeletion :exec
DELETE FROM libraries WHERE id = $1 AND deleted_at IS NOT NULL;

-- name: ListDeletedLibraryIDs :many
SELECT id FROM libraries WHERE deleted_at IS NOT NULL;

-- name: GetLibraryNameIncludingDeleted :one
SELECT name FROM libraries WHERE id = $1;

-- name: AddLibraryPath :exec
UPDATE libraries SET paths = array_append(paths, $1::text) WHERE id = $2;

-- name: UpdateLibraryImage :exec
UPDATE libraries SET primary_image_path = $1, primary_image_tag = $2 WHERE id = $3;

-- name: DeleteLibraryImage :exec
UPDATE libraries SET primary_image_path = NULL, primary_image_tag = NULL WHERE id = $1;

-- name: RemoveLibraryPath :exec
UPDATE libraries SET paths = array_remove(paths, $1::text) WHERE id = $2;
