package models

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
)

type SourceConfigImport = repository.SourceConfigImport
type SourceProvider = repository.SourceProvider
type SourceItem = repository.SourceItem
type SourcePlaySource = repository.SourcePlaySource
type SourceUserItemData = repository.SourceUserItemData
type SourceLibraryView = repository.SourceLibraryView

type SourceConfigImportUpsert = repository.SourceConfigImportUpsert
type SourceProviderUpsert = repository.SourceProviderUpsert
type SourceItemUpsert = repository.SourceItemUpsert
type SourcePlaySourceUpsert = repository.SourcePlaySourceUpsert
type SourceUserItemDataUpsert = repository.SourceUserItemDataUpsert
type SourceLibraryViewUpsert = repository.SourceLibraryViewUpsert

func UpsertSourceConfigImport(ctx context.Context, pool *pgxpool.Pool, in SourceConfigImportUpsert) (*SourceConfigImport, error) {
	return repository.NewSourceRepository(pool).UpsertConfigImport(ctx, in)
}

func UpsertSourceProvider(ctx context.Context, pool *pgxpool.Pool, in SourceProviderUpsert) (*SourceProvider, error) {
	return repository.NewSourceRepository(pool).UpsertProvider(ctx, in)
}

func UpsertSourceItem(ctx context.Context, pool *pgxpool.Pool, in SourceItemUpsert) (*SourceItem, error) {
	return repository.NewSourceRepository(pool).UpsertSourceItem(ctx, in)
}

func UpsertSourcePlaySource(ctx context.Context, pool *pgxpool.Pool, in SourcePlaySourceUpsert) (*SourcePlaySource, error) {
	return repository.NewSourceRepository(pool).UpsertPlaySource(ctx, in)
}

func UpsertSourceUserItemData(ctx context.Context, pool *pgxpool.Pool, in SourceUserItemDataUpsert) (*SourceUserItemData, error) {
	return repository.NewSourceRepository(pool).UpsertUserItemData(ctx, in)
}

func UpsertSourceLibraryView(ctx context.Context, pool *pgxpool.Pool, in SourceLibraryViewUpsert) (*SourceLibraryView, error) {
	return repository.NewSourceRepository(pool).UpsertLibraryView(ctx, in)
}

func GetSourceItemByID(ctx context.Context, pool *pgxpool.Pool, id int64) (*SourceItem, error) {
	return repository.NewSourceRepository(pool).GetSourceItemByID(ctx, id)
}

func GetSourceItemByPublicUUID(ctx context.Context, pool *pgxpool.Pool, publicUUID string) (*SourceItem, error) {
	return repository.NewSourceRepository(pool).GetSourceItemByPublicUUID(ctx, publicUUID)
}

func GetSourcePlaySourceByID(ctx context.Context, pool *pgxpool.Pool, id int64) (*SourcePlaySource, error) {
	return repository.NewSourceRepository(pool).GetPlaySourceByID(ctx, id)
}

func GetSourcePlaySourceByPublicUUID(ctx context.Context, pool *pgxpool.Pool, publicUUID string) (*SourcePlaySource, error) {
	return repository.NewSourceRepository(pool).GetPlaySourceByPublicUUID(ctx, publicUUID)
}

func GetSourceLibraryViewByID(ctx context.Context, pool *pgxpool.Pool, id int64) (*SourceLibraryView, error) {
	return repository.NewSourceRepository(pool).GetLibraryViewByID(ctx, id)
}

func NewSourceUserItemDataUpsert(userID string, sourceItemID int64, position *int64, playCount *int32, favorite *bool, played *bool, lastPlayed *time.Time) SourceUserItemDataUpsert {
	return SourceUserItemDataUpsert{
		UserID:                userID,
		SourceItemID:          sourceItemID,
		PlaybackPositionTicks: position,
		PlayCount:             playCount,
		IsFavorite:            favorite,
		Played:                played,
		LastPlayedDate:        lastPlayed,
	}
}
