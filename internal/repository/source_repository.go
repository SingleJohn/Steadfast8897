package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SourceRepository struct {
	pool *pgxpool.Pool
}

func NewSourceRepository(pool *pgxpool.Pool) *SourceRepository {
	return &SourceRepository{pool: pool}
}

type SourceConfigImport struct {
	ID            int64
	SourceType    string
	Name          string
	SourceURL     *string
	BaseURL       *string
	ContentSHA256 string
	SpiderRef     *string
	SpiderMD5     *string
	RawConfig     []byte
	ImportStatus  string
	Enabled       bool
	ImportedBy    *string
	ImportedAt    time.Time
	UpdatedAt     time.Time
}

type SourceProvider struct {
	ID           int64
	ConfigID     *int64
	SourceKey    string
	Name         string
	ProviderKind string
	RuntimeKind  string
	TVBoxType    *int32
	API          string
	Ext          []byte
	Categories   []byte
	Headers      []byte
	Capabilities []byte
	TimeoutMS    int32
	Enabled      bool
	Visible      bool
	Searchable   bool
	HealthStatus string
	LastCheckAt  *time.Time
	LastError    *string
	RawSite      []byte
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type SourceItem struct {
	ID             int64
	PublicUUID     string
	ProviderID     int64
	SourceItemID   string
	SourceParentID *string
	ItemType       string
	Title          string
	OriginalTitle  *string
	SortTitle      *string
	Year           *int32
	Region         *string
	Area           *string
	Language       *string
	CategoryName   *string
	NormalizedKind string
	SeasonNumber   *int32
	EpisodeNumber  *int32
	PosterURL      *string
	BackdropURL    *string
	Remarks        *string
	Summary        *string
	Directors      []string
	Actors         []string
	ProviderIDs    []byte
	Raw            []byte
	DetailLoaded   bool
	LastSeenAt     time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type SourcePlaySource struct {
	ID              int64
	PublicUUID      string
	SourceItemID    int64
	ProviderID      int64
	LineName        string
	EpisodeTitle    string
	EpisodeKey      string
	EpisodeNumber   *int32
	RawURL          string
	ParseMode       string
	Flag            *string
	Headers         []byte
	ResolverPayload []byte
	SortOrder       int32
	HealthStatus    string
	SuccessCount    int32
	FailureCount    int32
	AvgLatencyMS    *int32
	LastSuccessAt   *time.Time
	LastFailureAt   *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type SourceUserItemData struct {
	UserID                string
	SourceItemID          int64
	PlaybackPositionTicks int64
	PlayCount             int32
	IsFavorite            bool
	Played                bool
	LastPlayedDate        *time.Time
	UpdatedAt             time.Time
}

type SourceLibraryView struct {
	ID             int64
	PublicUUID     string
	Name           string
	DisplayName    *string
	Dimension      string
	MatchValue     string
	MatchValues    []string
	CollectionType string
	ProviderIDs    []int64
	Filter         []byte
	Enabled        bool
	ExposeToEmby   bool
	SortOrder      int32
	Config         []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type SourceConfigImportUpsert struct {
	SourceType    string
	Name          string
	SourceURL     *string
	BaseURL       *string
	ContentSHA256 string
	SpiderRef     *string
	SpiderMD5     *string
	RawConfig     []byte
	ImportStatus  string
	Enabled       bool
	ImportedBy    *string
}

type SourceProviderUpsert struct {
	ConfigID     *int64
	SourceKey    string
	Name         string
	ProviderKind string
	RuntimeKind  string
	TVBoxType    *int32
	API          string
	Ext          []byte
	Categories   []byte
	Headers      []byte
	Capabilities []byte
	TimeoutMS    int32
	Enabled      bool
	Visible      bool
	Searchable   bool
	HealthStatus string
	LastError    *string
	RawSite      []byte
}

type SourceItemUpsert struct {
	PublicUUID     string
	ProviderID     int64
	SourceItemID   string
	SourceParentID *string
	ItemType       string
	Title          string
	OriginalTitle  *string
	SortTitle      *string
	Year           *int32
	Region         *string
	Area           *string
	Language       *string
	CategoryName   *string
	NormalizedKind string
	SeasonNumber   *int32
	EpisodeNumber  *int32
	PosterURL      *string
	BackdropURL    *string
	Remarks        *string
	Summary        *string
	Directors      []string
	Actors         []string
	ProviderIDs    []byte
	Raw            []byte
	DetailLoaded   bool
}

type SourcePlaySourceUpsert struct {
	PublicUUID      string
	SourceItemID    int64
	ProviderID      int64
	LineName        string
	EpisodeTitle    string
	EpisodeKey      string
	EpisodeNumber   *int32
	RawURL          string
	ParseMode       string
	Flag            *string
	Headers         []byte
	ResolverPayload []byte
	SortOrder       int32
}

type SourceUserItemDataUpsert struct {
	UserID                string
	SourceItemID          int64
	PlaybackPositionTicks *int64
	PlayCount             *int32
	IsFavorite            *bool
	Played                *bool
	LastPlayedDate        *time.Time
}

type SourceLibraryViewUpsert struct {
	PublicUUID     string
	Name           string
	DisplayName    *string
	Dimension      string
	MatchValue     string
	MatchValues    []string
	CollectionType string
	ProviderIDs    []int64
	Filter         []byte
	Enabled        bool
	ExposeToEmby   bool
	SortOrder      int32
	Config         []byte
}

func (r *SourceRepository) ResolveSourceItemPublicUUID(ctx context.Context, publicUUID string) (int64, bool, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM source_items WHERE public_uuid = $1::uuid`,
		publicUUID).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func (r *SourceRepository) ResolveSourceLibraryViewPublicUUID(ctx context.Context, publicUUID string) (int64, bool, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM source_library_views WHERE public_uuid = $1::uuid`,
		publicUUID).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func (r *SourceRepository) UpsertConfigImport(ctx context.Context, in SourceConfigImportUpsert) (*SourceConfigImport, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO source_config_imports (
			source_type, name, source_url, base_url, content_sha256, spider_ref, spider_md5,
			raw_config, import_status, enabled, imported_by
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11
		)
		ON CONFLICT (source_type, content_sha256) DO UPDATE SET
			name = EXCLUDED.name,
			source_url = EXCLUDED.source_url,
			base_url = EXCLUDED.base_url,
			spider_ref = EXCLUDED.spider_ref,
			spider_md5 = EXCLUDED.spider_md5,
			raw_config = EXCLUDED.raw_config,
			import_status = EXCLUDED.import_status,
			enabled = EXCLUDED.enabled,
			imported_by = EXCLUDED.imported_by,
			updated_at = NOW()
		RETURNING id, source_type, name, source_url, base_url, content_sha256, spider_ref, spider_md5,
		          raw_config, import_status, enabled, imported_by::text, imported_at, updated_at`,
		defaultString(in.SourceType, "tvbox"), in.Name, in.SourceURL, in.BaseURL, in.ContentSHA256,
		in.SpiderRef, in.SpiderMD5, jsonBytesOrObject(in.RawConfig), defaultString(in.ImportStatus, "active"),
		in.Enabled, nullableUUIDText(in.ImportedBy))
	return scanSourceConfigImport(row)
}

func (r *SourceRepository) UpsertProvider(ctx context.Context, in SourceProviderUpsert) (*SourceProvider, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO source_providers (
			config_id, source_key, name, provider_kind, runtime_kind, tvbox_type, api, ext,
			categories, headers, capabilities, timeout_ms, enabled, visible, searchable,
			health_status, last_error, raw_site
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10::jsonb, $11::jsonb,
			$12, $13, $14, $15, $16, $17, $18::jsonb
		)
		ON CONFLICT (config_id, source_key) DO UPDATE SET
			name = EXCLUDED.name,
			provider_kind = EXCLUDED.provider_kind,
			runtime_kind = EXCLUDED.runtime_kind,
			tvbox_type = EXCLUDED.tvbox_type,
			api = EXCLUDED.api,
			ext = EXCLUDED.ext,
			categories = EXCLUDED.categories,
			headers = EXCLUDED.headers,
			capabilities = EXCLUDED.capabilities,
			timeout_ms = EXCLUDED.timeout_ms,
			enabled = EXCLUDED.enabled,
			visible = EXCLUDED.visible,
			searchable = EXCLUDED.searchable,
			health_status = EXCLUDED.health_status,
			last_error = EXCLUDED.last_error,
			raw_site = EXCLUDED.raw_site,
			updated_at = NOW()
		RETURNING id, config_id, source_key, name, provider_kind, runtime_kind, tvbox_type, api,
		          ext, categories, headers, capabilities, timeout_ms, enabled, visible, searchable,
		          health_status, last_check_at, last_error, raw_site, created_at, updated_at`,
		in.ConfigID, in.SourceKey, in.Name, in.ProviderKind, in.RuntimeKind, in.TVBoxType, in.API,
		jsonBytesOrObject(in.Ext), jsonBytesOrArray(in.Categories), jsonBytesOrObject(in.Headers),
		jsonBytesOrObject(in.Capabilities), defaultInt32(in.TimeoutMS, 8000), in.Enabled, in.Visible,
		in.Searchable, defaultString(in.HealthStatus, "unknown"), in.LastError, jsonBytesOrObject(in.RawSite))
	return scanSourceProvider(row)
}

func (r *SourceRepository) UpsertSourceItem(ctx context.Context, in SourceItemUpsert) (*SourceItem, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO source_items (
			public_uuid, provider_id, source_item_id, source_parent_id, item_type, title,
			original_title, sort_title, year, region, area, language, category_name,
			normalized_kind, season_number, episode_number, poster_url, backdrop_url,
			remarks, summary, directors, actors, provider_ids, raw, detail_loaded
		) VALUES (
			$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18, $19, $20, $21, $22, $23::jsonb, $24::jsonb, $25
		)
		ON CONFLICT (public_uuid) DO UPDATE SET
			provider_id = EXCLUDED.provider_id,
			source_item_id = EXCLUDED.source_item_id,
			source_parent_id = EXCLUDED.source_parent_id,
			item_type = EXCLUDED.item_type,
			title = EXCLUDED.title,
			original_title = EXCLUDED.original_title,
			sort_title = EXCLUDED.sort_title,
			year = EXCLUDED.year,
			region = EXCLUDED.region,
			area = EXCLUDED.area,
			language = EXCLUDED.language,
			category_name = EXCLUDED.category_name,
			normalized_kind = EXCLUDED.normalized_kind,
			season_number = EXCLUDED.season_number,
			episode_number = EXCLUDED.episode_number,
			poster_url = EXCLUDED.poster_url,
			backdrop_url = EXCLUDED.backdrop_url,
			remarks = EXCLUDED.remarks,
			summary = EXCLUDED.summary,
			directors = EXCLUDED.directors,
			actors = EXCLUDED.actors,
			provider_ids = EXCLUDED.provider_ids,
			raw = EXCLUDED.raw,
			detail_loaded = EXCLUDED.detail_loaded,
			last_seen_at = NOW(),
			updated_at = NOW()
		RETURNING id, public_uuid::text, provider_id, source_item_id, source_parent_id, item_type, title,
		          original_title, sort_title, year, region, area, language, category_name, normalized_kind,
		          season_number, episode_number, poster_url, backdrop_url, remarks, summary, directors, actors,
		          provider_ids, raw, detail_loaded, last_seen_at, created_at, updated_at`,
		in.PublicUUID, in.ProviderID, in.SourceItemID, in.SourceParentID, defaultString(in.ItemType, "unknown"),
		in.Title, in.OriginalTitle, in.SortTitle, in.Year, in.Region, in.Area, in.Language, in.CategoryName,
		defaultString(in.NormalizedKind, "unknown"), in.SeasonNumber, in.EpisodeNumber, in.PosterURL,
		in.BackdropURL, in.Remarks, in.Summary, in.Directors, in.Actors, jsonBytesOrObject(in.ProviderIDs),
		jsonBytesOrObject(in.Raw), in.DetailLoaded)
	return scanSourceItem(row)
}

func (r *SourceRepository) UpsertPlaySource(ctx context.Context, in SourcePlaySourceUpsert) (*SourcePlaySource, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO source_play_sources (
			public_uuid, source_item_id, provider_id, line_name, episode_title, episode_key,
			episode_number, raw_url, parse_mode, flag, headers, resolver_payload, sort_order
		) VALUES (
			$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12::jsonb, $13
		)
		ON CONFLICT (public_uuid) DO UPDATE SET
			source_item_id = EXCLUDED.source_item_id,
			provider_id = EXCLUDED.provider_id,
			line_name = EXCLUDED.line_name,
			episode_title = EXCLUDED.episode_title,
			episode_key = EXCLUDED.episode_key,
			episode_number = EXCLUDED.episode_number,
			raw_url = EXCLUDED.raw_url,
			parse_mode = EXCLUDED.parse_mode,
			flag = EXCLUDED.flag,
			headers = EXCLUDED.headers,
			resolver_payload = EXCLUDED.resolver_payload,
			sort_order = EXCLUDED.sort_order,
			updated_at = NOW()
		RETURNING id, public_uuid::text, source_item_id, provider_id, line_name, episode_title, episode_key,
		          episode_number, raw_url, parse_mode, flag, headers, resolver_payload, sort_order,
		          health_status, success_count, failure_count, avg_latency_ms,
		          last_success_at, last_failure_at, created_at, updated_at`,
		in.PublicUUID, in.SourceItemID, in.ProviderID, in.LineName, in.EpisodeTitle, in.EpisodeKey,
		in.EpisodeNumber, in.RawURL, defaultString(in.ParseMode, "unknown"), in.Flag,
		jsonBytesOrObject(in.Headers), jsonBytesOrObject(in.ResolverPayload), in.SortOrder)
	return scanSourcePlaySource(row)
}

func (r *SourceRepository) UpsertUserItemData(ctx context.Context, in SourceUserItemDataUpsert) (*SourceUserItemData, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO source_user_item_data (
			user_id, source_item_id, playback_position_ticks, play_count,
			is_favorite, played, last_played_date
		) VALUES (
			$1::uuid, $2, COALESCE($3, 0), COALESCE($4, 0), COALESCE($5, FALSE), COALESCE($6, FALSE), $7
		)
		ON CONFLICT (user_id, source_item_id) DO UPDATE SET
			playback_position_ticks = COALESCE($3, source_user_item_data.playback_position_ticks),
			play_count = COALESCE($4, source_user_item_data.play_count),
			is_favorite = COALESCE($5, source_user_item_data.is_favorite),
			played = COALESCE($6, source_user_item_data.played),
			last_played_date = COALESCE($7, source_user_item_data.last_played_date),
			updated_at = NOW()
		RETURNING user_id::text, source_item_id, playback_position_ticks, play_count,
		          is_favorite, played, last_played_date, updated_at`,
		in.UserID, in.SourceItemID, in.PlaybackPositionTicks, in.PlayCount,
		in.IsFavorite, in.Played, in.LastPlayedDate)
	return scanSourceUserItemData(row)
}

func (r *SourceRepository) UpsertLibraryView(ctx context.Context, in SourceLibraryViewUpsert) (*SourceLibraryView, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO source_library_views (
			public_uuid, name, display_name, dimension, match_value, match_values,
			collection_type, provider_ids, filter, enabled, expose_to_emby, sort_order, config
		) VALUES (
			$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, $12, $13::jsonb
		)
		ON CONFLICT (dimension, match_value) DO UPDATE SET
			public_uuid = EXCLUDED.public_uuid,
			name = EXCLUDED.name,
			display_name = EXCLUDED.display_name,
			match_values = EXCLUDED.match_values,
			collection_type = EXCLUDED.collection_type,
			provider_ids = EXCLUDED.provider_ids,
			filter = EXCLUDED.filter,
			enabled = EXCLUDED.enabled,
			expose_to_emby = EXCLUDED.expose_to_emby,
			sort_order = EXCLUDED.sort_order,
			config = EXCLUDED.config,
			updated_at = NOW()
		RETURNING id, public_uuid::text, name, display_name, dimension, match_value, match_values,
		          collection_type, provider_ids, filter, enabled, expose_to_emby, sort_order,
		          config, created_at, updated_at`,
		in.PublicUUID, in.Name, in.DisplayName, in.Dimension, in.MatchValue, in.MatchValues,
		defaultString(in.CollectionType, "mixed"), in.ProviderIDs, jsonBytesOrObject(in.Filter),
		in.Enabled, in.ExposeToEmby, in.SortOrder, jsonBytesOrObject(in.Config))
	return scanSourceLibraryView(row)
}

func (r *SourceRepository) GetSourceItemByID(ctx context.Context, id int64) (*SourceItem, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, public_uuid::text, provider_id, source_item_id, source_parent_id, item_type, title,
		       original_title, sort_title, year, region, area, language, category_name, normalized_kind,
		       season_number, episode_number, poster_url, backdrop_url, remarks, summary, directors, actors,
		       provider_ids, raw, detail_loaded, last_seen_at, created_at, updated_at
		  FROM source_items
		 WHERE id = $1`, id)
	return scanSourceItem(row)
}

func (r *SourceRepository) GetSourceItemByPublicUUID(ctx context.Context, publicUUID string) (*SourceItem, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, public_uuid::text, provider_id, source_item_id, source_parent_id, item_type, title,
		       original_title, sort_title, year, region, area, language, category_name, normalized_kind,
		       season_number, episode_number, poster_url, backdrop_url, remarks, summary, directors, actors,
		       provider_ids, raw, detail_loaded, last_seen_at, created_at, updated_at
		  FROM source_items
		 WHERE public_uuid = $1::uuid`, publicUUID)
	return scanSourceItem(row)
}

func (r *SourceRepository) GetPlaySourceByID(ctx context.Context, id int64) (*SourcePlaySource, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, public_uuid::text, source_item_id, provider_id, line_name, episode_title, episode_key,
		       episode_number, raw_url, parse_mode, flag, headers, resolver_payload, sort_order,
		       health_status, success_count, failure_count, avg_latency_ms,
		       last_success_at, last_failure_at, created_at, updated_at
		  FROM source_play_sources
		 WHERE id = $1`, id)
	return scanSourcePlaySource(row)
}

func (r *SourceRepository) GetPlaySourceByPublicUUID(ctx context.Context, publicUUID string) (*SourcePlaySource, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, public_uuid::text, source_item_id, provider_id, line_name, episode_title, episode_key,
		       episode_number, raw_url, parse_mode, flag, headers, resolver_payload, sort_order,
		       health_status, success_count, failure_count, avg_latency_ms,
		       last_success_at, last_failure_at, created_at, updated_at
		  FROM source_play_sources
		 WHERE public_uuid = $1::uuid`, publicUUID)
	return scanSourcePlaySource(row)
}

func (r *SourceRepository) GetLibraryViewByID(ctx context.Context, id int64) (*SourceLibraryView, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, public_uuid::text, name, display_name, dimension, match_value, match_values,
		       collection_type, provider_ids, filter, enabled, expose_to_emby, sort_order,
		       config, created_at, updated_at
		  FROM source_library_views
		 WHERE id = $1`, id)
	return scanSourceLibraryView(row)
}

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
		&out.Enabled, &out.ExposeToEmby, &out.SortOrder, &out.Config, &out.CreatedAt,
		&out.UpdatedAt); err != nil {
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
