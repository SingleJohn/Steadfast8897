package repository

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	dbgen "fyms/internal/db/gen"
	"fyms/internal/dto"
)

type PlaybackRepository struct {
	queries *dbgen.Queries
}

type MediaVersion struct {
	ID           string
	UUID         uuid.UUID
	Name         string
	FilePath     string
	Container    *string
	IsPrimary    bool
	RuntimeTicks *int64
	Bitrate      *int64
	Size         *int64
	MediaInfo    []byte
	Resolution   *string
	HDRFormat    *string
	VideoCodec   *string
	AudioCodec   *string
	Source       *string
	QualityLabel *string
	ChaptersJSON []byte
}

type MediaVersionUpsert struct {
	ItemID       string
	Name         string
	FilePath     string
	Container    string
	IsPrimary    bool
	MediaInfo    any
	RuntimeTicks *int64
	Bitrate      *int64
	Size         *int64
	Resolution   *string
	HDRFormat    *string
	VideoCodec   *string
	AudioCodec   *string
	Source       *string
	QualityLabel *string
}

type MergedSibling struct {
	ID      string
	LibName string
}

type ItemDetailExtras struct {
	OriginalTitle *string
	TrailerURL    *string
}

type MediaVersionItemInfo struct {
	ItemID    string
	MediaInfo []byte
}

func NewPlaybackRepository(pool *pgxpool.Pool) *PlaybackRepository {
	return &PlaybackRepository{queries: dbgen.New(pool)}
}

func (r *PlaybackRepository) ListMediaVersionsForItem(ctx context.Context, itemID string) ([]MediaVersion, error) {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.ListMediaVersionsForItem(ctx, toPGUUID(uid))
	if err != nil {
		return nil, err
	}
	out := make([]MediaVersion, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapMediaVersion(row))
	}
	return out, nil
}

func (r *PlaybackRepository) UpsertMediaVersion(ctx context.Context, in MediaVersionUpsert) (uuid.UUID, error) {
	itemID, err := uuid.Parse(in.ItemID)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := r.queries.UpsertMediaVersion(ctx, dbgen.UpsertMediaVersionParams{
		Column1:      toPGUUID(itemID),
		Name:         in.Name,
		FilePath:     in.FilePath,
		Container:    nullableText(in.Container),
		IsPrimary:    in.IsPrimary,
		Column6:      marshalJSONOrNil(in.MediaInfo),
		RuntimeTicks: optionalInt8(in.RuntimeTicks),
		Bitrate:      optionalInt4FromInt64(in.Bitrate),
		Size:         optionalInt8(in.Size),
		Resolution:   optionalText(in.Resolution),
		HdrFormat:    optionalText(in.HDRFormat),
		VideoCodec:   optionalText(in.VideoCodec),
		AudioCodec:   optionalText(in.AudioCodec),
		Source:       optionalText(in.Source),
		QualityLabel: optionalText(in.QualityLabel),
	})
	if err != nil {
		return uuid.Nil, err
	}
	return uuid.Parse(id)
}

func (r *PlaybackRepository) GetMergedPrimaryID(ctx context.Context, itemID string) (*string, error) {
	return r.getStringByID(ctx, itemID, r.queries.GetMergedPrimaryID)
}

func (r *PlaybackRepository) ListMergedSiblingItems(ctx context.Context, primaryID string) ([]MergedSibling, error) {
	uid, err := uuid.Parse(primaryID)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.ListMergedSiblingItems(ctx, toPGUUID(uid))
	if err != nil {
		return nil, err
	}
	out := make([]MergedSibling, 0, len(rows))
	for _, row := range rows {
		out = append(out, MergedSibling{ID: row.SID, LibName: row.LibName})
	}
	return out, nil
}

func (r *PlaybackRepository) GetMediaVersionFilePath(ctx context.Context, mediaVersionID string) (*string, error) {
	return r.getStringByID(ctx, mediaVersionID, r.queries.GetMediaVersionFilePath)
}

func (r *PlaybackRepository) GetPrimaryMediaVersionFilePath(ctx context.Context, itemID string) (*string, error) {
	return r.getStringByID(ctx, itemID, r.queries.GetPrimaryMediaVersionFilePath)
}

func (r *PlaybackRepository) GetLocalTrailerPath(ctx context.Context, itemID string) (*string, error) {
	return r.getTextByID(ctx, itemID, r.queries.GetLocalTrailerPath)
}

func (r *PlaybackRepository) GetMediaVersionItemAndInfo(ctx context.Context, mediaVersionID string) (*MediaVersionItemInfo, error) {
	uid, err := uuid.Parse(mediaVersionID)
	if err != nil {
		return nil, err
	}
	row, err := r.queries.GetMediaVersionItemAndInfo(ctx, toPGUUID(uid))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &MediaVersionItemInfo{ItemID: row.ItemID, MediaInfo: row.Mediainfo}, nil
}

func (r *PlaybackRepository) GetPrimaryMediaStreamsJSON(ctx context.Context, itemID string) ([]byte, error) {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return nil, err
	}
	raw, err := r.queries.GetPrimaryMediaStreamsJSON(ctx, toPGUUID(uid))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return jsonValueBytes(raw), nil
}

func (r *PlaybackRepository) GetItemDetailExtras(ctx context.Context, itemID string) (*ItemDetailExtras, error) {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return nil, err
	}
	row, err := r.queries.GetItemDetailExtras(ctx, toPGUUID(uid))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ItemDetailExtras{
		OriginalTitle: ptrTextFromPG(row.OriginalTitle),
		TrailerURL:    ptrTextFromPG(row.TrailerUrl),
	}, nil
}

func (r *PlaybackRepository) CountItemsByLibrary(ctx context.Context, libraryID uuid.UUID) (int64, error) {
	return r.queries.CountItemsByLibrary(ctx, toPGUUID(libraryID))
}

func (r *PlaybackRepository) GetItemLibraryID(ctx context.Context, itemID string) (*string, error) {
	return r.getStringByID(ctx, itemID, r.queries.GetItemLibraryID)
}

func (r *PlaybackRepository) ListSimilarItemIDsByLibrary(ctx context.Context, libraryID, excludedID string, limit int64) ([]string, error) {
	libID, itemID, err := parseTwoUUIDs(libraryID, excludedID)
	if err != nil {
		return nil, err
	}
	return r.queries.ListSimilarItemIDsByLibrary(ctx, dbgen.ListSimilarItemIDsByLibraryParams{
		Column1: toPGUUID(libID),
		Column2: toPGUUID(itemID),
		Column3: limit,
	})
}

func (r *PlaybackRepository) ListSeasonIDsForCompat(ctx context.Context, seriesID string) ([]string, error) {
	uid, err := uuid.Parse(seriesID)
	if err != nil {
		return nil, err
	}
	return r.queries.ListSeasonIDsForCompat(ctx, toPGUUID(uid))
}

func (r *PlaybackRepository) GetSeasonParentSeriesID(ctx context.Context, seasonID string) (*string, error) {
	return r.getStringByID(ctx, seasonID, r.queries.GetSeasonParentSeriesID)
}

func (r *PlaybackRepository) FindSeasonIDByNumber(ctx context.Context, seriesID string, seasonNumber int32) (*string, error) {
	uid, err := uuid.Parse(seriesID)
	if err != nil {
		return nil, err
	}
	return itemHelperStringPtrOrNil(r.queries.FindSeasonIDByNumber(ctx, dbgen.FindSeasonIDByNumberParams{
		Column1:     toPGUUID(uid),
		IndexNumber: pgtype.Int4{Int32: seasonNumber, Valid: true},
	}))
}

func (r *PlaybackRepository) CountEpisodesBySeason(ctx context.Context, seasonID string) (int64, error) {
	return r.countByID(ctx, seasonID, r.queries.CountEpisodesBySeason)
}

func (r *PlaybackRepository) ListEpisodeIDsBySeason(ctx context.Context, seasonID string, limit, offset int64) ([]string, error) {
	uid, err := uuid.Parse(seasonID)
	if err != nil {
		return nil, err
	}
	return r.queries.ListEpisodeIDsBySeason(ctx, dbgen.ListEpisodeIDsBySeasonParams{
		Column1: toPGUUID(uid),
		Limit:   normalizedLimit(limit),
		Offset:  int32(offset),
	})
}

func (r *PlaybackRepository) CountEpisodesBySeries(ctx context.Context, seriesID string) (int64, error) {
	return r.countByID(ctx, seriesID, r.queries.CountEpisodesBySeries)
}

func (r *PlaybackRepository) ListEpisodeIDsBySeries(ctx context.Context, seriesID string, limit, offset int64) ([]string, error) {
	uid, err := uuid.Parse(seriesID)
	if err != nil {
		return nil, err
	}
	return r.queries.ListEpisodeIDsBySeries(ctx, dbgen.ListEpisodeIDsBySeriesParams{
		Column1: toPGUUID(uid),
		Limit:   normalizedLimit(limit),
		Offset:  int32(offset),
	})
}

func (r *PlaybackRepository) ListMediaStreamsForItem(ctx context.Context, itemID string) ([]dto.StreamRow, error) {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.ListMediaStreamsForItem(ctx, toPGUUID(uid))
	if err != nil {
		return nil, err
	}
	out := make([]dto.StreamRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, dto.StreamRow{
			Codec:        ptrTextFromPG(row.Codec),
			StreamType:   row.Type,
			StreamIndex:  row.StreamIndex,
			Language:     ptrTextFromPG(row.Language),
			Title:        ptrTextFromPG(row.Title),
			IsDefault:    ptrBool(row.IsDefault),
			IsForced:     ptrBool(row.IsForced),
			Width:        ptrInt32FromPG(row.Width),
			Height:       ptrInt32FromPG(row.Height),
			BitRate:      ptrInt64FromInt4(row.BitRate),
			Channels:     ptrInt32FromPG(row.Channels),
			SampleRate:   ptrInt32FromPG(row.SampleRate),
			BitDepth:     ptrInt32FromPG(row.BitDepth),
			PixelFormat:  ptrTextFromPG(row.PixelFormat),
			DisplayTitle: ptrTextFromPG(row.DisplayTitle),
		})
	}
	return out, nil
}

func (r *PlaybackRepository) getTextByID(ctx context.Context, id string, fn func(context.Context, pgtype.UUID) (pgtype.Text, error)) (*string, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	value, err := fn(ctx, toPGUUID(uid))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ptrTextFromPG(value), nil
}

func (r *PlaybackRepository) getStringByID(ctx context.Context, id string, fn func(context.Context, pgtype.UUID) (string, error)) (*string, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	value, err := fn(ctx, toPGUUID(uid))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if value == "" {
		return nil, nil
	}
	return &value, nil
}

func (r *PlaybackRepository) countByID(ctx context.Context, id string, fn func(context.Context, pgtype.UUID) (int64, error)) (int64, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return 0, err
	}
	return fn(ctx, toPGUUID(uid))
}

func mapMediaVersion(row dbgen.ListMediaVersionsForItemRow) MediaVersion {
	id, _ := uuid.Parse(row.ID)
	return MediaVersion{
		ID:           row.ID,
		UUID:         id,
		Name:         row.Name,
		FilePath:     row.FilePath,
		Container:    ptrTextFromPG(row.Container),
		IsPrimary:    row.IsPrimary,
		RuntimeTicks: ptrInt64FromPG(row.RuntimeTicks),
		Bitrate:      ptrInt64FromInt4(row.Bitrate),
		Size:         ptrInt64FromPG(row.Size),
		MediaInfo:    row.Mediainfo,
		Resolution:   ptrTextFromPG(row.Resolution),
		HDRFormat:    ptrTextFromPG(row.HdrFormat),
		VideoCodec:   ptrTextFromPG(row.VideoCodec),
		AudioCodec:   ptrTextFromPG(row.AudioCodec),
		Source:       ptrTextFromPG(row.Source),
		QualityLabel: ptrTextFromPG(row.QualityLabel),
		ChaptersJSON: row.Chapters,
	}
}

func marshalJSONOrNil(v any) []byte {
	switch raw := v.(type) {
	case nil:
		return nil
	case []byte:
		return raw
	case string:
		if raw == "" {
			return nil
		}
		return []byte(raw)
	default:
		b, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		return b
	}
}

func optionalInt8(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

func optionalInt4FromInt64(v *int64) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(*v), Valid: true}
}

func ptrInt64FromPG(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	i := v.Int64
	return &i
}

func ptrInt64FromInt4(v pgtype.Int4) *int64 {
	if !v.Valid {
		return nil
	}
	i := int64(v.Int32)
	return &i
}

func ptrBool(v bool) *bool {
	return &v
}

func normalizedLimit(limit int64) int32 {
	if limit > 0 {
		if limit > 2147483647 {
			return 2147483647
		}
		return int32(limit)
	}
	return 2147483647
}

func jsonValueBytes(v any) []byte {
	switch raw := v.(type) {
	case nil:
		return nil
	case []byte:
		return raw
	case string:
		return []byte(raw)
	default:
		b, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		return b
	}
}
