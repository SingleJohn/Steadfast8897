package models

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
)

type User struct {
	ID               uuid.UUID  `json:"Id"`
	Name             string     `json:"Name"`
	PasswordHash     string     `json:"-"`
	IsAdmin          bool       `json:"IsAdmin"`
	IsDisabled       bool       `json:"IsDisabled"`
	IsHidden         bool       `json:"IsHidden"`
	LastLoginDate    *time.Time `json:"LastLoginDate,omitempty"`
	LastActivityDate *time.Time `json:"LastActivityDate,omitempty"`
	CreatedAt        time.Time  `json:"CreatedAt"`
	EmbyPasswordHash *string    `json:"-"`
}

type UserPolicy struct {
	UserID                     uuid.UUID `json:"-"`
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

type PolicyUpdate struct {
	IsAdministrator            *bool    `json:"IsAdministrator,omitempty"`
	EnableAllFolders           *bool    `json:"EnableAllFolders,omitempty"`
	EnableRemoteAccess         *bool    `json:"EnableRemoteAccess,omitempty"`
	EnableMediaPlayback        *bool    `json:"EnableMediaPlayback,omitempty"`
	EnableAudioTranscoding     *bool    `json:"EnableAudioPlaybackTranscoding,omitempty"`
	EnableVideoTranscoding     *bool    `json:"EnableVideoPlaybackTranscoding,omitempty"`
	EnablePlaybackRemuxing     *bool    `json:"EnablePlaybackRemuxing,omitempty"`
	EnableContentDeletion      *bool    `json:"EnableContentDeletion,omitempty"`
	EnableContentDownloading   *bool    `json:"EnableContentDownloading,omitempty"`
	EnableSubtitleManagement   *bool    `json:"EnableSubtitleManagement,omitempty"`
	EnableLiveTvAccess         *bool    `json:"EnableLiveTvAccess,omitempty"`
	EnableLiveTvManagement     *bool    `json:"EnableLiveTvManagement,omitempty"`
	EnableUserPreferenceAccess *bool    `json:"EnableUserPreferenceAccess,omitempty"`
	EnableRemoteControl        *bool    `json:"EnableRemoteControlOfOtherUsers,omitempty"`
	EnableSharedDeviceControl  *bool    `json:"EnableSharedDeviceControl,omitempty"`
	RemoteClientBitrateLimit   *int32   `json:"RemoteClientBitrateLimit,omitempty"`
	SimultaneousStreamLimit    *int32   `json:"SimultaneousStreamLimit,omitempty"`
	IsHidden                   *bool    `json:"IsHidden,omitempty"`
	IsHiddenRemotely           *bool    `json:"IsHiddenRemotely,omitempty"`
	IsDisabled                 *bool    `json:"IsDisabled,omitempty"`
	BlockedMediaFolders        []string `json:"BlockedMediaFolders,omitempty"`
	EnabledFolders             []string `json:"EnabledFolders,omitempty"`
}

const userColumns = `id, name, password_hash, is_admin, created_at, is_disabled, is_hidden, last_login_date, last_activity_date, emby_password_hash`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(
		&u.ID, &u.Name, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt,
		&u.IsDisabled, &u.IsHidden, &u.LastLoginDate, &u.LastActivityDate,
		&u.EmbyPasswordHash,
	)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func scanUsers(rows pgx.Rows) ([]User, error) {
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		err := rows.Scan(
			&u.ID, &u.Name, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt,
			&u.IsDisabled, &u.IsHidden, &u.LastLoginDate, &u.LastActivityDate,
			&u.EmbyPasswordHash,
		)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func FindUserByName(ctx context.Context, pool *pgxpool.Pool, name string) (*User, error) {
	u, err := repository.NewUserRepository(pool).GetUserByName(ctx, name)
	return modelUserFromRepo(u), err
}

func FindUserByID(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (*User, error) {
	u, err := repository.NewUserRepository(pool).GetUserByID(ctx, id)
	return modelUserFromRepo(u), err
}

func GetPublicUsers(ctx context.Context, pool *pgxpool.Pool) ([]User, error) {
	users, err := repository.NewUserRepository(pool).ListVisibleUsers(ctx)
	return modelUsersFromRepo(users), err
}

func GetAllUsers(ctx context.Context, pool *pgxpool.Pool) ([]User, error) {
	users, err := repository.NewUserRepository(pool).ListUsers(ctx)
	return modelUsersFromRepo(users), err
}

func GetUserCount(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	return repository.NewUserRepository(pool).CountUsers(ctx)
}

func CreateUser(ctx context.Context, pool *pgxpool.Pool, name, password string, isAdmin bool) (*User, error) {
	u, err := repository.NewUserRepository(pool).CreateUser(ctx, name, password, isAdmin)
	return modelUserFromRepo(u), err
}

func UpdateUser(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, name *string, isHidden *bool) (*User, error) {
	u, err := repository.NewUserRepository(pool).UpdateUser(ctx, id, name, isHidden)
	return modelUserFromRepo(u), err
}

func DeleteUser(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	return repository.NewUserRepository(pool).DeleteUser(ctx, id)
}

func UpdatePassword(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, newPassword string) error {
	return repository.NewUserRepository(pool).UpdatePassword(ctx, id, newPassword)
}

func VerifyPassword(ctx context.Context, pool *pgxpool.Pool, user *User, password string) bool {
	return repository.NewUserRepository(pool).VerifyPassword(ctx, repoUserFromModel(user), password)
}

func SetUserDisabled(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, disabled bool) error {
	return repository.NewUserRepository(pool).SetUserDisabled(ctx, id, disabled)
}

func UpdateLastLogin(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	return repository.NewUserRepository(pool).UpdateLastLogin(ctx, id)
}

func GetUserPolicy(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) (*UserPolicy, error) {
	p, err := repository.NewUserRepository(pool).GetUserPolicy(ctx, userID)
	return modelUserPolicyFromRepo(p), err
}

func UpsertUserPolicy(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, policy *PolicyUpdate) error {
	userRepo := repository.NewUserRepository(pool)
	if err := userRepo.EnsureUserPolicyDefaults(ctx, userID); err != nil {
		return err
	}

	type fieldUpdate struct {
		column string
		value  interface{}
	}
	var updates []fieldUpdate

	if policy.IsAdministrator != nil {
		updates = append(updates, fieldUpdate{"is_administrator", *policy.IsAdministrator})
	}
	if policy.EnableAllFolders != nil {
		updates = append(updates, fieldUpdate{"enable_all_folders", *policy.EnableAllFolders})
	}
	if policy.EnableRemoteAccess != nil {
		updates = append(updates, fieldUpdate{"enable_remote_access", *policy.EnableRemoteAccess})
	}
	if policy.EnableMediaPlayback != nil {
		updates = append(updates, fieldUpdate{"enable_media_playback", *policy.EnableMediaPlayback})
	}
	if policy.EnableAudioTranscoding != nil {
		updates = append(updates, fieldUpdate{"enable_audio_transcoding", *policy.EnableAudioTranscoding})
	}
	if policy.EnableVideoTranscoding != nil {
		updates = append(updates, fieldUpdate{"enable_video_transcoding", *policy.EnableVideoTranscoding})
	}
	if policy.EnablePlaybackRemuxing != nil {
		updates = append(updates, fieldUpdate{"enable_playback_remuxing", *policy.EnablePlaybackRemuxing})
	}
	if policy.EnableContentDeletion != nil {
		updates = append(updates, fieldUpdate{"enable_content_deletion", *policy.EnableContentDeletion})
	}
	if policy.EnableContentDownloading != nil {
		updates = append(updates, fieldUpdate{"enable_content_downloading", *policy.EnableContentDownloading})
	}
	if policy.EnableSubtitleManagement != nil {
		updates = append(updates, fieldUpdate{"enable_subtitle_management", *policy.EnableSubtitleManagement})
	}
	if policy.EnableLiveTvAccess != nil {
		updates = append(updates, fieldUpdate{"enable_live_tv_access", *policy.EnableLiveTvAccess})
	}
	if policy.EnableLiveTvManagement != nil {
		updates = append(updates, fieldUpdate{"enable_live_tv_management", *policy.EnableLiveTvManagement})
	}
	if policy.EnableUserPreferenceAccess != nil {
		updates = append(updates, fieldUpdate{"enable_user_preference_access", *policy.EnableUserPreferenceAccess})
	}
	if policy.EnableRemoteControl != nil {
		updates = append(updates, fieldUpdate{"enable_remote_control", *policy.EnableRemoteControl})
	}
	if policy.EnableSharedDeviceControl != nil {
		updates = append(updates, fieldUpdate{"enable_shared_device_control", *policy.EnableSharedDeviceControl})
	}
	if policy.RemoteClientBitrateLimit != nil {
		updates = append(updates, fieldUpdate{"remote_client_bitrate_limit", *policy.RemoteClientBitrateLimit})
	}
	if policy.SimultaneousStreamLimit != nil {
		updates = append(updates, fieldUpdate{"simultaneous_stream_limit", *policy.SimultaneousStreamLimit})
	}

	for _, u := range updates {
		sql := fmt.Sprintf("UPDATE user_policies SET %s = $1 WHERE user_id = $2", u.column)
		if _, err := pool.Exec(ctx, sql, u.value, userID); err != nil {
			return err
		}
	}

	if policy.BlockedMediaFolders != nil {
		if err := userRepo.UpdateUserPolicyBlockedFolders(ctx, userID, policy.BlockedMediaFolders); err != nil {
			return err
		}
	}
	if policy.EnabledFolders != nil {
		if err := userRepo.UpdateUserPolicyEnabledFolders(ctx, userID, policy.EnabledFolders); err != nil {
			return err
		}
	}

	if policy.IsAdministrator != nil {
		_, err := pool.Exec(ctx, "UPDATE users SET is_admin = $1 WHERE id = $2",
			*policy.IsAdministrator, userID)
		if err != nil {
			return err
		}
	}
	if policy.IsHidden != nil || policy.IsHiddenRemotely != nil {
		hidden := false
		if policy.IsHidden != nil {
			hidden = *policy.IsHidden
		} else if policy.IsHiddenRemotely != nil {
			hidden = *policy.IsHiddenRemotely
		}
		pool.Exec(ctx, "UPDATE users SET is_hidden = $1 WHERE id = $2", hidden, userID)
	}
	if policy.IsDisabled != nil {
		pool.Exec(ctx, "UPDATE users SET is_disabled = $1 WHERE id = $2", *policy.IsDisabled, userID)
	}

	return nil
}

func FormatPolicyResponse(policy *UserPolicy, isAdmin bool) map[string]interface{} {
	boolOr := func(val *bool, def bool) bool {
		if val != nil {
			return *val
		}
		return def
	}
	int32Or := func(val *int32, def int32) int32 {
		if val != nil {
			return *val
		}
		return def
	}

	if policy != nil {
		blockedFolders := policy.BlockedMediaFolders
		if blockedFolders == nil {
			blockedFolders = []string{}
		}
		enabledFolders := policy.EnabledFolders
		if enabledFolders == nil {
			enabledFolders = []string{}
		}
		return map[string]interface{}{
			"IsAdministrator":                 policy.IsAdministrator,
			"IsDisabled":                      false,
			"IsHidden":                        false,
			"EnableAllFolders":                policy.EnableAllFolders,
			"BlockedMediaFolders":             blockedFolders,
			"EnabledFolders":                  enabledFolders,
			"EnableRemoteAccess":              policy.EnableRemoteAccess,
			"EnableMediaPlayback":             policy.EnableMediaPlayback,
			"EnableAudioPlaybackTranscoding":  policy.EnableAudioTranscoding,
			"EnableVideoPlaybackTranscoding":  policy.EnableVideoTranscoding,
			"EnablePlaybackRemuxing":          policy.EnablePlaybackRemuxing,
			"EnableContentDeletion":           policy.EnableContentDeletion,
			"EnableContentDownloading":        policy.EnableContentDownloading,
			"EnableSubtitleDownloading":       policy.EnableSubtitleManagement,
			"EnableSubtitleManagement":        policy.EnableSubtitleManagement,
			"EnableLiveTvAccess":              policy.EnableLiveTvAccess,
			"EnableLiveTvManagement":          policy.EnableLiveTvManagement,
			"EnableUserPreferenceAccess":      policy.EnableUserPreferenceAccess,
			"EnableRemoteControlOfOtherUsers": policy.EnableRemoteControl,
			"EnableSharedDeviceControl":       policy.EnableSharedDeviceControl,
			"MaxParentalRating":               policy.MaxParentalRating,
			"RemoteClientBitrateLimit":        policy.RemoteClientBitrateLimit,
			"SimultaneousStreamLimit":         policy.SimultaneousStreamLimit,
			"EnableSyncTranscoding":           true,
			"EnableMediaConversion":           true,
		}
	}

	_ = boolOr
	_ = int32Or

	return map[string]interface{}{
		"IsAdministrator":                 isAdmin,
		"IsDisabled":                      false,
		"IsHidden":                        false,
		"EnableAllFolders":                true,
		"EnableRemoteAccess":              true,
		"EnableMediaPlayback":             true,
		"EnableAudioPlaybackTranscoding":  true,
		"EnableVideoPlaybackTranscoding":  true,
		"EnablePlaybackRemuxing":          true,
		"EnableContentDeletion":           isAdmin,
		"EnableContentDownloading":        true,
		"EnableSubtitleDownloading":       true,
		"EnableSubtitleManagement":        true,
		"EnableLiveTvAccess":              true,
		"EnableLiveTvManagement":          false,
		"EnableUserPreferenceAccess":      true,
		"EnableRemoteControlOfOtherUsers": false,
		"EnableSharedDeviceControl":       false,
		"MaxParentalRating":               nil,
		"RemoteClientBitrateLimit":        0,
		"SimultaneousStreamLimit":         0,
		"EnableSyncTranscoding":           true,
		"EnableMediaConversion":           true,
	}
}

func GetUserLibraryAccess(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) ([]uuid.UUID, error) {
	return repository.NewUserRepository(pool).ListUserLibraryAccess(ctx, userID)
}

func SetUserLibraryAccess(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, libraryIDs []uuid.UUID) error {
	return repository.NewUserRepository(pool).SetUserLibraryAccess(ctx, userID, libraryIDs)
}

func modelUsersFromRepo(users []repository.User) []User {
	result := make([]User, 0, len(users))
	for _, u := range users {
		result = append(result, *modelUserFromRepo(&u))
	}
	return result
}

func modelUserFromRepo(u *repository.User) *User {
	if u == nil {
		return nil
	}
	return &User{
		ID:               u.ID,
		Name:             u.Name,
		PasswordHash:     u.PasswordHash,
		IsAdmin:          u.IsAdmin,
		IsDisabled:       u.IsDisabled,
		IsHidden:         u.IsHidden,
		LastLoginDate:    u.LastLoginDate,
		LastActivityDate: u.LastActivityDate,
		CreatedAt:        u.CreatedAt,
		EmbyPasswordHash: u.EmbyPasswordHash,
	}
}

func repoUserFromModel(u *User) *repository.User {
	if u == nil {
		return nil
	}
	return &repository.User{
		ID:               u.ID,
		Name:             u.Name,
		PasswordHash:     u.PasswordHash,
		IsAdmin:          u.IsAdmin,
		IsDisabled:       u.IsDisabled,
		IsHidden:         u.IsHidden,
		LastLoginDate:    u.LastLoginDate,
		LastActivityDate: u.LastActivityDate,
		CreatedAt:        u.CreatedAt,
		EmbyPasswordHash: u.EmbyPasswordHash,
	}
}

func modelUserPolicyFromRepo(p *repository.UserPolicy) *UserPolicy {
	if p == nil {
		return nil
	}
	return &UserPolicy{
		UserID:                     p.UserID,
		IsAdministrator:            p.IsAdministrator,
		EnableAllFolders:           p.EnableAllFolders,
		EnableRemoteAccess:         p.EnableRemoteAccess,
		EnableMediaPlayback:        p.EnableMediaPlayback,
		EnableAudioTranscoding:     p.EnableAudioTranscoding,
		EnableVideoTranscoding:     p.EnableVideoTranscoding,
		EnablePlaybackRemuxing:     p.EnablePlaybackRemuxing,
		EnableContentDeletion:      p.EnableContentDeletion,
		EnableContentDownloading:   p.EnableContentDownloading,
		EnableSubtitleManagement:   p.EnableSubtitleManagement,
		EnableLiveTvAccess:         p.EnableLiveTvAccess,
		EnableLiveTvManagement:     p.EnableLiveTvManagement,
		EnableUserPreferenceAccess: p.EnableUserPreferenceAccess,
		EnableRemoteControl:        p.EnableRemoteControl,
		EnableSharedDeviceControl:  p.EnableSharedDeviceControl,
		MaxParentalRating:          p.MaxParentalRating,
		RemoteClientBitrateLimit:   p.RemoteClientBitrateLimit,
		SimultaneousStreamLimit:    p.SimultaneousStreamLimit,
		InvalidLoginAttemptCount:   p.InvalidLoginAttemptCount,
		LoginAttemptsBeforeLockout: p.LoginAttemptsBeforeLockout,
		BlockedMediaFolders:        p.BlockedMediaFolders,
		EnabledFolders:             p.EnabledFolders,
	}
}
