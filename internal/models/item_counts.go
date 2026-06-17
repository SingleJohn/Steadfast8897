package models

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
	"fyms/internal/repository"
)

func GetChildCount(ctx context.Context, pool *pgxpool.Pool, parentID string) (int64, error) {
	return repository.NewItemHelperRepository(pool).GetChildCount(ctx, parentID)
}

func GetLibraryDisplayItemCount(ctx context.Context, pool *pgxpool.Pool, libraryID string) (int64, error) {
	return repository.NewItemReadRepository(pool).CountLibraryDisplayItems(ctx, libraryID)
}

func GetRecursiveItemCount(ctx context.Context, pool *pgxpool.Pool, parentID string) (int64, error) {
	return repository.NewItemHelperRepository(pool).GetRecursiveItemCount(ctx, parentID)
}

func GetUnplayedEpisodeCounts(ctx context.Context, pool *pgxpool.Pool, userID string, itemIDs []string, itemType string) (map[string]int64, error) {
	return repository.NewItemReadRepository(pool).UnplayedEpisodeCounts(ctx, userID, itemIDs, itemType)
}

func GetLatestItems(ctx context.Context, pool *pgxpool.Pool, libraryID string, limit int64) ([]dto.ItemRow, error) {
	libType, err := repository.NewItemHelperRepository(pool).GetCollectionTypeByLibraryID(ctx, libraryID)
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}

	rows, err := repository.NewItemReadRepository(pool).LatestItems(ctx, libraryID, libType, limit)
	if err != nil {
		return nil, err
	}
	items := make([]dto.ItemRow, 0, len(rows))
	for _, row := range rows {
		items = append(items, MapColsToItemRow(row))
	}
	return items, nil
}

func GetMediaStreams(ctx context.Context, pool *pgxpool.Pool, itemID string) ([]dto.StreamRow, error) {
	rows, err := repository.NewItemReadRepository(pool).MediaStreams(ctx, itemID)
	if err != nil {
		return nil, err
	}

	streams := make([]dto.StreamRow, 0, len(rows))
	for _, m := range rows {
		streams = append(streams, dto.StreamRow{
			Codec:        getStringPtr(m, "codec"),
			StreamType:   getString(m, "stream_type"),
			StreamIndex:  int32(getInt32OrZero(m, "stream_index")),
			Language:     getStringPtr(m, "language"),
			Title:        getStringPtr(m, "title"),
			IsDefault:    getBoolPtr(m, "is_default"),
			IsForced:     getBoolPtr(m, "is_forced"),
			Width:        getInt32Ptr(m, "width"),
			Height:       getInt32Ptr(m, "height"),
			BitRate:      getInt64Ptr(m, "bit_rate"),
			Channels:     getInt32Ptr(m, "channels"),
			SampleRate:   getInt32Ptr(m, "sample_rate"),
			BitDepth:     getInt32Ptr(m, "bit_depth"),
			PixelFormat:  getStringPtr(m, "pixel_format"),
			DisplayTitle: getStringPtr(m, "display_title"),
		})
	}
	return streams, nil
}

func getInt32OrZero(m map[string]interface{}, key string) int32 {
	if p := getInt32Ptr(m, key); p != nil {
		return *p
	}
	return 0
}
