package source

import "context"

type Provider interface {
	Categories(ctx context.Context) ([]ProviderCategory, error)
	Search(ctx context.Context, req SearchRequest) (*ProviderPage, error)
	Category(ctx context.Context, req CategoryRequest) (*ProviderPage, error)
	Detail(ctx context.Context, sourceItemID string) (*ProviderDetail, error)
	ResolvePlay(ctx context.Context, play PlaySourceSnapshot) (*PlayResult, error)
}

type ProviderCategory struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type SearchRequest struct {
	Keyword string
	Page    int
}

type CategoryRequest struct {
	CategoryID string
	Page       int
}

type ProviderPage struct {
	Page      int                  `json:"page"`
	PageCount int                  `json:"page_count"`
	Total     int                  `json:"total"`
	Items     []SourceItemSnapshot `json:"items"`
}

type ProviderDetail struct {
	Item        SourceItemSnapshot   `json:"item"`
	PlaySources []PlaySourceSnapshot `json:"play_sources"`
}

type SourceItemSnapshot struct {
	SourceItemID   string         `json:"source_item_id"`
	SourceParentID *string        `json:"source_parent_id,omitempty"`
	ItemType       string         `json:"item_type"`
	Title          string         `json:"title"`
	OriginalTitle  *string        `json:"original_title,omitempty"`
	SortTitle      *string        `json:"sort_title,omitempty"`
	Year           *int32         `json:"year,omitempty"`
	Region         *string        `json:"region,omitempty"`
	Area           *string        `json:"area,omitempty"`
	Language       *string        `json:"language,omitempty"`
	CategoryID     *string        `json:"category_id,omitempty"`
	CategoryName   *string        `json:"category_name,omitempty"`
	NormalizedKind string         `json:"normalized_kind"`
	SeasonNumber   *int32         `json:"season_number,omitempty"`
	EpisodeNumber  *int32         `json:"episode_number,omitempty"`
	PosterURL      *string        `json:"poster_url,omitempty"`
	BackdropURL    *string        `json:"backdrop_url,omitempty"`
	Remarks        *string        `json:"remarks,omitempty"`
	Summary        *string        `json:"summary,omitempty"`
	Directors      []string       `json:"directors,omitempty"`
	Actors         []string       `json:"actors,omitempty"`
	ProviderIDs    map[string]any `json:"provider_ids,omitempty"`
	Raw            map[string]any `json:"raw,omitempty"`
	DetailLoaded   bool           `json:"detail_loaded"`
}

type PlaySourceSnapshot struct {
	LineName        string         `json:"line_name"`
	EpisodeTitle    string         `json:"episode_title"`
	EpisodeKey      string         `json:"episode_key"`
	EpisodeNumber   *int32         `json:"episode_number,omitempty"`
	RawURL          string         `json:"raw_url"`
	ParseMode       string         `json:"parse_mode"`
	Flag            *string        `json:"flag,omitempty"`
	Headers         map[string]any `json:"headers,omitempty"`
	ResolverPayload map[string]any `json:"resolver_payload,omitempty"`
	SortOrder       int32          `json:"sort_order"`
}
