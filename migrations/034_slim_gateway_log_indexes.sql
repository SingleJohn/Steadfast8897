-- gateway_request_logs 是高频写入表(每请求一行),过多的单列索引导致
-- 每次 INSERT 要维护全部 B-tree,在 PG CPU 热路径里占比很高。
-- 保留: idx_grl_created_at(cleanup + ORDER BY 主力)
--       idx_grl_tag_status_created(既覆盖 tag 前缀又覆盖 tag+status 组合)
--       idx_grl_source_id(前端按网关源筛选主入口)
-- 砍掉: status / client_ip / redirect_backend / emby_user_id / route_id / pool_id / tag 单列
-- 被砍的字段要么基数太低(索引不被选择)、要么只用在 GROUP BY 聚合场景(全表扫 + hash agg 足够)。

DROP INDEX IF EXISTS idx_grl_tag;
DROP INDEX IF EXISTS idx_grl_status;
DROP INDEX IF EXISTS idx_grl_client_ip;
DROP INDEX IF EXISTS idx_grl_redirect_backend;
DROP INDEX IF EXISTS idx_grl_emby_user_id;
DROP INDEX IF EXISTS idx_grl_route_id;
DROP INDEX IF EXISTS idx_grl_pool_id;
