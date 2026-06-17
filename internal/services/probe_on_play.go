package services

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
)

// probingMVs 记录正在探测中的 media_version id,用于多端并发播放时去重,
// 避免同一条 media_version 被重复 ffprobe。
var probingMVs sync.Map

// ProbeOnPlay 在客户端开始播放时被异步调用:若当前播放的 media_version 尚未探测出
// MediaStreams(strm 远程媒体首次播放即属此情况),用 ffprobe 探测真实直链并回填
// media_versions.mediainfo。下次打开详情即可看到音视频轨道信息,行为对齐 Emby。
//
// 设计为 fire-and-forget:由调用方用 goroutine 异步调用(内部自带独立 context 与
// 超时,不依赖请求生命周期)。失败仅记日志,绝不影响播放。
func ProbeOnPlay(pool *pgxpool.Pool, itemID, mediaSourceID string) {
	if pool == nil || itemID == "" {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("[ProbeOnPlay] panic recovered", "err", r)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// 选当前播放、且缺 MediaStreams 或尚未采集过章节的那条 media_version;
	// 优先匹配本次播放的 mediaSourceID。
	target, err := repository.NewBackgroundTaskRepository(pool).GetProbeOnPlayTarget(ctx, itemID, mediaSourceID)
	if err != nil {
		// 无缺失行 / itemID 非 uuid 等情况都安静跳过。
		slog.Debug("[ProbeOnPlay] no probe target", "item", itemID, "err", err)
		return
	}
	if target == nil {
		return
	}
	mvID := target.MediaVersionID.String()

	// 在途去重:同一 media_version 多端并发播放只探一次。
	if _, loaded := probingMVs.LoadOrStore(mvID, struct{}{}); loaded {
		return
	}
	defer probingMVs.Delete(mvID)

	mappings := getProbePathMappings(ctx, pool)
	if err := probeOneItem(ctx, pool, mvID, target.ItemID.String(), target.FilePath, target.Name, mappings); err != nil {
		slog.Warn("[ProbeOnPlay] probe failed", "item", itemID, "mv", mvID, "path", target.FilePath, "err", err)
		return
	}
	slog.Info("[ProbeOnPlay] mediainfo backfilled", "item", itemID, "mv", mvID)
}
