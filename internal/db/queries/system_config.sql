-- name: GetSystemConfigValue :one
SELECT value FROM system_config WHERE key = $1;

-- name: UpsertSystemConfigValue :exec
INSERT INTO system_config (key, value)
VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
