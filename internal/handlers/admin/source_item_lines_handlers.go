package admin

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"fyms/internal/repository"
)

type sourceItemLinesResponse struct {
	Item         sourceItemLineGroup   `json:"item"`
	Alternatives []sourceItemLineGroup `json:"alternatives"`
}

type sourceItemLineGroup struct {
	ID              int64                   `json:"id"`
	PublicUUID      string                  `json:"public_uuid"`
	ProviderID      int64                   `json:"provider_id"`
	ProviderName    string                  `json:"provider_name"`
	ProviderKey     string                  `json:"provider_key"`
	ProviderHealth  string                  `json:"provider_health"`
	SourceItemID    string                  `json:"source_item_id"`
	Title           string                  `json:"title"`
	ItemType        string                  `json:"item_type"`
	Year            *int32                  `json:"year,omitempty"`
	Remarks         *string                 `json:"remarks,omitempty"`
	DetailLoaded    bool                    `json:"detail_loaded"`
	PlaySourceCount int                     `json:"play_source_count"`
	PlaySources     []sourcePlayLineSummary `json:"play_sources"`
}

type sourcePlayLineSummary struct {
	ID           int64  `json:"id"`
	PublicUUID   string `json:"public_uuid"`
	LineName     string `json:"line_name"`
	EpisodeTitle string `json:"episode_title"`
	EpisodeKey   string `json:"episode_key"`
	ParseMode    string `json:"parse_mode"`
	HealthStatus string `json:"health_status"`
	SuccessCount int32  `json:"success_count"`
	FailureCount int32  `json:"failure_count"`
	AvgLatencyMS *int32 `json:"avg_latency_ms,omitempty"`
}

func getSourceItemLines(c *gin.Context, state *AppState) {
	publicUUID := strings.TrimSpace(c.Param("uuid"))
	if publicUUID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid uuid"})
		return
	}
	ctx := c.Request.Context()
	item, err := state.Repo.Source.GetSourceItemByPublicUUID(ctx, publicUUID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "source item not found"})
		return
	}
	alternatives, err := state.Repo.Source.ListSourceItemAlternatives(ctx, *item, 12)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	groups := make([]sourceItemLineGroup, 0, len(alternatives))
	for i := range alternatives {
		group, err := sourceItemLineGroupDTO(ctx, state, alternatives[i])
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		groups = append(groups, group)
	}
	resp := sourceItemLinesResponse{Alternatives: groups}
	if len(groups) > 0 {
		resp.Item = groups[0]
	}
	c.JSON(http.StatusOK, resp)
}

func sourceItemLineGroupDTO(ctx context.Context, state *AppState, item repository.SourceItem) (sourceItemLineGroup, error) {
	provider, err := state.Repo.Source.GetProviderByID(ctx, item.ProviderID)
	if err != nil {
		return sourceItemLineGroup{}, err
	}
	providerName := ""
	providerKey := ""
	providerHealth := ""
	if provider != nil {
		providerName = provider.Name
		providerKey = provider.SourceKey
		providerHealth = provider.HealthStatus
	}
	playSources, err := state.Repo.Source.ListPlaySourcesForItem(ctx, item.ID)
	if err != nil {
		return sourceItemLineGroup{}, err
	}
	lines := make([]sourcePlayLineSummary, 0, len(playSources))
	for i := range playSources {
		lines = append(lines, sourcePlayLineSummary{
			ID:           playSources[i].ID,
			PublicUUID:   playSources[i].PublicUUID,
			LineName:     playSources[i].LineName,
			EpisodeTitle: playSources[i].EpisodeTitle,
			EpisodeKey:   playSources[i].EpisodeKey,
			ParseMode:    playSources[i].ParseMode,
			HealthStatus: playSources[i].HealthStatus,
			SuccessCount: playSources[i].SuccessCount,
			FailureCount: playSources[i].FailureCount,
			AvgLatencyMS: playSources[i].AvgLatencyMS,
		})
	}
	return sourceItemLineGroup{
		ID:              item.ID,
		PublicUUID:      item.PublicUUID,
		ProviderID:      item.ProviderID,
		ProviderName:    providerName,
		ProviderKey:     providerKey,
		ProviderHealth:  providerHealth,
		SourceItemID:    item.SourceItemID,
		Title:           item.Title,
		ItemType:        item.ItemType,
		Year:            item.Year,
		Remarks:         item.Remarks,
		DetailLoaded:    item.DetailLoaded,
		PlaySourceCount: len(lines),
		PlaySources:     lines,
	}, nil
}
