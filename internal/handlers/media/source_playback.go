package media

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"fyms/internal/dto"
	embysupport "fyms/internal/handlers/mediasupport"
	"fyms/internal/middleware"
	"fyms/internal/repository"
	"fyms/internal/source"
)

func handleSourcePlaybackInfo(c *gin.Context, state *AppState, itemID, selectedMediaSourceID string) bool {
	ctx := c.Request.Context()
	resolved, err := source.ResolveEntity(ctx, state.DB, itemID)
	if err != nil || resolved == nil {
		return false
	}
	if resolved.Kind != source.EntityKindSourceItem && resolved.Kind != source.EntityKindSourceEpisode {
		return false
	}
	authUser := middleware.GetAuthUser(c)
	if authUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
		return true
	}
	if !strings.HasPrefix(authUser.ID, "api-key-") {
		userUUID, err := uuid.Parse(authUser.ID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
			return true
		}
		policy, err := state.Repo.Users.GetUserPolicy(ctx, userUUID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Policy error"})
			return true
		}
		if policy != nil && !policy.EnableMediaPlayback {
			c.JSON(http.StatusForbidden, gin.H{"message": "Playback disabled"})
			return true
		}
		if policy != nil && policy.SimultaneousStreamLimit > 0 {
			if n := state.SessionManager.CountActiveStreams(authUser.ID); int32(n) >= policy.SimultaneousStreamLimit {
				c.JSON(http.StatusTooManyRequests, gin.H{"message": "Too many simultaneous streams"})
				return true
			}
		}
	}

	playSources, err := sourcePlaybackSources(ctx, state, resolved)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return true
	}
	sources := make([]dto.MediaSourceInfo, 0, len(playSources))
	for i := range playSources {
		sources = append(sources, sourceMediaSource(playSources[i]))
	}
	sources = preferMediaSource(sources, selectedMediaSourceID)
	if sources == nil {
		sources = []dto.MediaSourceInfo{}
	}
	playSessionID := strings.ReplaceAll(uuid.New().String(), "-", "")
	c.JSON(http.StatusOK, gin.H{
		"MediaSources":  embysupport.MediaSourcesToEmbyMaps(sources),
		"PlaySessionId": playSessionID,
	})
	return true
}

func sourcePlaybackSources(ctx context.Context, state *AppState, resolved *source.ResolvedEntity) ([]repository.SourcePlaySource, error) {
	if resolved.Kind == source.EntityKindSourceItem || resolved.Kind == source.EntityKindSourceEpisode {
		if _, err := source.EnsureItemDetailLoaded(ctx, state.Repo.Source, state.HTTPClient, state.JSRuntime, resolved.SourceItemID); err != nil {
			slog.Warn("[Source] ensure detail failed",
				"log_target", "source",
				"action", "ensure_detail",
				"source_item_id", resolved.SourceItemID,
				"error_type", source.ErrorType(err),
				"error", err)
		}
	}
	all, err := state.Repo.Source.ListPlaySourcesForItem(ctx, resolved.SourceItemID)
	if err != nil || resolved.Kind != source.EntityKindSourceEpisode {
		return all, err
	}
	out := make([]repository.SourcePlaySource, 0, len(all))
	for i := range all {
		key := all[i].EpisodeKey
		if strings.TrimSpace(key) == "" {
			key = all[i].EpisodeTitle
		}
		if strings.TrimSpace(key) == resolved.EpisodeKey {
			out = append(out, all[i])
		}
	}
	return out, nil
}

func sourceMediaSource(playSource repository.SourcePlaySource) dto.MediaSourceInfo {
	container := sourceContainer(playSource.RawURL)
	ms := dto.MediaSourceInfo{
		ID:                    playSource.PublicUUID,
		Path:                  "/SourcePlay/" + playSource.PublicUUID + "/stream",
		Protocol:              "Http",
		Type:                  "Default",
		Container:             container,
		Name:                  "在线: " + playSource.LineName,
		IsRemote:              true,
		SupportsDirectPlay:    true,
		SupportsDirectStream:  true,
		SupportsTranscoding:   false,
		SupportsProbing:       false,
		ReadAtNativeFramerate: false,
		ETag:                  playSource.PublicUUID,
		Formats:               []string{},
		MediaStreams:          []dto.MediaStreamInfo{},
		RequiredHTTPHeaders:   map[string]string{},
		Chapters:              []dto.ChapterInfo{},
	}
	ApplyMediaSourceCompatDefaults(&ms, "")
	return ms
}

func sourceContainer(rawURL string) string {
	lower := strings.ToLower(strings.TrimSpace(rawURL))
	switch {
	case strings.Contains(lower, ".m3u8"):
		return "m3u8"
	case strings.Contains(lower, ".mp4"):
		return "mp4"
	case strings.Contains(lower, ".flv"):
		return "flv"
	default:
		return "m3u8"
	}
}
