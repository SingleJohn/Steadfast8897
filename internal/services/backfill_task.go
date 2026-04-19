package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BackfillStage 表示一个存量回填子任务。
type BackfillStage string

const (
	BackfillStageQuality BackfillStage = "quality" // C:media_versions 画质标签(纯本地)
	BackfillStageName    BackfillStage = "name"    // A:Episode 标题清洗
	BackfillStageImage   BackfillStage = "image"   // B:Episode 缩略图
)

// DefaultBackfillStages 是"全部执行"的默认顺序:C → A → B。
// 先做最快的本地任务,用户立刻看到变化;再跑轻量 API(name),最后跑重 API(image)。
var DefaultBackfillStages = []BackfillStage{
	BackfillStageQuality,
	BackfillStageName,
	BackfillStageImage,
}

// BackfillProgress 是 /Library/Backfill/Progress 返回的结构,保持字段稳定。
type BackfillProgress struct {
	Status       string        `json:"status"`        // idle / running / stopping / completed / stopped / error
	Stage        BackfillStage `json:"stage"`         // 当前 stage(空串代表空闲)
	Processed    int64         `json:"processed"`     // 当前 stage 已处理
	Total        int64         `json:"total"`         // 当前 stage 总数
	LastError    string        `json:"last_error"`    // 最近一次错误信息
	StartedAt    *time.Time    `json:"started_at,omitempty"`
	CompletedAt  *time.Time    `json:"completed_at,omitempty"`
	LastRunAt    *time.Time    `json:"last_run_at,omitempty"` // 上次 completed / stopped 时间
	Counters     map[string]int64 `json:"counters"`           // 各 stage 命中 / 改写计数
}

// BackfillTask 管理三个子任务的串行执行,提供 Start/Stop/Progress。
type BackfillTask struct {
	mu       sync.Mutex
	progress BackfillProgress
	stop     atomic.Bool
}

func NewBackfillTask() *BackfillTask {
	return &BackfillTask{
		progress: BackfillProgress{
			Status:   "idle",
			Counters: map[string]int64{},
		},
	}
}

// GetProgress 返回当前进度的快照(map 会深拷贝)。
func (t *BackfillTask) GetProgress() BackfillProgress {
	t.mu.Lock()
	defer t.mu.Unlock()
	p := t.progress
	p.Counters = make(map[string]int64, len(t.progress.Counters))
	for k, v := range t.progress.Counters {
		p.Counters[k] = v
	}
	return p
}

// Stop 请求终止正在运行的回填;已完成的 stage 不会回滚。
func (t *BackfillTask) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.progress.Status == "running" {
		t.stop.Store(true)
		t.progress.Status = "stopping"
	}
}

// Start 按给定顺序串行跑 stages(如 nil/空则用 DefaultBackfillStages)。
// 仅允许串行:若已在 running/stopping 则立刻返回 error。
func (t *BackfillTask) Start(ctx context.Context, pool *pgxpool.Pool, stages []BackfillStage) error {
	if len(stages) == 0 {
		stages = DefaultBackfillStages
	}
	validStages, err := validateStages(stages)
	if err != nil {
		return err
	}

	t.mu.Lock()
	if t.progress.Status == "running" || t.progress.Status == "stopping" {
		t.mu.Unlock()
		return fmt.Errorf("already running")
	}
	now := time.Now()
	t.progress = BackfillProgress{
		Status:    "running",
		StartedAt: &now,
		Counters:  map[string]int64{},
	}
	t.stop.Store(false)
	t.mu.Unlock()

	go t.run(context.Background(), pool, validStages)
	return nil
}

func (t *BackfillTask) run(ctx context.Context, pool *pgxpool.Pool, stages []BackfillStage) {
	defer func() {
		t.mu.Lock()
		now := time.Now()
		t.progress.CompletedAt = &now
		t.progress.LastRunAt = &now
		if t.progress.Status == "stopping" {
			t.progress.Status = "stopped"
		} else if t.progress.Status == "running" {
			t.progress.Status = "completed"
		}
		t.progress.Stage = ""
		t.mu.Unlock()
		_ = setSystemConfigValue(ctx, pool, "backfill_last_run_at", now.UTC().Format(time.RFC3339))
	}()

	for _, stage := range stages {
		if t.stop.Load() {
			return
		}
		t.mu.Lock()
		t.progress.Stage = stage
		t.progress.Processed = 0
		t.progress.Total = 0
		t.mu.Unlock()

		var err error
		switch stage {
		case BackfillStageQuality:
			err = t.runQualityBackfill(ctx, pool)
		case BackfillStageName:
			err = t.runEpisodeNameBackfill(ctx, pool)
		case BackfillStageImage:
			err = t.runEpisodeImageBackfill(ctx, pool)
		}
		if err != nil && !t.stop.Load() {
			t.mu.Lock()
			t.progress.Status = "error"
			t.progress.LastError = err.Error()
			t.mu.Unlock()
			slog.Warn("[Backfill] stage failed", "stage", stage, "error", err)
			return
		}
	}
}

// advanceProgress 供各 stage 实现回调更新进度。
func (t *BackfillTask) advanceProgress(total int64, processed int64, counterKey string, counterDelta int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if total > 0 {
		t.progress.Total = total
	}
	t.progress.Processed = processed
	if counterDelta != 0 {
		t.progress.Counters[counterKey] += counterDelta
	}
}

func (t *BackfillTask) setStageTotal(total int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.progress.Total = total
}

func (t *BackfillTask) shouldStop() bool {
	return t.stop.Load()
}

func validateStages(stages []BackfillStage) ([]BackfillStage, error) {
	known := map[BackfillStage]bool{
		BackfillStageQuality: true,
		BackfillStageName:    true,
		BackfillStageImage:   true,
	}
	seen := make(map[BackfillStage]bool, len(stages))
	out := make([]BackfillStage, 0, len(stages))
	for _, s := range stages {
		s = BackfillStage(strings.ToLower(string(s)))
		if !known[s] {
			return nil, fmt.Errorf("unknown stage: %q", s)
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out, nil
}

// ShouldAutoRunOnStartup 返回是否要在启动时自动触发 backfill。
// 条件:`backfill_enabled_on_startup = true` 且距离 `backfill_last_run_at` >= 24h(或从未跑过)。
func ShouldAutoRunOnStartup(ctx context.Context, pool *pgxpool.Pool) bool {
	if !readBackfillEnabledOnStartup(ctx, pool) {
		return false
	}
	last := readBackfillLastRunAt(ctx, pool)
	if last.IsZero() {
		return true
	}
	return time.Since(last) >= 24*time.Hour
}

func readBackfillEnabledOnStartup(ctx context.Context, pool *pgxpool.Pool) bool {
	return readBoolSystemConfig(ctx, pool, "backfill_enabled_on_startup", false)
}

// ReadBackfillEnabledOnStartup 导出给 handlers。
func ReadBackfillEnabledOnStartup(ctx context.Context, pool *pgxpool.Pool) bool {
	return readBackfillEnabledOnStartup(ctx, pool)
}

// ReadEpisodeStillFetch 导出给 handlers(复用 episode_fetch.go 的内部函数)。
func ReadEpisodeStillFetch(ctx context.Context, pool *pgxpool.Pool) bool {
	return readEpisodeStillFetchEnabled(ctx, pool)
}

// ReadBoolSystemConfig 导出给 handlers / 任务链，读取布尔配置项，未设置时返回 def。
func ReadBoolSystemConfig(ctx context.Context, pool *pgxpool.Pool, key string, def bool) bool {
	return readBoolSystemConfig(ctx, pool, key, def)
}

// ReadSystemConfigValue 导出：读取任意字符串配置项，未设置返回空串。
func ReadSystemConfigValue(ctx context.Context, pool *pgxpool.Pool, key string) string {
	return readSystemConfigValue(ctx, pool, key)
}

// WriteSystemConfigValue 导出：写任意字符串配置项。
func WriteSystemConfigValue(ctx context.Context, pool *pgxpool.Pool, key, value string) error {
	return setSystemConfigValue(ctx, pool, key, value)
}

// WriteBoolSystemConfig 导出给 handlers,用 "true"/"false" 文本存。
func WriteBoolSystemConfig(ctx context.Context, pool *pgxpool.Pool, key string, value bool) error {
	v := "false"
	if value {
		v = "true"
	}
	return setSystemConfigValue(ctx, pool, key, v)
}

func readBackfillLastRunAt(ctx context.Context, pool *pgxpool.Pool) time.Time {
	val := readSystemConfigValue(ctx, pool, "backfill_last_run_at")
	if val == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339, val); err == nil {
		return ts
	}
	return time.Time{}
}

func readSystemConfigValue(ctx context.Context, pool *pgxpool.Pool, key string) string {
	var val *string
	if err := pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = $1", key).Scan(&val); err != nil || val == nil {
		return ""
	}
	return *val
}

func readBoolSystemConfig(ctx context.Context, pool *pgxpool.Pool, key string, def bool) bool {
	raw := readSystemConfigValue(ctx, pool, key)
	if raw == "" {
		return def
	}
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func setSystemConfigValue(ctx context.Context, pool *pgxpool.Pool, key, value string) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO system_config (key, value) VALUES ($1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		key, value)
	return err
}
