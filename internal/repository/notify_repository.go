package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type NotifyRepository struct {
	pool *pgxpool.Pool
}

type WebhookSubscription struct {
	ID         string
	Name       string
	URL        string
	Events     []string
	Enabled    bool
	GroupItems bool
	LastStatus *int32
	LastError  *string
	LastSentAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func NewNotifyRepository(pool *pgxpool.Pool) *NotifyRepository {
	return &NotifyRepository{pool: pool}
}

func (r *NotifyRepository) ListSubscriptions(ctx context.Context) ([]WebhookSubscription, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id::text, name, url, events, enabled, group_items,
		        last_status, last_error, last_sent_at, created_at, updated_at
		   FROM webhook_subscriptions
		  ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWebhookSubscriptionRows(rows)
}

func (r *NotifyRepository) CreateSubscription(ctx context.Context, name, url string, events []string, enabled, groupItems bool) (WebhookSubscription, error) {
	var sub WebhookSubscription
	err := r.pool.QueryRow(ctx,
		`INSERT INTO webhook_subscriptions (name, url, events, enabled, group_items)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id::text, name, url, events, enabled, group_items,
		           last_status, last_error, last_sent_at, created_at, updated_at`,
		name, url, events, enabled, groupItems,
	).Scan(&sub.ID, &sub.Name, &sub.URL, &sub.Events, &sub.Enabled, &sub.GroupItems,
		&sub.LastStatus, &sub.LastError, &sub.LastSentAt, &sub.CreatedAt, &sub.UpdatedAt)
	return sub, err
}

func (r *NotifyRepository) UpdateSubscription(ctx context.Context, id, name, url string, events []string, enabled, groupItems bool) (WebhookSubscription, error) {
	var sub WebhookSubscription
	err := r.pool.QueryRow(ctx,
		`UPDATE webhook_subscriptions
		    SET name = $2,
		        url = $3,
		        events = $4,
		        enabled = $5,
		        group_items = $6,
		        updated_at = NOW()
		  WHERE id = $1::uuid
		  RETURNING id::text, name, url, events, enabled, group_items,
		            last_status, last_error, last_sent_at, created_at, updated_at`,
		id, name, url, events, enabled, groupItems,
	).Scan(&sub.ID, &sub.Name, &sub.URL, &sub.Events, &sub.Enabled, &sub.GroupItems,
		&sub.LastStatus, &sub.LastError, &sub.LastSentAt, &sub.CreatedAt, &sub.UpdatedAt)
	return sub, err
}

func (r *NotifyRepository) DeleteSubscription(ctx context.Context, id string) (bool, error) {
	tag, err := r.pool.Exec(ctx, "DELETE FROM webhook_subscriptions WHERE id = $1::uuid", id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *NotifyRepository) LoadItemColumns(ctx context.Context, itemID string) (map[string]any, error) {
	if _, err := uuid.Parse(itemID); err != nil {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx,
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
	return colMap, rows.Err()
}

func (r *NotifyRepository) ClaimLibraryNew(ctx context.Context, itemID string) (bool, error) {
	if itemID == "" {
		return false, nil
	}
	var id string
	err := r.pool.QueryRow(ctx,
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

func (r *NotifyRepository) ListSubscriptionsForEvent(ctx context.Context, event string) ([]WebhookSubscription, error) {
	rows, err := r.pool.Query(ctx,
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
	return scanWebhookSubscriptions(rows)
}

func (r *NotifyRepository) GetSubscription(ctx context.Context, subID string) (WebhookSubscription, error) {
	var sub WebhookSubscription
	err := r.pool.QueryRow(ctx,
		`SELECT id::text, url
		   FROM webhook_subscriptions
		  WHERE id = $1::uuid`,
		subID,
	).Scan(&sub.ID, &sub.URL)
	sub.URL = strings.TrimSpace(sub.URL)
	return sub, err
}

func (r *NotifyRepository) UpdateDeliverySuccess(ctx context.Context, subID string, status int) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE webhook_subscriptions
		    SET last_status = $2,
		        last_error = NULL,
		        last_sent_at = NOW(),
		        updated_at = NOW()
		  WHERE id = $1::uuid`,
		subID, status,
	)
	return err
}

func (r *NotifyRepository) UpdateDeliveryFailure(ctx context.Context, subID string, status int, errText string) error {
	var statusVal any
	if status > 0 {
		statusVal = status
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE webhook_subscriptions
		    SET last_status = $2,
		        last_error = $3,
		        last_sent_at = NOW(),
		        updated_at = NOW()
		  WHERE id = $1::uuid`,
		subID, statusVal, strings.TrimSpace(errText),
	)
	return err
}

func (r *NotifyRepository) ListLibraryNewSweepCandidateIDs(ctx context.Context, libraryID string, gracePeriod, maxWait time.Duration, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 200
	}
	args := []any{
		intervalLiteral(gracePeriod),
		intervalLiteral(maxWait),
	}
	where := ""
	if libraryID != "" {
		where = " AND i.library_id = $3::uuid"
		args = append(args, libraryID)
	}
	query := fmt.Sprintf(`SELECT i.id::text
	          FROM items i
	         WHERE i.library_new_notified_at IS NULL
	           AND i.type IN ('Movie', 'Episode', 'Series')
	           AND i.updated_at < NOW() - $1::interval
	           AND (
	                NOT EXISTS (
	                    SELECT 1 FROM scrape_queue q
	                     WHERE q.item_id = i.id
	                       AND q.status IN ('pending', 'running')
	                )
	                OR i.updated_at < NOW() - $2::interval
	           )%s
	         ORDER BY i.updated_at ASC
	         LIMIT %d`, where, limit)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringRows(rows)
}

func scanWebhookSubscriptions(rows pgx.Rows) ([]WebhookSubscription, error) {
	var subs []WebhookSubscription
	for rows.Next() {
		var sub WebhookSubscription
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

func scanWebhookSubscriptionRows(rows pgx.Rows) ([]WebhookSubscription, error) {
	var subs []WebhookSubscription
	for rows.Next() {
		var sub WebhookSubscription
		if err := rows.Scan(&sub.ID, &sub.Name, &sub.URL, &sub.Events, &sub.Enabled, &sub.GroupItems,
			&sub.LastStatus, &sub.LastError, &sub.LastSentAt, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

func intervalLiteral(d time.Duration) string {
	return fmt.Sprintf("%d seconds", int(d.Seconds()))
}
