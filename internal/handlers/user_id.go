package handlers

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func parseUserIDParam(c *gin.Context) (uuid.UUID, error) {
	return uuid.Parse(strings.TrimSpace(c.Param("userId")))
}

func canonicalUserIDString(userID string) string {
	uid, err := uuid.Parse(strings.TrimSpace(userID))
	if err != nil {
		return strings.TrimSpace(userID)
	}
	return uid.String()
}
