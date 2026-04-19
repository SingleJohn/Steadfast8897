package services

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// scrapeEpisodeMetadata 对已识别 tmdb_id 的 Series,遍历其 Season,从 TMDB 拉每季的
// episodes 列表,补 items(type=Episode) 的 name / overview。
//
// 规则:
//   - 只更新 overview 为空的 episode,避免覆盖用户已手动编辑的内容
//   - name 同样:空或等于"第X集/Episode X"占位符时才更新
//   - 单季限 50 集拉取,避免长剧抓爆配额
//   - 每季拉取间隔 200ms
func scrapeEpisodeMetadata(ctx context.Context, pool *pgxpool.Pool, client *TmdbClient, seriesItemID string, tmdbID int64) {
	if client == nil || tmdbID <= 0 {
		return
	}
	rows, err := pool.Query(ctx,
		"SELECT id::text, index_number FROM items WHERE parent_id = $1::uuid AND type = 'Season' ORDER BY index_number",
		seriesItemID)
	if err != nil {
		return
	}
	type seasonRow struct {
		id       string
		indexNum *int32
	}
	var seasons []seasonRow
	for rows.Next() {
		var s seasonRow
		if err := rows.Scan(&s.id, &s.indexNum); err != nil {
			continue
		}
		seasons = append(seasons, s)
	}
	rows.Close()

	for _, s := range seasons {
		num := int32(1)
		if s.indexNum != nil {
			num = *s.indexNum
		}
		if err := updateSeasonEpisodes(ctx, pool, client, s.id, tmdbID, num); err != nil {
			slog.Debug("[TMDB] episode fetch failed", "season_id", s.id, "tmdb_id", tmdbID, "season", num, "error", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func updateSeasonEpisodes(ctx context.Context, pool *pgxpool.Pool, client *TmdbClient, seasonItemID string, tmdbID int64, seasonNum int32) error {
	endpoint := fmt.Sprintf("%s/tv/%d/season/%d?api_key={API_KEY}&language=%s", TMDB_BASE, tmdbID, seasonNum, client.language)
	data, err := client.tmdbGet(ctx, endpoint)
	if err != nil {
		return err
	}
	episodesRaw, ok := data["episodes"].([]any)
	if !ok || len(episodesRaw) == 0 {
		return nil
	}

	type episodeMeta struct {
		episodeNumber int32
		name          string
		overview      string
	}
	metas := make(map[int32]episodeMeta, len(episodesRaw))
	const maxEpisodes = 50
	for i, raw := range episodesRaw {
		if i >= maxEpisodes {
			break
		}
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ep, ok := jsonInt64(m, "episode_number")
		if !ok || ep <= 0 {
			continue
		}
		name, _ := m["name"].(string)
		overview, _ := m["overview"].(string)
		metas[int32(ep)] = episodeMeta{
			episodeNumber: int32(ep),
			name:          strings.TrimSpace(name),
			overview:      strings.TrimSpace(overview),
		}
	}
	if len(metas) == 0 {
		return nil
	}

	// 拉出本季所有 Episode items。
	epRows, err := pool.Query(ctx,
		"SELECT id::text, index_number, name, overview FROM items WHERE parent_id = $1::uuid AND type = 'Episode'",
		seasonItemID)
	if err != nil {
		return err
	}
	defer epRows.Close()

	for epRows.Next() {
		var id string
		var indexNum *int32
		var currentName, currentOverview *string
		if err := epRows.Scan(&id, &indexNum, &currentName, &currentOverview); err != nil {
			continue
		}
		if indexNum == nil {
			continue
		}
		meta, ok := metas[*indexNum]
		if !ok {
			continue
		}

		shouldUpdateName := isEpisodePlaceholderName(currentName) && meta.name != ""
		shouldUpdateOverview := strings.TrimSpace(deref(currentOverview)) == "" && meta.overview != ""
		if !shouldUpdateName && !shouldUpdateOverview {
			continue
		}
		if shouldUpdateName && shouldUpdateOverview {
			_, _ = pool.Exec(ctx,
				"UPDATE items SET name = $1, overview = $2, updated_at = NOW() WHERE id = $3::uuid",
				meta.name, meta.overview, id)
		} else if shouldUpdateName {
			_, _ = pool.Exec(ctx,
				"UPDATE items SET name = $1, updated_at = NOW() WHERE id = $2::uuid",
				meta.name, id)
		} else {
			_, _ = pool.Exec(ctx,
				"UPDATE items SET overview = $1, updated_at = NOW() WHERE id = $2::uuid",
				meta.overview, id)
		}
	}
	return nil
}

// isEpisodePlaceholderName 判断当前 name 是否为扫库自动生成的占位符(Episode N / Special N / 第X集 / S01E02)。
// 这些名字没有信息量,可以安全被 TMDB 的真实 episode title 覆盖。
// 注意:判定要严格,避免误把用户编辑过的真实标题当成占位符再次覆盖。
func isEpisodePlaceholderName(name *string) bool {
	if name == nil {
		return true
	}
	s := strings.TrimSpace(*name)
	if s == "" {
		return true
	}
	return episodePlaceholderRE.MatchString(s)
}

// Episode N / Special N(M7.1 扫库写入) / 第X集|话 / 第X回 / S01E02 / 01x02
var episodePlaceholderRE = regexp.MustCompile(`^(?i)(?:episode\s+\d+|special\s+\d+|s\d{1,2}e\d{1,3}|\d{1,2}x\d{1,3}|第\s*\d+\s*[集话回])$`)

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
