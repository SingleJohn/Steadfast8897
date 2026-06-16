package models

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
	"fyms/internal/repository"
)

func GetExternalSubtitlesForMediaVersion(ctx context.Context, pool *pgxpool.Pool, mediaVersionID string) ([]dto.ExternalSubtitleRow, error) {
	return repository.NewItemHelperRepository(pool).ListExternalSubtitlesForMediaVersion(ctx, mediaVersionID)
}
