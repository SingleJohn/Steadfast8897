package services

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
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
	return repository.NewScanIngestRepository(pool).ListSeriesSeasonIDs(ctx, seriesID)
}

func loadRemoteSeasonNumber(ctx context.Context, pool *pgxpool.Pool, seasonID string) (*int32, error) {
	repo := repository.NewScanIngestRepository(pool)
	episodeSeasonNum, err := repo.GetDominantEpisodeSeasonNumber(ctx, seasonID)
	if err == nil && episodeSeasonNum != nil {
		return episodeSeasonNum, nil
	}

	return repo.GetSeasonIndexNumber(ctx, seasonID)
}

func loadRefreshItemType(ctx context.Context, pool *pgxpool.Pool, itemID string) (string, error) {
	return repository.NewScanIngestRepository(pool).GetRefreshItemType(ctx, itemID)
}

func loadSeriesSubtreeTargetIDs(ctx context.Context, pool *pgxpool.Pool, seriesID string) ([]string, []string, error) {
	seasonIDs, err := loadSeriesSeasonIDs(ctx, pool, seriesID)
	if err != nil {
		return nil, nil, err
	}

	episodeIDs, err := repository.NewScanIngestRepository(pool).ListSeriesEpisodeIDs(ctx, seriesID)
	if err != nil {
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
