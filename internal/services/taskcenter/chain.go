package taskcenter

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"sync"
	"sync/atomic"
)

// ChainRule 描述一条任务链规则：当 Upstream 任务进入 Status 终态时，
// 自动用 Params 启动 Target 任务（trigger 记为 chain）。
type ChainRule struct {
	Upstream Kind        `json:"upstream"`
	Status   Status      `json:"status"`
	Target   Kind        `json:"target"`
	Params   StartParams `json:"params,omitempty"`
}

// DefaultChainRules 是任务链的默认规则：
//   - 扫描完成 → 探测缺失画质
//   - 探测完成 → 回填 Episode 封面
// quality / name 两个 Backfill stage 已在 scan/刮削阶段产生，因此链里只接 image。
var DefaultChainRules = []ChainRule{
	{Upstream: KindScan, Status: StatusSucceeded, Target: KindProbe},
	{Upstream: KindProbe, Status: StatusSucceeded, Target: KindBackfill, Params: StartParams{"stages": []any{"image"}}},
}

// ChainEngine 订阅 Registry 的事件流，在合适的边沿触发下游任务。
type ChainEngine struct {
	reg     *Registry
	enabled atomic.Bool

	mu    sync.RWMutex
	rules []ChainRule
}

func NewChainEngine(reg *Registry, rules []ChainRule) *ChainEngine {
	e := &ChainEngine{reg: reg}
	if len(rules) == 0 {
		rules = DefaultChainRules
	}
	e.rules = cloneRules(rules)
	return e
}

func (e *ChainEngine) SetEnabled(v bool) { e.enabled.Store(v) }
func (e *ChainEngine) IsEnabled() bool   { return e.enabled.Load() }

func (e *ChainEngine) Rules() []ChainRule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return cloneRules(e.rules)
}

func (e *ChainEngine) SetRules(rules []ChainRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = cloneRules(rules)
}

// RulesJSON 序列化当前规则，用于持久化到 system_config。
func (e *ChainEngine) RulesJSON() (string, error) {
	b, err := json.Marshal(e.Rules())
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// LoadRulesJSON 从 JSON 字符串加载规则。空串或解析失败时保留当前规则。
func (e *ChainEngine) LoadRulesJSON(s string) error {
	if s == "" {
		return nil
	}
	var rules []ChainRule
	if err := json.Unmarshal([]byte(s), &rules); err != nil {
		return err
	}
	if len(rules) > 0 {
		e.SetRules(rules)
	}
	return nil
}

// Start 启动订阅 goroutine，进程生命期运行；ctx 取消后退出。
//
// 去重逻辑：对每个 Kind 记录上一次见到的 Status，只在 "非目标 → 目标" 边沿触发，
// 避免同一终态因 broadcaster 多次推送而被重复派发。
func (e *ChainEngine) Start(ctx context.Context) {
	ch, cancel := e.reg.Subscribe()
	go func() {
		defer cancel()
		last := map[Kind]Status{}
		for {
			select {
			case <-ctx.Done():
				return
			case s, ok := <-ch:
				if !ok {
					return
				}
				prev, had := last[s.Kind]
				last[s.Kind] = s.Status
				if had && prev == s.Status {
					continue
				}
				if !e.enabled.Load() {
					continue
				}
				e.dispatch(s)
			}
		}
	}()
}

func (e *ChainEngine) dispatch(s Snapshot) {
	rules := e.Rules()
	for _, r := range rules {
		if r.Upstream != s.Kind || r.Status != s.Status {
			continue
		}
		target := e.reg.Get(r.Target)
		if target == nil {
			slog.Warn("chain: target kind not registered", "target", r.Target)
			continue
		}
		params := cloneParams(r.Params)
		go func(upstream Kind, tgt Task, targetKind Kind, p StartParams) {
			runID, err := tgt.Start(context.Background(), p, TriggerChain)
			if err != nil {
				slog.Info("chain: start skipped", "upstream", upstream, "target", targetKind, "error", err)
				return
			}
			slog.Info("chain: started", "upstream", upstream, "target", targetKind, "runId", runID)
		}(r.Upstream, target, r.Target, params)
	}
}

func cloneRules(in []ChainRule) []ChainRule {
	out := make([]ChainRule, len(in))
	for i, r := range in {
		out[i] = ChainRule{
			Upstream: r.Upstream,
			Status:   r.Status,
			Target:   r.Target,
			Params:   cloneParams(r.Params),
		}
	}
	return out
}

func cloneParams(p StartParams) StartParams {
	if p == nil {
		return nil
	}
	out := make(StartParams, len(p))
	maps.Copy(out, p)
	return out
}
