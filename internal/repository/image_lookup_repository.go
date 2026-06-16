package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	dbgen "fyms/internal/db/gen"
)

type ImageLookupRepository struct {
	queries *dbgen.Queries
}

type ItemImageInfo struct {
	PrimaryPath  *string
	BackdropPath *string
	Type         string
}

type ImagePaths struct {
	PrimaryPath  *string
	BackdropPath *string
}

func NewImageLookupRepository(pool *pgxpool.Pool) *ImageLookupRepository {
	return &ImageLookupRepository{queries: dbgen.New(pool)}
}

func (r *ImageLookupRepository) GetItemImageInfo(ctx context.Context, id string) (*ItemImageInfo, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	row, err := r.queries.GetItemImageInfo(ctx, toPGUUID(uid))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ItemImageInfo{
		PrimaryPath:  ptrText(row.PrimaryImagePath),
		BackdropPath: ptrText(row.BackdropImagePath),
		Type:         row.Type,
	}, nil
}

func (r *ImageLookupRepository) GetLibraryPrimaryImagePath(ctx context.Context, id string) (*string, error) {
	return r.getTextByID(ctx, id, r.queries.GetLibraryPrimaryImagePath)
}

func (r *ImageLookupRepository) GetCastImageURL(ctx context.Context, id string) (*string, error) {
	return r.getTextByID(ctx, id, r.queries.GetCastImageURL)
}

func (r *ImageLookupRepository) GetCastImageURLByTagAndItem(ctx context.Context, tagID, itemID string) (*string, error) {
	tagUUID, itemUUID, err := parseTwoUUIDs(tagID, itemID)
	if err != nil {
		return nil, err
	}
	value, err := r.queries.GetCastImageURLByTagAndItem(ctx, dbgen.GetCastImageURLByTagAndItemParams{
		TagID:  toPGUUID(tagUUID),
		ItemID: toPGUUID(itemUUID),
	})
	return textPtrOrNil(value, err)
}

func (r *ImageLookupRepository) GetItemExtraImagePath(ctx context.Context, itemID string, idx int32) (*string, error) {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return nil, err
	}
	value, err := r.queries.GetItemExtraImagePath(ctx, dbgen.GetItemExtraImagePathParams{
		ItemID: toPGUUID(uid),
		Idx:    idx,
	})
	return stringPtrOrNil(value, err)
}

func (r *ImageLookupRepository) GetMergedSecondaryImagePaths(ctx context.Context, id string) (*ImagePaths, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	row, err := r.queries.GetMergedSecondaryImagePaths(ctx, toPGUUID(uid))
	return imagePathsOrNil(row.PrimaryImagePath, row.BackdropImagePath, err)
}

func (r *ImageLookupRepository) GetMergedPrimaryImagePaths(ctx context.Context, id string) (*ImagePaths, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	row, err := r.queries.GetMergedPrimaryImagePaths(ctx, toPGUUID(uid))
	return imagePathsOrNil(row.PrimaryImagePath, row.BackdropImagePath, err)
}

func (r *ImageLookupRepository) GetEpisodeSeriesImageParentID(ctx context.Context, id string) (*string, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	value, err := r.queries.GetEpisodeSeriesImageParentID(ctx, toPGUUID(uid))
	if err == pgx.ErrNoRows || value == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func (r *ImageLookupRepository) GetItemPrimaryImagePath(ctx context.Context, id string) (*string, error) {
	return r.getTextByID(ctx, id, r.queries.GetItemPrimaryImagePath)
}

func (r *ImageLookupRepository) GetItemBackdropImagePath(ctx context.Context, id string) (*string, error) {
	return r.getTextByID(ctx, id, r.queries.GetItemBackdropImagePath)
}

func (r *ImageLookupRepository) getTextByID(ctx context.Context, id string, fn func(context.Context, pgtype.UUID) (pgtype.Text, error)) (*string, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	value, err := fn(ctx, toPGUUID(uid))
	return textPtrOrNil(value, err)
}

func imagePathsOrNil(primaryPath, backdropPath pgtype.Text, err error) (*ImagePaths, error) {
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ImagePaths{
		PrimaryPath:  ptrText(primaryPath),
		BackdropPath: ptrText(backdropPath),
	}, nil
}

func textPtrOrNil(value pgtype.Text, err error) (*string, error) {
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ptrText(value), nil
}

func stringPtrOrNil(value string, err error) (*string, error) {
	if err == pgx.ErrNoRows || value == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &value, nil
}
