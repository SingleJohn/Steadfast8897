package library

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"fyms/internal/models"
	"fyms/internal/services"
)

func getItemDeleteInfo(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	ctx := c.Request.Context()

	resolved, err := models.ResolveToUUID(ctx, state.DB, itemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if resolved == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
		return
	}

	item, err := models.GetItemByID(ctx, state.DB, *resolved)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"CanDelete":               true,
		"CanDeleteFiles":          true,
		"WillDeleteFiles":         true,
		"NotAllowed":              false,
		"NotDeletable":            false,
		"IsBlocked":               false,
		"ProtectFromBeingDeleted": false,
		"ItemId":                  *resolved,
		"ItemName":                item.Name,
		"ItemType":                item.ItemType,
	})
}

func deleteItemCompat(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	ctx := c.Request.Context()

	resolved, err := models.ResolveToUUID(ctx, state.DB, itemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if resolved == nil {
		c.Status(http.StatusNoContent)
		return
	}

	plan, err := services.BuildItemDeletePlan(ctx, state.DB, *resolved)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if plan == nil {
		c.Status(http.StatusNoContent)
		return
	}
	result, err := services.ExecuteItemDeletePlan(plan)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	deleted, err := services.DeleteItemRecord(ctx, state.DB, *resolved)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if !deleted {
		c.Status(http.StatusNoContent)
		return
	}

	slog.Info("[DeleteItem] deleted item",
		"itemId", plan.ItemID,
		"name", plan.ItemName,
		"type", plan.ItemType,
		"files", result.DeletedFiles,
		"missingFiles", result.MissingFiles,
		"dirs", result.DeletedDirs,
		"skippedPaths", result.SkippedPaths)

	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}
