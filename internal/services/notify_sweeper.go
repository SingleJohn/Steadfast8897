package services

import (
	"context"
	"log/slog"
	"time"
)

const (
	libraryNewGracePeriod = 90 * time.Second
	libraryNewMaxWait     = 15 * time.Minute
	libraryNewSweepLimit  = 200
)

func (d *NotifyDispatcher) RunLibraryNewSweeper(ctx context.Context) {
	if d == nil {
		return
	}
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := d.SweepLibraryNew(ctx, ""); err != nil {
				slog.Warn("[Notify] library.new sweep failed", "error", err)
			} else if n > 0 {
				slog.Info("[Notify] library.new candidates enqueued", "count", n)
			}
		}
	}
}

func ScheduleLibraryNewSweep(ctx context.Context, libraryID string) {
	n := GetNotifier()
	if n == nil {
		return
	}
	n.ScheduleLibraryNewSweep(ctx, libraryID, libraryNewGracePeriod)
}

func (d *NotifyDispatcher) ScheduleLibraryNewSweep(ctx context.Context, libraryID string, delay time.Duration) {
	if d == nil || libraryID == "" {
		return
	}
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if n, err := d.SweepLibraryNew(ctx, libraryID); err != nil {
			slog.Warn("[Notify] scheduled library.new sweep failed", "library", libraryID, "error", err)
		} else if n > 0 {
			slog.Info("[Notify] scheduled library.new candidates enqueued", "library", libraryID, "count", n)
		}
	}()
}

func (d *NotifyDispatcher) SweepLibraryNew(ctx context.Context, libraryID string) (int, error) {
	if d == nil {
		return 0, nil
	}

	sql := `SELECT i.id::text
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
	           )`
	args := []any{
		intervalLiteral(libraryNewGracePeriod),
		intervalLiteral(libraryNewMaxWait),
	}
	if libraryID != "" {
		sql += " AND i.library_id = $3::uuid"
		args = append(args, libraryID)
	}
	sql += " ORDER BY i.updated_at ASC LIMIT " + itoa(libraryNewSweepLimit)

	rows, err := d.pool.Query(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	enqueued := 0
	for rows.Next() {
		var itemID string
		if err := rows.Scan(&itemID); err != nil {
			return enqueued, err
		}
		if d.Submit(NotifyEvent{Event: NotifyEventLibraryNew, ItemID: itemID}) {
			enqueued++
		}
	}
	return enqueued, rows.Err()
}

func intervalLiteral(d time.Duration) string {
	return itoa(int(d.Seconds())) + " seconds"
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	n := v
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
