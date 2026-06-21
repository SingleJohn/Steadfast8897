package repository

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func scanSourceConfigImport(row pgx.Row) (*SourceConfigImport, error) {
	var out SourceConfigImport
	var importedBy *string
	if err := row.Scan(&out.ID, &out.SourceType, &out.Name, &out.SourceURL, &out.BaseURL, &out.ContentSHA256,
		&out.SpiderRef, &out.SpiderMD5, &out.RawConfig, &out.ImportStatus, &out.Enabled, &importedBy,
		&out.ImportedAt, &out.UpdatedAt); err != nil {
		return nilIfNoRows[SourceConfigImport](err)
	}
	out.ImportedBy = importedBy
	return &out, nil
}

func scanSourceProvider(row pgx.Row) (*SourceProvider, error) {
	var out SourceProvider
	if err := row.Scan(&out.ID, &out.ConfigID, &out.SourceKey, &out.Name, &out.ProviderKind,
		&out.RuntimeKind, &out.TVBoxType, &out.API, &out.Ext, &out.Categories, &out.Headers,
		&out.Capabilities, &out.TimeoutMS, &out.Enabled, &out.Visible, &out.Searchable,
		&out.HealthStatus, &out.LastCheckAt, &out.LastError, &out.RawSite, &out.CreatedAt,
		&out.UpdatedAt); err != nil {
		return nilIfNoRows[SourceProvider](err)
	}
	return &out, nil
}

func scanSourceItem(row pgx.Row) (*SourceItem, error) {
	var out SourceItem
	if err := row.Scan(&out.ID, &out.PublicUUID, &out.ProviderID, &out.SourceItemID,
		&out.SourceParentID, &out.ItemType, &out.Title, &out.OriginalTitle, &out.SortTitle,
		&out.Year, &out.Region, &out.Area, &out.Language, &out.CategoryName, &out.NormalizedKind,
		&out.SeasonNumber, &out.EpisodeNumber, &out.PosterURL, &out.BackdropURL, &out.Remarks,
		&out.Summary, &out.Directors, &out.Actors, &out.ProviderIDs, &out.Raw, &out.DetailLoaded,
		&out.LastSeenAt, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return nilIfNoRows[SourceItem](err)
	}
	return &out, nil
}

func scanSourcePlaySource(row pgx.Row) (*SourcePlaySource, error) {
	var out SourcePlaySource
	if err := row.Scan(&out.ID, &out.PublicUUID, &out.SourceItemID, &out.ProviderID,
		&out.LineName, &out.EpisodeTitle, &out.EpisodeKey, &out.EpisodeNumber, &out.RawURL,
		&out.ParseMode, &out.Flag, &out.Headers, &out.ResolverPayload, &out.SortOrder,
		&out.HealthStatus, &out.SuccessCount, &out.FailureCount, &out.AvgLatencyMS,
		&out.LastSuccessAt, &out.LastFailureAt, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return nilIfNoRows[SourcePlaySource](err)
	}
	return &out, nil
}

func scanSourceUserItemData(row pgx.Row) (*SourceUserItemData, error) {
	var out SourceUserItemData
	if err := row.Scan(&out.UserID, &out.SourceItemID, &out.PlaybackPositionTicks,
		&out.PlayCount, &out.IsFavorite, &out.Played, &out.LastPlayedDate, &out.UpdatedAt); err != nil {
		return nilIfNoRows[SourceUserItemData](err)
	}
	return &out, nil
}

func scanSourceLibraryView(row pgx.Row) (*SourceLibraryView, error) {
	var out SourceLibraryView
	if err := row.Scan(&out.ID, &out.PublicUUID, &out.Name, &out.DisplayName, &out.Dimension,
		&out.MatchValue, &out.MatchValues, &out.CollectionType, &out.ProviderIDs, &out.Filter,
		&out.Enabled, &out.ExposeToEmby, &out.SortOrder, &out.Config, &out.CoverImagePath,
		&out.CoverImageTag, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return nilIfNoRows[SourceLibraryView](err)
	}
	return &out, nil
}

func nilIfNoRows[T any](err error) (*T, error) {
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return nil, err
}

func jsonBytesOrObject(raw []byte) []byte {
	if len(raw) == 0 || !json.Valid(raw) {
		return []byte("{}")
	}
	return raw
}

func jsonBytesOrArray(raw []byte) []byte {
	if len(raw) == 0 || !json.Valid(raw) {
		return []byte("[]")
	}
	return raw
}

func jsonBytesOrNull(raw []byte) []byte {
	if len(raw) == 0 || !json.Valid(raw) {
		return []byte("null")
	}
	return raw
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func defaultString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func defaultInt32(v, fallback int32) int32 {
	if v == 0 {
		return fallback
	}
	return v
}

func nullableUUIDText(v *string) *pgtype.UUID {
	if v == nil || *v == "" {
		return nil
	}
	id, err := uuid.Parse(*v)
	if err != nil {
		return nil
	}
	pgID := toPGUUID(id)
	return &pgID
}
