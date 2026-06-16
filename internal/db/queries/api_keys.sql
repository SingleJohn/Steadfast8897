-- name: CreateApiKey :one
INSERT INTO api_keys (name, key, created_by)
VALUES ($1, $2, $3)
RETURNING id, name, key, created_at, last_used_at;

-- name: ListApiKeys :many
SELECT ak.id,
       ak.name,
       ak.key,
       ak.created_at,
       ak.last_used_at,
       COALESCE(u.name, 'Unknown') AS created_by_name
FROM api_keys ak
LEFT JOIN users u ON ak.created_by = u.id
ORDER BY ak.created_at DESC;

-- name: GetApiKeyIDByKey :one
SELECT id
FROM api_keys
WHERE key = $1;

-- name: TouchApiKeyLastUsed :exec
UPDATE api_keys
SET last_used_at = NOW()
WHERE id = $1::uuid;

-- name: DeleteApiKey :execrows
DELETE FROM api_keys
WHERE id = $1::uuid;
