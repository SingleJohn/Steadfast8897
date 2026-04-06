package models

import (
	"context"
	"crypto/sha1"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
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
	IsAdministrator            *bool  `json:"IsAdministrator,omitempty"`
	EnableAllFolders           *bool  `json:"EnableAllFolders,omitempty"`
	EnableRemoteAccess         *bool  `json:"EnableRemoteAccess,omitempty"`
	EnableMediaPlayback        *bool  `json:"EnableMediaPlayback,omitempty"`
	EnableAudioTranscoding     *bool  `json:"EnableAudioPlaybackTranscoding,omitempty"`
	EnableVideoTranscoding     *bool  `json:"EnableVideoPlaybackTranscoding,omitempty"`
	EnablePlaybackRemuxing     *bool  `json:"EnablePlaybackRemuxing,omitempty"`
	EnableContentDeletion      *bool  `json:"EnableContentDeletion,omitempty"`
	EnableContentDownloading   *bool  `json:"EnableContentDownloading,omitempty"`
	EnableSubtitleManagement   *bool  `json:"EnableSubtitleManagement,omitempty"`
	EnableLiveTvAccess         *bool  `json:"EnableLiveTvAccess,omitempty"`
	EnableLiveTvManagement     *bool  `json:"EnableLiveTvManagement,omitempty"`
	EnableUserPreferenceAccess *bool  `json:"EnableUserPreferenceAccess,omitempty"`
	EnableRemoteControl        *bool  `json:"EnableRemoteControlOfOtherUsers,omitempty"`
	EnableSharedDeviceControl  *bool  `json:"EnableSharedDeviceControl,omitempty"`
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
	row := pool.QueryRow(ctx, "SELECT "+userColumns+" FROM users WHERE name = $1", name)
	u, err := scanUser(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func FindUserByID(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (*User, error) {
	row := pool.QueryRow(ctx, "SELECT "+userColumns+" FROM users WHERE id = $1", id)
	u, err := scanUser(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func GetPublicUsers(ctx context.Context, pool *pgxpool.Pool) ([]User, error) {
	rows, err := pool.Query(ctx,
		"SELECT "+userColumns+" FROM users WHERE is_hidden = FALSE AND is_disabled = FALSE ORDER BY name")
	if err != nil {
		return nil, err
	}
	return scanUsers(rows)
}

func GetAllUsers(ctx context.Context, pool *pgxpool.Pool) ([]User, error) {
	rows, err := pool.Query(ctx, "SELECT "+userColumns+" FROM users ORDER BY name")
	if err != nil {
		return nil, err
	}
	return scanUsers(rows)
}

func GetUserCount(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var count int64
	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

func CreateUser(ctx context.Context, pool *pgxpool.Pool, name, password string, isAdmin bool) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return nil, fmt.Errorf("bcrypt: %w", err)
	}

	row := pool.QueryRow(ctx,
		"INSERT INTO users (name, password_hash, is_admin) VALUES ($1, $2, $3) RETURNING "+userColumns,
		name, string(hash), isAdmin)
	u, err := scanUser(row)
	if err != nil {
		return nil, err
	}

	_, err = pool.Exec(ctx,
		"INSERT INTO user_policies (user_id, is_administrator, enable_content_deletion) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING",
		u.ID, isAdmin, isAdmin)
	if err != nil {
		return nil, err
	}

	return u, nil
}

func UpdateUser(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, name *string, isHidden *bool) (*User, error) {
	if name != nil {
		_, err := pool.Exec(ctx, "UPDATE users SET name = $1 WHERE id = $2", *name, id)
		if err != nil {
			return nil, err
		}
	}
	if isHidden != nil {
		_, err := pool.Exec(ctx, "UPDATE users SET is_hidden = $1 WHERE id = $2", *isHidden, id)
		if err != nil {
			return nil, err
		}
	}
	return FindUserByID(ctx, pool, id)
}

func DeleteUser(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	_, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", id)
	return err
}

func UpdatePassword(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), 10)
	if err != nil {
		return fmt.Errorf("bcrypt: %w", err)
	}
	_, err = pool.Exec(ctx, "UPDATE users SET password_hash = $1 WHERE id = $2", string(hash), id)
	return err
}

func VerifyPassword(ctx context.Context, pool *pgxpool.Pool, user *User, password string) bool {
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) == nil {
		return true
	}

	if user.EmbyPasswordHash != nil && *user.EmbyPasswordHash != "" {
		h := sha1.Sum([]byte(password))
		result := fmt.Sprintf("%X", h)
		if result == *user.EmbyPasswordHash {
			newHash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
			if err == nil {
				pool.Exec(ctx,
					"UPDATE users SET password_hash = $1, emby_password_hash = NULL WHERE id = $2",
					string(newHash), user.ID)
				slog.Info("Upgraded Emby password to bcrypt", "user", user.Name)
			}
			return true
		}
	}
	return false
}

func SetUserDisabled(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, disabled bool) error {
	_, err := pool.Exec(ctx, "UPDATE users SET is_disabled = $1 WHERE id = $2", disabled, id)
	return err
}

func UpdateLastLogin(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	_, err := pool.Exec(ctx,
		"UPDATE users SET last_login_date = NOW(), last_activity_date = NOW() WHERE id = $1", id)
	return err
}

func GetUserPolicy(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) (*UserPolicy, error) {
	var p UserPolicy
	err := pool.QueryRow(ctx, "SELECT * FROM user_policies WHERE user_id = $1", userID).Scan(
		&p.UserID, &p.IsAdministrator, &p.EnableAllFolders, &p.EnableRemoteAccess,
		&p.EnableMediaPlayback, &p.EnableAudioTranscoding, &p.EnableVideoTranscoding,
		&p.EnablePlaybackRemuxing, &p.EnableContentDeletion, &p.EnableContentDownloading,
		&p.EnableSubtitleManagement, &p.EnableLiveTvAccess, &p.EnableLiveTvManagement,
		&p.EnableUserPreferenceAccess, &p.EnableRemoteControl, &p.EnableSharedDeviceControl,
		&p.MaxParentalRating, &p.RemoteClientBitrateLimit, &p.SimultaneousStreamLimit,
		&p.InvalidLoginAttemptCount, &p.LoginAttemptsBeforeLockout,
		&p.BlockedMediaFolders, &p.EnabledFolders,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func UpsertUserPolicy(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, policy *PolicyUpdate) error {
	_, err := pool.Exec(ctx,
		"INSERT INTO user_policies (user_id) VALUES ($1) ON CONFLICT DO NOTHING", userID)
	if err != nil {
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
		_, err := pool.Exec(ctx, "UPDATE user_policies SET blocked_media_folders = $1 WHERE user_id = $2",
			policy.BlockedMediaFolders, userID)
		if err != nil {
			return err
		}
	}
	if policy.EnabledFolders != nil {
		_, err := pool.Exec(ctx, "UPDATE user_policies SET enabled_folders = $1 WHERE user_id = $2",
			policy.EnabledFolders, userID)
		if err != nil {
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
			"IsAdministrator":                  policy.IsAdministrator,
			"IsDisabled":                       false,
			"IsHidden":                         false,
			"EnableAllFolders":                 policy.EnableAllFolders,
			"BlockedMediaFolders":              blockedFolders,
			"EnabledFolders":                   enabledFolders,
			"EnableRemoteAccess":               policy.EnableRemoteAccess,
			"EnableMediaPlayback":              policy.EnableMediaPlayback,
			"EnableAudioPlaybackTranscoding":   policy.EnableAudioTranscoding,
			"EnableVideoPlaybackTranscoding":   policy.EnableVideoTranscoding,
			"EnablePlaybackRemuxing":           policy.EnablePlaybackRemuxing,
			"EnableContentDeletion":            policy.EnableContentDeletion,
			"EnableContentDownloading":         policy.EnableContentDownloading,
			"EnableSubtitleDownloading":        policy.EnableSubtitleManagement,
			"EnableSubtitleManagement":         policy.EnableSubtitleManagement,
			"EnableLiveTvAccess":               policy.EnableLiveTvAccess,
			"EnableLiveTvManagement":           policy.EnableLiveTvManagement,
			"EnableUserPreferenceAccess":       policy.EnableUserPreferenceAccess,
			"EnableRemoteControlOfOtherUsers":  policy.EnableRemoteControl,
			"EnableSharedDeviceControl":        policy.EnableSharedDeviceControl,
			"MaxParentalRating":                policy.MaxParentalRating,
			"RemoteClientBitrateLimit":         policy.RemoteClientBitrateLimit,
			"SimultaneousStreamLimit":          policy.SimultaneousStreamLimit,
			"EnableSyncTranscoding":            true,
			"EnableMediaConversion":            true,
		}
	}

	_ = boolOr
	_ = int32Or

	return map[string]interface{}{
		"IsAdministrator":                  isAdmin,
		"IsDisabled":                       false,
		"IsHidden":                         false,
		"EnableAllFolders":                 true,
		"EnableRemoteAccess":               true,
		"EnableMediaPlayback":              true,
		"EnableAudioPlaybackTranscoding":   true,
		"EnableVideoPlaybackTranscoding":   true,
		"EnablePlaybackRemuxing":           true,
		"EnableContentDeletion":            isAdmin,
		"EnableContentDownloading":         true,
		"EnableSubtitleDownloading":        true,
		"EnableSubtitleManagement":         true,
		"EnableLiveTvAccess":               true,
		"EnableLiveTvManagement":           false,
		"EnableUserPreferenceAccess":       true,
		"EnableRemoteControlOfOtherUsers":  false,
		"EnableSharedDeviceControl":        false,
		"MaxParentalRating":                nil,
		"RemoteClientBitrateLimit":         0,
		"SimultaneousStreamLimit":          0,
		"EnableSyncTranscoding":            true,
		"EnableMediaConversion":            true,
	}
}

func GetUserLibraryAccess(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := pool.Query(ctx,
		"SELECT library_id FROM user_library_access WHERE user_id = $1", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func SetUserLibraryAccess(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, libraryIDs []uuid.UUID) error {
	_, err := pool.Exec(ctx, "DELETE FROM user_library_access WHERE user_id = $1", userID)
	if err != nil {
		return err
	}
	for _, libID := range libraryIDs {
		_, err := pool.Exec(ctx,
			"INSERT INTO user_library_access (user_id, library_id) VALUES ($1, $2)",
			userID, libID)
		if err != nil {
			return err
		}
	}
	return nil
}
