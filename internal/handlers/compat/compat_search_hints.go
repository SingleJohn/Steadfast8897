package compat

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"fyms/internal/repository"
	"fyms/internal/source"
)

func dedupeCompatSourceSearchHintRows(ctx context.Context, state *AppState, rows []repository.SourceItem) ([]repository.SourceItem, error) {
	if len(rows) <= 1 {
		return rows, nil
	}
	healthByProviderID, err := compatSourceProviderHealthMap(ctx, state)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]int, len(rows))
	out := make([]repository.SourceItem, 0, len(rows))
	for _, row := range rows {
		key := source.SourceItemSearchKey(row)
		if existingIdx, ok := byKey[key]; ok {
			if compatSourceHintPriority(row, healthByProviderID) > compatSourceHintPriority(out[existingIdx], healthByProviderID) {
				out[existingIdx] = row
			}
			continue
		}
		byKey[key] = len(out)
		out = append(out, row)
	}
	return out, nil
}

func compatSourceProviderHealthMap(ctx context.Context, state *AppState) (map[int64]string, error) {
	providers, err := state.Repo.Source.ListProviders(ctx, repository.SourceProviderListOptions{
		Limit:      1000,
		OnlyUsable: true,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(providers))
	for _, provider := range providers {
		out[provider.ID] = strings.ToLower(strings.TrimSpace(provider.HealthStatus))
	}
	return out, nil
}

func compatSourceHintPriority(item repository.SourceItem, healthByProviderID map[int64]string) int {
	score := 0
	if healthByProviderID[item.ProviderID] == "ok" {
		score += 10
	}
	if item.PosterURL != nil && strings.TrimSpace(*item.PosterURL) != "" {
		score += 2
	}
	return score
}

func compatSourceSearchHint(state *AppState, item repository.SourceItem) gin.H {
	itemType := compatSourceItemType(item.ItemType)
	hint := gin.H{
		"Id":        item.PublicUUID,
		"ItemId":    item.PublicUUID,
		"Name":      item.Title,
		"Type":      itemType,
		"MediaType": "Video",
		"ServerId":  state.Config.ServerID,
		"IsFolder":  itemType == "Series" || itemType == "Folder",
	}
	if item.Year != nil {
		hint["ProductionYear"] = *item.Year
	}
	if item.PosterURL != nil && strings.TrimSpace(*item.PosterURL) != "" {
		tag := compatSourceImageTag(item.ProviderID, item.PublicUUID, *item.PosterURL)
		hint["PrimaryImageTag"] = tag
		hint["ThumbImageTag"] = tag
	}
	if item.BackdropURL != nil && strings.TrimSpace(*item.BackdropURL) != "" {
		hint["BackdropImageTag"] = compatSourceImageTag(item.ProviderID, item.PublicUUID, *item.BackdropURL)
	}
	return hint
}
