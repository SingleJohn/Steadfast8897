package compat

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"fyms/internal/middleware"
)

type apiKeyCreateBody struct {
	Name string `json:"Name"`
}

func createApiKey(c *gin.Context, state *AppState) {
	var body apiKeyCreateBody
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Name required"})
		return
	}
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	key := hex.EncodeToString(keyBytes)

	auth := middleware.GetAuthUser(c)
	var createdBy *uuid.UUID
	if auth != nil && !strings.HasPrefix(auth.ID, "api-key-") {
		if uid, err := uuid.Parse(auth.ID); err == nil {
			createdBy = &uid
		}
	}

	created, err := state.Repo.APIKeys.Create(c.Request.Context(), body.Name, key, createdBy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"Id":        created.ID.String(),
		"Name":      created.Name,
		"Key":       key,
		"CreatedAt": created.CreatedAt.UTC().Format(time.RFC3339),
	})
}

func listApiKeys(c *gin.Context, state *AppState) {
	keys, err := state.Repo.APIKeys.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	var out []gin.H
	for _, key := range keys {
		out = append(out, gin.H{
			"Id":         key.ID.String(),
			"Name":       key.Name,
			"Key":        key.Key,
			"CreatedAt":  key.CreatedAt,
			"LastUsedAt": key.LastUsedAt,
			"CreatedBy":  key.CreatedByName,
		})
	}
	if out == nil {
		out = []gin.H{}
	}
	c.JSON(http.StatusOK, gin.H{"Items": out})
}

func deleteApiKey(c *gin.Context, state *AppState) {
	keyID := c.Param("keyId")
	uid, err := uuid.Parse(keyID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid key id"})
		return
	}
	deleted, err := state.Repo.APIKeys.Delete(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if !deleted {
		c.JSON(http.StatusNotFound, gin.H{"message": "Not found"})
		return
	}
	c.Status(http.StatusNoContent)
}
