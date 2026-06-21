package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	CoverImagePath *string
	CoverImageTag  *string
	ItemCount      int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type SourceEpisode struct {
	SourceItemID   int64
	SourceItemUUID string
	ProviderID     int64
	SeriesTitle    string
	SeriesSummary  *string
	PosterURL      *string
	BackdropURL    *string
	EpisodeKey     string
	EpisodeTitle   string
	EpisodeNumber  *int32
	LineCount      int64
	FirstSeenAt    time.Time
}

type SourceItemListOptions struct {
	Limit        int64
	Offset       int64
	SearchTerm   string
	IncludeTypes []string
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
	CoverImagePath *string
	CoverImageTag  *string
}

type SourceConfigListOptions struct {
	Limit  int64
	Offset int64
}

type SourceProviderListOptions struct {
	Limit      int64
	Offset     int64
	ConfigID   *int64
	OnlyUsable bool
}

type SourceDimensionValue struct {
	Value        string
	Count        int64
	AlreadyAdded bool
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

func (r *SourceRepository) ResolveEpisodePublicUUID(ctx context.Context, publicUUID string, makeEpisodeUUID func(string, string) string) (int64, string, bool, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT si.id, si.public_uuid::text,
		       COALESCE(NULLIF(sps.episode_key, ''), sps.episode_title, sps.id::text) AS episode_key
		  FROM source_items si
		  JOIN source_play_sources sps ON sps.source_item_id = si.id
		 WHERE si.item_type = 'Series'
		 GROUP BY si.id, si.public_uuid,
		          COALESCE(NULLIF(sps.episode_key, ''), sps.episode_title, sps.id::text)`)
	if err != nil {
		return 0, "", false, err
	}
	defer rows.Close()
	for rows.Next() {
		var sourceItemID int64
		var sourceItemUUID, episodeKey string
		if err := rows.Scan(&sourceItemID, &sourceItemUUID, &episodeKey); err != nil {
			return 0, "", false, err
		}
		if makeEpisodeUUID(sourceItemUUID, episodeKey) == publicUUID {
			return sourceItemID, episodeKey, true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return 0, "", false, err
	}
	return 0, "", false, nil
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

func (r *SourceRepository) UpsertProviderBySourceKey(ctx context.Context, in SourceProviderUpsert) (*SourceProvider, error) {
	if strings.TrimSpace(in.SourceKey) == "" {
		return nil, fmt.Errorf("source_key is required")
	}
	existing, err := r.GetProviderBySourceKey(ctx, in.SourceKey)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return r.UpsertProvider(ctx, in)
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE source_providers
		   SET config_id = $2,
		       name = $3,
		       provider_kind = $4,
		       runtime_kind = $5,
		       tvbox_type = $6,
		       api = $7,
		       ext = $8::jsonb,
		       categories = $9::jsonb,
		       headers = $10::jsonb,
		       capabilities = $11::jsonb,
		       timeout_ms = $12,
		       enabled = $13,
		       visible = $14,
		       searchable = $15,
		       health_status = $16,
		       last_error = $17,
		       raw_site = $18::jsonb,
		       updated_at = NOW()
		 WHERE id = $1
		RETURNING id, config_id, source_key, name, provider_kind, runtime_kind, tvbox_type, api,
		          ext, categories, headers, capabilities, timeout_ms, enabled, visible, searchable,
		          health_status, last_check_at, last_error, raw_site, created_at, updated_at`,
		existing.ID, in.ConfigID, in.Name, in.ProviderKind, in.RuntimeKind, in.TVBoxType, in.API,
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
			collection_type, provider_ids, filter, enabled, expose_to_emby, sort_order, config,
			cover_image_path, cover_image_tag
		) VALUES (
			$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, $12, $13::jsonb, $14, $15
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
			cover_image_path = EXCLUDED.cover_image_path,
			cover_image_tag = EXCLUDED.cover_image_tag,
			updated_at = NOW()
		RETURNING id, public_uuid::text, name, display_name, dimension, match_value, match_values,
		          collection_type, provider_ids, filter, enabled, expose_to_emby, sort_order,
		          config, cover_image_path, cover_image_tag, created_at, updated_at`,
		in.PublicUUID, in.Name, in.DisplayName, in.Dimension, in.MatchValue, in.MatchValues,
		defaultString(in.CollectionType, "mixed"), in.ProviderIDs, jsonBytesOrObject(in.Filter),
		in.Enabled, in.ExposeToEmby, in.SortOrder, jsonBytesOrObject(in.Config), in.CoverImagePath, in.CoverImageTag)
	return scanSourceLibraryView(row)
}

func (r *SourceRepository) SupersedeConfigImportsForSourceKeys(ctx context.Context, sourceType string, activeConfigID int64, sourceKeys []string) error {
	keys := compactStrings(sourceKeys)
	if len(keys) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE source_config_imports sci
		   SET import_status = 'superseded',
		       updated_at = NOW()
		  FROM source_providers sp
		 WHERE sp.config_id = sci.id
		   AND sci.source_type = $1
		   AND sci.id <> $2
		   AND sp.source_key = ANY($3::text[])
		   AND sci.import_status <> 'superseded'`,
		defaultString(sourceType, "tvbox"), activeConfigID, keys)
	return err
}

func (r *SourceRepository) ListConfigImports(ctx context.Context, opts SourceConfigListOptions) ([]SourceConfigImport, error) {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, source_type, name, source_url, base_url, content_sha256, spider_ref, spider_md5,
		       raw_config, import_status, enabled, imported_by::text, imported_at, updated_at
		  FROM source_config_imports
		 ORDER BY imported_at DESC, id DESC
		 LIMIT $1 OFFSET $2`, opts.Limit, opts.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceConfigImport
	for rows.Next() {
		item, err := scanSourceConfigImport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (r *SourceRepository) SetConfigEnabled(ctx context.Context, id int64, enabled bool) (*SourceConfigImport, error) {
	status := "disabled"
	if enabled {
		status = "active"
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE source_config_imports
		   SET enabled = $2,
		       import_status = CASE WHEN import_status = 'invalid' THEN import_status ELSE $3 END,
		       updated_at = NOW()
		 WHERE id = $1
		RETURNING id, source_type, name, source_url, base_url, content_sha256, spider_ref, spider_md5,
		          raw_config, import_status, enabled, imported_by::text, imported_at, updated_at`,
		id, enabled, status)
	return scanSourceConfigImport(row)
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

func (r *SourceRepository) GetConfigImportByID(ctx context.Context, id int64) (*SourceConfigImport, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, source_type, name, source_url, base_url, content_sha256, spider_ref, spider_md5,
		       raw_config, import_status, enabled, imported_by::text, imported_at, updated_at
		  FROM source_config_imports
		 WHERE id = $1`, id)
	return scanSourceConfigImport(row)
}

func (r *SourceRepository) GetProviderByID(ctx context.Context, id int64) (*SourceProvider, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, config_id, source_key, name, provider_kind, runtime_kind, tvbox_type, api,
		       ext, categories, headers, capabilities, timeout_ms, enabled, visible, searchable,
		       health_status, last_check_at, last_error, raw_site, created_at, updated_at
		  FROM source_providers
		 WHERE id = $1`, id)
	return scanSourceProvider(row)
}

func (r *SourceRepository) GetProviderBySourceKey(ctx context.Context, sourceKey string) (*SourceProvider, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, config_id, source_key, name, provider_kind, runtime_kind, tvbox_type, api,
		       ext, categories, headers, capabilities, timeout_ms, enabled, visible, searchable,
		       health_status, last_check_at, last_error, raw_site, created_at, updated_at
		  FROM source_providers
		 WHERE source_key = $1
		 ORDER BY updated_at DESC, id DESC
		 LIMIT 1`, strings.TrimSpace(sourceKey))
	return scanSourceProvider(row)
}

func (r *SourceRepository) ListProviders(ctx context.Context, opts SourceProviderListOptions) ([]SourceProvider, error) {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	clauses := []string{"TRUE"}
	args := []any{}
	addArg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if opts.ConfigID != nil {
		clauses = append(clauses, "sp.config_id = "+addArg(*opts.ConfigID))
	}
	if opts.OnlyUsable {
		clauses = append(clauses, "sp.enabled = TRUE", "COALESCE(sci.enabled, TRUE) = TRUE", "COALESCE(sci.import_status, 'active') = 'active'")
	}
	limitArg := addArg(opts.Limit)
	offsetArg := addArg(opts.Offset)
	rows, err := r.pool.Query(ctx, `
		SELECT sp.id, sp.config_id, sp.source_key, sp.name, sp.provider_kind, sp.runtime_kind, sp.tvbox_type, sp.api,
		       sp.ext, sp.categories, sp.headers, sp.capabilities, sp.timeout_ms, sp.enabled, sp.visible, sp.searchable,
		       sp.health_status, sp.last_check_at, sp.last_error, sp.raw_site, sp.created_at, sp.updated_at
		  FROM source_providers sp
		  LEFT JOIN source_config_imports sci ON sci.id = sp.config_id
		 WHERE `+strings.Join(clauses, " AND ")+`
		 ORDER BY sp.updated_at DESC, sp.id DESC
		 LIMIT `+limitArg+` OFFSET `+offsetArg, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceProvider
	for rows.Next() {
		provider, err := scanSourceProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *provider)
	}
	return out, rows.Err()
}

func (r *SourceRepository) SetProviderEnabled(ctx context.Context, id int64, enabled bool) (*SourceProvider, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE source_providers
		   SET enabled = $2,
		       updated_at = NOW()
		 WHERE id = $1
		RETURNING id, config_id, source_key, name, provider_kind, runtime_kind, tvbox_type, api,
		          ext, categories, headers, capabilities, timeout_ms, enabled, visible, searchable,
		          health_status, last_check_at, last_error, raw_site, created_at, updated_at`, id, enabled)
	return scanSourceProvider(row)
}

func (r *SourceRepository) UpdateProviderHealth(ctx context.Context, id int64, status string, lastError *string, categories []byte) (*SourceProvider, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE source_providers
		   SET health_status = $2,
		       last_check_at = NOW(),
		       last_error = $3,
		       categories = CASE WHEN $4::jsonb = 'null'::jsonb THEN categories ELSE $4::jsonb END,
		       updated_at = NOW()
		 WHERE id = $1
		RETURNING id, config_id, source_key, name, provider_kind, runtime_kind, tvbox_type, api,
		          ext, categories, headers, capabilities, timeout_ms, enabled, visible, searchable,
		          health_status, last_check_at, last_error, raw_site, created_at, updated_at`,
		id, defaultString(status, "unknown"), lastError, jsonBytesOrNull(categories))
	return scanSourceProvider(row)
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
		       config, cover_image_path, cover_image_tag, created_at, updated_at
		  FROM source_library_views
		 WHERE id = $1`, id)
	return scanSourceLibraryView(row)
}

func (r *SourceRepository) ListLibraryViews(ctx context.Context, withCounts bool) ([]SourceLibraryView, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, public_uuid::text, name, display_name, dimension, match_value, match_values,
		       collection_type, provider_ids, filter, enabled, expose_to_emby, sort_order,
		       config, cover_image_path, cover_image_tag, created_at, updated_at
		  FROM source_library_views
		 ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceLibraryView
	for rows.Next() {
		view, err := scanSourceLibraryView(rows)
		if err != nil {
			return nil, err
		}
		if withCounts {
			view.ItemCount, _ = r.CountItemsForLibraryView(ctx, *view)
		}
		out = append(out, *view)
	}
	return out, rows.Err()
}

func (r *SourceRepository) DeleteLibraryView(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM source_library_views WHERE id = $1`, id)
	return err
}

func (r *SourceRepository) RenameLibraryView(ctx context.Context, id int64, name string) (*SourceLibraryView, error) {
	name = strings.TrimSpace(name)
	var displayName any
	if name != "" {
		displayName = name
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE source_library_views
		   SET display_name = $2,
		       updated_at = NOW()
		 WHERE id = $1
		RETURNING id, public_uuid::text, name, display_name, dimension, match_value, match_values,
		          collection_type, provider_ids, filter, enabled, expose_to_emby, sort_order,
		          config, cover_image_path, cover_image_tag, created_at, updated_at`, id, displayName)
	return scanSourceLibraryView(row)
}

func (r *SourceRepository) SetLibraryViewCover(ctx context.Context, id int64, path, tag string) (*SourceLibraryView, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE source_library_views
		   SET cover_image_path = $2,
		       cover_image_tag = $3,
		       updated_at = NOW()
		 WHERE id = $1
		RETURNING id, public_uuid::text, name, display_name, dimension, match_value, match_values,
		          collection_type, provider_ids, filter, enabled, expose_to_emby, sort_order,
		          config, cover_image_path, cover_image_tag, created_at, updated_at`, id, path, tag)
	return scanSourceLibraryView(row)
}

func (r *SourceRepository) ClearLibraryViewCover(ctx context.Context, id int64) (string, error) {
	var oldPath *string
	_ = r.pool.QueryRow(ctx, `SELECT cover_image_path FROM source_library_views WHERE id = $1`, id).Scan(&oldPath)
	_, err := r.pool.Exec(ctx, `
		UPDATE source_library_views
		   SET cover_image_path = NULL,
		       cover_image_tag = NULL,
		       updated_at = NOW()
		 WHERE id = $1`, id)
	if oldPath != nil {
		return *oldPath, err
	}
	return "", err
}

func (r *SourceRepository) UpdateLibraryViewSortOrder(ctx context.Context, orderedIDs []int64) error {
	for i, id := range orderedIDs {
		if _, err := r.pool.Exec(ctx, `UPDATE source_library_views SET sort_order = $1, updated_at = NOW() WHERE id = $2`, i, id); err != nil {
			return err
		}
	}
	return nil
}

func (r *SourceRepository) DiscoverLibraryViewValues(ctx context.Context, dimension, search string, minCount int64) ([]SourceDimensionValue, error) {
	dimension = strings.TrimSpace(dimension)
	search = strings.TrimSpace(search)
	if minCount <= 0 {
		minCount = 1
	}
	groupExpr, whereClause, err := sourceDimensionDiscoverSQL(dimension)
	if err != nil {
		return nil, err
	}
	args := []any{}
	if search != "" {
		args = append(args, "%"+search+"%")
		whereClause += fmt.Sprintf(" AND %s ILIKE $%d", groupExpr, len(args))
	}
	args = append(args, minCount)
	minArg := len(args)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT %s AS value, COUNT(*) AS count
		  FROM source_items si
		 WHERE %s
		 GROUP BY %s
		HAVING COUNT(*) >= $%d
		 ORDER BY count DESC, value ASC
		 LIMIT 2000`, groupExpr, whereClause, groupExpr, minArg), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceDimensionValue
	for rows.Next() {
		var value *string
		var count int64
		if err := rows.Scan(&value, &count); err != nil {
			return nil, err
		}
		if value == nil || strings.TrimSpace(*value) == "" {
			continue
		}
		out = append(out, SourceDimensionValue{Value: *value, Count: count})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	added, _ := r.addedSourceViewValues(ctx, dimension)
	for i := range out {
		if _, ok := added[out[i].Value]; ok {
			out[i].AlreadyAdded = true
		}
	}
	return out, nil
}

func (r *SourceRepository) ListExposedLibraryViews(ctx context.Context) ([]SourceLibraryView, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, public_uuid::text, name, display_name, dimension, match_value, match_values,
		       collection_type, provider_ids, filter, enabled, expose_to_emby, sort_order,
		       config, cover_image_path, cover_image_tag, created_at, updated_at
		  FROM source_library_views
		 WHERE enabled = TRUE AND expose_to_emby = TRUE
		 ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceLibraryView
	for rows.Next() {
		view, err := scanSourceLibraryView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *view)
	}
	return out, rows.Err()
}

func (r *SourceRepository) CountItemsForLibraryView(ctx context.Context, view SourceLibraryView) (int64, error) {
	where, args, err := sourceViewWhere(view, nil)
	if err != nil {
		return 0, err
	}
	var count int64
	err = r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM source_items si WHERE `+where, args...).Scan(&count)
	return count, err
}

func (r *SourceRepository) ListItemsForLibraryView(ctx context.Context, view SourceLibraryView, opts SourceItemListOptions) ([]SourceItem, int64, error) {
	where, args, err := sourceViewWhere(view, &opts)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM source_items si WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	limitIdx := len(args) + 1
	offsetIdx := len(args) + 2
	args = append(args, opts.Limit, opts.Offset)
	rows, err := r.pool.Query(ctx, `
		SELECT id, public_uuid::text, provider_id, source_item_id, source_parent_id, item_type, title,
		       original_title, sort_title, year, region, area, language, category_name, normalized_kind,
		       season_number, episode_number, poster_url, backdrop_url, remarks, summary, directors, actors,
		       provider_ids, raw, detail_loaded, last_seen_at, created_at, updated_at
		  FROM source_items si
		 WHERE `+where+`
		 ORDER BY COALESCE(sort_title, title), id
		 LIMIT $`+fmt.Sprint(limitIdx)+` OFFSET $`+fmt.Sprint(offsetIdx), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []SourceItem
	for rows.Next() {
		item, err := scanSourceItem(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *item)
	}
	return out, total, rows.Err()
}

func (r *SourceRepository) ListEpisodesForSeries(ctx context.Context, sourceItemID int64) ([]SourceEpisode, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT si.id, si.public_uuid::text, si.provider_id, si.title, si.summary, si.poster_url, si.backdrop_url,
		       COALESCE(NULLIF(sps.episode_key, ''), sps.episode_title, sps.id::text) AS episode_key,
		       COALESCE(NULLIF(sps.episode_title, ''), NULLIF(sps.episode_key, ''), sps.line_name) AS episode_title,
		       MIN(sps.episode_number) AS episode_number,
		       COUNT(*) AS line_count,
		       MIN(sps.created_at) AS first_seen_at
		  FROM source_items si
		  JOIN source_play_sources sps ON sps.source_item_id = si.id
		 WHERE si.id = $1
		 GROUP BY si.id, si.public_uuid, si.provider_id, si.title, si.summary, si.poster_url, si.backdrop_url,
		          COALESCE(NULLIF(sps.episode_key, ''), sps.episode_title, sps.id::text),
		          COALESCE(NULLIF(sps.episode_title, ''), NULLIF(sps.episode_key, ''), sps.line_name)
		 ORDER BY MIN(sps.sort_order), MIN(sps.episode_number) NULLS LAST, episode_key`, sourceItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceEpisode
	for rows.Next() {
		var ep SourceEpisode
		if err := rows.Scan(&ep.SourceItemID, &ep.SourceItemUUID, &ep.ProviderID, &ep.SeriesTitle,
			&ep.SeriesSummary, &ep.PosterURL, &ep.BackdropURL, &ep.EpisodeKey, &ep.EpisodeTitle,
			&ep.EpisodeNumber, &ep.LineCount, &ep.FirstSeenAt); err != nil {
			return nil, err
		}
		out = append(out, ep)
	}
	return out, rows.Err()
}

func (r *SourceRepository) GetEpisodeForSeries(ctx context.Context, sourceItemID int64, episodeKey string) (*SourceEpisode, error) {
	episodes, err := r.ListEpisodesForSeries(ctx, sourceItemID)
	if err != nil {
		return nil, err
	}
	for i := range episodes {
		if episodes[i].EpisodeKey == episodeKey {
			return &episodes[i], nil
		}
	}
	return nil, nil
}

func (r *SourceRepository) GetUserItemData(ctx context.Context, userID string, sourceItemID int64) (*SourceUserItemData, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT user_id::text, source_item_id, playback_position_ticks, play_count,
		       is_favorite, played, last_played_date, updated_at
		  FROM source_user_item_data
		 WHERE user_id = $1::uuid AND source_item_id = $2`, userID, sourceItemID)
	return scanSourceUserItemData(row)
}

func (r *SourceRepository) ListPlaySourcesForItem(ctx context.Context, sourceItemID int64) ([]SourcePlaySource, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, public_uuid::text, source_item_id, provider_id, line_name, episode_title, episode_key,
		       episode_number, raw_url, parse_mode, flag, headers, resolver_payload, sort_order,
		       health_status, success_count, failure_count, avg_latency_ms,
		       last_success_at, last_failure_at, created_at, updated_at
		  FROM source_play_sources
		 WHERE source_item_id = $1
		 ORDER BY sort_order, line_name, episode_number NULLS LAST, episode_key`, sourceItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourcePlaySource
	for rows.Next() {
		ps, err := scanSourcePlaySource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ps)
	}
	return out, rows.Err()
}

func (r *SourceRepository) MarkPlaySourceSuccess(ctx context.Context, id int64, latencyMS int64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE source_play_sources
		   SET health_status = 'ok',
		       success_count = success_count + 1,
		       avg_latency_ms = CASE
		         WHEN avg_latency_ms IS NULL THEN $2
		         ELSE ((avg_latency_ms + $2) / 2)::integer
		       END,
		       last_success_at = NOW(),
		       updated_at = NOW()
		 WHERE id = $1`, id, latencyMS)
	return err
}

func (r *SourceRepository) MarkPlaySourceFailure(ctx context.Context, id int64, latencyMS int64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE source_play_sources
		   SET health_status = 'error',
		       failure_count = failure_count + 1,
		       avg_latency_ms = CASE
		         WHEN avg_latency_ms IS NULL THEN $2
		         ELSE ((avg_latency_ms + $2) / 2)::integer
		       END,
		       last_failure_at = NOW(),
		       updated_at = NOW()
		 WHERE id = $1`, id, latencyMS)
	return err
}

func sourceViewWhere(view SourceLibraryView, opts *SourceItemListOptions) (string, []any, error) {
	clauses := []string{"si.item_type IN ('Movie', 'Series')"}
	args := []any{}
	addArg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	switch view.Dimension {
	case "normalized_kind":
		clauses = append(clauses, "si.normalized_kind = "+addArg(view.MatchValue))
	case "region":
		clauses = append(clauses, sourceRegionCondition("si.region", view.MatchValue, addArg))
	case "kind_region":
		kind, region, ok := strings.Cut(view.MatchValue, "/")
		if !ok || strings.TrimSpace(kind) == "" || strings.TrimSpace(region) == "" {
			return "", nil, fmt.Errorf("invalid kind_region match_value: %s", view.MatchValue)
		}
		clauses = append(clauses, "si.normalized_kind = "+addArg(strings.TrimSpace(kind)))
		clauses = append(clauses, sourceRegionCondition("si.region", strings.TrimSpace(region), addArg))
	case "provider":
		clauses = append(clauses, "si.provider_id = "+addArg(view.MatchValue))
	case "custom":
		values := view.MatchValues
		if len(values) == 0 {
			values = []string{view.MatchValue}
		}
		clauses = append(clauses, "(si.normalized_kind = ANY("+addArg(values)+"::text[]) OR si.region = ANY("+fmt.Sprintf("$%d", len(args))+"::text[]))")
	default:
		return "", nil, fmt.Errorf("unknown source view dimension: %s", view.Dimension)
	}

	if len(view.ProviderIDs) > 0 {
		clauses = append(clauses, "si.provider_id = ANY("+addArg(view.ProviderIDs)+"::bigint[])")
	}
	if len(view.Filter) > 0 && json.Valid(view.Filter) {
		var filter struct {
			Regions         []string `json:"regions"`
			NormalizedKinds []string `json:"normalized_kinds"`
			Years           []int32  `json:"years"`
			ProviderIDs     []int64  `json:"provider_ids"`
		}
		if err := json.Unmarshal(view.Filter, &filter); err == nil {
			if len(filter.Regions) > 0 {
				clauses = append(clauses, "si.region = ANY("+addArg(filter.Regions)+"::text[])")
			}
			if len(filter.NormalizedKinds) > 0 {
				clauses = append(clauses, "si.normalized_kind = ANY("+addArg(filter.NormalizedKinds)+"::text[])")
			}
			if len(filter.Years) > 0 {
				clauses = append(clauses, "si.year = ANY("+addArg(filter.Years)+"::integer[])")
			}
			if len(filter.ProviderIDs) > 0 {
				clauses = append(clauses, "si.provider_id = ANY("+addArg(filter.ProviderIDs)+"::bigint[])")
			}
		}
	}
	if opts != nil {
		if len(opts.IncludeTypes) > 0 {
			clauses = append(clauses, "si.item_type = ANY("+addArg(opts.IncludeTypes)+"::text[])")
		}
		if strings.TrimSpace(opts.SearchTerm) != "" {
			clauses = append(clauses, "si.title ILIKE "+addArg("%"+strings.TrimSpace(opts.SearchTerm)+"%"))
		}
	}
	return strings.Join(clauses, " AND "), args, nil
}

func sourceDimensionDiscoverSQL(dimension string) (string, string, error) {
	base := "si.item_type IN ('Movie', 'Series')"
	switch dimension {
	case "normalized_kind":
		return "si.normalized_kind", base + " AND si.normalized_kind IS NOT NULL AND si.normalized_kind <> ''", nil
	case "region":
		return "si.region", base + " AND si.region IS NOT NULL AND si.region <> ''", nil
	case "provider":
		return "si.provider_id::text", base, nil
	case "kind_region":
		return "si.normalized_kind || '/' || COALESCE(si.region, 'Foreign')", base + " AND si.normalized_kind IS NOT NULL AND si.normalized_kind <> ''", nil
	case "custom":
		return "COALESCE(si.normalized_kind, si.region)", base, nil
	default:
		return "", "", fmt.Errorf("unknown source view dimension: %s", dimension)
	}
}

func (r *SourceRepository) addedSourceViewValues(ctx context.Context, dimension string) (map[string]struct{}, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT unnest(CASE WHEN cardinality(match_values) > 0 THEN match_values ELSE ARRAY[match_value] END)
		   FROM source_library_views WHERE dimension = $1`, dimension)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err == nil {
			out[value] = struct{}{}
		}
	}
	return out, rows.Err()
}

func sourceRegionCondition(column, matchValue string, addArg func(any) string) string {
	if strings.EqualFold(matchValue, "Foreign") {
		return "(" + column + " IS NULL OR " + column + " <> 'CN')"
	}
	return column + " = " + addArg(matchValue)
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
