package models

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
)

type AccessToken struct {
	Token      string    `json:"Token"`
	UserID     uuid.UUID `json:"UserId"`
	DeviceID   string    `json:"DeviceId"`
	DeviceName string    `json:"DeviceName"`
	AppName    string    `json:"AppName"`
	AppVersion string    `json:"AppVersion"`
	CreatedAt  time.Time `json:"CreatedAt"`
}

func CreateAccessToken(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, deviceID, deviceName, appName, appVersion string) (string, error) {
	return repository.NewSessionRepository(pool).CreateAccessToken(ctx, userID, deviceID, deviceName, appName, appVersion)
}

func FindByToken(ctx context.Context, pool *pgxpool.Pool, token string) (*AccessToken, error) {
	t, err := repository.NewSessionRepository(pool).GetAccessToken(ctx, token)
	if t == nil || err != nil {
		return nil, err
	}
	return &AccessToken{
		Token:      t.Token,
		UserID:     t.UserID,
		DeviceID:   t.DeviceID,
		DeviceName: t.DeviceName,
		AppName:    t.AppName,
		AppVersion: t.AppVersion,
		CreatedAt:  t.CreatedAt,
	}, nil
}

func DeleteToken(ctx context.Context, pool *pgxpool.Pool, token string) error {
	return repository.NewSessionRepository(pool).DeleteAccessToken(ctx, token)
}

func DeleteUserTokens(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) error {
	return repository.NewSessionRepository(pool).DeleteAccessTokensByUserID(ctx, userID)
}
