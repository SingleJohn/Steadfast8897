package repository

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/db/gen"
)

type SessionRepository struct {
	queries *dbgen.Queries
}

func NewSessionRepository(pool *pgxpool.Pool) *SessionRepository {
	return &SessionRepository{queries: dbgen.New(pool)}
}

func (r *SessionRepository) CreateAccessToken(ctx context.Context, userID uuid.UUID, deviceID, deviceName, appName, appVersion string) (string, error) {
	token := strings.ReplaceAll(uuid.New().String(), "-", "")
	err := r.queries.CreateAccessToken(ctx, dbgen.CreateAccessTokenParams{
		Token:      token,
		UserID:     toPGUUID(userID),
		DeviceID:   deviceID,
		DeviceName: deviceName,
		AppName:    appName,
		AppVersion: appVersion,
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

func (r *SessionRepository) GetAccessToken(ctx context.Context, token string) (*AccessToken, error) {
	row, err := r.queries.GetAccessToken(ctx, token)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &AccessToken{
		Token:      row.Token,
		UserID:     fromPGUUID(row.UserID),
		DeviceID:   row.DeviceID,
		DeviceName: row.DeviceName,
		AppName:    row.AppName,
		AppVersion: row.AppVersion,
		CreatedAt:  row.CreatedAt.Time,
	}, nil
}

func (r *SessionRepository) DeleteAccessToken(ctx context.Context, token string) error {
	return r.queries.DeleteAccessToken(ctx, token)
}

func (r *SessionRepository) DeleteAccessTokensByUserID(ctx context.Context, userID uuid.UUID) error {
	return r.queries.DeleteAccessTokensByUserID(ctx, toPGUUID(userID))
}
