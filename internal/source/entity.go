package source

import (
	"context"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/models"
	"fyms/internal/repository"
)

type EntityKind string

const (
	EntityKindNotFound    EntityKind = ""
	EntityKindLocalItem   EntityKind = "local_item"
	EntityKindLocalView   EntityKind = "local_view"
	EntityKindSourceItem  EntityKind = "source_item"
	EntityKindSourceView  EntityKind = "source_view"
)

type ResolvedEntity struct {
	Kind         EntityKind
	LocalUUID    string
	SourceItemID int64
	SourceViewID int64
}

func ResolveEntity(ctx context.Context, pool *pgxpool.Pool, id string) (*ResolvedEntity, error) {
	if _, err := uuid.Parse(id); err == nil {
		if exists, err := repository.NewItemHelperRepository(pool).ItemExists(ctx, id); err != nil || exists {
			if err != nil {
				return nil, err
			}
			return &ResolvedEntity{Kind: EntityKindLocalItem, LocalUUID: id}, nil
		}
		if _, ok := models.ResolvePlatformVirtualID(ctx, pool, id); ok {
			return &ResolvedEntity{Kind: EntityKindLocalView, LocalUUID: id}, nil
		}
		sourceRepo := repository.NewSourceRepository(pool)
		if sourceItemID, ok, err := sourceRepo.ResolveSourceItemPublicUUID(ctx, id); err != nil || ok {
			if err != nil {
				return nil, err
			}
			return &ResolvedEntity{Kind: EntityKindSourceItem, SourceItemID: sourceItemID}, nil
		}
		if sourceViewID, ok, err := sourceRepo.ResolveSourceLibraryViewPublicUUID(ctx, id); err != nil || ok {
			if err != nil {
				return nil, err
			}
			return &ResolvedEntity{Kind: EntityKindSourceView, SourceViewID: sourceViewID}, nil
		}
		return nil, nil
	}

	if _, err := strconv.Atoi(id); err == nil {
		localUUID, err := models.ResolveToUUID(ctx, pool, id)
		if err != nil || localUUID == nil {
			return nil, err
		}
		return &ResolvedEntity{Kind: EntityKindLocalItem, LocalUUID: *localUUID}, nil
	}
	return nil, nil
}
