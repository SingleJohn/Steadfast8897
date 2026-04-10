package services

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

type GapScanProgress struct {
	Running   bool    `json:"running"`
	Progress  string  `json:"progress"`
	Error     *string `json:"error,omitempty"`
	HasResult bool    `json:"has_result"`
}

type GapScanSeriesDetail struct {
	Name   string             `json:"name"`
	Year   *int               `json:"year"`
	TmdbID *int64             `json:"tmdb_id"`
	Seasons []GapScanSeason   `json:"seasons"`
}

type GapScanSeason struct {
	Season          int32   `json:"season"`
	Missing         string  `json:"missing"`
	MissingCount    int     `json:"missing_count"`
	MissingEpisodes []int32 `json:"missing_episodes"`
}

type GapScanResult struct {
	Summary              string                `json:"summary"`
	TotalSeriesScanned   int                   `json:"total_series_scanned"`
	TotalSeriesWithGaps  int                   `json:"total_series_with_gaps"`
	Details              []GapScanSeriesDetail  `json:"details"`
}

type GapScanTask struct {
	mu       sync.Mutex
	progress GapScanProgress
	result   *GapScanResult
}

func NewGapScanTask() *GapScanTask {
	return &GapScanTask{}
}

func (t *GapScanTask) GetProgress() GapScanProgress {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.progress
}

func (t *GapScanTask) GetResult() *GapScanResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.result
}

func (t *GapScanTask) Start(pool *pgxpool.Pool) error {
	t.mu.Lock()
	if t.progress.Running {
		t.mu.Unlock()
		return fmt.Errorf("scan already running")
	}
	t.progress = GapScanProgress{Running: true, Progress: "正在获取剧集列表..."}
	t.result = nil
	t.mu.Unlock()

	go t.run(pool)
	return nil
}

func (t *GapScanTask) run(pool *pgxpool.Pool) {
	ctx := context.Background()
	defer func() {
		t.mu.Lock()
		t.progress.Running = false
		t.mu.Unlock()
	}()

	// 1. Get all Series with their episode counts per season
	type seriesRow struct {
		ID         string
		Name       string
		Year       *int
		TmdbID     *int64
	}

	rows, err := pool.Query(ctx, `
		SELECT s.id, s.name,
			CASE WHEN s.premiere_date IS NOT NULL THEN EXTRACT(YEAR FROM s.premiere_date)::int ELSE NULL END,
			CASE WHEN s.provider_ids->>'Tmdb' IS NOT NULL THEN (s.provider_ids->>'Tmdb')::bigint ELSE NULL END
		FROM items s
		WHERE s.type = 'Series'
		ORDER BY s.name
	`)
	if err != nil {
		errMsg := err.Error()
		t.mu.Lock()
		t.progress.Error = &errMsg
		t.mu.Unlock()
		return
	}

	var allSeries []seriesRow
	for rows.Next() {
		var s seriesRow
		if err := rows.Scan(&s.ID, &s.Name, &s.Year, &s.TmdbID); err != nil {
			continue
		}
		allSeries = append(allSeries, s)
	}
	rows.Close()

	total := len(allSeries)
	slog.Info("[GapScan] Starting scan", "total_series", total)

	var details []GapScanSeriesDetail
	scanned := 0

	for _, s := range allSeries {
		scanned++
		t.mu.Lock()
		t.progress.Progress = fmt.Sprintf("正在扫描 %s (%d/%d)", s.Name, scanned, total)
		t.mu.Unlock()

		// Get seasons for this series
		seasonRows, err := pool.Query(ctx,
			"SELECT id, index_number FROM items WHERE parent_id = $1::uuid AND type = 'Season' ORDER BY index_number",
			s.ID)
		if err != nil {
			continue
		}

		type seasonInfo struct {
			ID     string
			Number int32
		}
		var seasons []seasonInfo
		for seasonRows.Next() {
			var si seasonInfo
			if err := seasonRows.Scan(&si.ID, &si.Number); err == nil {
				seasons = append(seasons, si)
			}
		}
		seasonRows.Close()

		if len(seasons) == 0 {
			continue
		}

		var gapSeasons []GapScanSeason
		for _, season := range seasons {
			if season.Number <= 0 {
				continue // Skip Specials
			}

			// Get existing episode numbers
			epRows, err := pool.Query(ctx,
				"SELECT index_number FROM items WHERE parent_id = $1::uuid AND type = 'Episode' AND index_number IS NOT NULL ORDER BY index_number",
				season.ID)
			if err != nil {
				continue
			}

			existingEps := map[int32]bool{}
			var maxEp int32
			for epRows.Next() {
				var ep int32
				if epRows.Scan(&ep) == nil && ep > 0 {
					existingEps[ep] = true
					if ep > maxEp {
						maxEp = ep
					}
				}
			}
			epRows.Close()

			if maxEp <= 0 {
				continue
			}

			// Find gaps: episodes from 1 to maxEp that are missing
			var missing []int32
			for i := int32(1); i <= maxEp; i++ {
				if !existingEps[i] {
					missing = append(missing, i)
				}
			}

			if len(missing) > 0 {
				missingStr := ""
				for i, m := range missing {
					if i > 0 {
						missingStr += ","
					}
					missingStr += fmt.Sprintf("%d", m)
				}
				gapSeasons = append(gapSeasons, GapScanSeason{
					Season:          season.Number,
					Missing:         missingStr,
					MissingCount:    len(missing),
					MissingEpisodes: missing,
				})
			}
		}

		if len(gapSeasons) > 0 {
			details = append(details, GapScanSeriesDetail{
				Name:    s.Name,
				Year:    s.Year,
				TmdbID:  s.TmdbID,
				Seasons: gapSeasons,
			})
		}
	}

	// Sort by name
	sort.Slice(details, func(i, j int) bool {
		return details[i].Name < details[j].Name
	})

	result := &GapScanResult{
		Summary:             fmt.Sprintf("共扫描 %d 个剧集，%d 个存在缺集", total, len(details)),
		TotalSeriesScanned:  total,
		TotalSeriesWithGaps: len(details),
		Details:             details,
	}

	t.mu.Lock()
	t.result = result
	t.progress.HasResult = true
	t.progress.Progress = "扫描完成"
	t.mu.Unlock()

	slog.Info("[GapScan] Scan complete", "scanned", total, "with_gaps", len(details))
}
