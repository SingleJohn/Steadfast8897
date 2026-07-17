package services

import (
	"context"
	"errors"
	"time"
)

const (
	tmdbCircuitFailureThreshold = 3
	tmdbCircuitBaseCooldown     = 30 * time.Second
	tmdbCircuitMaxCooldown      = 10 * time.Minute
)

type ScrapeWorkerRuntimeSnapshot struct {
	Status                string     `json:"status"`
	TMDBState             string     `json:"tmdb_state"`
	RemoteClaimsAllowed   bool       `json:"remote_claims_allowed"`
	CircuitOpen           bool       `json:"circuit_open"`
	CooldownUntil         *time.Time `json:"cooldown_until,omitempty"`
	ConsecutiveFailures   int        `json:"consecutive_failures"`
	LastError             string     `json:"last_error,omitempty"`
	LastErrorAt           *time.Time `json:"last_error_at,omitempty"`
	LastSuccessAt         *time.Time `json:"last_success_at,omitempty"`
	ClaimFailuresTotal    int64      `json:"claim_failures_total"`
	StateWriteFailTotal   int64      `json:"state_write_failures_total"`
	StateWriteHealthy     bool       `json:"state_write_healthy"`
	LastStateWriteError   string     `json:"last_state_write_error,omitempty"`
	LastStateWriteErrorAt *time.Time `json:"last_state_write_error_at,omitempty"`
	CircuitOpeningsTotal  int64      `json:"circuit_openings_total"`
}

type scrapeWorkerRuntimeState struct {
	tmdbState             string
	remoteClaimsAllowed   bool
	circuitOpen           bool
	cooldownUntil         *time.Time
	consecutiveFailures   int
	lastError             string
	lastErrorAt           *time.Time
	lastSuccessAt         *time.Time
	circuitOpenings       int64
	circuitLevel          int
	stateWriteHealthy     bool
	lastStateWriteError   string
	lastStateWriteErrorAt *time.Time
}

func (w *ScrapeWorker) RuntimeSnapshot() ScrapeWorkerRuntimeSnapshot {
	w.runtimeMu.Lock()
	defer w.runtimeMu.Unlock()

	status := "healthy"
	if w.runtime.tmdbState == "not_configured" {
		status = "blocked"
	} else if !w.runtime.stateWriteHealthy || w.runtime.tmdbState != "ready" {
		status = "degraded"
	}

	return ScrapeWorkerRuntimeSnapshot{
		Status:                status,
		TMDBState:             w.runtime.tmdbState,
		RemoteClaimsAllowed:   w.runtime.remoteClaimsAllowed,
		CircuitOpen:           w.runtime.circuitOpen,
		CooldownUntil:         cloneTimePointer(w.runtime.cooldownUntil),
		ConsecutiveFailures:   w.runtime.consecutiveFailures,
		LastError:             w.runtime.lastError,
		LastErrorAt:           cloneTimePointer(w.runtime.lastErrorAt),
		LastSuccessAt:         cloneTimePointer(w.runtime.lastSuccessAt),
		ClaimFailuresTotal:    w.claimFailures.Load(),
		StateWriteFailTotal:   w.stateWriteFailures.Load(),
		StateWriteHealthy:     w.runtime.stateWriteHealthy,
		LastStateWriteError:   w.runtime.lastStateWriteError,
		LastStateWriteErrorAt: cloneTimePointer(w.runtime.lastStateWriteErrorAt),
		CircuitOpeningsTotal:  w.runtime.circuitOpenings,
	}
}

func (w *ScrapeWorker) ReloadTmdbClient(ctx context.Context) error {
	InvalidateScrapeAggregator()
	client, err := loadTmdbClientFromConfig(ctx, w.pool)
	if err != nil {
		w.cachedClient.Store(nil)
		w.noteTMDBConfigError(err)
		return err
	}
	w.cachedClient.Store(client)
	w.noteRemoteSuccess()
	return nil
}

func (w *ScrapeWorker) InvalidateCachedClient() {
	w.cachedClient.Store(nil)
	InvalidateScrapeAggregator()
	w.runtimeMu.Lock()
	w.runtime.tmdbState = "unknown"
	w.runtime.remoteClaimsAllowed = false
	w.runtime.circuitOpen = false
	w.runtime.cooldownUntil = nil
	w.runtimeMu.Unlock()
}

func (w *ScrapeWorker) noteTMDBConfigError(err error) {
	now := time.Now()
	w.runtimeMu.Lock()
	defer w.runtimeMu.Unlock()

	w.runtime.remoteClaimsAllowed = false
	w.runtime.circuitOpen = false
	w.runtime.cooldownUntil = nil
	w.runtime.consecutiveFailures = 0
	w.runtime.circuitLevel = 0
	w.runtime.lastError = err.Error()
	w.runtime.lastErrorAt = &now
	if errors.Is(err, ErrTMDBNotConfigured) {
		w.runtime.tmdbState = "not_configured"
		return
	}
	w.runtime.tmdbState = "config_error"
}

func (w *ScrapeWorker) remoteClaimsAllowed() bool {
	w.runtimeMu.Lock()
	defer w.runtimeMu.Unlock()

	if w.runtime.circuitOpen && w.runtime.cooldownUntil != nil && !time.Now().Before(*w.runtime.cooldownUntil) {
		w.runtime.circuitOpen = false
		w.runtime.remoteClaimsAllowed = true
		w.runtime.tmdbState = "probing"
	}
	return w.runtime.remoteClaimsAllowed
}

func (w *ScrapeWorker) shouldReloadTMDBConfig() bool {
	w.runtimeMu.Lock()
	defer w.runtimeMu.Unlock()
	return w.runtime.tmdbState == "not_configured" || w.runtime.tmdbState == "config_error" || w.runtime.tmdbState == "unknown"
}

func (w *ScrapeWorker) noteRemoteFailure(err error, fatal bool) {
	if err == nil || fatal {
		return
	}
	if errors.Is(err, ErrTMDBNotConfigured) {
		w.noteTMDBConfigError(err)
		return
	}

	now := time.Now()
	w.runtimeMu.Lock()
	defer w.runtimeMu.Unlock()
	w.runtime.lastError = err.Error()
	w.runtime.lastErrorAt = &now
	if w.runtime.circuitOpen {
		return
	}
	w.runtime.consecutiveFailures++
	if w.runtime.consecutiveFailures < tmdbCircuitFailureThreshold {
		w.runtime.tmdbState = "degraded"
		return
	}

	cooldown := tmdbCircuitBaseCooldown
	for i := 0; i < w.runtime.circuitLevel && cooldown < tmdbCircuitMaxCooldown; i++ {
		cooldown *= 2
	}
	if cooldown > tmdbCircuitMaxCooldown {
		cooldown = tmdbCircuitMaxCooldown
	}
	until := now.Add(cooldown)
	w.runtime.tmdbState = "cooldown"
	w.runtime.remoteClaimsAllowed = false
	w.runtime.circuitOpen = true
	w.runtime.cooldownUntil = &until
	w.runtime.circuitOpenings++
	w.runtime.circuitLevel++
}

func (w *ScrapeWorker) noteRemoteSuccess() {
	now := time.Now()
	w.runtimeMu.Lock()
	defer w.runtimeMu.Unlock()
	w.runtime.tmdbState = "ready"
	w.runtime.remoteClaimsAllowed = true
	w.runtime.circuitOpen = false
	w.runtime.cooldownUntil = nil
	w.runtime.consecutiveFailures = 0
	w.runtime.circuitLevel = 0
	w.runtime.lastSuccessAt = &now
}

func (w *ScrapeWorker) noteStateWriteFailure(err error) {
	if err == nil {
		return
	}
	now := time.Now()
	w.stateWriteFailures.Add(1)
	w.runtimeMu.Lock()
	w.runtime.stateWriteHealthy = false
	w.runtime.lastStateWriteError = err.Error()
	w.runtime.lastStateWriteErrorAt = &now
	w.runtimeMu.Unlock()
}

func (w *ScrapeWorker) noteStateWriteSuccess() {
	w.runtimeMu.Lock()
	w.runtime.stateWriteHealthy = true
	w.runtimeMu.Unlock()
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func scrapeWorkerFailureBackoff(streak int) time.Duration {
	if streak < 1 {
		streak = 1
	}
	if streak > 4 {
		streak = 4
	}
	return time.Duration(1<<(streak-1)) * scrapeIdleSleep
}

func isRemoteScrapeTask(taskType ScrapeTaskType) bool {
	return taskType != ScrapeTaskBackfillQuality
}
