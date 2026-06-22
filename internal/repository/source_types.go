package repository

import (
	"encoding/json"
	"time"
)

type SourceConfigImport struct {
	ID            int64
	SourceType    string
	Name          string
	SourceURL     *string
	BaseURL       *string
	ContentSHA256 string
	SpiderRef     *string
	SpiderMD5     *string
	RawConfig     json.RawMessage
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
	Ext          json.RawMessage
	Categories   json.RawMessage
	Headers      json.RawMessage
	Capabilities json.RawMessage
	TimeoutMS    int32
	Enabled      bool
	Visible      bool
	Searchable   bool
	HealthStatus string
	LastCheckAt  *time.Time
	LastError    *string
	RawSite      json.RawMessage
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type SourceRuntimeArtifact struct {
	ID            int64
	ProviderID    *int64
	SourceType    string
	ArtifactKind  string
	Name          string
	SourceURL     string
	BaseURL       *string
	RelativePath  *string
	LocalPath     string
	MD5           string
	SHA256        string
	ByteSize      int64
	ContentType   *string
	TrustStatus   string
	Status        string
	LastFetchedAt time.Time
	VerifiedAt    *time.Time
	LastError     *string
	Raw           json.RawMessage
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type SourceParser struct {
	ID          int64
	ConfigID    *int64
	SourceType  string
	Name        string
	ParserType  int32
	URL         string
	BaseURL     *string
	TimeoutMS   int32
	Enabled     bool
	TrustStatus string
	Status      string
	LastCheckAt *time.Time
	LastError   *string
	Raw         json.RawMessage
	CreatedAt   time.Time
	UpdatedAt   time.Time
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
	ProviderIDs    json.RawMessage
	Raw            json.RawMessage
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
	Headers         json.RawMessage
	ResolverPayload json.RawMessage
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
	Filter         json.RawMessage
	Enabled        bool
	ExposeToEmby   bool
	SortOrder      int32
	Config         json.RawMessage
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

type SourceItemSearchOptions struct {
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
	RawConfig     json.RawMessage
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
	Ext          json.RawMessage
	Categories   json.RawMessage
	Headers      json.RawMessage
	Capabilities json.RawMessage
	TimeoutMS    int32
	Enabled      bool
	Visible      bool
	Searchable   bool
	HealthStatus string
	LastError    *string
	RawSite      json.RawMessage
}

type SourceRuntimeArtifactUpsert struct {
	ProviderID   *int64
	SourceType   string
	ArtifactKind string
	Name         string
	SourceURL    string
	BaseURL      *string
	RelativePath *string
	LocalPath    string
	MD5          string
	SHA256       string
	ByteSize     int64
	ContentType  *string
	TrustStatus  string
	Status       string
	LastError    *string
	Raw          json.RawMessage
}

type SourceParserUpsert struct {
	ConfigID    *int64
	SourceType  string
	Name        string
	ParserType  int32
	URL         string
	BaseURL     *string
	TimeoutMS   int32
	Enabled     bool
	TrustStatus string
	Status      string
	LastError   *string
	Raw         json.RawMessage
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
	ProviderIDs    json.RawMessage
	Raw            json.RawMessage
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
	Headers         json.RawMessage
	ResolverPayload json.RawMessage
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
	Filter         json.RawMessage
	Enabled        bool
	ExposeToEmby   bool
	SortOrder      int32
	Config         json.RawMessage
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

type SourceParserListOptions struct {
	Limit       int64
	Offset      int64
	ConfigID    *int64
	OnlyEnabled bool
}

type SourceDimensionValue struct {
	Value        string
	Count        int64
	AlreadyAdded bool
}
