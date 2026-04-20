package services

import (
	"context"
	"log/slog"
	"time"
)

const metricsLogInterval = 60 * time.Second

// StartMetricsLogger 周期性(默认 60s)打印 ingest / scrape_queue / tmdb 关键指标
// 到 slog,供日志分析和排障用。不做独立 HTTP /metrics endpoint —— 运维可以
// 直接从 [Metrics] 前缀的日志行抓取,或外部工具查 scrape_queue 表。
//
// 停止:ctx 结束时 goroutine 自动退出。
func StartMetricsLogger(ctx context.Context, ingest *IngestWorker, queue *ScrapeQueue) {
	if ingest == nil || queue == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(metricsLogInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				logMetricsTick(ctx, ingest, queue)
			}
		}
	}()
}

func logMetricsTick(ctx context.Context, ingest *IngestWorker, queue *ScrapeQueue) {
	stats, err := queue.Stats(ctx)
	if err != nil {
		slog.Warn("[Metrics] scrape stats failed", "error", err)
		return
	}
	slog.Info("[Metrics]",
		"ingest_channel_depth", ingest.ChannelDepth(),
		"ingest_overflow_total", ingest.OverflowCount(),
		"scrape_pending", stats.Pending,
		"scrape_running", stats.Running,
		"scrape_failed", stats.Failed,
		"scrape_done", stats.Done,
		"tmdb_requests_total", TmdbRequestCount(),
	)
}
