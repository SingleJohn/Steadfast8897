package playback

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"fyms/internal/handlers/shared"
	"fyms/internal/middleware"
	"fyms/internal/repository"
	"fyms/internal/services"
	"fyms/internal/source"
)

func handleSourcePlaybackStart(c *gin.Context, st *AppState, auth *middleware.AuthUser, body playbackBody) bool {
	resolved, ok := resolveSourcePlaybackEntity(c, st, body.ItemId)
	if !ok {
		return false
	}
	if err := upsertSourcePlaybackPosition(c, st, auth.ID, resolved.SourceItemID, body.PositionTicks, nil, nil, nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return true
	}
	setSourceNowPlaying(c, st, auth, body, resolved.SourceItemID, false)
	c.Status(http.StatusNoContent)
	return true
}

func handleSourcePlaybackProgress(c *gin.Context, st *AppState, auth *middleware.AuthUser, body playbackBody) bool {
	resolved, ok := resolveSourcePlaybackEntity(c, st, body.ItemId)
	if !ok {
		return false
	}
	if err := upsertSourcePlaybackPosition(c, st, auth.ID, resolved.SourceItemID, body.PositionTicks, nil, nil, nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return true
	}
	setSourceNowPlaying(c, st, auth, body, resolved.SourceItemID, false)
	c.Status(http.StatusNoContent)
	return true
}

func handleSourcePlaybackStopped(c *gin.Context, st *AppState, auth *middleware.AuthUser, body playbackBody) bool {
	resolved, ok := resolveSourcePlaybackEntity(c, st, body.ItemId)
	if !ok {
		return false
	}
	deviceID := deviceIDFromRequest(c)
	st.SessionManager.ClearNowPlaying(auth.ID, deviceID)
	pos := body.PositionTicks
	var position *int64
	if pos > 0 {
		position = &pos
	}
	data, _ := st.Repo.Source.GetUserItemData(c.Request.Context(), auth.ID, resolved.SourceItemID)
	nextPlayCount := int32(1)
	if data != nil {
		nextPlayCount = data.PlayCount + 1
	}
	if err := upsertSourcePlaybackPosition(c, st, auth.ID, resolved.SourceItemID, 0, position, &nextPlayCount, nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return true
	}
	c.Status(http.StatusNoContent)
	return true
}

func handleSourceMarkPlayed(c *gin.Context, st *AppState, userID, itemID string, played bool) bool {
	resolved, ok := resolveSourcePlaybackEntity(c, st, itemID)
	if !ok {
		return false
	}
	pos := int64(0)
	if _, err := st.Repo.Source.UpsertUserItemData(c.Request.Context(), repository.SourceUserItemDataUpsert{
		UserID:                userID,
		SourceItemID:          resolved.SourceItemID,
		PlaybackPositionTicks: &pos,
		Played:                &played,
		LastPlayedDate:        sourceLastPlayed(played),
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return true
	}
	c.JSON(http.StatusOK, gin.H{"Played": played})
	return true
}

func handleSourceFavorite(c *gin.Context, st *AppState, userID, itemID string, favorite bool) bool {
	resolved, ok := resolveSourcePlaybackEntity(c, st, itemID)
	if !ok {
		return false
	}
	if _, err := st.Repo.Source.UpsertUserItemData(c.Request.Context(), repository.SourceUserItemDataUpsert{
		UserID:       userID,
		SourceItemID: resolved.SourceItemID,
		IsFavorite:   &favorite,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return true
	}
	data, _ := st.Repo.Source.GetUserItemData(c.Request.Context(), userID, resolved.SourceItemID)
	c.JSON(http.StatusOK, sourceUserDataResponse(data))
	return true
}

func resolveSourcePlaybackEntity(c *gin.Context, st *AppState, itemID string) (*source.ResolvedEntity, bool) {
	resolved, err := source.ResolveEntity(c.Request.Context(), st.DB, itemID)
	if err != nil || resolved == nil {
		return nil, false
	}
	if resolved.Kind != source.EntityKindSourceItem && resolved.Kind != source.EntityKindSourceEpisode {
		return nil, false
	}
	return resolved, true
}

func upsertSourcePlaybackPosition(c *gin.Context, st *AppState, userID string, sourceItemID int64, rawPosition int64, position *int64, playCount *int32, played *bool) error {
	if position == nil && rawPosition > 0 {
		position = &rawPosition
	}
	_, err := st.Repo.Source.UpsertUserItemData(c.Request.Context(), repository.SourceUserItemDataUpsert{
		UserID:                userID,
		SourceItemID:          sourceItemID,
		PlaybackPositionTicks: position,
		PlayCount:             playCount,
		Played:                played,
		LastPlayedDate:        sourceLastPlayed(played != nil && *played),
	})
	return err
}

func setSourceNowPlaying(c *gin.Context, st *AppState, auth *middleware.AuthUser, body playbackBody, sourceItemID int64, played bool) {
	info := middleware.GetAuthInfo(c)
	userAgent := c.GetHeader("User-Agent")
	clientIP := getClientIP(c)
	deviceID := deviceIDFromRequest(c)
	clientName := resolveClientName(shared.StrOrPtr(info.Client, ""), userAgent)
	deviceName := resolveDeviceName(shared.StrOrPtr(info.Device, ""), userAgent)
	st.SessionManager.UpdateSession(auth.ID, auth.Name, deviceID, deviceName, clientName, shared.StrOrPtr(info.Version, ""), clientIP)
	itemID := body.ItemId
	itemName := "在线媒体"
	itemType := "Video"
	if item, _ := st.Repo.Source.GetSourceItemByID(c.Request.Context(), sourceItemID); item != nil {
		itemName = item.Title
		itemType = item.ItemType
	}
	np := &services.NowPlaying{
		ItemID:        itemID,
		ItemName:      itemName,
		ItemType:      itemType,
		PositionTicks: body.PositionTicks,
		IsPaused:      body.IsPaused,
		PlaySessionID: body.PlaySessionId,
		PlayMethod:    "DirectPlay",
	}
	if !played {
		st.SessionManager.SetNowPlaying(auth.ID, deviceID, np)
	}
}

func sourceLastPlayed(mark bool) *time.Time {
	if !mark {
		return nil
	}
	now := time.Now()
	return &now
}

func sourceUserDataResponse(data *repository.SourceUserItemData) gin.H {
	resp := gin.H{"PlaybackPositionTicks": int64(0), "PlayCount": int32(0), "IsFavorite": false, "Played": false}
	if data == nil {
		return resp
	}
	resp["PlaybackPositionTicks"] = data.PlaybackPositionTicks
	resp["PlayCount"] = data.PlayCount
	resp["IsFavorite"] = data.IsFavorite
	resp["Played"] = data.Played
	return resp
}
