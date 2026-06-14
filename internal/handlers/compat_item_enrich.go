package handlers

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
	"fyms/internal/models"
)

func applyUnplayedItemCounts(ctx context.Context, db *pgxpool.Pool, userID string, items []dto.BaseItemDto) {
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

func applySeasonNames(ctx context.Context, db *pgxpool.Pool, items []dto.BaseItemDto) {
	seasonIDs := make([]string, 0, len(items))
	for i := range items {
		if items[i].Type == "Episode" && items[i].SeasonID != nil && items[i].SeasonName == nil {
			seasonIDs = append(seasonIDs, *items[i].SeasonID)
		}
	}
	if len(seasonIDs) == 0 {
		return
	}

	rows, err := db.Query(ctx,
		`SELECT id::text, name FROM items WHERE id::text = ANY($1::text[]) AND type = 'Season'`,
		seasonIDs)
	if err != nil {
		return
	}
	defer rows.Close()

	names := make(map[string]string, len(seasonIDs))
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return
		}
		names[id] = name
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

func embyTotalRecordCount(c *gin.Context, actual int64) int64 {
	v := strings.TrimSpace(queryAny(c, "EnableTotalRecordCount", "enableTotalRecordCount", "enabletotalrecordcount"))
	if strings.EqualFold(v, "false") {
		return 0
	}
	return actual
}
