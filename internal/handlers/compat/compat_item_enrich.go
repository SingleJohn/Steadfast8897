package compat

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
	"fyms/internal/handlers/shared"
	"fyms/internal/models"
	"fyms/internal/repository"
)

func ApplyUnplayedItemCounts(ctx context.Context, db *pgxpool.Pool, userID string, items []dto.BaseItemDto) {
	if userID == "" || len(items) == 0 {
		return
	}
	if _, err := uuid.Parse(userID); err != nil {
		return
	}

	byType := map[string][]string{
		"Series": {},
		"Season": {},
	}
	for i := range items {
		if _, ok := byType[items[i].Type]; ok {
			byType[items[i].Type] = append(byType[items[i].Type], items[i].ID)
		}
	}

	for itemType, ids := range byType {
		if len(ids) == 0 {
			continue
		}
		counts, err := models.GetUnplayedEpisodeCounts(ctx, db, userID, ids, itemType)
		if err != nil {
			continue
		}
		for i := range items {
			if items[i].Type != itemType {
				continue
			}
			count := counts[items[i].ID]
			if items[i].UserData == nil {
				items[i].UserData = &dto.UserItemDataDto{}
			}
			items[i].UserData.UnplayedItemCount = &count
		}
	}
}

func ApplySeasonNames(ctx context.Context, db *pgxpool.Pool, items []dto.BaseItemDto) {
	seasonIDs := make([]string, 0, len(items))
	for i := range items {
		if items[i].Type == "Episode" && items[i].SeasonID != nil && items[i].SeasonName == nil {
			seasonIDs = append(seasonIDs, *items[i].SeasonID)
		}
	}
	if len(seasonIDs) == 0 {
		return
	}

	names, err := repository.NewItemHelperRepository(db).ListSeasonNames(ctx, seasonIDs)
	if err != nil {
		return
	}

	for i := range items {
		if items[i].Type != "Episode" || items[i].SeasonID == nil || items[i].SeasonName != nil {
			continue
		}
		if name, ok := names[*items[i].SeasonID]; ok {
			items[i].SeasonName = &name
		}
	}
}

func EmbyTotalRecordCount(c *gin.Context, actual int64) int64 {
	return shared.EmbyTotalRecordCount(c, actual)
}
