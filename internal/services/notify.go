package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/config"
	"fyms/internal/dto"
	"fyms/internal/models"
)

const (
	NotifyEventLibraryNew       = "library.new"
	NotifyEventLibraryDeleted   = "library.deleted"
	NotifyEventItemRate         = "item.rate"
	NotifyEventItemMarkPlayed   = "item.markplayed"
	NotifyEventItemMarkUnplayed = "item.markunplayed"
	NotifyEventPlaybackStart    = "playback.start"
	NotifyEventPlaybackStop     = "playback.stop"
	NotifyEventSystemTest       = "system.notificationtest"
)

const (
	notifyChannelBuffer = 10000
	notifyWorkers       = 2
)

var sharedNotifier atomic.Pointer[NotifyDispatcher]

type NotifyUser struct {
	Name string `json:"Name"`
	ID   string `json:"Id"`
}

type NotifySession struct {
	RemoteEndPoint     string `json:"RemoteEndPoint,omitempty"`
	Client             string `json:"Client,omitempty"`
	DeviceName         string `json:"DeviceName,omitempty"`
	DeviceID           string `json:"DeviceId,omitempty"`
	ApplicationVersion string `json:"ApplicationVersion,omitempty"`
	ID                 string `json:"Id,omitempty"`
}

type NotifyPlaybackInfo struct {
	PlayedToCompletion bool  `json:"PlayedToCompletion"`
	PositionTicks      int64 `json:"PositionTicks"`
	PlaylistIndex      int   `json:"PlaylistIndex"`
	PlaylistLength     int   `json:"PlaylistLength"`
}

type NotifyDeletedItem struct {
	ID   string
	Name string
	Type string
	Path *string
}

type NotifyEvent struct {
	Event        string
	Title        string
	Description  string
	ItemID       string
	DeletedItem  *NotifyDeletedItem
	User         *NotifyUser
	UserData     *dto.UserDataRow
	Session      *NotifySession
	PlaybackInfo *NotifyPlaybackInfo
}

type NotifyDispatcher struct {
	pool     *pgxpool.Pool
	cfg      *config.AppConfig
	client   *http.Client
	ch       chan NotifyEvent
	overflow atomic.Int64
}

type notifyServer struct {
	Name    string `json:"Name"`
	ID      string `json:"Id"`
	Version string `json:"Version"`
}

type notifyEnvelope struct {
	Title        string              `json:"Title"`
	Description  *string             `json:"Description,omitempty"`
	Date         string              `json:"Date"`
	Event        string              `json:"Event"`
	User         *NotifyUser         `json:"User,omitempty"`
	Item         any                 `json:"Item,omitempty"`
	Server       notifyServer        `json:"Server"`
	Session      *NotifySession      `json:"Session,omitempty"`
	PlaybackInfo *NotifyPlaybackInfo `json:"PlaybackInfo,omitempty"`
}

type webhookSubscription struct {
	ID  string
	URL string
}

func NewNotifyDispatcher(pool *pgxpool.Pool, cfg *config.AppConfig, client *http.Client) *NotifyDispatcher {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &NotifyDispatcher{
		pool:   pool,
		cfg:    cfg,
		client: client,
		ch:     make(chan NotifyEvent, notifyChannelBuffer),
	}
}

func SetNotifier(n *NotifyDispatcher) {
	sharedNotifier.Store(n)
}

func GetNotifier() *NotifyDispatcher {
	return sharedNotifier.Load()
}

func SupportedNotifyEvents() []string {
	return []string{
		NotifyEventLibraryNew,
		NotifyEventLibraryDeleted,
		NotifyEventItemRate,
		NotifyEventItemMarkPlayed,
		NotifyEventItemMarkUnplayed,
		NotifyEventPlaybackStart,
		NotifyEventPlaybackStop,
		NotifyEventSystemTest,
	}
}

func (d *NotifyDispatcher) Run(ctx context.Context) {
	if d == nil {
		return
	}
	for i := 0; i < notifyWorkers; i++ {
		go d.consume(ctx, i)
	}
	slog.Info("[Notify] dispatcher started", "workers", notifyWorkers, "buffer", notifyChannelBuffer)
	<-ctx.Done()
}

func (d *NotifyDispatcher) Submit(e NotifyEvent) bool {
	if d == nil || e.Event == "" {
		return false
	}
	select {
	case d.ch <- e:
		return true
	default:
		prev := d.overflow.Add(1)
		if prev%100 == 1 {
			slog.Warn("[Notify] channel overflow, event dropped", "total_dropped", prev, "event", e.Event, "item", e.ItemID)
		}
		return false
	}
}

func (d *NotifyDispatcher) OverflowCount() int64 {
	if d == nil {
		return 0
	}
	return d.overflow.Load()
}

func (d *NotifyDispatcher) ChannelDepth() int {
	if d == nil {
		return 0
	}
	return len(d.ch)
}

func (d *NotifyDispatcher) consume(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-d.ch:
			if err := d.handle(ctx, e); err != nil {
				slog.Warn("[Notify] event handling failed", "worker", workerID, "event", e.Event, "item", e.ItemID, "error", err)
			}
		}
	}
}

func (d *NotifyDispatcher) handle(ctx context.Context, e NotifyEvent) error {
	if e.Event == NotifyEventLibraryNew {
		claimed, err := d.claimLibraryNew(ctx, e.ItemID)
		if err != nil {
			return err
		}
		if !claimed {
			return nil
		}
	}

	env, err := d.buildEnvelope(ctx, e)
	if err != nil {
		return err
	}
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}

	subs, err := d.loadSubscriptions(ctx, e.Event)
	if err != nil {
		return err
	}
	for _, sub := range subs {
		d.deliverWithRetry(ctx, sub, body)
	}
	return nil
}

func (d *NotifyDispatcher) buildEnvelope(ctx context.Context, e NotifyEvent) (*notifyEnvelope, error) {
	title := strings.TrimSpace(e.Title)
	if title == "" {
		title = defaultNotifyTitle(e)
	}

	branding := LoadBrandingConfig(ctx, d.pool, d.cfg)
	env := &notifyEnvelope{
		Title: title,
		Date:  embyNotifyTime(time.Now()),
		Event: e.Event,
		Server: notifyServer{
			Name:    branding.ServerName,
			ID:      d.cfg.ServerID,
			Version: d.cfg.Version,
		},
		User:         e.User,
		Session:      e.Session,
		PlaybackInfo: e.PlaybackInfo,
	}
	if desc := strings.TrimSpace(e.Description); desc != "" {
		env.Description = &desc
	}

	if e.DeletedItem != nil {
		env.Item = deletedItemPayload(e.DeletedItem)
		return env, nil
	}

	if e.ItemID != "" {
		item, err := d.loadItem(ctx, e.ItemID)
		if err != nil {
			return nil, err
		}
		if item != nil {
			env.Item = dto.FormatItemDto(item, d.cfg.ServerID, e.UserData)
		} else {
			env.Item = map[string]string{"Id": e.ItemID}
		}
	}
	return env, nil
}

func defaultNotifyTitle(e NotifyEvent) string {
	switch e.Event {
	case NotifyEventLibraryNew:
		return "New media added"
	case NotifyEventLibraryDeleted:
		return "Media removed"
	case NotifyEventSystemTest:
		return "Test Notification"
	default:
		return e.Event
	}
}

func deletedItemPayload(item *NotifyDeletedItem) map[string]any {
	out := map[string]any{
		"Id":   item.ID,
		"Name": item.Name,
		"Type": item.Type,
	}
	if item.Path != nil && *item.Path != "" {
		out["Path"] = *item.Path
	}
	return out
}

func (d *NotifyDispatcher) loadItem(ctx context.Context, itemID string) (*dto.ItemRow, error) {
	if _, err := uuid.Parse(itemID); err != nil {
		return nil, nil
	}
	rows, err := d.pool.Query(ctx,
		`SELECT i.*,
		        NULL::bigint AS playback_position_ticks,
		        0::int AS play_count,
		        FALSE AS is_favorite,
		        FALSE AS played,
		        NULL::timestamp AS last_played_date,
		        series_fallback.primary_image_tag AS series_primary_image_tag,
		        series_fallback.backdrop_image_tag AS series_backdrop_image_tag,
		        series_fallback.id AS series_fallback_id
		   FROM items i
		   LEFT JOIN items series_fallback
		          ON series_fallback.id = COALESCE(i.series_id, CASE WHEN i.type = 'Season' THEN i.parent_id END)
		  WHERE i.id = $1::uuid
		  LIMIT 1`,
		itemID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, rows.Err()
	}
	vals, err := rows.Values()
	if err != nil {
		return nil, err
	}
	fields := rows.FieldDescriptions()
	colMap := make(map[string]any, len(fields))
	for i, fd := range fields {
		colMap[string(fd.Name)] = vals[i]
	}
	item := models.MapColsToItemRow(colMap)
	return &item, rows.Err()
}

func (d *NotifyDispatcher) claimLibraryNew(ctx context.Context, itemID string) (bool, error) {
	if itemID == "" {
		return false, nil
	}
	var id string
	err := d.pool.QueryRow(ctx,
		`UPDATE items
		    SET library_new_notified_at = NOW()
		  WHERE id = $1::uuid
		    AND library_new_notified_at IS NULL
		    AND type IN ('Movie', 'Episode', 'Series')
		  RETURNING id::text`,
		itemID,
	).Scan(&id)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (d *NotifyDispatcher) loadSubscriptions(ctx context.Context, event string) ([]webhookSubscription, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT id::text, url
		   FROM webhook_subscriptions
		  WHERE enabled = TRUE
		    AND $1 = ANY(events)
		  ORDER BY created_at ASC`,
		event,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []webhookSubscription
	for rows.Next() {
		var sub webhookSubscription
		if err := rows.Scan(&sub.ID, &sub.URL); err != nil {
			return nil, err
		}
		sub.URL = strings.TrimSpace(sub.URL)
		if sub.URL != "" {
			subs = append(subs, sub)
		}
	}
	return subs, rows.Err()
}

func (d *NotifyDispatcher) SendTestToSubscription(ctx context.Context, subID string) error {
	if d == nil {
		return fmt.Errorf("notifier is not configured")
	}
	var sub webhookSubscription
	if err := d.pool.QueryRow(ctx,
		`SELECT id::text, url
		   FROM webhook_subscriptions
		  WHERE id = $1::uuid`,
		subID,
	).Scan(&sub.ID, &sub.URL); err != nil {
		return err
	}
	env, err := d.buildEnvelope(ctx, NotifyEvent{
		Event:       NotifyEventSystemTest,
		Title:       "Test Notification",
		Description: "This is a test notification from FYMS",
	})
	if err != nil {
		return err
	}
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	status, errText := d.deliverOnce(ctx, sub.URL, body)
	if errText == "" {
		d.updateDeliverySuccess(ctx, sub.ID, status)
		return nil
	}
	d.updateDeliveryFailure(ctx, sub.ID, status, errText)
	return fmt.Errorf("%s", errText)
}

func (d *NotifyDispatcher) deliverWithRetry(ctx context.Context, sub webhookSubscription, body []byte) {
	delays := []time.Duration{0, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	var lastStatus int
	var lastErr string
	for i, delay := range delays {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
		status, errText := d.deliverOnce(ctx, sub.URL, body)
		if errText == "" {
			d.updateDeliverySuccess(ctx, sub.ID, status)
			return
		}
		lastStatus = status
		lastErr = errText
		if i == len(delays)-1 {
			break
		}
	}
	d.updateDeliveryFailure(ctx, sub.ID, lastStatus, lastErr)
}

func (d *NotifyDispatcher) deliverOnce(ctx context.Context, url string, body []byte) (int, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		return resp.StatusCode, ""
	}
	sample, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	errText := fmt.Sprintf("HTTP %d", resp.StatusCode)
	if len(sample) > 0 {
		errText += ": " + strings.TrimSpace(string(sample))
	}
	return resp.StatusCode, truncateNotifyError(errText)
}

func (d *NotifyDispatcher) updateDeliverySuccess(ctx context.Context, subID string, status int) {
	_, _ = d.pool.Exec(ctx,
		`UPDATE webhook_subscriptions
		    SET last_status = $2,
		        last_error = NULL,
		        last_sent_at = NOW(),
		        updated_at = NOW()
		  WHERE id = $1::uuid`,
		subID, status,
	)
}

func (d *NotifyDispatcher) updateDeliveryFailure(ctx context.Context, subID string, status int, errText string) {
	var statusVal any
	if status > 0 {
		statusVal = status
	}
	_, _ = d.pool.Exec(ctx,
		`UPDATE webhook_subscriptions
		    SET last_status = $2,
		        last_error = $3,
		        last_sent_at = NOW(),
		        updated_at = NOW()
		  WHERE id = $1::uuid`,
		subID, statusVal, truncateNotifyError(errText),
	)
}

func truncateNotifyError(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		return s[:500]
	}
	return s
}

func embyNotifyTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.0000000Z")
}
