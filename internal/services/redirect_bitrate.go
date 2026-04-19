package services

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RedirectBitrateEstimator 基于 SessionManager 的活跃会话，汇总当前正在播放项的码率。
// 因为 302 转发流量不经过本进程，无法直接测量 —— 这里用"活跃会话的声明码率"做估算。
type RedirectBitrateEstimator struct {
	pool *pgxpool.Pool
	sm   *SessionManager

	mu    sync.Mutex
	cache map[string]bitrateEntry // itemID → bitrate(bytes/s)
	ttl   time.Duration
}

type bitrateEntry struct {
	bps       uint64
	expiresAt time.Time
}

func NewRedirectBitrateEstimator(pool *pgxpool.Pool, sm *SessionManager) *RedirectBitrateEstimator {
	return &RedirectBitrateEstimator{
		pool:  pool,
		sm:    sm,
		cache: make(map[string]bitrateEntry),
		ttl:   5 * time.Minute,
	}
}

// Estimate 返回 (活跃会话码率合计 bytes/s, 活跃会话数)。
// 暂停的会话计入活跃数但不计入码率（与直觉对齐：暂停时实际不下行）。
func (e *RedirectBitrateEstimator) Estimate() (uint64, int) {
	if e == nil || e.sm == nil {
		return 0, 0
	}
	sessions := e.sm.GetActiveSessions()
	if len(sessions) == 0 {
		return 0, 0
	}

	ids := make([]string, 0, len(sessions))
	active := 0
	for _, s := range sessions {
		np := s.NowPlaying
		if np == nil || np.ItemID == "" {
			continue
		}
		active++
		if np.IsPaused {
			continue
		}
		if _, err := uuid.Parse(np.ItemID); err != nil {
			continue
		}
		ids = append(ids, np.ItemID)
	}
	if len(ids) == 0 {
		return 0, active
	}

	now := time.Now()
	e.mu.Lock()
	var total uint64
	var missing []string
	for _, id := range ids {
		if ent, ok := e.cache[id]; ok && ent.expiresAt.After(now) {
			total += ent.bps
			continue
		}
		missing = append(missing, id)
	}
	e.mu.Unlock()

	if len(missing) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		fetched := e.fetch(ctx, missing)
		e.mu.Lock()
		exp := now.Add(e.ttl)
		for _, id := range missing {
			bps := fetched[id]
			e.cache[id] = bitrateEntry{bps: bps, expiresAt: exp}
			total += bps
		}
		e.mu.Unlock()
	}

	// 清理过期项（机会主义：超过 200 条时扫一遍）
	e.mu.Lock()
	if len(e.cache) > 200 {
		for id, ent := range e.cache {
			if ent.expiresAt.Before(now) {
				delete(e.cache, id)
			}
		}
	}
	e.mu.Unlock()

	return total, active
}

// fetch 批量查询 itemID → bitrate(bytes/s)。优先 media_versions.bitrate（文件级总码率），
// 缺失时回退 media_streams.bit_rate 求和。DB 里 bit_rate 单位是 bits/s，这里转 bytes/s。
func (e *RedirectBitrateEstimator) fetch(ctx context.Context, ids []string) map[string]uint64 {
	out := make(map[string]uint64, len(ids))
	if len(ids) == 0 {
		return out
	}

	rows, err := e.pool.Query(ctx, `
		SELECT item_id::text, MAX(bitrate)::bigint
		FROM media_versions
		WHERE item_id = ANY($1::uuid[]) AND bitrate IS NOT NULL AND bitrate > 0
		GROUP BY item_id
	`, ids)
	if err == nil {
		for rows.Next() {
			var id string
			var bps int64
			if err := rows.Scan(&id, &bps); err == nil && bps > 0 {
				out[id] = uint64(bps) / 8 // bits → bytes
			}
		}
		rows.Close()
	}

	var miss []string
	for _, id := range ids {
		if _, ok := out[id]; !ok {
			miss = append(miss, id)
		}
	}
	if len(miss) == 0 {
		return out
	}

	rows2, err := e.pool.Query(ctx, `
		SELECT item_id::text, SUM(bit_rate)::bigint
		FROM media_streams
		WHERE item_id = ANY($1::uuid[]) AND bit_rate IS NOT NULL
		GROUP BY item_id
	`, miss)
	if err == nil {
		for rows2.Next() {
			var id string
			var bps int64
			if err := rows2.Scan(&id, &bps); err == nil && bps > 0 {
				out[id] = uint64(bps) / 8
			}
		}
		rows2.Close()
	}
	return out
}
