package dto

import (
	"encoding/json"
	"time"
)

type BaseItemDto struct {
	ID                       string                   `json:"Id"`
	Name                     string                   `json:"Name"`
	ServerID                 string                   `json:"ServerId"`
	Type                     string                   `json:"Type"`
	MediaType                *string                  `json:"MediaType,omitempty"`
	IsFolder                 *bool                    `json:"IsFolder,omitempty"`
	Overview                 *string                  `json:"Overview,omitempty"`
	ProductionYear           *int32                   `json:"ProductionYear,omitempty"`
	PremiereDate             *string                  `json:"PremiereDate,omitempty"`
	DateLastMediaAdded       *string                  `json:"DateLastMediaAdded,omitempty"`
	CommunityRating          *float64                 `json:"CommunityRating,omitempty"`
	OfficialRating           *string                  `json:"OfficialRating,omitempty"`
	RunTimeTicks             *int64                   `json:"RunTimeTicks,omitempty"`
	IndexNumber              *int32                   `json:"IndexNumber,omitempty"`
	ParentIndexNumber        *int32                   `json:"ParentIndexNumber,omitempty"`
	ParentID                 *string                  `json:"ParentId,omitempty"`
	CanDelete                *bool                    `json:"CanDelete,omitempty"`
	CanDownload              *bool                    `json:"CanDownload,omitempty"`
	SupportsSync             *bool                    `json:"SupportsSync,omitempty"`
	SortName                 *string                  `json:"SortName,omitempty"`
	ForcedSortName           *string                  `json:"ForcedSortName,omitempty"`
	CollectionType           *string                  `json:"CollectionType,omitempty"`
	ImageTags                map[string]string        `json:"ImageTags,omitempty"`
	BackdropImageTags        []string                 `json:"BackdropImageTags,omitempty"`
	PrimaryImageAspectRatio  *float64                 `json:"PrimaryImageAspectRatio,omitempty"`
	ChildCount               *int64                   `json:"ChildCount,omitempty"`
	RecursiveItemCount       *int64                   `json:"RecursiveItemCount,omitempty"`
	SeriesID                 *string                  `json:"SeriesId,omitempty"`
	SeriesName               *string                  `json:"SeriesName,omitempty"`
	SeasonID                 *string                  `json:"SeasonId,omitempty"`
	SeasonName               *string                  `json:"SeasonName,omitempty"`
	AirDays                  *[]string                `json:"AirDays,omitempty"`
	Container                *string                  `json:"Container,omitempty"`
	ProviderIDs              *json.RawMessage         `json:"ProviderIds,omitempty"`
	MediaSourceCount         *int32                   `json:"MediaSourceCount,omitempty"`
	MediaSources             []MediaSourceInfo        `json:"MediaSources,omitempty"`
	MediaStreams             []MediaStreamInfo        `json:"MediaStreams,omitempty"`
	UserData                 *UserItemDataDto         `json:"UserData,omitempty"`
	Path                     *string                  `json:"Path,omitempty"`
	GenreItems               []GenreItem              `json:"GenreItems,omitempty"`
	Genres                   []string                 `json:"Genres,omitempty"`
	Tags                     []string                 `json:"Tags,omitempty"`
	RemoteTrailers           []MediaUrl               `json:"RemoteTrailers,omitempty"`
	People                   []map[string]interface{} `json:"People,omitempty"`
	OriginalTitle            *string                  `json:"OriginalTitle,omitempty"`
	Taglines                 []string                 `json:"Taglines,omitempty"`
	DateCreated              *string                  `json:"DateCreated,omitempty"`
	DateModified             *string                  `json:"DateModified,omitempty"`
	Studios                  []StudioItem             `json:"Studios,omitempty"`
	ProductionLocations      []string                 `json:"ProductionLocations,omitempty"`
	Etag                     *string                  `json:"Etag,omitempty"`
	PresentationUniqueKey    *string                  `json:"PresentationUniqueKey,omitempty"`
	DisplayPreferencesID     *string                  `json:"DisplayPreferencesId,omitempty"`
	ExternalURLs             []ExternalUrl            `json:"ExternalUrls,omitempty"`
	TagItems                 []TagItem                `json:"TagItems,omitempty"`
	LockedFields             []string                 `json:"LockedFields,omitempty"`
	LockData                 *bool                    `json:"LockData,omitempty"`
	LocationType             *string                  `json:"LocationType,omitempty"`
	PlayAccess               *string                  `json:"PlayAccess,omitempty"`
	ChannelID                *string                  `json:"ChannelId,omitempty"`
	SeriesPrimaryImageItemID *string                  `json:"SeriesPrimaryImageItemId,omitempty"`
	SeriesPrimaryImageTag    *string                  `json:"SeriesPrimaryImageTag,omitempty"`
	ParentBackdropItemID     *string                  `json:"ParentBackdropItemId,omitempty"`
	ParentBackdropImageTags  []string                 `json:"ParentBackdropImageTags,omitempty"`
	ParentThumbItemID        *string                  `json:"ParentThumbItemId,omitempty"`
	ParentThumbImageTag      *string                  `json:"ParentThumbImageTag,omitempty"`
	ParentPrimaryImageItemID *string                  `json:"ParentPrimaryImageItemId,omitempty"`
	ParentPrimaryImageTag    *string                  `json:"ParentPrimaryImageTag,omitempty"`
}

type GenreItem struct {
	Name string `json:"Name"`
	ID   string `json:"Id"`
}

// MediaUrl 对应 Emby BaseItemDto.RemoteTrailers 的元素,Url 为预告片直链。
type MediaUrl struct {
	Url  string `json:"Url"`
	Name string `json:"Name,omitempty"`
}

type StudioItem struct {
	Name string `json:"Name"`
	ID   string `json:"Id"`
}

type ExternalUrl struct {
	Name string `json:"Name"`
	Url  string `json:"Url"`
}

type TagItem struct {
	Name string `json:"Name"`
	ID   string `json:"Id"`
}

type ChapterInfo struct {
	ChapterIndex       int    `json:"ChapterIndex"`
	MarkerType         string `json:"MarkerType"`
	Name               string `json:"Name"`
	StartPositionTicks int64  `json:"StartPositionTicks"`
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
	HasMixedProtocols          bool              `json:"HasMixedProtocols"`
	RunTimeTicks               *int64            `json:"RunTimeTicks,omitempty"`
	SupportsDirectPlay         bool              `json:"SupportsDirectPlay"`
	SupportsDirectStream       bool              `json:"SupportsDirectStream"`
	SupportsTranscoding        bool              `json:"SupportsTranscoding"`
	SupportsProbing            bool              `json:"SupportsProbing"`
	IsInfiniteStream           bool              `json:"IsInfiniteStream"`
	RequiresOpening            bool              `json:"RequiresOpening"`
	RequiresClosing            bool              `json:"RequiresClosing"`
	RequiresLooping            bool              `json:"RequiresLooping"`
	MediaStreams               []MediaStreamInfo `json:"MediaStreams"`
	Bitrate                    *int64            `json:"Bitrate,omitempty"`
	RequiredHTTPHeaders        map[string]string `json:"RequiredHttpHeaders"`
	ReadAtNativeFramerate      bool              `json:"ReadAtNativeFramerate"`
	DirectStreamURL            string            `json:"DirectStreamUrl,omitempty"`
	AddApiKeyToDirectStreamURL bool              `json:"AddApiKeyToDirectStreamUrl"`
	ItemID                     string            `json:"ItemId,omitempty"`
	DefaultAudioStreamIndex    *int32            `json:"DefaultAudioStreamIndex,omitempty"`
	DefaultSubtitleStreamIndex *int32            `json:"DefaultSubtitleStreamIndex,omitempty"`
	Formats                    []string          `json:"Formats"`
	AnalyzeDurationMs          *int32            `json:"AnalyzeDurationMs,omitempty"`
	DefaultAudioStreamID       *string           `json:"DefaultAudioStreamId,omitempty"`
	DefaultSubtitleStreamID    *string           `json:"DefaultSubtitleStreamId,omitempty"`
	TranscodingURL             *string           `json:"TranscodingUrl,omitempty"`
	TranscodingSubProtocol     *string           `json:"TranscodingSubProtocol,omitempty"`
	TranscodingContainer       *string           `json:"TranscodingContainer,omitempty"`
	VideoType                  *string           `json:"VideoType,omitempty"`
	Video3DFormat              *string           `json:"Video3DFormat,omitempty"`
	MediaAttachments           []interface{}     `json:"MediaAttachments,omitempty"`

	// M7.4 FYMS 专用画质标签(前端胶囊用);Emby 客户端会忽略未知字段。
	FymsResolution   *string `json:"FymsResolution,omitempty"`
	FymsHdrFormat    *string `json:"FymsHdrFormat,omitempty"`
	FymsVideoCodec   *string `json:"FymsVideoCodec,omitempty"`
	FymsAudioCodec   *string `json:"FymsAudioCodec,omitempty"`
	FymsSource       *string `json:"FymsSource,omitempty"`
	FymsQualityLabel *string `json:"FymsQualityLabel,omitempty"`

	Chapters []ChapterInfo `json:"Chapters"`
}

type MediaStreamInfo struct {
	Codec            string   `json:"Codec"`
	Type             string   `json:"Type"`
	Index            int32    `json:"Index"`
	Language         *string  `json:"Language,omitempty"`
	Title            *string  `json:"Title,omitempty"`
	IsDefault        bool     `json:"IsDefault"`
	IsForced         bool     `json:"IsForced"`
	IsExternal       bool     `json:"IsExternal"`
	Path             *string  `json:"Path,omitempty"`
	DeliveryMethod   *string  `json:"DeliveryMethod,omitempty"`
	DeliveryUrl      *string  `json:"DeliveryUrl,omitempty"`
	Width            *int32   `json:"Width,omitempty"`
	Height           *int32   `json:"Height,omitempty"`
	BitRate          *int64   `json:"BitRate,omitempty"`
	Channels         *int32   `json:"Channels,omitempty"`
	SampleRate       *int32   `json:"SampleRate,omitempty"`
	BitDepth         *int32   `json:"BitDepth,omitempty"`
	PixelFormat      *string  `json:"PixelFormat,omitempty"`
	DisplayTitle     *string  `json:"DisplayTitle,omitempty"`
	Profile          *string  `json:"Profile,omitempty"`
	Level            *float64 `json:"Level,omitempty"`
	IsAVC            *bool    `json:"IsAVC,omitempty"`
	RefFrames        *int32   `json:"RefFrames,omitempty"`
	AverageFrameRate *float64 `json:"AverageFrameRate,omitempty"`
	RealFrameRate    *float64 `json:"RealFrameRate,omitempty"`
	TimeBase         *string  `json:"TimeBase,omitempty"`
	VideoRange       *string  `json:"VideoRange,omitempty"`
	VideoRangeType   *string  `json:"VideoRangeType,omitempty"`
	ColorPrimaries   *string  `json:"ColorPrimaries,omitempty"`
	ColorSpace       *string  `json:"ColorSpace,omitempty"`
	ColorTransfer    *string  `json:"ColorTransfer,omitempty"`
	AspectRatio      *string  `json:"AspectRatio,omitempty"`

	// 以下字段对齐 Emby MediaStream(主要用于外挂字幕),指针类型保证仅在显式赋值时序列化,
	// 不污染视频/音频流。注意:指针指向 false/0 时 omitempty 仍会输出(omitempty 只判断 nil)。
	DisplayLanguage                 *string `json:"DisplayLanguage,omitempty"`
	IsInterlaced                    *bool   `json:"IsInterlaced,omitempty"`
	IsHearingImpaired               *bool   `json:"IsHearingImpaired,omitempty"`
	IsExternalUrl                   *bool   `json:"IsExternalUrl,omitempty"`
	IsChunkedResponse               *bool   `json:"IsChunkedResponse,omitempty"`
	IsTextSubtitleStream            *bool   `json:"IsTextSubtitleStream,omitempty"`
	SupportsExternalStream          *bool   `json:"SupportsExternalStream,omitempty"`
	Protocol                        *string `json:"Protocol,omitempty"`
	ExtendedVideoType               *string `json:"ExtendedVideoType,omitempty"`
	ExtendedVideoSubType            *string `json:"ExtendedVideoSubType,omitempty"`
	ExtendedVideoSubTypeDescription *string `json:"ExtendedVideoSubTypeDescription,omitempty"`
	AttachmentSize                  *int64  `json:"AttachmentSize,omitempty"`
}

type UserItemDataDto struct {
	PlaybackPositionTicks int64    `json:"PlaybackPositionTicks"`
	PlayCount             int32    `json:"PlayCount"`
	IsFavorite            bool     `json:"IsFavorite"`
	Played                bool     `json:"Played"`
	UnplayedItemCount     *int64   `json:"UnplayedItemCount,omitempty"`
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
	PrimaryImagePath       *string
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
	UnplayedItemCount     *int64
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

type ExternalSubtitleRow struct {
	ID             string
	ItemID         string
	MediaVersionID string
	FilePath       string
	Codec          string
	Language       *string
	Title          *string
	IsDefault      bool
	IsForced       bool
}
