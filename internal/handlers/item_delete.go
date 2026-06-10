package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"fyms/internal/models"
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
		"CanDeleteFiles":          false,
		"WillDeleteFiles":         false,
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

	ct, err := state.DB.Exec(ctx, "DELETE FROM items WHERE id = $1::uuid", *resolved)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if ct.RowsAffected() == 0 {
		c.Status(http.StatusNoContent)
		return
	}

	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}
