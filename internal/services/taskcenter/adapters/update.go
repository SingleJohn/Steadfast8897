package adapters

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/services"
	"fyms/internal/services/taskcenter"
)

// UpdateAdapter 包装 Docker 自更新任务（services.Updater）。
//
// 两种 action：
//   - "check" 同步阻塞，直接记录 Begin → End（成功/失败），无 tracker。
//   - "apply" 异步，StartApply 启动后再 Begin + tracker；apply 成功会替换容器，
//     本进程的 tracker 见不到 completed，由下次启动时 ReconcileOnStartup 收尾。
//
// 默认 action = "check"。
type UpdateAdapter struct {
	Updater *services.Updater
	DB      *pgxpool.Pool

	mu            sync.Mutex
	currentRunID  int64
	stopRequested atomic.Bool
}

var ErrUpdateUnknownAction = errors.New("update action must be 'check' or 'apply'")

func NewUpdateAdapter(u *services.Updater, db *pgxpool.Pool) *UpdateAdapter {
	return &UpdateAdapter{Updater: u, DB: db}
}

func (a *UpdateAdapter) Kind() taskcenter.Kind { return taskcenter.KindUpdate }

func (a *UpdateAdapter) Snapshot() taskcenter.Snapshot {
	st := a.Updater.GetStatus(context.Background())
	status := mapLegacyStatus(st.Status)

	msg := st.Message
	if st.TargetVersion != "" && msg == "" {
		msg = "target=" + st.TargetVersion
	}

	snap := taskcenter.Snapshot{
		Kind:        taskcenter.KindUpdate,
		Status:      status,
		Phase:       st.Status,
		Message:     msg,
		Cancellable: false,
	}
	if st.Error != nil {
		snap.Error = *st.Error
	}
	snap.StartedAt = parseRFC3339Millis(st.StartedAt)
	snap.CompletedAt = parseRFC3339Millis(st.CompletedAt)

	counters := map[string]int64{}
	if st.HasUpdate {
		counters["hasUpdate"] = 1
	}
	if len(st.Logs) > 0 {
		counters["logLines"] = int64(len(st.Logs))
	}
	if len(counters) > 0 {
		snap.Counters = counters
	}

	a.mu.Lock()
	snap.RunID = a.currentRunID
	a.mu.Unlock()
	return snap
}

func (a *UpdateAdapter) Start(ctx context.Context, params taskcenter.StartParams, trigger taskcenter.Trigger) (int64, error) {
	action := paramString(params, "action", "check")
	switch action {
	case "check":
		return a.startCheck(ctx, trigger)
	case "apply":
		return a.startApply(ctx, trigger)
	default:
		return 0, ErrUpdateUnknownAction
	}
}

func (a *UpdateAdapter) Stop() error {
	// Docker 更新不支持中断。Stop 设置 flag 仅用于 apply 路径的 tracker 判定。
	a.stopRequested.Store(true)
	return nil
}

// startCheck 同步跑完 Check 后一次性写 Begin/End。
// Check 耗时通常很短（HTTP 查 Docker Hub / GitHub），tracker 粒度会浪费一圈轮询。
func (a *UpdateAdapter) startCheck(ctx context.Context, trigger taskcenter.Trigger) (int64, error) {
	runID, err := taskcenter.Begin(ctx, a.DB, taskcenter.BeginParams{
		Kind:    taskcenter.KindUpdate,
		Trigger: trigger,
		Payload: map[string]any{"action": "check"},
	})
	if err != nil {
		slog.Warn("update check: begin run failed", "error", err)
	}

	st, checkErr := a.Updater.Check(ctx)

	if runID > 0 {
		var msg, errMsg string
		status := taskcenter.StatusSucceeded
		if checkErr != nil {
			status = taskcenter.StatusFailed
			errMsg = checkErr.Error()
		} else {
			if st.HasUpdate {
				msg = "available: " + st.LatestVersion
			} else {
				msg = "up to date"
			}
		}
		_ = taskcenter.End(context.Background(), a.DB, runID, status, msg, errMsg)
	}

	return runID, checkErr
}

// startApply 启动异步更新流程，不阻塞请求。apply 成功会重启容器，
// 本进程 tracker 捕捉不到 completed，由下次启动的 ReconcileOnStartup 兜底。
func (a *UpdateAdapter) startApply(ctx context.Context, trigger taskcenter.Trigger) (int64, error) {
	a.mu.Lock()
	if a.currentRunID != 0 {
		rid := a.currentRunID
		a.mu.Unlock()
		return rid, nil
	}
	a.mu.Unlock()

	if _, err := a.Updater.StartApply(ctx); err != nil {
		return 0, err
	}

	runID, err := taskcenter.Begin(ctx, a.DB, taskcenter.BeginParams{
		Kind:    taskcenter.KindUpdate,
		Trigger: trigger,
		Payload: map[string]any{"action": "apply"},
	})
	if err != nil {
		slog.Warn("update apply: begin run failed", "error", err)
		return 0, nil
	}

	a.mu.Lock()
	a.currentRunID = runID
	a.mu.Unlock()
	a.stopRequested.Store(false)

	go a.trackApply(runID)
	return runID, nil
}

// trackApply 监视 update apply 的终态。见到 completed/error 即写 End；
// 若进程被 helper container 杀掉替换，tracker 永远停在循环里直到进程退出。
func (a *UpdateAdapter) trackApply(runID int64) {
	ctx := context.Background()
	ticker := time.NewTicker(trackerTick)
	defer ticker.Stop()

	var seenRunning bool
	for range ticker.C {
		s := a.Snapshot()
		if s.Status.Running() {
			seenRunning = true
			continue
		}
		if !seenRunning {
			continue
		}

		final := s.Status
		if final == taskcenter.StatusIdle {
			if a.stopRequested.Load() {
				final = taskcenter.StatusCancelled
			} else {
				final = taskcenter.StatusSucceeded
			}
		}
		_ = taskcenter.End(ctx, a.DB, runID, final, s.Message, s.Error)

		a.mu.Lock()
		a.currentRunID = 0
		a.mu.Unlock()
		return
	}
}

func paramString(p taskcenter.StartParams, key, def string) string {
	if p == nil {
		return def
	}
	if v, ok := p[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return def
}

func parseRFC3339Millis(s *string) int64 {
	if s == nil || *s == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}
