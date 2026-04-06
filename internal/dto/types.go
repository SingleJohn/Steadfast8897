package dto

import (
	"encoding/json"
	"time"
)

type BaseItemDto struct {
	ID                         string                 `json:"Id"`
	Name                       string                 `json:"Name"`
	ServerID                   string                 `json:"ServerId"`
	Type                       string                 `json:"Type"`
	MediaType                  *string                `json:"MediaType,omitempty"`
	IsFolder                   *bool                  `json:"IsFolder,omitempty"`
	Overview                   *string                `json:"Overview,omitempty"`
	ProductionYear             *int32                 `json:"ProductionYear,omitempty"`
	PremiereDate               *string                `json:"PremiereDate,omitempty"`
	CommunityRating            *float64               `json:"CommunityRating,omitempty"`
	OfficialRating             *string                `json:"OfficialRating,omitempty"`
	RunTimeTicks               *int64                 `json:"RunTimeTicks,omitempty"`
	IndexNumber                *int32                 `json:"IndexNumber,omitempty"`
	ParentIndexNumber          *int32                 `json:"ParentIndexNumber,omitempty"`
	ParentID                   *string                `json:"ParentId,omitempty"`
	SortName                   *string                `json:"SortName,omitempty"`
	CollectionType             *string                `json:"CollectionType,omitempty"`
	ImageTags                  map[string]string      `json:"ImageTags,omitempty"`
	BackdropImageTags          []string               `json:"BackdropImageTags,omitempty"`
	ChildCount                 *int64                 `json:"ChildCount,omitempty"`
	RecursiveItemCount         *int64                 `json:"RecursiveItemCount,omitempty"`
	SeriesID                   *string                `json:"SeriesId,omitempty"`
	SeriesName                 *string                `json:"SeriesName,omitempty"`
	SeasonID                   *string                `json:"SeasonId,omitempty"`
	Container                  *string                `json:"Container,omitempty"`
	ProviderIDs                *json.RawMessage       `json:"ProviderIds,omitempty"`
	MediaSourceCount           *int32                 `json:"MediaSourceCount,omitempty"`
	MediaSources               []MediaSourceInfo      `json:"MediaSources,omitempty"`
	MediaStreams               []MediaStreamInfo      `json:"MediaStreams,omitempty"`
	UserData                   *UserItemDataDto       `json:"UserData,omitempty"`
	Path                       *string                `json:"Path,omitempty"`
	GenreItems                 []GenreItem            `json:"GenreItems,omitempty"`
	Genres                     []string               `json:"Genres,omitempty"`
	People                     []map[string]interface{} `json:"People,omitempty"`
	OriginalTitle              *string                `json:"OriginalTitle,omitempty"`
	Taglines                   []string               `json:"Taglines,omitempty"`
	DateCreated                *string                `json:"DateCreated,omitempty"`
	Studios                    []StudioItem           `json:"Studios,omitempty"`
	ProductionLocations        []string               `json:"ProductionLocations,omitempty"`
	Etag                       *string                `json:"Etag,omitempty"`
	SeriesPrimaryImageItemID   *string                `json:"SeriesPrimaryImageItemId,omitempty"`
	SeriesPrimaryImageTag      *string                `json:"SeriesPrimaryImageTag,omitempty"`
	ParentBackdropItemID       *string                `json:"ParentBackdropItemId,omitempty"`
	ParentBackdropImageTags    []string               `json:"ParentBackdropImageTags,omitempty"`
	ParentThumbItemID          *string                `json:"ParentThumbItemId,omitempty"`
	ParentThumbImageTag        *string                `json:"ParentThumbImageTag,omitempty"`
	ParentPrimaryImageItemID   *string                `json:"ParentPrimaryImageItemId,omitempty"`
	ParentPrimaryImageTag      *string                `json:"ParentPrimaryImageTag,omitempty"`
}

type GenreItem struct {
	Name string `json:"Name"`
	ID   string `json:"Id"`
}

type StudioItem struct {
	Name string `json:"Name"`
	ID   string `json:"Id,omitempty"`
}

type MediaSourceInfo struct {
	ID                         string            `json:"Id"`
	Path                       string            `json:"Path"`
	Protocol                   string            `json:"Protocol"`
	Type                       string            `json:"Type"`
	Container                  string            `json:"Container"`
	Size                       *int64            `json:"Size,omitempty"`
	Name                       string            `json:"Name"`
	IsRemote                   bool              `json:"IsRemote"`
	ETag                       string            `json:"ETag,omitempty"`
	RunTimeTicks               *int64            `json:"RunTimeTicks,omitempty"`
	SupportsDirectPlay         bool              `json:"SupportsDirectPlay"`
	SupportsDirectStream       bool              `json:"SupportsDirectStream"`
	SupportsTranscoding        bool              `json:"SupportsTranscoding"`
	RequiresOpening            bool              `json:"RequiresOpening"`
	RequiresClosing            bool              `json:"RequiresClosing"`
	RequiresLooping            bool              `json:"RequiresLooping"`
	MediaStreams                []MediaStreamInfo `json:"MediaStreams"`
	Bitrate                    *int64            `json:"Bitrate,omitempty"`
	ReadAtNativeFramerate      bool              `json:"ReadAtNativeFramerate"`
	DirectStreamURL            string            `json:"DirectStreamUrl,omitempty"`
	DefaultAudioStreamIndex    *int32            `json:"DefaultAudioStreamIndex,omitempty"`
	DefaultSubtitleStreamIndex *int32            `json:"DefaultSubtitleStreamIndex,omitempty"`
	Formats                    []string          `json:"Formats"`
}

type MediaStreamInfo struct {
	Codec        string  `json:"Codec"`
	Type         string  `json:"Type"`
	Index        int32   `json:"Index"`
	Language     *string `json:"Language,omitempty"`
	Title        *string `json:"Title,omitempty"`
	IsDefault    bool    `json:"IsDefault"`
	IsForced     bool    `json:"IsForced"`
	Width        *int32  `json:"Width,omitempty"`
	Height       *int32  `json:"Height,omitempty"`
	BitRate      *int64  `json:"BitRate,omitempty"`
	Channels     *int32  `json:"Channels,omitempty"`
	SampleRate   *int32  `json:"SampleRate,omitempty"`
	BitDepth     *int32  `json:"BitDepth,omitempty"`
	PixelFormat  *string `json:"PixelFormat,omitempty"`
	DisplayTitle *string `json:"DisplayTitle,omitempty"`
}

type UserItemDataDto struct {
	PlaybackPositionTicks int64    `json:"PlaybackPositionTicks"`
	PlayCount             int32    `json:"PlayCount"`
	IsFavorite            bool     `json:"IsFavorite"`
	Played                bool     `json:"Played"`
	LastPlayedDate        *string  `json:"LastPlayedDate,omitempty"`
	PlayedPercentage      *float64 `json:"PlayedPercentage,omitempty"`
}

type ItemRow struct {
	ID                     string
	Name                   string
	ItemType               string
	SortName               *string
	CollectionType         *string
	Overview               *string
	ProductionYear         *int32
	PremiereDate           *time.Time
	CommunityRating        *float64
	OfficialRating         *string
	RuntimeTicks           *int64
	IndexNumber            *int32
	ParentIndexNumber      *int32
	ParentID               *string
	SeriesID               *string
	SeriesName             *string
	SeasonID               *string
	Container              *string
	FilePath               *string
	ResolvedPath           *string
	ProviderIDs            *json.RawMessage
	PrimaryImageTag        *string
	BackdropImageTag       *string
	SeriesPrimaryImageTag  *string
	SeriesBackdropImageTag *string
	SeriesFallbackID       *string
	ChildCount             *int64
	RecursiveItemCount     *int64
	Tagline                *string
	Studio                 *string
	CreatedAt              *time.Time
}

type UserDataRow struct {
	PlaybackPositionTicks *int64
	PlayCount             *int32
	IsFavorite            *bool
	Played                *bool
	LastPlayedDate        *time.Time
}

type StreamRow struct {
	Codec        *string
	StreamType   string
	StreamIndex  int32
	Language     *string
	Title        *string
	IsDefault    *bool
	IsForced     *bool
	Width        *int32
	Height       *int32
	BitRate      *int64
	Channels     *int32
	SampleRate   *int32
	BitDepth     *int32
	PixelFormat  *string
	DisplayTitle *string
}
