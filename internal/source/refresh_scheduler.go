package source

import (
	"context"
	"log/slog"
	"time"

	"fyms/internal/repository"
)

const (
	sourceRefreshSchedulerEnabledKey = "source_refresh_scheduler_enabled"
	// 每隔多久给所有启用 provider 入队一轮分类抓取（填充虚拟库）。
	sourceCatalogSweepEvery = 6 * time.Hour
	// 每隔多久扫描需要追更的连载剧。
	sourceDetailSweepEvery = 1 * time.Hour
	// 单轮追更扫描入队的连载剧上限，避免一次塞爆队列。
	sourceDetailSweepBatch = 200
	// 启动后首扫延迟，避开启动峰值、等 runtime sidecar 就绪。
	sourceRefreshFirstDelay = 30 * time.Second
)

// SourceRefreshScheduler 定时把 catalog_fetch / detail_refresh 任务入队，
// 由 SourceRefreshWorker 实际消费。
type SourceRefreshScheduler struct {
	queue        *SourceRefreshQueue
	repo         *repository.SourceRepository
	systemConfig *repository.SystemConfigRepository
	logger       *slog.Logger
}

func NewSourceRefreshScheduler(repo *repository.SourceRepository, systemConfig *repository.SystemConfigRepository) *SourceRefreshScheduler {
	return &SourceRefreshScheduler{
		queue:        NewSourceRefreshQueue(repo),
		repo:         repo,
		systemConfig: systemConfig,
		logger:       SourceLogger("refresh"),
	}
}

func (s *SourceRefreshScheduler) Run(ctx context.Context) {
	first := time.NewTimer(sourceRefreshFirstDelay)
	defer first.Stop()
	catalogTicker := time.NewTicker(sourceCatalogSweepEvery)
	defer catalogTicker.Stop()
	detailTicker := time.NewTicker(sourceDetailSweepEvery)
	defer detailTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
			s.sweepIfEnabled(ctx)
		case <-catalogTicker.C:
			s.sweepCatalogIfEnabled(ctx)
		case <-detailTicker.C:
			s.sweepDetailIfEnabled(ctx)
		}
	}
}

func (s *SourceRefreshScheduler) enabled(ctx context.Context) bool {
	if s.systemConfig == nil {
		return false
	}
	return s.systemConfig.GetBoolOrDefault(ctx, sourceRefreshSchedulerEnabledKey, false)
}

func (s *SourceRefreshScheduler) sweepIfEnabled(ctx context.Context) {
	if !s.enabled(ctx) {
		s.logger.Debug("[SourceRefresh] scheduler disabled", "log_target", "refresh")
		return
	}
	s.sweepCatalog(ctx)
	s.sweepDetail(ctx)
}

func (s *SourceRefreshScheduler) sweepCatalogIfEnabled(ctx context.Context) {
	if !s.enabled(ctx) {
		return
	}
	s.sweepCatalog(ctx)
}

func (s *SourceRefreshScheduler) sweepDetailIfEnabled(ctx context.Context) {
	if !s.enabled(ctx) {
		return
	}
	s.sweepDetail(ctx)
}

// sweepCatalog 给所有启用 provider 入队一轮分类抓取。
func (s *SourceRefreshScheduler) sweepCatalog(ctx context.Context) {
	providers, err := s.repo.ListProviders(ctx, repository.SourceProviderListOptions{Limit: 1000, OnlyUsable: true})
	if err != nil {
		s.logger.Warn("[SourceRefresh] sweep catalog: list providers failed", "log_target", "refresh", "error", err)
		return
	}
	enqueued := 0
	for _, p := range providers {
		if !p.Enabled {
			continue
		}
		if err := s.queue.EnqueueCatalogFetch(ctx, p.ID, RefreshPriorityScheduled); err == nil {
			enqueued++
		}
	}
	if enqueued > 0 {
		s.logger.Info("[SourceRefresh] catalog sweep enqueued", "log_target", "refresh", "providers", enqueued)
	}
}

// sweepDetail 给超过 TTL 的连载剧入队追更。
func (s *SourceRefreshScheduler) sweepDetail(ctx context.Context) {
	ids, err := s.repo.ListStaleSeriesItemIDs(ctx, sourceSeriesDetailTTL, sourceDetailSweepBatch)
	if err != nil {
		s.logger.Warn("[SourceRefresh] sweep detail: list stale series failed", "log_target", "refresh", "error", err)
		return
	}
	enqueued := 0
	for _, id := range ids {
		if err := s.queue.EnqueueDetailRefresh(ctx, id, RefreshPriorityScheduled); err == nil {
			enqueued++
		}
	}
	if enqueued > 0 {
		s.logger.Info("[SourceRefresh] detail sweep enqueued", "log_target", "refresh", "series", enqueued)
	}
}
