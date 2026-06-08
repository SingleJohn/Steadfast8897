package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/services"
)

func RegisterWebhookRoutes(group *gin.RouterGroup, state *AppState) {
	group.POST("/Library/Webhook/CloudDrive", cloudDriveWebhookHandler)
}

func cloudDriveWebhookHandler(c *gin.Context) {
	state := GetState(c)
	secret := c.GetHeader("x-webhook-secret")
	expected, err := getWebhookSecret(c.Request.Context(), state.DB)
	if err != nil {
		slog.Error("webhook secret lookup", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if expected != "" && secret != expected {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Invalid webhook secret"})
		return
	}

	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	var events []json.RawMessage
	if raw, ok := body["data"]; ok {
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
			events = arr
		}
	}
	if len(events) == 0 {
		if _, has := body["action"]; has {
			single := make(map[string]interface{})
			for _, k := range []string{"action", "is_dir", "source_file", "destination_file"} {
				if raw, ok := body[k]; ok {
					var v interface{}
					_ = json.Unmarshal(raw, &v)
					single[k] = v
				}
			}
			b, err := json.Marshal(single)
			if err == nil {
				events = append(events, b)
			}
		} else if len(body) > 0 {
			b, err := json.Marshal(body)
			if err == nil {
				events = append(events, b)
			}
		}
	}

	if len(events) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "No events to process"})
		return
	}

	var fileEvents []services.FileChangeEvent
	for _, raw := range events {
		if ev, ok := decodeFileChangeEvent(raw); ok {
			fileEvents = append(fileEvents, ev)
		}
	}
	if len(fileEvents) > 0 {
		services.HandleFileChangeEvents(c.Request.Context(), state.Ingest, fileEvents)
	}

	slog.Info("[Webhook] Received file change event(s) from CloudDrive2", "count", len(events))
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Accepted %d event(s)", len(events))})
}

func decodeFileChangeEvent(raw json.RawMessage) (services.FileChangeEvent, bool) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return services.FileChangeEvent{}, false
	}

	ev := services.FileChangeEvent{
		Action:     rawString(m, "action"),
		SourceFile: rawString(m, "source_file"),
	}
	if ev.Action == "" || ev.SourceFile == "" {
		return services.FileChangeEvent{}, false
	}
	ev.IsDir = rawBool(m, "is_dir")
	if dst := rawString(m, "destination_file"); dst != "" {
		ev.DestinationFile = &dst
	}
	return ev, true
}

func rawString(m map[string]json.RawMessage, key string) string {
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err == nil && v != nil {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}

func rawBool(m map[string]json.RawMessage, key string) bool {
	raw, ok := m[key]
	if !ok {
		return false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		s = strings.TrimSpace(strings.ToLower(s))
		return s == "true" || s == "1" || s == "yes"
	}
	return false
}

func getWebhookSecret(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var v *string
	err := pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'webhook_secret'").Scan(&v)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if v == nil {
		return "", nil
	}
	return *v, nil
}
