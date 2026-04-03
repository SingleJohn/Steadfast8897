package models

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
	token := strings.ReplaceAll(uuid.New().String(), "-", "")
	_, err := pool.Exec(ctx,
		"INSERT INTO access_tokens (token, user_id, device_id, device_name, app_name, app_version) VALUES ($1, $2, $3, $4, $5, $6)",
		token, userID, deviceID, deviceName, appName, appVersion)
	if err != nil {
		return "", err
	}
	return token, nil
}

func FindByToken(ctx context.Context, pool *pgxpool.Pool, token string) (*AccessToken, error) {
	var t AccessToken
	err := pool.QueryRow(ctx,
		"SELECT token, user_id, device_id, device_name, app_name, app_version, created_at FROM access_tokens WHERE token = $1",
		token).Scan(&t.Token, &t.UserID, &t.DeviceID, &t.DeviceName, &t.AppName, &t.AppVersion, &t.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func DeleteToken(ctx context.Context, pool *pgxpool.Pool, token string) error {
	_, err := pool.Exec(ctx, "DELETE FROM access_tokens WHERE token = $1", token)
	return err
}

func DeleteUserTokens(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) error {
	_, err := pool.Exec(ctx, "DELETE FROM access_tokens WHERE user_id = $1", userID)
	return err
}
