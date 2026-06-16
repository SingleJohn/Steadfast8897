package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	dbgen "fyms/internal/db/gen"
)

type ScanIngestRepository struct {
	queries *dbgen.Queries
}

type DeletedItem struct {
	ID   string
	Name string
	Type string
	Path *string
}

type ItemPathRow struct {
	ID       uuid.UUID
	FilePath string
}

type MediaVersionPathRow struct {
	ItemID   uuid.UUID
	ID       uuid.UUID
	FilePath string
}

type SeasonNumberRow struct {
	ID          uuid.UUID
	IndexNumber *int32
}

type ExternalSubtitleUpsert struct {
	ItemID         uuid.UUID
	MediaVersionID uuid.UUID
	FilePath       string
	Codec          string
	Language       *string
	Title          *string
	IsDefault      bool
	IsForced       bool
}

func NewScanIngestRepository(pool *pgxpool.Pool) *ScanIngestRepository {
	return &ScanIngestRepository{queries: dbgen.New(pool)}
}

func (r *ScanIngestRepository) UpsertExternalSubtitle(ctx context.Context, row ExternalSubtitleUpsert) error {
	return r.queries.UpsertExternalSubtitle(ctx, dbgen.UpsertExternalSubtitleParams{
		Column1:   toPGUUID(row.ItemID),
		Column2:   toPGUUID(row.MediaVersionID),
		FilePath:  row.FilePath,
		Codec:     row.Codec,
		Language:  optionalText(row.Language),
		Title:     optionalText(row.Title),
		IsDefault: row.IsDefault,
		IsForced:  row.IsForced,
	})
}

func (r *ScanIngestRepository) DeleteExternalSubtitlesForMediaVersion(ctx context.Context, mediaVersionID uuid.UUID) error {
	return r.queries.DeleteExternalSubtitlesForMediaVersion(ctx, toPGUUID(mediaVersionID))
}

func (r *ScanIngestRepository) PruneExternalSubtitlesForMediaVersion(ctx context.Context, mediaVersionID uuid.UUID, paths []string) error {
	return r.queries.PruneExternalSubtitlesForMediaVersion(ctx, dbgen.PruneExternalSubtitlesForMediaVersionParams{
		Column1: toPGUUID(mediaVersionID),
		Column2: paths,
	})
}

func (r *ScanIngestRepository) ListMediaVersionsByPath(ctx context.Context, path, cleanPath string) ([]MediaVersionPathRow, error) {
	rows, err := r.queries.ListMediaVersionsByPath(ctx, dbgen.ListMediaVersionsByPathParams{FilePath: path, FilePath_2: cleanPath})
	if err != nil {
		return nil, err
	}
	out := make([]MediaVersionPathRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, MediaVersionPathRow{
			ItemID:   fromPGUUID(row.ItemID),
			ID:       fromPGUUID(row.ID),
			FilePath: row.FilePath,
		})
	}
	return out, nil
}

func (r *ScanIngestRepository) DeleteItemsByExactPath(ctx context.Context, path string) ([]DeletedItem, error) {
	rows, err := r.queries.DeleteItemsByExactPath(ctx, textValue(path))
	if err != nil {
		return nil, err
	}
	out := make([]DeletedItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, DeletedItem{ID: row.ID, Name: row.Name, Type: row.Type, Path: ptrTextFromPG(row.FilePath)})
	}
	return out, nil
}

func (r *ScanIngestRepository) DeleteItemsByPathPrefix(ctx context.Context, path, prefix string) ([]DeletedItem, error) {
	rows, err := r.queries.DeleteItemsByPathPrefix(ctx, dbgen.DeleteItemsByPathPrefixParams{
		FilePath:   textValue(path),
		FilePath_2: textValue(prefix),
	})
	if err != nil {
		return nil, err
	}
	out := make([]DeletedItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, DeletedItem{ID: row.ID, Name: row.Name, Type: row.Type, Path: ptrTextFromPG(row.FilePath)})
	}
	return out, nil
}

func (r *ScanIngestRepository) ListItemsByPathPrefix(ctx context.Context, path, prefix string) ([]ItemPathRow, error) {
	rows, err := r.queries.ListItemsByPathPrefix(ctx, dbgen.ListItemsByPathPrefixParams{
		FilePath:   textValue(path),
		FilePath_2: textValue(prefix),
	})
	if err != nil {
		return nil, err
	}
	out := make([]ItemPathRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, ItemPathRow{ID: fromPGUUID(row.ID), FilePath: textOrEmpty(row.FilePath)})
	}
	return out, nil
}

func (r *ScanIngestRepository) UpdateItemFilePathByID(ctx context.Context, id uuid.UUID, path string) error {
	return r.queries.UpdateItemFilePathByID(ctx, dbgen.UpdateItemFilePathByIDParams{
		FilePath: textValue(path),
		ID:       toPGUUID(id),
	})
}

func (r *ScanIngestRepository) UpdateItemFilePathByOldPath(ctx context.Context, newPath, oldPath string) (int64, error) {
	return r.queries.UpdateItemFilePathByOldPath(ctx, dbgen.UpdateItemFilePathByOldPathParams{
		FilePath:   textValue(newPath),
		FilePath_2: textValue(oldPath),
	})
}

func (r *ScanIngestRepository) UpdateMediaVersionFilePath(ctx context.Context, newPath, oldPath string) error {
	return r.queries.UpdateMediaVersionFilePath(ctx, dbgen.UpdateMediaVersionFilePathParams{FilePath: newPath, FilePath_2: oldPath})
}

func (r *ScanIngestRepository) RenameExternalSubtitlePaths(ctx context.Context, newPath, oldPath string, substringStart int, prefix string) error {
	return r.queries.RenameExternalSubtitlePaths(ctx, dbgen.RenameExternalSubtitlePathsParams{
		FilePath:   newPath,
		FilePath_2: oldPath,
		Substring:  int32(substringStart),
		FilePath_3: prefix,
	})
}

func (r *ScanIngestRepository) ListSeriesSeasonIDs(ctx context.Context, seriesID string) ([]string, error) {
	uid, err := uuid.Parse(seriesID)
	if err != nil {
		return nil, err
	}
	return r.queries.ListSeriesSeasonIDs(ctx, toPGUUID(uid))
}

func (r *ScanIngestRepository) ListSeriesEpisodeIDs(ctx context.Context, seriesID string) ([]string, error) {
	uid, err := uuid.Parse(seriesID)
	if err != nil {
		return nil, err
	}
	return r.queries.ListSeriesEpisodeIDs(ctx, toPGUUID(uid))
}

func (r *ScanIngestRepository) GetDominantEpisodeSeasonNumber(ctx context.Context, seasonID string) (*int32, error) {
	uid, err := uuid.Parse(seasonID)
	if err != nil {
		return nil, err
	}
	v, err := r.queries.GetDominantEpisodeSeasonNumber(ctx, toPGUUID(uid))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ptrInt32FromPG(v), nil
}

func (r *ScanIngestRepository) GetSeasonParentIndexNumber(ctx context.Context, seasonID string) (*int32, error) {
	uid, err := uuid.Parse(seasonID)
	if err != nil {
		return nil, err
	}
	v, err := r.queries.GetSeasonParentIndexNumber(ctx, toPGUUID(uid))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ptrInt32FromPG(v), nil
}

func (r *ScanIngestRepository) GetSeasonIndexNumber(ctx context.Context, seasonID string) (*int32, error) {
	uid, err := uuid.Parse(seasonID)
	if err != nil {
		return nil, err
	}
	v, err := r.queries.GetSeasonIndexNumber(ctx, toPGUUID(uid))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ptrInt32FromPG(v), nil
}

func (r *ScanIngestRepository) GetRefreshItemType(ctx context.Context, itemID string) (string, error) {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return "", err
	}
	return r.queries.GetRefreshItemType(ctx, toPGUUID(uid))
}

func (r *ScanIngestRepository) ListSeriesSubtreeTargetIDs(ctx context.Context, seriesID string) ([]string, error) {
	uid, err := uuid.Parse(seriesID)
	if err != nil {
		return nil, err
	}
	return r.queries.ListSeriesSubtreeTargetIDs(ctx, toPGUUID(uid))
}

func (r *ScanIngestRepository) ListRefreshTargetIDs(ctx context.Context, libraryID *uuid.UUID, itemTypes []string) ([]string, error) {
	if libraryID != nil {
		return r.queries.ListRefreshTargetIDsForLibrary(ctx, dbgen.ListRefreshTargetIDsForLibraryParams{
			Column1: toPGUUID(*libraryID),
			Column2: itemTypes,
		})
	}
	return r.queries.ListRefreshTargetIDs(ctx, itemTypes)
}

func (r *ScanIngestRepository) GetLibraryByItemID(ctx context.Context, itemID string) (*Library, error) {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return nil, err
	}
	row, err := r.queries.GetLibraryByItemID(ctx, toPGUUID(uid))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lib := mapLibrary(row.ID, row.Name, row.CollectionType, row.Paths, row.CreatedAt, row.PrimaryImagePath, row.PrimaryImageTag, row.SortOrder, row.ScrapeConfig)
	return &lib, nil
}

func (r *ScanIngestRepository) GetItemFilePath(ctx context.Context, itemID string) (*string, error) {
	return r.getTextByID(ctx, itemID, r.queries.GetItemFilePath)
}

func (r *ScanIngestRepository) GetFirstSeriesEpisodeFilePath(ctx context.Context, seriesID string) (*string, error) {
	return r.getTextByID(ctx, seriesID, r.queries.GetFirstSeriesEpisodeFilePath)
}

func (r *ScanIngestRepository) GetFirstSeasonEpisodeFilePath(ctx context.Context, seasonID string) (*string, error) {
	return r.getTextByID(ctx, seasonID, r.queries.GetFirstSeasonEpisodeFilePath)
}

func (r *ScanIngestRepository) GetEpisodeFilePath(ctx context.Context, episodeID string) (*string, error) {
	return r.getTextByID(ctx, episodeID, r.queries.GetEpisodeFilePath)
}

func (r *ScanIngestRepository) UpdateItemTMDBAndIMDB(ctx context.Context, itemID string, tmdbID int64, imdbID string) error {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return err
	}
	return r.queries.UpdateItemTMDBAndIMDB(ctx, dbgen.UpdateItemTMDBAndIMDBParams{
		TmdbID:  pgtype.Int4{Int32: int32(tmdbID), Valid: true},
		Column2: imdbID,
		Column3: toPGUUID(uid),
	})
}

func (r *ScanIngestRepository) UpdateItemTMDBID(ctx context.Context, itemID string, tmdbID int64) error {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return err
	}
	return r.queries.UpdateItemTMDBID(ctx, dbgen.UpdateItemTMDBIDParams{
		TmdbID:  pgtype.Int4{Int32: int32(tmdbID), Valid: true},
		Column2: toPGUUID(uid),
	})
}

func (r *ScanIngestRepository) UpdateItemPrimaryImage(ctx context.Context, itemID, imagePath string, imageTag *string) error {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return err
	}
	return r.queries.UpdateItemPrimaryImage(ctx, dbgen.UpdateItemPrimaryImageParams{
		PrimaryImagePath: textValue(imagePath),
		PrimaryImageTag:  optionalText(imageTag),
		Column3:          toPGUUID(uid),
	})
}

func (r *ScanIngestRepository) UpdateItemBackdropImage(ctx context.Context, itemID, imagePath string, imageTag *string) error {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return err
	}
	return r.queries.UpdateItemBackdropImage(ctx, dbgen.UpdateItemBackdropImageParams{
		BackdropImagePath: textValue(imagePath),
		BackdropImageTag:  optionalText(imageTag),
		Column3:           toPGUUID(uid),
	})
}

func (r *ScanIngestRepository) ListSeasonIDsAndNumbers(ctx context.Context, seriesID string) ([]SeasonNumberRow, error) {
	uid, err := uuid.Parse(seriesID)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.ListSeasonIDsAndNumbers(ctx, toPGUUID(uid))
	if err != nil {
		return nil, err
	}
	out := make([]SeasonNumberRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, SeasonNumberRow{ID: fromPGUUID(row.ID), IndexNumber: ptrInt32FromPG(row.IndexNumber)})
	}
	return out, nil
}

func (r *ScanIngestRepository) GetItemPrimaryImageTag(ctx context.Context, itemID string) (*string, error) {
	return r.getTextByID(ctx, itemID, r.queries.GetItemPrimaryImageTag)
}

func (r *ScanIngestRepository) getTextByID(ctx context.Context, id string, fn func(context.Context, pgtype.UUID) (pgtype.Text, error)) (*string, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	v, err := fn(ctx, toPGUUID(uid))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ptrTextFromPG(v), nil
}

func optionalText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return textValue(*s)
}
