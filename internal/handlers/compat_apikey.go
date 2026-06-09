package handlers

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
	var createdBy interface{}
	if auth != nil && !strings.HasPrefix(auth.ID, "api-key-") {
		if uid, err := uuid.Parse(auth.ID); err == nil {
			createdBy = uid
		}
	}

	var newID uuid.UUID
	var createdAt time.Time
	err := state.DB.QueryRow(c.Request.Context(),
		`INSERT INTO api_keys (name, key, created_by) VALUES ($1, $2, $3) RETURNING id, created_at`,
		body.Name, key, createdBy).Scan(&newID, &createdAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"Id":        newID.String(),
		"Name":      body.Name,
		"Key":       key,
		"CreatedAt": createdAt.UTC().Format(time.RFC3339),
	})
}

func listApiKeys(c *gin.Context, state *AppState) {
	rows, err := state.DB.Query(c.Request.Context(),
		`SELECT ak.id, ak.name, ak.key, ak.created_at, ak.last_used_at, COALESCE(u.name, 'Unknown') as created_by_name
		 FROM api_keys ak LEFT JOIN users u ON ak.created_by = u.id ORDER BY ak.created_at DESC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	var out []gin.H
	for rows.Next() {
		var id uuid.UUID
		var name, key, createdByName string
		var createdAt interface{}
		var lastUsed interface{}
		if err := rows.Scan(&id, &name, &key, &createdAt, &lastUsed, &createdByName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		out = append(out, gin.H{
			"Id":         id.String(),
			"Name":       name,
			"Key":        key,
			"CreatedAt":  createdAt,
			"LastUsedAt": lastUsed,
			"CreatedBy":  createdByName,
		})
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if out == nil {
		out = []gin.H{}
	}
	c.JSON(http.StatusOK, gin.H{"Items": out})
}

func deleteApiKey(c *gin.Context, state *AppState) {
	keyID := c.Param("keyId")
	ct, err := state.DB.Exec(c.Request.Context(), `DELETE FROM api_keys WHERE id = $1::uuid`, keyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if ct.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"message": "Not found"})
		return
	}
	c.Status(http.StatusNoContent)
}
