package services

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type seriesSubtreeRemotePlan struct {
	EnqueueSeriesRefresh bool
	EnqueueEpisodeNames  bool
	EnqueueEpisodeImages bool
	EnqueueActorImages   bool
}

func RefreshSeriesSubtree(ctx context.Context, pool *pgxpool.Pool, refreshQueue *RefreshQueue, scrapeQueue *ScrapeQueue, seriesID string, source RefreshSource, refreshPriority int16, opts RefreshOptions) error {
	if refreshQueue == nil {
		return fmt.Errorf("refresh queue not ready")
	}

	itemType, err := loadRefreshItemType(ctx, pool, seriesID)
	if err != nil {
		return err
	}
	if itemType != "Series" {
		return fmt.Errorf("subtree refresh 仅支持 Series")
	}

	seasonIDs, episodeIDs, err := loadSeriesSubtreeTargetIDs(ctx, pool, seriesID)
	if err != nil {
		return err
	}

	childOpts := opts
	childOpts.AllowRemote = false
	childOpts.RefreshSubtree = false

	if _, err := refreshQueue.EnqueueBatch(ctx, seasonIDs, RefreshScopeImages, source, refreshPriority, childOpts); err != nil {
		return err
	}
	if _, err := refreshQueue.EnqueueBatch(ctx, episodeIDs, RefreshScopeMetadata, source, refreshPriority, childOpts); err != nil {
		return err
	}
	if _, err := refreshQueue.EnqueueBatch(ctx, episodeIDs, RefreshScopeImages, source, refreshPriority, childOpts); err != nil {
		return err
	}

	if opts.ValidateOnly || !opts.AllowRemote {
		return nil
	}
	return enqueueSeriesSubtreeRemote(ctx, scrapeQueue, seriesID, seasonIDs, scrapePriorityForRefreshSource(source), seriesSubtreeRemotePlan{
		EnqueueSeriesRefresh: true,
		EnqueueEpisodeNames:  true,
		EnqueueEpisodeImages: true,
		EnqueueActorImages:   true,
	})
}

func enqueueSeriesSubtreeRemote(ctx context.Context, scrapeQueue *ScrapeQueue, seriesID string, seasonIDs []string, priority int16, plan seriesSubtreeRemotePlan) error {
	if scrapeQueue == nil {
		return fmt.Errorf("scrape queue not ready")
	}

	if plan.EnqueueSeriesRefresh {
		if err := scrapeQueue.Enqueue(ctx, seriesID, ScrapeTaskRefresh, priority); err != nil {
			return err
		}
	}
	// Series 的 ScrapeTaskRefresh 内部已经会跑 scrapeEpisodeMetadata，
	// 所以只有在"不做 series refresh"时，才需要单独补 episode name 任务。
	if plan.EnqueueEpisodeNames && !plan.EnqueueSeriesRefresh {
		if err := scrapeQueue.Enqueue(ctx, seriesID, ScrapeTaskBackfillEpisodeName, priority); err != nil {
			return err
		}
	}
	if plan.EnqueueActorImages {
		if err := scrapeQueue.Enqueue(ctx, seriesID, ScrapeTaskBackfillActorImg, priority); err != nil {
			return err
		}
	}
	if plan.EnqueueEpisodeImages {
		if _, err := scrapeQueue.EnqueueBatch(ctx, compactStringIDs(seasonIDs), ScrapeTaskBackfillEpisodeImg, priority); err != nil {
			return err
		}
	}
	return nil
}

func compactStringIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func loadSeriesSeasonIDs(ctx context.Context, pool *pgxpool.Pool, seriesID string) ([]string, error) {
	rows, err := pool.Query(ctx,
		`SELECT id::text
		   FROM items
		  WHERE parent_id = $1::uuid
		    AND type = 'Season'
		  ORDER BY index_number ASC NULLS FIRST, created_at ASC`,
		seriesID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seasonIDs := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		seasonIDs = append(seasonIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return seasonIDs, nil
}

func loadRefreshItemType(ctx context.Context, pool *pgxpool.Pool, itemID string) (string, error) {
	var itemType string
	if err := pool.QueryRow(ctx,
		`SELECT type
		   FROM items
		  WHERE id = $1::uuid`,
		itemID,
	).Scan(&itemType); err != nil {
		return "", err
	}
	return itemType, nil
}

func loadSeriesSubtreeTargetIDs(ctx context.Context, pool *pgxpool.Pool, seriesID string) ([]string, []string, error) {
	seasonIDs, err := loadSeriesSeasonIDs(ctx, pool, seriesID)
	if err != nil {
		return nil, nil, err
	}

	episodeRows, err := pool.Query(ctx,
		`SELECT id::text
		   FROM items
		  WHERE series_id = $1::uuid
		    AND type = 'Episode'
		  ORDER BY parent_index_number ASC NULLS FIRST, index_number ASC NULLS FIRST, created_at ASC`,
		seriesID,
	)
	if err != nil {
		return nil, nil, err
	}
	defer episodeRows.Close()

	episodeIDs := make([]string, 0)
	for episodeRows.Next() {
		var id string
		if err := episodeRows.Scan(&id); err != nil {
			return nil, nil, err
		}
		episodeIDs = append(episodeIDs, id)
	}
	if err := episodeRows.Err(); err != nil {
		return nil, nil, err
	}

	return seasonIDs, episodeIDs, nil
}

func scrapePriorityForRefreshSource(source RefreshSource) int16 {
	switch source {
	case RefreshSourceManual:
		return ScrapePriorityRefresh
	default:
		return ScrapePriorityScan
	}
}
