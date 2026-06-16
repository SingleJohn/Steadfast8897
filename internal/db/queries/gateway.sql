-- name: GetGatewayConfig :one
SELECT value
FROM gateway_config
WHERE key = 'main';

-- name: UpsertGatewayConfig :exec
INSERT INTO gateway_config (key, value, updated_at)
VALUES ('main', $1, NOW())
ON CONFLICT (key) DO UPDATE SET value = $1, updated_at = NOW();

-- name: CountGatewayRequestLogs :one
SELECT COUNT(*)::bigint
FROM gateway_request_logs
WHERE (NOT $1::boolean OR tag = $2)
  AND (NOT $3::boolean OR source_id = $4)
  AND (NOT $5::boolean OR status = $6);

-- name: ListGatewayRequestLogs :many
SELECT id, created_at, tag, source_id, client_ip, method, path, query, status, latency_ms,
       bytes_in, bytes_out, emby_user_id, emby_user_name, redirect_backend, redirect_source,
       redirect_location, redirect_trace, object_key, route_id, pool_id, user_agent, referer, headers
FROM gateway_request_logs
WHERE (NOT $1::boolean OR tag = $2)
  AND (NOT $3::boolean OR source_id = $4)
  AND (NOT $5::boolean OR status = $6)
ORDER BY created_at DESC
LIMIT $7 OFFSET $8;

-- name: UpsertGatewayDailyStat :exec
INSERT INTO gateway_daily_stats (day, tag, source_id, requests, redirects302, status4xx, status5xx, bytes_in, bytes_out, latency_ms_sum, latency_ms_max, latency_ms_min, last_updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NOW())
ON CONFLICT (day, tag, source_id) DO UPDATE SET
  requests = gateway_daily_stats.requests + EXCLUDED.requests,
  redirects302 = gateway_daily_stats.redirects302 + EXCLUDED.redirects302,
  status4xx = gateway_daily_stats.status4xx + EXCLUDED.status4xx,
  status5xx = gateway_daily_stats.status5xx + EXCLUDED.status5xx,
  bytes_in = gateway_daily_stats.bytes_in + EXCLUDED.bytes_in,
  bytes_out = gateway_daily_stats.bytes_out + EXCLUDED.bytes_out,
  latency_ms_sum = gateway_daily_stats.latency_ms_sum + EXCLUDED.latency_ms_sum,
  latency_ms_max = GREATEST(gateway_daily_stats.latency_ms_max, EXCLUDED.latency_ms_max),
  latency_ms_min = LEAST(gateway_daily_stats.latency_ms_min, EXCLUDED.latency_ms_min),
  last_updated_at = NOW();

-- name: ListGatewayDailyStats :many
SELECT id, day, tag, source_id, requests, redirects302, status4xx, status5xx,
       bytes_in, bytes_out, latency_ms_sum, latency_ms_max, latency_ms_min, last_updated_at
FROM gateway_daily_stats
WHERE day >= NOW() - make_interval(days => $1::int)
  AND (NOT $2::boolean OR source_id = $3)
ORDER BY day ASC;

-- name: CountGatewayRedirects :one
SELECT COUNT(*)::bigint
FROM gateway_request_logs
WHERE tag = 'proxy'
  AND status = 302
  AND redirect_backend <> ''
  AND created_at >= NOW() - make_interval(hours => $1::int)
  AND (NOT $2::boolean OR source_id = $3);

-- name: CountGatewayRedirectsByBackend :many
SELECT redirect_backend, COUNT(*)::bigint AS count
FROM gateway_request_logs
WHERE tag = 'proxy'
  AND status = 302
  AND redirect_backend <> ''
  AND created_at >= NOW() - make_interval(hours => $1::int)
  AND (NOT $2::boolean OR source_id = $3)
GROUP BY redirect_backend
ORDER BY COUNT(*) DESC;

-- name: ListGatewayRedirectTopUsers :many
SELECT COALESCE(NULLIF(emby_user_name,''), emby_user_id) AS key, COUNT(*)::bigint AS count
FROM gateway_request_logs
WHERE tag = 'proxy'
  AND status = 302
  AND redirect_backend <> ''
  AND created_at >= NOW() - make_interval(hours => $1::int)
  AND (NOT $2::boolean OR source_id = $3)
  AND emby_user_id <> ''
GROUP BY 1
ORDER BY 2 DESC
LIMIT 10;

-- name: ListGatewayRedirectTopIPs :many
SELECT client_ip AS key, COUNT(*)::bigint AS count
FROM gateway_request_logs
WHERE tag = 'proxy'
  AND status = 302
  AND redirect_backend <> ''
  AND created_at >= NOW() - make_interval(hours => $1::int)
  AND (NOT $2::boolean OR source_id = $3)
GROUP BY client_ip
ORDER BY 2 DESC
LIMIT 10;

-- name: CleanupOldGatewayLogs :exec
DELETE FROM gateway_request_logs
WHERE created_at < NOW() - make_interval(days => $1::int);

-- name: CleanupOldGatewayStats :exec
DELETE FROM gateway_daily_stats
WHERE day < NOW() - make_interval(days => $1::int);
