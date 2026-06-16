-- name: CreateAccessToken :exec
INSERT INTO access_tokens (token, user_id, device_id, device_name, app_name, app_version)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetAccessToken :one
SELECT token, user_id, device_id, device_name, app_name, app_version, created_at
FROM access_tokens
WHERE token = $1;

-- name: DeleteAccessToken :exec
DELETE FROM access_tokens WHERE token = $1;

-- name: DeleteAccessTokensByUserID :exec
DELETE FROM access_tokens WHERE user_id = $1;
