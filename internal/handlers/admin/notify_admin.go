package admin

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"fyms/internal/repository"
	"fyms/internal/services"
)

type webhookSubscriptionInput struct {
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	Events     []string `json:"events"`
	Enabled    *bool    `json:"enabled"`
	GroupItems bool     `json:"group_items"`
	enabled    bool
}

type webhookSubscriptionOutput struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	URL        string     `json:"url"`
	Events     []string   `json:"events"`
	Enabled    bool       `json:"enabled"`
	GroupItems bool       `json:"group_items"`
	LastStatus *int32     `json:"last_status,omitempty"`
	LastError  *string    `json:"last_error,omitempty"`
	LastSentAt *time.Time `json:"last_sent_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func RegisterNotifyAdminRoutes(group *gin.RouterGroup, state *AppState, adminMW gin.HandlerFunc) {
	group.GET("/Admin/Notifications/Subscriptions", adminMW, func(c *gin.Context) { listWebhookSubscriptions(c, state) })
	group.POST("/Admin/Notifications/Subscriptions", adminMW, func(c *gin.Context) { createWebhookSubscription(c, state) })
	group.PUT("/Admin/Notifications/Subscriptions/:id", adminMW, func(c *gin.Context) { updateWebhookSubscription(c, state) })
	group.DELETE("/Admin/Notifications/Subscriptions/:id", adminMW, func(c *gin.Context) { deleteWebhookSubscription(c, state) })
	group.POST("/Admin/Notifications/Subscriptions/:id/Test", adminMW, func(c *gin.Context) { testWebhookSubscription(c, state) })
	group.GET("/Admin/Notifications/SamplePayload", adminMW, func(c *gin.Context) { sampleWebhookPayload(c, state) })
	group.GET("/Admin/Notifications/SupportedEvents", adminMW, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"events": services.SupportedNotifyEvents()})
	})
}

func listWebhookSubscriptions(c *gin.Context, st *AppState) {
	subs, err := repository.NewNotifyRepository(st.DB).ListSubscriptions(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	out := make([]webhookSubscriptionOutput, 0, len(subs))
	for _, sub := range subs {
		out = append(out, webhookSubscriptionOutputFromRepo(sub))
	}
	c.JSON(http.StatusOK, out)
}

func createWebhookSubscription(c *gin.Context, st *AppState) {
	input, ok := bindWebhookSubscriptionInput(c)
	if !ok {
		return
	}
	sub, err := repository.NewNotifyRepository(st.DB).CreateSubscription(c.Request.Context(), input.Name, input.URL, input.Events, input.enabled, input.GroupItems)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, webhookSubscriptionOutputFromRepo(sub))
}

func updateWebhookSubscription(c *gin.Context, st *AppState) {
	input, ok := bindWebhookSubscriptionInput(c)
	if !ok {
		return
	}
	sub, err := repository.NewNotifyRepository(st.DB).UpdateSubscription(c.Request.Context(), c.Param("id"), input.Name, input.URL, input.Events, input.enabled, input.GroupItems)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"message": "Subscription not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, webhookSubscriptionOutputFromRepo(sub))
}

func deleteWebhookSubscription(c *gin.Context, st *AppState) {
	ok, err := repository.NewNotifyRepository(st.DB).DeleteSubscription(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"message": "Subscription not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

func testWebhookSubscription(c *gin.Context, st *AppState) {
	notifier := st.Notifier
	if notifier == nil {
		notifier = services.GetNotifier()
	}
	if notifier == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "Notifier is not running"})
		return
	}
	if err := notifier.SendTestToSubscription(c.Request.Context(), c.Param("id")); err != nil {
		if err == pgx.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"message": "Subscription not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func sampleWebhookPayload(c *gin.Context, st *AppState) {
	event := strings.TrimSpace(c.Query("Event"))
	if event == "" {
		event = services.NotifyEventLibraryNew
	}
	branding := services.LoadBrandingConfig(c.Request.Context(), st.DB, st.Config)
	server := gin.H{"Name": branding.ServerName, "Id": st.Config.ServerID, "Version": st.Config.Version}
	base := gin.H{
		"Title":  "Sample Notification",
		"Date":   time.Now().UTC().Format("2006-01-02T15:04:05.0000000Z"),
		"Event":  event,
		"Server": server,
	}

	switch event {
	case services.NotifyEventSystemTest:
		base["Title"] = "Test Notification"
		base["Description"] = "This is a test notification from FYMS"
	case services.NotifyEventLibraryDeleted:
		base["Item"] = gin.H{"Id": "00000000-0000-0000-0000-000000000000", "Name": "Sample Movie", "Type": "Movie", "Path": "/media/Sample Movie.mkv"}
	case services.NotifyEventItemRate, services.NotifyEventItemMarkPlayed, services.NotifyEventItemMarkUnplayed:
		base["User"] = gin.H{"Name": "sample", "Id": "00000000-0000-0000-0000-000000000001"}
		base["Item"] = sampleNotifyItem()
	case services.NotifyEventPlaybackStart, services.NotifyEventPlaybackStop:
		base["User"] = gin.H{"Name": "sample", "Id": "00000000-0000-0000-0000-000000000001"}
		base["Item"] = sampleNotifyItem()
		base["Session"] = gin.H{"RemoteEndPoint": "127.0.0.1", "Client": "Infuse", "DeviceName": "iPhone", "DeviceId": "sample-device", "Id": "sample-session"}
		base["PlaybackInfo"] = gin.H{"PlayedToCompletion": event == services.NotifyEventPlaybackStop, "PositionTicks": 12663608, "PlaylistIndex": 0, "PlaylistLength": 1}
	default:
		base["Item"] = sampleNotifyItem()
	}
	c.JSON(http.StatusOK, base)
}

func bindWebhookSubscriptionInput(c *gin.Context) (webhookSubscriptionInput, bool) {
	var input webhookSubscriptionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return input, false
	}
	input.Name = strings.TrimSpace(input.Name)
	input.URL = strings.TrimSpace(input.URL)
	if input.Name == "" {
		input.Name = input.URL
	}
	if input.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "url is required"})
		return input, false
	}
	parsed, err := url.Parse(input.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "url must be an http or https URL"})
		return input, false
	}
	events, err := normalizeNotifyEvents(input.Events)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return input, false
	}
	input.Events = events
	input.enabled = true
	if input.Enabled != nil {
		input.enabled = *input.Enabled
	}
	return input, true
}

func normalizeNotifyEvents(events []string) ([]string, error) {
	supported := map[string]bool{}
	for _, e := range services.SupportedNotifyEvents() {
		supported[e] = true
	}
	if len(events) == 0 {
		return []string{services.NotifyEventLibraryNew, services.NotifyEventLibraryDeleted}, nil
	}
	out := make([]string, 0, len(events))
	seen := map[string]bool{}
	for _, e := range events {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if !supported[e] {
			return nil, fmt.Errorf("unsupported event: %s", e)
		}
		if !seen[e] {
			out = append(out, e)
			seen[e] = true
		}
	}
	if len(out) == 0 {
		return []string{services.NotifyEventLibraryNew, services.NotifyEventLibraryDeleted}, nil
	}
	return out, nil
}

func webhookSubscriptionOutputFromRepo(sub repository.WebhookSubscription) webhookSubscriptionOutput {
	return webhookSubscriptionOutput{
		ID:         sub.ID,
		Name:       sub.Name,
		URL:        sub.URL,
		Events:     sub.Events,
		Enabled:    sub.Enabled,
		GroupItems: sub.GroupItems,
		LastStatus: sub.LastStatus,
		LastError:  sub.LastError,
		LastSentAt: sub.LastSentAt,
		CreatedAt:  sub.CreatedAt,
		UpdatedAt:  sub.UpdatedAt,
	}
}

func sampleNotifyItem() gin.H {
	return gin.H{
		"Id":           "00000000-0000-0000-0000-000000000000",
		"Name":         "Sample Movie",
		"Type":         "Movie",
		"Path":         "/media/Sample Movie.mkv",
		"RunTimeTicks": int64(88800000000),
		"ProviderIds":  gin.H{"Tmdb": "27205", "Imdb": "tt1375666"},
		"UserData":     gin.H{"PlaybackPositionTicks": 0, "PlayCount": 1, "IsFavorite": true, "Played": true},
	}
}
