package source

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"fyms/internal/repository"
)

const (
	AutoDisableSearchEnabledKey = "source_auto_disable_search_failed_enabled"
	AutoDisablePlayEnabledKey   = "source_auto_disable_play_failed_enabled"
	AutoDisableThresholdKey     = "source_auto_disable_threshold"

	autoDisableSearchScope = "search"
	autoDisablePlayScope   = "play"
)

const defaultAutoDisableThreshold = 3

type AutoDisableConfigReader interface {
	GetBoolOrDefault(ctx context.Context, key string, def bool) bool
	GetIntOrDefault(ctx context.Context, key string, def int) int
}

type AutoDisableDecision struct {
	Enabled      bool   `json:"enabled"`
	ProviderID   int64  `json:"provider_id,omitempty"`
	ProviderName string `json:"provider_name,omitempty"`
	Scope        string `json:"scope,omitempty"`
	FailureCount int    `json:"failure_count,omitempty"`
	Threshold    int    `json:"threshold,omitempty"`
	Disabled     bool   `json:"disabled,omitempty"`
}

func AutoDisableThreshold(config AutoDisableConfigReader, ctx context.Context) int {
	if config == nil {
		return defaultAutoDisableThreshold
	}
	threshold := config.GetIntOrDefault(ctx, AutoDisableThresholdKey, defaultAutoDisableThreshold)
	if threshold <= 0 {
		return defaultAutoDisableThreshold
	}
	return threshold
}

func SearchAutoDisableEnabled(config AutoDisableConfigReader, ctx context.Context) bool {
	return config != nil && config.GetBoolOrDefault(ctx, AutoDisableSearchEnabledKey, false)
}

func PlayAutoDisableEnabled(config AutoDisableConfigReader, ctx context.Context) bool {
	return config != nil && config.GetBoolOrDefault(ctx, AutoDisablePlayEnabledKey, false)
}

func RecordProviderSearchSuccess(ctx context.Context, repo *repository.SourceRepository, config AutoDisableConfigReader, providerID int64) {
	if SearchAutoDisableEnabled(config, ctx) {
		resetProviderAutoDisableFailure(ctx, repo, providerID, autoDisableSearchScope)
	}
}

func RecordProviderSearchFailure(ctx context.Context, repo *repository.SourceRepository, config AutoDisableConfigReader, provider repository.SourceProvider, err error) AutoDisableDecision {
	return recordProviderAutoDisableFailure(ctx, repo, config, provider, err, autoDisableSearchScope, SearchAutoDisableEnabled(config, ctx))
}

func RecordProviderPlaySuccess(ctx context.Context, repo *repository.SourceRepository, config AutoDisableConfigReader, providerID int64) {
	if PlayAutoDisableEnabled(config, ctx) {
		resetProviderAutoDisableFailure(ctx, repo, providerID, autoDisablePlayScope)
	}
}

func RecordProviderPlayFailure(ctx context.Context, repo *repository.SourceRepository, config AutoDisableConfigReader, providerID int64, err error) AutoDisableDecision {
	if repo == nil || providerID <= 0 {
		return AutoDisableDecision{}
	}
	provider, getErr := repo.GetProviderByID(ctx, providerID)
	if getErr != nil || provider == nil {
		return AutoDisableDecision{}
	}
	return recordProviderAutoDisableFailure(ctx, repo, config, *provider, err, autoDisablePlayScope, PlayAutoDisableEnabled(config, ctx))
}

func resetProviderAutoDisableFailure(ctx context.Context, repo *repository.SourceRepository, providerID int64, scope string) {
	if repo == nil || providerID <= 0 {
		return
	}
	if err := repo.ResetProviderAutoDisableFailure(ctx, providerID, scope); err != nil {
		SourceLogger("provider").Warn("[Provider] reset auto-disable failure failed",
			"log_target", "source",
			"provider_id", providerID,
			"scope", scope,
			"error", err)
	}
}

func recordProviderAutoDisableFailure(ctx context.Context, repo *repository.SourceRepository, config AutoDisableConfigReader, provider repository.SourceProvider, err error, scope string, enabled bool) AutoDisableDecision {
	decision := AutoDisableDecision{
		Enabled:      enabled,
		ProviderID:   provider.ID,
		ProviderName: provider.Name,
		Scope:        scope,
		Threshold:    AutoDisableThreshold(config, ctx),
	}
	if !enabled || repo == nil || provider.ID <= 0 || !isSearchableRuntimeProvider(provider) {
		return decision
	}
	errorType := ErrorType(err)
	message := ""
	if err != nil {
		message = strings.TrimSpace(err.Error())
	}
	if message == "" {
		message = "provider failure"
	}
	item, count, disabled, updateErr := repo.RecordProviderAutoDisableFailure(ctx, provider.ID, scope, errorType, message, decision.Threshold)
	if updateErr != nil {
		SourceLogger("provider").Warn("[Provider] auto-disable failure record failed",
			"log_target", "source",
			"provider_id", provider.ID,
			"scope", scope,
			"error", updateErr)
		return decision
	}
	decision.FailureCount = count
	decision.Disabled = disabled
	if item != nil {
		decision.ProviderName = item.Name
	}
	level := slog.LevelInfo
	if disabled {
		level = slog.LevelWarn
	}
	LogSourceAction(SourceLogger("provider"), time.Now(), level, "[Provider] auto_disable_failure",
		"action", "auto_disable_failure",
		"provider_id", provider.ID,
		"scope", scope,
		"failure_count", count,
		"threshold", decision.Threshold,
		"disabled", disabled,
		"error_type", errorType)
	return decision
}
