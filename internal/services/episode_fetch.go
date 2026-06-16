package services

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
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

	// stillEnabled / saveMode 在整个 Series 刮削里只读一次,避免每季重复查 system_config。
	stillEnabled := readEpisodeStillFetchEnabled(ctx, pool)
	saveMode := getScrapeSaveMode(ctx, pool)

	for _, s := range seasons {
		remoteSeasonNum, err := loadRemoteSeasonNumber(ctx, pool, s.id)
		if err != nil || remoteSeasonNum == nil {
			num := int32(1)
			if s.indexNum != nil {
				num = *s.indexNum
			}
			remoteSeasonNum = &num
		}
		if err := updateSeasonEpisodes(ctx, pool, client, s.id, tmdbID, *remoteSeasonNum, stillEnabled, saveMode); err != nil {
			slog.Debug("[TMDB] episode fetch failed", "season_id", s.id, "tmdb_id", tmdbID, "season", *remoteSeasonNum, "error", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func updateSeasonEpisodes(ctx context.Context, pool *pgxpool.Pool, client *TmdbClient, seasonItemID string, tmdbID int64, seasonNum int32, stillFetchEnabled bool, saveMode string) error {
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
		stillPath     string // M7.2: TMDB episode still_path (e.g. "/abc.jpg")
	}
	metas := make(map[int32]episodeMeta, len(episodesRaw))
	// 历史版本曾用 maxEpisodes=50 做兜底,现在 TMDB 调用受 sharedTmdbLimiter(3 rps)
	// 节流,长剧全量抓取不会打爆配额;DB 用 batch UPDATE(unnest)一次 round-trip 收尾,
	// 无需上限。唯一长耗时点是 still 串行下载,也在 limiter 控制下。
	for _, raw := range episodesRaw {
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
		stillPath, _ := m["still_path"].(string)
		metas[int32(ep)] = episodeMeta{
			episodeNumber: int32(ep),
			name:          strings.TrimSpace(name),
			overview:      strings.TrimSpace(overview),
			stillPath:     strings.TrimSpace(stillPath),
		}
	}
	if len(metas) == 0 {
		return nil
	}

	// 拉出本季所有 Episode items。
	epRows, err := pool.Query(ctx,
		"SELECT id::text, index_number, name, overview, primary_image_path FROM items WHERE parent_id = $1::uuid AND type = 'Episode'",
		seasonItemID)
	if err != nil {
		return err
	}

	// 第一轮:先把 name/overview 需要改的攒起来一次性 UPDATE。
	// still 下载因为涉及网络 I/O,不应该占着批量事务,单独在第二轮做。
	type stillTarget struct {
		id        string
		stillPath string
	}
	var (
		updIDs       []string
		updNames     []*string
		updOverviews []*string
		stillTargets []stillTarget
	)
	// 诊断:跳过 still 下载的三种原因分别计数
	var stillsTmdbEmpty, stillsAlreadyHas, stillsDisabled int
	for epRows.Next() {
		var id string
		var indexNum *int32
		var currentName, currentOverview, currentImagePath *string
		if err := epRows.Scan(&id, &indexNum, &currentName, &currentOverview, &currentImagePath); err != nil {
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
		if shouldUpdateName || shouldUpdateOverview {
			var nn, no *string
			if shouldUpdateName {
				n := meta.name
				nn = &n
			}
			if shouldUpdateOverview {
				o := meta.overview
				no = &o
			}
			updIDs = append(updIDs, id)
			updNames = append(updNames, nn)
			updOverviews = append(updOverviews, no)
		}

		if !stillFetchEnabled {
			stillsDisabled++
		} else if meta.stillPath == "" {
			stillsTmdbEmpty++
		} else if strings.TrimSpace(deref(currentImagePath)) != "" {
			stillsAlreadyHas++
		} else {
			stillTargets = append(stillTargets, stillTarget{id: id, stillPath: meta.stillPath})
		}
	}
	epRows.Close()

	// 一次 UPDATE 收尾所有 name/overview 变更,用 COALESCE 保留未指定字段的原值。
	if len(updIDs) > 0 {
		_, uerr := pool.Exec(ctx, `
			UPDATE items SET
				name = COALESCE(v.new_name, items.name),
				overview = COALESCE(v.new_overview, items.overview),
				updated_at = NOW()
			FROM unnest($1::uuid[], $2::text[], $3::text[]) AS v(id, new_name, new_overview)
			WHERE items.id = v.id`,
			updIDs, updNames, updOverviews)
		if uerr != nil {
			slog.Warn("[TMDB] batch update episodes failed", "season_id", seasonItemID, "error", uerr)
		}
	}

	// still 图下载 + 回写 primary_image_path;串行保持原有节奏避免压爆 TMDB CDN。
	// saveMode=media_dir/both 时先尝试写到 `<视频同目录>/<basename>-thumb.jpg`(Emby/Jellyfin
	// 约定,scanner 端 FindEpisodeThumbCached 首要识别的 pattern);media 写失败或路径解析
	// 不出(http / strm URL / 空 file_path)时回退 data/metadata。
	saveToData := saveMode == "database" || saveMode == "both"
	saveToMedia := saveMode == "media_dir" || saveMode == "both"
	var stillOK, stillFail int
	for _, t := range stillTargets {
		var dbPath string
		var dbTag *string
		mediaSaved := false
		if saveToMedia {
			if mediaPath := resolveEpisodeThumbMediaPath(ctx, pool, t.id); mediaPath != "" {
				if client.DownloadImage(ctx, t.stillPath, mediaPath, "w300") {
					dbPath = mediaPath
					dbTag = GenerateImageTag(mediaPath)
					mediaSaved = true
				}
			}
		}
		if saveToData || (saveToMedia && !mediaSaved) {
			dataPath := fmt.Sprintf("data/metadata/%s/still.jpg", t.id)
			if client.DownloadImage(ctx, t.stillPath, dataPath, "w300") && dbPath == "" {
				dbPath = dataPath
				dbTag = GenerateImageTag(dataPath)
			}
		}
		if dbPath != "" {
			_, _ = pool.Exec(ctx,
				"UPDATE items SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid",
				dbPath, dbTag, t.id)
			stillOK++
		} else {
			stillFail++
			slog.Debug("[TMDB] episode still download failed",
				"episode_id", t.id, "still_path", t.stillPath)
		}
	}
	if len(stillTargets) > 0 || len(updIDs) > 0 || stillsTmdbEmpty > 0 || stillsAlreadyHas > 0 {
		slog.Info("[TMDB] season episodes processed",
			"season_id", seasonItemID, "tmdb_id", tmdbID, "season", seasonNum,
			"name_overview_updated", len(updIDs),
			"stills_targeted", len(stillTargets), "stills_ok", stillOK, "stills_failed", stillFail,
			"stills_skipped_tmdb_empty", stillsTmdbEmpty,
			"stills_skipped_already_has", stillsAlreadyHas,
			"stills_skipped_disabled", stillsDisabled,
			"still_fetch_enabled", stillFetchEnabled)
	}
	return nil
}

// readEpisodeStillFetchEnabled 读 system_config.episode_still_fetch,默认 true。
func readEpisodeStillFetchEnabled(ctx context.Context, pool *pgxpool.Pool) bool {
	val, ok, err := repository.NewSystemConfigRepository(pool).GetString(ctx, "episode_still_fetch")
	if err != nil || !ok {
		return true
	}
	s := strings.ToLower(strings.TrimSpace(val))
	if s == "" {
		return true
	}
	return s == "1" || s == "true" || s == "yes" || s == "on"
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
