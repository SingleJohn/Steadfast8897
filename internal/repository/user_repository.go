package repository

import (
	"context"
	"crypto/sha1"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"fyms/internal/db/gen"
)

type UserRepository struct {
	queries *dbgen.Queries
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{queries: dbgen.New(pool)}
}

func (r *UserRepository) GetUserByName(ctx context.Context, name string) (*User, error) {
	row, err := r.queries.GetUserByName(ctx, name)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return mapUser(row), nil
}

func (r *UserRepository) GetUserByID(ctx context.Context, id uuid.UUID) (*User, error) {
	row, err := r.queries.GetUserByID(ctx, toPGUUID(id))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return mapUser(row), nil
}

func (r *UserRepository) ListVisibleUsers(ctx context.Context) ([]User, error) {
	rows, err := r.queries.ListVisibleUsers(ctx)
	if err != nil {
		return nil, err
	}
	return mapUsers(rows), nil
}

func (r *UserRepository) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := r.queries.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	return mapUsers(rows), nil
}

func (r *UserRepository) CountUsers(ctx context.Context) (int64, error) {
	return r.queries.CountUsers(ctx)
}

func (r *UserRepository) CreateUser(ctx context.Context, name, password string, isAdmin bool) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return nil, fmt.Errorf("bcrypt: %w", err)
	}

	row, err := r.queries.CreateUser(ctx, dbgen.CreateUserParams{
		Name:         name,
		PasswordHash: string(hash),
		IsAdmin:      isAdmin,
	})
	if err != nil {
		return nil, err
	}

	user := mapUser(row)
	if err := r.queries.EnsureUserPolicy(ctx, dbgen.EnsureUserPolicyParams{
		UserID:                toPGUUID(user.ID),
		IsAdministrator:       isAdmin,
		EnableContentDeletion: isAdmin,
	}); err != nil {
		return nil, err
	}
	return user, nil
}

func (r *UserRepository) UpdateUser(ctx context.Context, id uuid.UUID, name *string, isHidden *bool) (*User, error) {
	pgID := toPGUUID(id)
	if name != nil {
		if err := r.queries.UpdateUserName(ctx, dbgen.UpdateUserNameParams{Name: *name, ID: pgID}); err != nil {
			return nil, err
		}
	}
	if isHidden != nil {
		if err := r.queries.UpdateUserHidden(ctx, dbgen.UpdateUserHiddenParams{IsHidden: *isHidden, ID: pgID}); err != nil {
			return nil, err
		}
	}
	return r.GetUserByID(ctx, id)
}

func (r *UserRepository) DeleteUser(ctx context.Context, id uuid.UUID) error {
	return r.queries.DeleteUser(ctx, toPGUUID(id))
}

func (r *UserRepository) UpdatePassword(ctx context.Context, id uuid.UUID, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), 10)
	if err != nil {
		return fmt.Errorf("bcrypt: %w", err)
	}
	return r.queries.UpdateUserPasswordHash(ctx, dbgen.UpdateUserPasswordHashParams{
		PasswordHash: string(hash),
		ID:           toPGUUID(id),
	})
}

func (r *UserRepository) VerifyPassword(ctx context.Context, user *User, password string) bool {
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) == nil {
		return true
	}

	if user.EmbyPasswordHash != nil && *user.EmbyPasswordHash != "" {
		h := sha1.Sum([]byte(password))
		result := fmt.Sprintf("%X", h)
		if result == *user.EmbyPasswordHash {
			newHash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
			if err == nil {
				_ = r.queries.UpgradeEmbyPasswordHash(ctx, dbgen.UpgradeEmbyPasswordHashParams{
					PasswordHash: string(newHash),
					ID:           toPGUUID(user.ID),
				})
				slog.Info("Upgraded Emby password to bcrypt", "user", user.Name)
			}
			return true
		}
	}
	return false
}

func (r *UserRepository) SetUserDisabled(ctx context.Context, id uuid.UUID, disabled bool) error {
	return r.queries.SetUserDisabled(ctx, dbgen.SetUserDisabledParams{
		IsDisabled: disabled,
		ID:         toPGUUID(id),
	})
}

func (r *UserRepository) UpdateUserAdmin(ctx context.Context, id uuid.UUID, isAdmin bool) error {
	return r.queries.UpdateUserAdmin(ctx, dbgen.UpdateUserAdminParams{
		IsAdmin: isAdmin,
		ID:      toPGUUID(id),
	})
}

func (r *UserRepository) UpdateLastLogin(ctx context.Context, id uuid.UUID) error {
	return r.queries.UpdateLastLogin(ctx, toPGUUID(id))
}

func (r *UserRepository) GetUserPolicy(ctx context.Context, userID uuid.UUID) (*UserPolicy, error) {
	row, err := r.queries.GetUserPolicy(ctx, toPGUUID(userID))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return mapUserPolicy(row), nil
}

func (r *UserRepository) EnsureUserPolicyDefaults(ctx context.Context, userID uuid.UUID) error {
	return r.queries.EnsureUserPolicyDefaults(ctx, toPGUUID(userID))
}

func (r *UserRepository) UpdateUserPolicyBlockedFolders(ctx context.Context, userID uuid.UUID, folders []string) error {
	return r.queries.UpdateUserPolicyBlockedFolders(ctx, dbgen.UpdateUserPolicyBlockedFoldersParams{
		BlockedMediaFolders: folders,
		UserID:              toPGUUID(userID),
	})
}

func (r *UserRepository) UpdateUserPolicyEnabledFolders(ctx context.Context, userID uuid.UUID, folders []string) error {
	return r.queries.UpdateUserPolicyEnabledFolders(ctx, dbgen.UpdateUserPolicyEnabledFoldersParams{
		EnabledFolders: folders,
		UserID:         toPGUUID(userID),
	})
}

func (r *UserRepository) UpdateUserPolicyFields(ctx context.Context, userID uuid.UUID, update UserPolicyFieldUpdate) error {
	return r.queries.UpdateUserPolicyFields(ctx, dbgen.UpdateUserPolicyFieldsParams{
		IsAdministrator:            optionalBool(update.IsAdministrator),
		EnableAllFolders:           optionalBool(update.EnableAllFolders),
		EnableRemoteAccess:         optionalBool(update.EnableRemoteAccess),
		EnableMediaPlayback:        optionalBool(update.EnableMediaPlayback),
		EnableAudioTranscoding:     optionalBool(update.EnableAudioTranscoding),
		EnableVideoTranscoding:     optionalBool(update.EnableVideoTranscoding),
		EnablePlaybackRemuxing:     optionalBool(update.EnablePlaybackRemuxing),
		EnableContentDeletion:      optionalBool(update.EnableContentDeletion),
		EnableContentDownloading:   optionalBool(update.EnableContentDownloading),
		EnableSubtitleManagement:   optionalBool(update.EnableSubtitleManagement),
		EnableLiveTvAccess:         optionalBool(update.EnableLiveTvAccess),
		EnableLiveTvManagement:     optionalBool(update.EnableLiveTvManagement),
		EnableUserPreferenceAccess: optionalBool(update.EnableUserPreferenceAccess),
		EnableRemoteControl:        optionalBool(update.EnableRemoteControl),
		EnableSharedDeviceControl:  optionalBool(update.EnableSharedDeviceControl),
		RemoteClientBitrateLimit:   optionalInt32(update.RemoteClientBitrateLimit),
		SimultaneousStreamLimit:    optionalInt32(update.SimultaneousStreamLimit),
		UserID:                     toPGUUID(userID),
	})
}

func (r *UserRepository) GetItemLibraryIDForAccess(ctx context.Context, itemID string) (string, error) {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return "", err
	}
	return r.queries.GetItemLibraryIDForAccess(ctx, toPGUUID(uid))
}

func (r *UserRepository) ListUserLibraryAccess(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.queries.ListUserLibraryAccess(ctx, toPGUUID(userID))
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, fromPGUUID(row))
	}
	return ids, nil
}

func (r *UserRepository) SetUserLibraryAccess(ctx context.Context, userID uuid.UUID, libraryIDs []uuid.UUID) error {
	if err := r.queries.ClearUserLibraryAccess(ctx, toPGUUID(userID)); err != nil {
		return err
	}
	for _, libID := range libraryIDs {
		if err := r.queries.AddUserLibraryAccess(ctx, dbgen.AddUserLibraryAccessParams{
			UserID:    toPGUUID(userID),
			LibraryID: toPGUUID(libID),
		}); err != nil {
			return err
		}
	}
	return nil
}

func mapUsers(rows []dbgen.User) []User {
	users := make([]User, 0, len(rows))
	for _, row := range rows {
		users = append(users, *mapUser(row))
	}
	return users
}

func mapUser(row dbgen.User) *User {
	return &User{
		ID:               fromPGUUID(row.ID),
		Name:             row.Name,
		PasswordHash:     row.PasswordHash,
		IsAdmin:          row.IsAdmin,
		IsDisabled:       row.IsDisabled,
		IsHidden:         row.IsHidden,
		LastLoginDate:    ptrTime(row.LastLoginDate),
		LastActivityDate: ptrTime(row.LastActivityDate),
		CreatedAt:        row.CreatedAt.Time,
		EmbyPasswordHash: ptrText(row.EmbyPasswordHash),
	}
}

func mapUserPolicy(row dbgen.UserPolicy) *UserPolicy {
	return &UserPolicy{
		UserID:                     fromPGUUID(row.UserID),
		IsAdministrator:            row.IsAdministrator,
		EnableAllFolders:           row.EnableAllFolders,
		EnableRemoteAccess:         row.EnableRemoteAccess,
		EnableMediaPlayback:        row.EnableMediaPlayback,
		EnableAudioTranscoding:     row.EnableAudioTranscoding,
		EnableVideoTranscoding:     row.EnableVideoTranscoding,
		EnablePlaybackRemuxing:     row.EnablePlaybackRemuxing,
		EnableContentDeletion:      row.EnableContentDeletion,
		EnableContentDownloading:   row.EnableContentDownloading,
		EnableSubtitleManagement:   row.EnableSubtitleManagement,
		EnableLiveTvAccess:         row.EnableLiveTvAccess,
		EnableLiveTvManagement:     row.EnableLiveTvManagement,
		EnableUserPreferenceAccess: row.EnableUserPreferenceAccess,
		EnableRemoteControl:        row.EnableRemoteControl,
		EnableSharedDeviceControl:  row.EnableSharedDeviceControl,
		MaxParentalRating:          ptrInt32(row.MaxParentalRating),
		RemoteClientBitrateLimit:   row.RemoteClientBitrateLimit,
		SimultaneousStreamLimit:    row.SimultaneousStreamLimit,
		InvalidLoginAttemptCount:   row.InvalidLoginAttemptCount,
		LoginAttemptsBeforeLockout: row.LoginAttemptsBeforeLockout,
		BlockedMediaFolders:        row.BlockedMediaFolders,
		EnabledFolders:             row.EnabledFolders,
	}
}
