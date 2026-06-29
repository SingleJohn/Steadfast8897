package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	dbgen "fyms/internal/db/gen"
	"fyms/internal/dto"
)

type ItemHelperRepository struct {
	pool    *pgxpool.Pool
	queries *dbgen.Queries
}

type IDNameCount struct {
	ID    string
	Name  string
	Count int64
}

type ActorImageStats struct {
	Total     int64
	WithImage int64
	Locked    int64
}

type SeasonIndexRow struct {
	ID          string
	IndexNumber *int32
}

type EpisodeMetadataRow struct {
	ID               string
	IndexNumber      *int32
	Name             *string
	Overview         *string
	PrimaryImagePath *string
}

func NewItemHelperRepository(pool *pgxpool.Pool) *ItemHelperRepository {
	return &ItemHelperRepository{pool: pool, queries: dbgen.New(pool)}
}

func (r *ItemHelperRepository) ListItemGenres(ctx context.Context, itemID string) ([][2]string, error) {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.ListItemGenres(ctx, toPGUUID(uid))
	if err != nil {
		return nil, err
	}
	out := make([][2]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, [2]string{row.GID, row.Name})
	}
	return out, nil
}

func (r *ItemHelperRepository) ListItemTags(ctx context.Context, itemID string) ([]string, error) {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return nil, err
	}
	return r.queries.ListItemTags(ctx, toPGUUID(uid))
}

func (r *ItemHelperRepository) ListAllTagsWithCounts(ctx context.Context) ([]IDNameCount, error) {
	rows, err := r.queries.ListAllTagsWithCounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]IDNameCount, 0, len(rows))
	for _, row := range rows {
		out = append(out, IDNameCount{ID: fmt.Sprint(row.ID), Name: row.Name, Count: row.ItemCount})
	}
	return out, nil
}

func (r *ItemHelperRepository) ListItemExtraBackdropTags(ctx context.Context, itemID string) ([]string, error) {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return nil, err
	}
	return r.queries.ListItemExtraBackdropTags(ctx, toPGUUID(uid))
}

func (r *ItemHelperRepository) ListItemCast(ctx context.Context, itemID string) ([]map[string]interface{}, error) {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.ListItemCast(ctx, toPGUUID(uid))
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		personID := stringFromAny(row.PersonID)
		image := stringPtrFromAny(row.Image)
		imageTag := stringFromAny(row.ImageTag)
		val := map[string]interface{}{
			"Name": row.Name,
			"Role": textOrEmpty(row.Character),
			"Type": row.Role,
			"Id":   personID,
		}
		if image != nil && *image != "" {
			val["PrimaryImageTag"] = imageTag
			val["HasPrimaryImage"] = true
			if strings.HasPrefix(*image, "http://") || strings.HasPrefix(*image, "https://") {
				val["ImageUrl"] = *image
			}
		}
		val["OrderIndex"] = row.OrderIndex
		out = append(out, val)
	}
	return out, nil
}

func (r *ItemHelperRepository) ListAllGenresWithCounts(ctx context.Context) ([]IDNameCount, error) {
	rows, err := r.queries.ListAllGenresWithCounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]IDNameCount, 0, len(rows))
	for _, row := range rows {
		out = append(out, IDNameCount{ID: row.GID, Name: row.Name, Count: row.ItemCount})
	}
	return out, nil
}

func (r *ItemHelperRepository) CountMergedVersionPrimaries(ctx context.Context) (int64, error) {
	return r.queries.CountMergedVersionPrimaries(ctx)
}

func (r *ItemHelperRepository) CountMergedVersionSecondaries(ctx context.Context) (int64, error) {
	return r.queries.CountMergedVersionSecondaries(ctx)
}

func (r *ItemHelperRepository) GetPrimaryMediaVersionInfo(ctx context.Context, itemID string) (*PrimaryMediaVersionInfo, error) {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return nil, err
	}
	row, err := r.queries.GetPrimaryMediaVersionInfo(ctx, toPGUUID(uid))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &PrimaryMediaVersionInfo{
		Container: textOrEmpty(row.Container),
		Bitrate:   ptrInt32FromPG(row.Bitrate),
	}, nil
}

func (r *ItemHelperRepository) ListExternalSubtitlesForMediaVersion(ctx context.Context, mediaVersionID string) ([]dto.ExternalSubtitleRow, error) {
	uid, err := uuid.Parse(mediaVersionID)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.ListExternalSubtitlesForMediaVersion(ctx, toPGUUID(uid))
	if err != nil {
		return nil, err
	}
	out := make([]dto.ExternalSubtitleRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, dto.ExternalSubtitleRow{
			ID:             row.ID,
			ItemID:         row.ItemID,
			MediaVersionID: row.MediaVersionID,
			FilePath:       row.FilePath,
			Codec:          row.Codec,
			Language:       ptrTextFromPG(row.Language),
			Title:          ptrTextFromPG(row.Title),
			IsDefault:      row.IsDefault,
			IsForced:       row.IsForced,
		})
	}
	return out, nil
}

func (r *ItemHelperRepository) GetUserItemData(ctx context.Context, userID, itemID string) (*dto.UserDataRow, error) {
	uid, iid, err := parseTwoUUIDs(userID, itemID)
	if err != nil {
		return nil, err
	}
	row, err := r.queries.GetUserItemData(ctx, dbgen.GetUserItemDataParams{
		Column1: toPGUUID(uid),
		Column2: toPGUUID(iid),
	})
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := &dto.UserDataRow{
		PlaybackPositionTicks: &row.PlaybackPositionTicks,
		PlayCount:             &row.PlayCount,
		IsFavorite:            &row.IsFavorite,
		Played:                &row.Played,
		LastPlayedDate:        ptrTime(row.LastPlayedDate),
	}
	if versionData, err := NewMediaVersionUserDataRepository(r.pool).GetLatestForItem(ctx, userID, itemID); err == nil && versionData != nil {
		pos := versionData.PlaybackPositionTicks
		played := versionData.Played
		out.PlaybackPositionTicks = &pos
		out.Played = &played
		out.LastPlayedDate = versionData.LastPlayedDate
	}
	return out, nil
}

func (r *ItemHelperRepository) UpsertUserItemData(ctx context.Context, userID, itemID string, position *int64, playCount *int32, isFavorite *bool, played *bool) error {
	uid, iid, err := parseTwoUUIDs(userID, itemID)
	if err != nil {
		return err
	}
	return r.queries.UpsertUserItemData(ctx, dbgen.UpsertUserItemDataParams{
		Column1:    toPGUUID(uid),
		Column2:    toPGUUID(iid),
		Position:   optionalInt64(position),
		PlayCount:  optionalInt32(playCount),
		IsFavorite: optionalBool(isFavorite),
		Played:     optionalBool(played),
	})
}

func (r *ItemHelperRepository) GetUserPersonData(ctx context.Context, userID, personID string) (*dto.UserDataRow, error) {
	uid, pid, err := parseTwoUUIDs(userID, personID)
	if err != nil {
		return nil, err
	}
	isFavorite, err := r.queries.GetUserPersonData(ctx, dbgen.GetUserPersonDataParams{
		Column1: toPGUUID(uid),
		Column2: toPGUUID(pid),
	})
	if err == pgx.ErrNoRows {
		return &dto.UserDataRow{}, nil
	}
	if err != nil {
		return nil, err
	}
	return personUserDataRow(isFavorite), nil
}

func (r *ItemHelperRepository) GetUserPersonFavoriteMap(ctx context.Context, userID string, personIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(personIDs))
	if userID == "" || len(personIDs) == 0 {
		return out, nil
	}
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, err
	}
	pgPersonIDs := make([]pgtype.UUID, 0, len(personIDs))
	for _, id := range personIDs {
		pid, err := uuid.Parse(id)
		if err != nil {
			return nil, err
		}
		pgPersonIDs = append(pgPersonIDs, toPGUUID(pid))
	}
	rows, err := r.queries.ListUserPersonFavorites(ctx, dbgen.ListUserPersonFavoritesParams{
		Column1: toPGUUID(uid),
		Column2: pgPersonIDs,
	})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.PersonID] = row.IsFavorite
	}
	return out, nil
}

func (r *ItemHelperRepository) UpsertUserPersonFavorite(ctx context.Context, userID, personID string, favorite bool) error {
	uid, pid, err := parseTwoUUIDs(userID, personID)
	if err != nil {
		return err
	}
	return r.queries.UpsertUserPersonFavorite(ctx, dbgen.UpsertUserPersonFavoriteParams{
		Column1:    toPGUUID(uid),
		Column2:    toPGUUID(pid),
		IsFavorite: favorite,
	})
}

func (r *ItemHelperRepository) SetHiddenFromResume(ctx context.Context, userID, itemID string, hidden bool) error {
	uid, iid, err := parseTwoUUIDs(userID, itemID)
	if err != nil {
		return err
	}
	return r.queries.SetHiddenFromResume(ctx, dbgen.SetHiddenFromResumeParams{
		Column1:            toPGUUID(uid),
		Column2:            toPGUUID(iid),
		IsHiddenFromResume: hidden,
	})
}

func (r *ItemHelperRepository) GetChildCount(ctx context.Context, parentID string) (int64, error) {
	return r.countByID(ctx, parentID, r.queries.GetChildCount)
}

func (r *ItemHelperRepository) GetRecursiveItemCount(ctx context.Context, parentID string) (int64, error) {
	return r.countByID(ctx, parentID, r.queries.GetRecursiveItemCount)
}

func (r *ItemHelperRepository) GetCollectionTypeByLibraryID(ctx context.Context, libraryID string) (string, error) {
	uid, err := uuid.Parse(libraryID)
	if err != nil {
		return "", err
	}
	return r.queries.GetCollectionTypeByLibraryID(ctx, toPGUUID(uid))
}

func (r *ItemHelperRepository) ResolveItemUUIDByEmbyID(ctx context.Context, embyID int32) (*string, error) {
	id, err := r.queries.ResolveItemUUIDByEmbyID(ctx, pgtype.Int4{Int32: embyID, Valid: true})
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (r *ItemHelperRepository) ItemExists(ctx context.Context, itemID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM items WHERE id = $1::uuid)`, itemID).Scan(&exists)
	return exists, err
}

func (r *ItemHelperRepository) GetItemEmbyID(ctx context.Context, itemID string) (*int32, error) {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return nil, err
	}
	v, err := r.queries.GetItemEmbyID(ctx, toPGUUID(uid))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ptrInt32FromPG(v), nil
}

func (r *ItemHelperRepository) GetPersonImagePath(ctx context.Context, personID string) (string, bool, error) {
	img, found, err := r.getPersonText(ctx, personID, r.queries.GetPersonImagePath)
	return img, found, err
}

func (r *ItemHelperRepository) SetPersonImage(ctx context.Context, personID, imagePath string, locked bool) error {
	uid, err := uuid.Parse(personID)
	if err != nil {
		return err
	}
	return r.queries.SetPersonImage(ctx, dbgen.SetPersonImageParams{
		ImagePath:   textValue(imagePath),
		ImageLocked: locked,
		Column3:     toPGUUID(uid),
	})
}

func (r *ItemHelperRepository) ClearPersonImage(ctx context.Context, personID string) error {
	return r.execByID(ctx, personID, r.queries.ClearPersonImage)
}

func (r *ItemHelperRepository) FillPersonImageIfUnlocked(ctx context.Context, personID, imagePath string) (bool, error) {
	uid, err := uuid.Parse(personID)
	if err != nil {
		return false, err
	}
	affected, err := r.queries.FillPersonImageIfUnlocked(ctx, dbgen.FillPersonImageIfUnlockedParams{
		ImagePath: textValue(imagePath),
		Column2:   toPGUUID(uid),
	})
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (r *ItemHelperRepository) ListItemsForActorImageBackfill(ctx context.Context) ([]string, error) {
	return r.queries.ListItemsForActorImageBackfill(ctx)
}

func (r *ItemHelperRepository) GetActorImageStats(ctx context.Context) (ActorImageStats, error) {
	row, err := r.queries.GetActorImageStats(ctx)
	return ActorImageStats{Total: row.Total, WithImage: row.WithImage, Locked: row.Locked}, err
}

func (r *ItemHelperRepository) CountItemsByType(ctx context.Context, itemType string) (int64, error) {
	return r.queries.CountItemsByType(ctx, itemType)
}

func (r *ItemHelperRepository) ListSeasonsForSeries(ctx context.Context, seriesID string) ([]SeasonIndexRow, error) {
	uid, err := uuid.Parse(seriesID)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.ListSeasonsForSeries(ctx, toPGUUID(uid))
	if err != nil {
		return nil, err
	}
	out := make([]SeasonIndexRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, SeasonIndexRow{ID: row.ID, IndexNumber: ptrInt32FromPG(row.IndexNumber)})
	}
	return out, nil
}

func (r *ItemHelperRepository) ListEpisodeIndexesForSeason(ctx context.Context, seasonID string) ([]int32, error) {
	uid, err := uuid.Parse(seasonID)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.ListEpisodeIndexesForSeason(ctx, toPGUUID(uid))
	if err != nil {
		return nil, err
	}
	out := make([]int32, 0, len(rows))
	for _, v := range rows {
		if v.Valid && v.Int32 > 0 {
			out = append(out, v.Int32)
		}
	}
	return out, nil
}

func (r *ItemHelperRepository) ListSeasonNames(ctx context.Context, seasonIDs []string) (map[string]string, error) {
	names := make(map[string]string, len(seasonIDs))
	if len(seasonIDs) == 0 {
		return names, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id::text, name
		   FROM items
		  WHERE id::text = ANY($1::text[])
		    AND type = 'Season'`,
		seasonIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	return names, rows.Err()
}

func (r *ItemHelperRepository) CountItemsByTypeAndTmdbProvider(ctx context.Context, itemType, tmdbID string) (int64, error) {
	return r.queries.CountItemsByTypeAndTmdbProvider(ctx, dbgen.CountItemsByTypeAndTmdbProviderParams{
		ItemType: itemType,
		TmdbID:   tmdbID,
	})
}

func (r *ItemHelperRepository) CountItemsByTypeNameYear(ctx context.Context, itemType, name string, year int) (int64, error) {
	return r.queries.CountItemsByTypeNameYear(ctx, dbgen.CountItemsByTypeNameYearParams{
		ItemType: itemType,
		Name:     name,
		Year:     int32(year),
	})
}

func (r *ItemHelperRepository) CountItemsByTypeName(ctx context.Context, itemType, name string) (int64, error) {
	return r.queries.CountItemsByTypeName(ctx, dbgen.CountItemsByTypeNameParams{
		ItemType: itemType,
		Name:     name,
	})
}

func (r *ItemHelperRepository) FindSeriesIDByTmdbProvider(ctx context.Context, tmdbID string) (*string, error) {
	return itemHelperStringPtrOrNil(r.queries.FindSeriesIDByTmdbProvider(ctx, tmdbID))
}

func (r *ItemHelperRepository) FindSeriesIDByNameYear(ctx context.Context, name string, year int) (*string, error) {
	return itemHelperStringPtrOrNil(r.queries.FindSeriesIDByNameYear(ctx, dbgen.FindSeriesIDByNameYearParams{
		Name: name,
		Year: int32(year),
	}))
}

func (r *ItemHelperRepository) FindSeriesIDByName(ctx context.Context, name string) (*string, error) {
	return itemHelperStringPtrOrNil(r.queries.FindSeriesIDByName(ctx, name))
}

func (r *ItemHelperRepository) ListSeasonRowsForEpisodeMetadata(ctx context.Context, seriesID string) ([]SeasonIndexRow, error) {
	uid, err := uuid.Parse(seriesID)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.ListSeasonRowsForEpisodeMetadata(ctx, toPGUUID(uid))
	if err != nil {
		return nil, err
	}
	out := make([]SeasonIndexRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, SeasonIndexRow{ID: row.ID, IndexNumber: ptrInt32FromPG(row.IndexNumber)})
	}
	return out, nil
}

func (r *ItemHelperRepository) ListEpisodeRowsForMetadataUpdate(ctx context.Context, seasonID string) ([]EpisodeMetadataRow, error) {
	uid, err := uuid.Parse(seasonID)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.ListEpisodeRowsForMetadataUpdate(ctx, toPGUUID(uid))
	if err != nil {
		return nil, err
	}
	out := make([]EpisodeMetadataRow, 0, len(rows))
	for _, row := range rows {
		name := row.Name
		out = append(out, EpisodeMetadataRow{
			ID:               row.ID,
			IndexNumber:      ptrInt32FromPG(row.IndexNumber),
			Name:             &name,
			Overview:         ptrTextFromPG(row.Overview),
			PrimaryImagePath: ptrTextFromPG(row.PrimaryImagePath),
		})
	}
	return out, nil
}

func (r *ItemHelperRepository) BatchUpdateEpisodeMetadata(ctx context.Context, ids []string, names, overviews []*string) error {
	pgIDs, err := parseUUIDSlice(ids)
	if err != nil {
		return err
	}
	return r.queries.BatchUpdateEpisodeMetadata(ctx, dbgen.BatchUpdateEpisodeMetadataParams{
		Ids:       pgIDs,
		Names:     stringPointerSliceToNullableStrings(names),
		Overviews: stringPointerSliceToNullableStrings(overviews),
	})
}

func (r *ItemHelperRepository) UpdateEpisodeStillImage(ctx context.Context, episodeID, imagePath string, imageTag *string) error {
	uid, err := uuid.Parse(episodeID)
	if err != nil {
		return err
	}
	return r.queries.UpdateEpisodeStillImage(ctx, dbgen.UpdateEpisodeStillImageParams{
		PrimaryImagePath: textValue(imagePath),
		PrimaryImageTag:  optionalText(imageTag),
		Column3:          toPGUUID(uid),
	})
}

func (r *ItemHelperRepository) UpdateItemPrimaryImage(ctx context.Context, itemID, imagePath, imageTag string) error {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return err
	}
	return r.queries.UpdateItemPrimaryImage(ctx, dbgen.UpdateItemPrimaryImageParams{
		PrimaryImagePath: textValue(imagePath),
		PrimaryImageTag:  textValue(imageTag),
		Column3:          toPGUUID(uid),
	})
}

func (r *ItemHelperRepository) UpdateItemBackdropImage(ctx context.Context, itemID, imagePath, imageTag string) error {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return err
	}
	return r.queries.UpdateItemBackdropImage(ctx, dbgen.UpdateItemBackdropImageParams{
		BackdropImagePath: textValue(imagePath),
		BackdropImageTag:  textValue(imageTag),
		Column3:           toPGUUID(uid),
	})
}

func (r *ItemHelperRepository) ClearItemPrimaryImage(ctx context.Context, itemID string) error {
	_, err := r.pool.Exec(ctx,
		"UPDATE items SET primary_image_path = NULL, primary_image_tag = NULL, updated_at = NOW() WHERE id = $1::uuid",
		itemID)
	return err
}

func (r *ItemHelperRepository) ClearItemBackdropImage(ctx context.Context, itemID string) error {
	_, err := r.pool.Exec(ctx,
		"UPDATE items SET backdrop_image_path = NULL, backdrop_image_tag = NULL, updated_at = NOW() WHERE id = $1::uuid",
		itemID)
	return err
}

func (r *ItemHelperRepository) PersonExists(ctx context.Context, id string) (bool, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return false, err
	}
	return r.queries.PersonExists(ctx, toPGUUID(uid))
}

func (r *ItemHelperRepository) GetPersonBackdropPath(ctx context.Context, personID string) (string, bool, error) {
	img, found, err := r.getPersonText(ctx, personID, func(ctx context.Context, id pgtype.UUID) (interface{}, error) {
		return r.queries.GetPersonBackdropPath(ctx, id)
	})
	return img, found, err
}

func (r *ItemHelperRepository) SetPersonBackdrop(ctx context.Context, personID, path string) error {
	uid, err := uuid.Parse(personID)
	if err != nil {
		return err
	}
	return r.queries.SetPersonBackdrop(ctx, dbgen.SetPersonBackdropParams{
		BackdropPath: textValue(path),
		Column2:      toPGUUID(uid),
	})
}

func (r *ItemHelperRepository) ClearPersonBackdrop(ctx context.Context, personID string) error {
	return r.execByID(ctx, personID, r.queries.ClearPersonBackdrop)
}

func (r *ItemHelperRepository) countByID(ctx context.Context, id string, fn func(context.Context, pgtype.UUID) (int64, error)) (int64, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return 0, err
	}
	return fn(ctx, toPGUUID(uid))
}

func (r *ItemHelperRepository) execByID(ctx context.Context, id string, fn func(context.Context, pgtype.UUID) error) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return err
	}
	return fn(ctx, toPGUUID(uid))
}

func (r *ItemHelperRepository) getPersonText(ctx context.Context, id string, fn func(context.Context, pgtype.UUID) (interface{}, error)) (string, bool, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return "", false, err
	}
	raw, err := fn(ctx, toPGUUID(uid))
	if err == pgx.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	s := stringFromAny(raw)
	if s == "" {
		return "", false, nil
	}
	return s, true, nil
}

func parseTwoUUIDs(a, b string) (uuid.UUID, uuid.UUID, error) {
	ua, err := uuid.Parse(a)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	ub, err := uuid.Parse(b)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return ua, ub, nil
}

func parseUUIDSlice(ids []string) ([]pgtype.UUID, error) {
	out := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		uid, err := uuid.Parse(id)
		if err != nil {
			return nil, err
		}
		out = append(out, toPGUUID(uid))
	}
	return out, nil
}

func stringPointerSliceToNullableStrings(values []*string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == nil {
			out = append(out, "")
			continue
		}
		out = append(out, *v)
	}
	return out
}

func itemHelperStringPtrOrNil(value string, err error) (*string, error) {
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func optionalInt64(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

func optionalInt32(v *int32) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *v, Valid: true}
}

func optionalBool(v *bool) pgtype.Bool {
	if v == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *v, Valid: true}
}

func personUserDataRow(isFavorite bool) *dto.UserDataRow {
	pos := int64(0)
	playCount := int32(0)
	played := false
	return &dto.UserDataRow{
		PlaybackPositionTicks: &pos,
		PlayCount:             &playCount,
		IsFavorite:            &isFavorite,
		Played:                &played,
	}
}

func stringPtrFromAny(v interface{}) *string {
	s := stringFromAny(v)
	if s == "" {
		return nil
	}
	return &s
}

func stringFromAny(v interface{}) string {
	switch raw := v.(type) {
	case nil:
		return ""
	case string:
		return raw
	case []byte:
		return string(raw)
	case pgtype.Text:
		return textOrEmpty(raw)
	default:
		return fmt.Sprint(raw)
	}
}
