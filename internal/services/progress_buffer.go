package services

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ProgressEntry struct {
	UserID        string
	ItemID        string
	PositionTicks int64
	PlayCount     *int32
	IsFavorite    *bool
	Played        *bool
}

type ProgressBuffer struct {
	mu       sync.Mutex
	buffer   map[string]*ProgressEntry
	pool     *pgxpool.Pool
	stopCh   chan struct{}
	doneCh   chan struct{}
}

func NewProgressBuffer(pool *pgxpool.Pool) *ProgressBuffer {
	pb := &ProgressBuffer{
		buffer: make(map[string]*ProgressEntry),
		pool:   pool,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	go pb.flusher()
	return pb
}

func (pb *ProgressBuffer) flusher() {
	defer close(pb.doneCh)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pb.flushOnce()
		case <-pb.stopCh:
			pb.flushOnce()
			return
		}
	}
}

func (pb *ProgressBuffer) flushOnce() {
	pb.mu.Lock()
	entries := make([]*ProgressEntry, 0, len(pb.buffer))
	for _, e := range pb.buffer {
		entries = append(entries, e)
	}
	pb.buffer = make(map[string]*ProgressEntry)
	pb.mu.Unlock()

	if len(entries) == 0 {
		return
	}

	ctx := context.Background()
	for _, entry := range entries {
		_, err := pb.pool.Exec(ctx,
			`INSERT INTO user_item_data (user_id, item_id, playback_position_ticks, play_count, is_favorite, played, last_played_date)
			 VALUES ($1::uuid, $2::uuid, $3, COALESCE($4, 0), COALESCE($5, false), COALESCE($6, false), NOW())
			 ON CONFLICT (user_id, item_id) DO UPDATE SET
			   playback_position_ticks = $3,
			   play_count = CASE WHEN $4 IS NOT NULL THEN $4 ELSE user_item_data.play_count END,
			   is_favorite = CASE WHEN $5 IS NOT NULL THEN $5 ELSE user_item_data.is_favorite END,
			   played = CASE WHEN $6 IS NOT NULL THEN $6 ELSE user_item_data.played END,
			   last_played_date = NOW()`,
			entry.UserID, entry.ItemID, entry.PositionTicks,
			entry.PlayCount, entry.IsFavorite, entry.Played)
		if err != nil {
			slog.Error("Progress flush error", "error", err)
		}
	}
}

func (pb *ProgressBuffer) BufferProgress(entry *ProgressEntry) {
	key := entry.UserID + ":" + entry.ItemID
	pb.mu.Lock()
	defer pb.mu.Unlock()

	if existing, ok := pb.buffer[key]; ok {
		existing.PositionTicks = entry.PositionTicks
		if entry.PlayCount != nil {
			existing.PlayCount = entry.PlayCount
		}
		if entry.IsFavorite != nil {
			existing.IsFavorite = entry.IsFavorite
		}
		if entry.Played != nil {
			existing.Played = entry.Played
		}
	} else {
		pb.buffer[key] = entry
	}
}

func (pb *ProgressBuffer) Shutdown() {
	close(pb.stopCh)
	<-pb.doneCh
}
