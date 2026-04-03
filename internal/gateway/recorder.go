package gateway

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Recorder asynchronously batches request logs and writes them to the DB.
type Recorder struct {
	store   *Store
	config  ObservabilityConfig
	logger  *slog.Logger
	mu      sync.Mutex
	buffer  []RequestLog
	flushCh chan struct{}
}

func NewRecorder(store *Store, cfg ObservabilityConfig, logger *slog.Logger) *Recorder {
	if cfg.DBBatchSize <= 0 {
		cfg.DBBatchSize = 100
	}
	if cfg.DBFlushIntervalMs <= 0 {
		cfg.DBFlushIntervalMs = 2000
	}
	return &Recorder{
		store:   store,
		config:  cfg,
		logger:  logger,
		buffer:  make([]RequestLog, 0, cfg.DBBatchSize),
		flushCh: make(chan struct{}, 1),
	}
}

func (rec *Recorder) Record(log RequestLog) {
	log.CreatedAt = time.Now()
	rec.mu.Lock()
	rec.buffer = append(rec.buffer, log)
	shouldFlush := len(rec.buffer) >= rec.config.DBBatchSize
	rec.mu.Unlock()

	if shouldFlush {
		select {
		case rec.flushCh <- struct{}{}:
		default:
		}
	}
}

func (rec *Recorder) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(rec.config.DBFlushIntervalMs) * time.Millisecond)
	defer ticker.Stop()

	cleanupTicker := time.NewTicker(1 * time.Hour)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			rec.Flush(context.Background())
			return
		case <-ticker.C:
			rec.Flush(ctx)
		case <-rec.flushCh:
			rec.Flush(ctx)
		case <-cleanupTicker.C:
			rec.cleanup(ctx)
		}
	}
}

func (rec *Recorder) Flush(ctx context.Context) {
	rec.mu.Lock()
	if len(rec.buffer) == 0 {
		rec.mu.Unlock()
		return
	}
	batch := rec.buffer
	rec.buffer = make([]RequestLog, 0, rec.config.DBBatchSize)
	rec.mu.Unlock()

	if err := rec.store.InsertRequestLogs(ctx, batch); err != nil {
		rec.logger.Error("failed to insert request logs", "count", len(batch), "error", err)
		return
	}

	// Update daily stats
	statMap := map[string]*DailyStat{}
	for _, l := range batch {
		day := l.CreatedAt.Truncate(24 * time.Hour)
		key := day.Format("2006-01-02") + "|" + l.Tag + "|" + l.SourceID
		stat, ok := statMap[key]
		if !ok {
			stat = &DailyStat{
				Day:          day,
				Tag:          l.Tag,
				SourceID:     l.SourceID,
				LatencyMsMin: l.LatencyMs,
			}
			statMap[key] = stat
		}
		stat.Requests++
		if l.Status == 302 {
			stat.Redirects302++
		}
		if l.Status >= 400 && l.Status < 500 {
			stat.Status4xx++
		}
		if l.Status >= 500 {
			stat.Status5xx++
		}
		stat.BytesIn += l.BytesIn
		stat.BytesOut += l.BytesOut
		stat.LatencyMsSum += l.LatencyMs
		if l.LatencyMs > stat.LatencyMsMax {
			stat.LatencyMsMax = l.LatencyMs
		}
		if l.LatencyMs < stat.LatencyMsMin {
			stat.LatencyMsMin = l.LatencyMs
		}
	}

	for _, stat := range statMap {
		if err := rec.store.UpsertDailyStat(ctx, *stat); err != nil {
			rec.logger.Error("failed to upsert daily stat", "error", err)
		}
	}
}

func (rec *Recorder) cleanup(ctx context.Context) {
	if err := rec.store.CleanupOldLogs(ctx, rec.config.RequestLogRetentionDays); err != nil {
		rec.logger.Error("failed to cleanup old logs", "error", err)
	}
	if err := rec.store.CleanupOldStats(ctx, rec.config.StatRetentionDays); err != nil {
		rec.logger.Error("failed to cleanup old stats", "error", err)
	}
}
