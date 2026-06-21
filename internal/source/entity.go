package source

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/models"
	"fyms/internal/repository"
)

type EntityKind string

const (
	EntityKindNotFound      EntityKind = ""
	EntityKindLocalItem     EntityKind = "local_item"
	EntityKindLocalView     EntityKind = "local_view"
	EntityKindSourceItem    EntityKind = "source_item"
	EntityKindSourceEpisode EntityKind = "source_episode"
	EntityKindSourceView    EntityKind = "source_view"
)

type ResolvedEntity struct {
	Kind         EntityKind
	LocalUUID    string
	SourceItemID int64
	EpisodeKey   string
	SourceViewID int64
}

func ResolveEntity(ctx context.Context, pool *pgxpool.Pool, id string) (*ResolvedEntity, error) {
	start := time.Now()
	logger := SourceLogger("resolver")
	logDone := func(kind EntityKind, status string, err error) {
		level := slog.LevelInfo
		attrs := []any{
			"action", "resolve_entity",
			"status", status,
			"kind", string(kind),
			"id_hash", URLHash(id),
			"cache_hit", false,
		}
		if err != nil {
			level = slog.LevelWarn
			attrs = append(attrs, "error_type", ErrorType(err), "error", err)
		}
		LogSourceAction(logger, start, level, "[Resolver] resolve_entity", attrs...)
	}
	if _, err := uuid.Parse(id); err == nil {
		if exists, err := repository.NewItemHelperRepository(pool).ItemExists(ctx, id); err != nil || exists {
			if err != nil {
				logDone(EntityKindNotFound, "error", err)
				return nil, err
			}
			logDone(EntityKindLocalItem, "ok", nil)
			return &ResolvedEntity{Kind: EntityKindLocalItem, LocalUUID: id}, nil
		}
		if _, ok := models.ResolvePlatformVirtualID(ctx, pool, id); ok {
			logDone(EntityKindLocalView, "ok", nil)
			return &ResolvedEntity{Kind: EntityKindLocalView, LocalUUID: id}, nil
		}
		sourceRepo := repository.NewSourceRepository(pool)
		if sourceItemID, ok, err := sourceRepo.ResolveSourceItemPublicUUID(ctx, id); err != nil || ok {
			if err != nil {
				logDone(EntityKindNotFound, "error", err)
				return nil, err
			}
			logDone(EntityKindSourceItem, "ok", nil)
			return &ResolvedEntity{Kind: EntityKindSourceItem, SourceItemID: sourceItemID}, nil
		}
		if sourceViewID, ok, err := sourceRepo.ResolveSourceLibraryViewPublicUUID(ctx, id); err != nil || ok {
			if err != nil {
				logDone(EntityKindNotFound, "error", err)
				return nil, err
			}
			logDone(EntityKindSourceView, "ok", nil)
			return &ResolvedEntity{Kind: EntityKindSourceView, SourceViewID: sourceViewID}, nil
		}
		if sourceItemID, episodeKey, ok, err := sourceRepo.ResolveEpisodePublicUUID(ctx, id, EpisodePublicUUID); err != nil || ok {
			if err != nil {
				logDone(EntityKindNotFound, "error", err)
				return nil, err
			}
			logDone(EntityKindSourceEpisode, "ok", nil)
			return &ResolvedEntity{Kind: EntityKindSourceEpisode, SourceItemID: sourceItemID, EpisodeKey: episodeKey}, nil
		}
		logDone(EntityKindNotFound, "not_found", nil)
		return nil, nil
	}

	if _, err := strconv.Atoi(id); err == nil {
		localUUID, err := models.ResolveToUUID(ctx, pool, id)
		if err != nil || localUUID == nil {
			if err != nil {
				logDone(EntityKindNotFound, "error", err)
			} else {
				logDone(EntityKindNotFound, "not_found", nil)
			}
			return nil, err
		}
		logDone(EntityKindLocalItem, "ok", nil)
		return &ResolvedEntity{Kind: EntityKindLocalItem, LocalUUID: *localUUID}, nil
	}
	logDone(EntityKindNotFound, "not_found", nil)
	return nil, nil
}
