package services

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/models"
)

// ActorImageBackfillResult 是「批量补演员头像」的结果摘要。
type ActorImageBackfillResult struct {
	NameSourceOn    bool  `json:"name_source_on"`    // 是否启用了按名源(本地库/外部)
	PersonsScanned  int   `json:"persons_scanned"`   // 扫到的缺头像 person 数
	PersonsFilled   int   `json:"persons_filled"`    // 按名源补上的 person 数
	TmdbItemsQueued int64 `json:"tmdb_items_queued"` // 入队 TMDB 补全的 Movie/Series 数
}

// BackfillAllActorImages 全库批量补演员头像:
//  1. 按名源(本地头像库 / 外部源)全局补 persons.image_path —— 覆盖番号/JAV。
//  2. 给有 tmdb_id 的 Movie/Series 入队 per-item TMDB 演员头像补全。
func BackfillAllActorImages(ctx context.Context, pool *pgxpool.Pool) (ActorImageBackfillResult, error) {
	cfg := LoadActorImageConfig(ctx, pool)
	res := ActorImageBackfillResult{NameSourceOn: cfg.LocalLib || cfg.ExtSource}

	if res.NameSourceOn {
		persons, err := models.ListPersonsMissingImage(ctx, pool, 0)
		if err != nil {
			return res, err
		}
		res.PersonsScanned = len(persons)
		for _, p := range persons {
			avatar := resolveActorAvatarByName(cfg, p.Name)
			if avatar == "" {
				continue
			}
			if ok, err := models.FillPersonImageIfUnlocked(ctx, pool, p.ID, avatar); err == nil && ok {
				res.PersonsFilled++
			}
		}
	}

	ids, err := models.ListItemsForActorImageBackfill(ctx, pool)
	if err != nil {
		slog.Warn("[ActorImg-BackfillAll] list tmdb items failed", "error", err)
	} else if len(ids) > 0 {
		n, qerr := NewScrapeQueue(pool).EnqueueBatch(ctx, ids, ScrapeTaskBackfillActorImg, ScrapePriorityScan)
		if qerr != nil {
			slog.Warn("[ActorImg-BackfillAll] enqueue tmdb backfill failed", "error", qerr)
		}
		res.TmdbItemsQueued = n
	}

	slog.Info("[ActorImg-BackfillAll] done",
		"name_source_on", res.NameSourceOn,
		"persons_scanned", res.PersonsScanned,
		"persons_filled", res.PersonsFilled,
		"tmdb_items_queued", res.TmdbItemsQueued)
	return res, nil
}
