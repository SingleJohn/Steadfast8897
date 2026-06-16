package repository

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID               uuid.UUID
	Name             string
	PasswordHash     string
	IsAdmin          bool
	IsDisabled       bool
	IsHidden         bool
	LastLoginDate    *time.Time
	LastActivityDate *time.Time
	CreatedAt        time.Time
	EmbyPasswordHash *string
}

type UserPolicy struct {
	UserID                     uuid.UUID
	IsAdministrator            bool
	EnableAllFolders           bool
	EnableRemoteAccess         bool
	EnableMediaPlayback        bool
	EnableAudioTranscoding     bool
	EnableVideoTranscoding     bool
	EnablePlaybackRemuxing     bool
	EnableContentDeletion      bool
	EnableContentDownloading   bool
	EnableSubtitleManagement   bool
	EnableLiveTvAccess         bool
	EnableLiveTvManagement     bool
	EnableUserPreferenceAccess bool
	EnableRemoteControl        bool
	EnableSharedDeviceControl  bool
	MaxParentalRating          *int32
	RemoteClientBitrateLimit   int32
	SimultaneousStreamLimit    int32
	InvalidLoginAttemptCount   int32
	LoginAttemptsBeforeLockout int32
	BlockedMediaFolders        []string
	EnabledFolders             []string
}

type AccessToken struct {
	Token      string
	UserID     uuid.UUID
	DeviceID   string
	DeviceName string
	AppName    string
	AppVersion string
	CreatedAt  time.Time
}

type Library struct {
	ID               uuid.UUID
	Name             string
	CollectionType   string
	Paths            []string
	CreatedAt        time.Time
	PrimaryImagePath *string
	PrimaryImageTag  *string
	SortOrder        int
	ScrapeConfig     *string
}

type DisplayOrderEntry struct {
	Kind string
	ID   string
}
