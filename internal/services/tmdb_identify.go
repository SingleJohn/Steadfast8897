package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/services/scraper"
)

type externalIDRecord struct {
	Provider string
	Value    string
}

type identifyCandidateRecord struct {
	ID         string                 `json:"id"`
	ItemID     string                 `json:"item_id"`
	Provider   string                 `json:"provider"`
	ExternalID string                 `json:"external_id"`
	Title      string                 `json:"title"`
	Year       *int32                 `json:"year,omitempty"`
	PosterURL  string                 `json:"poster_url"`
	Score      float64                `json:"score"`
	Payload    map[string]interface{} `json:"payload,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
}

type identifyFailureDetail struct {
	Stage                  string                           `json:"stage"`
	Reason                 string                           `json:"reason"`
	Threshold              float64                          `json:"threshold"`
	AutoApply              bool                             `json:"auto_apply"`
	AdultFilterEnabled     bool                             `json:"adult_filter_enabled"`
	Providers              []string                         `json:"providers,omitempty"`
	Parsed                 identifyFailureParsed            `json:"parsed"`
	Matched                *identifyFailureMatched          `json:"matched,omitempty"`
	SearchAttempts         []identifyFailureSearchAttempt   `json:"search_attempts,omitempty"`
	CandidatesTotal        int                              `json:"candidates_total"`
	BlockedCandidatesTotal int                              `json:"blocked_candidates_total,omitempty"`
	BestScore              *float64                         `json:"best_score,omitempty"`
	Candidates             []identifyFailureCandidateRecord `json:"candidates,omitempty"`
	BlockedCandidates      []identifyFailureCandidateRecord `json:"blocked_candidates,omitempty"`
}

type identifyFailureParsed struct {
	Title         string            `json:"title,omitempty"`
	OriginalTitle string            `json:"original_title,omitempty"`
	Year          *int32            `json:"year,omitempty"`
	IDs           map[string]string `json:"ids,omitempty"`
	MediaHint     string            `json:"media_hint,omitempty"`
	Junk          []string          `json:"junk,omitempty"`
}

type identifyFailureSearchAttempt struct {
	Source string `json:"source"`
	Query  string `json:"query"`
	Year   *int32 `json:"year,omitempty"`
}

type identifyFailureMatched struct {
	Provider    string            `json:"provider"`
	ProviderID  string            `json:"provider_id"`
	Source      string            `json:"source,omitempty"`
	Score       float64           `json:"score,omitempty"`
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
}

type identifyFailureCandidateRecord struct {
	Provider       string            `json:"provider"`
	ProviderID     string            `json:"provider_id"`
	Title          string            `json:"title"`
	OriginalTitle  string            `json:"original_title,omitempty"`
	Year           *int32            `json:"year,omitempty"`
	Score          float64           `json:"score"`
	Popularity     float64           `json:"popularity,omitempty"`
	Source         string            `json:"source,omitempty"`
	ExternalIDs    map[string]string `json:"external_ids,omitempty"`
	PosterURL      string            `json:"poster_url,omitempty"`
	Blocked        bool              `json:"blocked,omitempty"`
	AdultReasons   []string          `json:"adult_reasons,omitempty"`
	Certifications []string          `json:"certifications,omitempty"`
}

type scrapeItemMeta struct {
	ItemType    string
	Name        string
	Year        *int32
	TmdbID      *int32
	ImdbID      *string
	FilePath    *string
	LibraryID   string // 用于 per-library 刮削配置
	ExternalIDs map[string]string
}

func loadScrapeItemMeta(ctx context.Context, pool *pgxpool.Pool, itemID string) (*scrapeItemMeta, error) {
	meta := &scrapeItemMeta{ExternalIDs: map[string]string{}}
	var providerIDsRaw []byte
	err := pool.QueryRow(ctx,
		"SELECT type, name, production_year, tmdb_id, imdb_id, file_path, library_id::text, provider_ids FROM items WHERE id = $1::uuid", itemID,
	).Scan(&meta.ItemType, &meta.Name, &meta.Year, &meta.TmdbID, &meta.ImdbID, &meta.FilePath, &meta.LibraryID, &providerIDsRaw)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("item not found")
	}
	if err != nil {
		return nil, fmt.Errorf("query item: %w", err)
	}
	mergeProviderIDs(meta.ExternalIDs, providerIDsRaw)

	rows, err := pool.Query(ctx,
		"SELECT provider, external_id FROM item_external_ids WHERE item_id = $1::uuid",
		itemID)
	if err != nil {
		return nil, fmt.Errorf("query item external ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var provider, externalID string
		if err := rows.Scan(&provider, &externalID); err != nil {
			return nil, fmt.Errorf("scan item external ids: %w", err)
		}
		provider = strings.ToLower(strings.TrimSpace(provider))
		externalID = strings.TrimSpace(externalID)
		if provider != "" && externalID != "" {
			meta.ExternalIDs[provider] = externalID
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate item external ids: %w", err)
	}
	return meta, nil
}

// tmdbSetIdentifyAttempted 记录"尝试识别过一次"(不区分成功/失败)。
// Phase 5 前这里还会同时设置 identify_cooldown_until 做整块冷却,现在冷却语义
// 由 scrape_queue.next_run_at + 指数退避接管,attempted_at 仅作诊断/审计。
func tmdbSetIdentifyAttempted(ctx context.Context, pool *pgxpool.Pool, itemID string) {
	_, err := pool.Exec(ctx,
		"UPDATE items SET identify_attempted_at = NOW() WHERE id = $1::uuid",
		itemID)
	if err != nil {
		slog.Debug("[TMDB] set identify_attempted_at failed", "item_id", itemID, "error", err)
	}
}

// buildParsedName 从 item meta + 文件路径构造 ParsedName，给 Matcher 使用。
func buildParsedName(meta *scrapeItemMeta) scraper.ParsedName {
	mode := scraper.ModeMovie
	if meta.ItemType == "Series" {
		mode = scraper.ModeSeries
	}

	candidates := collectParsedNameCandidates(meta, mode)
	parsed := pickPrimaryParsedCandidate(candidates, meta.ItemType)
	parsed.SearchSeeds = buildSearchSeeds(candidates, parsed)

	// DB 侧的 year 最可信，覆盖解析结果
	if meta.Year != nil && *meta.Year > 0 {
		parsed.Year = meta.Year
		for i := range parsed.SearchSeeds {
			parsed.SearchSeeds[i].Year = meta.Year
		}
	}
	if parsed.IDs == nil {
		parsed.IDs = map[string]string{}
	}
	for _, cand := range candidates {
		for kind, id := range cand.Parsed.IDs {
			if strings.TrimSpace(parsed.IDs[kind]) == "" && strings.TrimSpace(id) != "" {
				parsed.IDs[kind] = strings.TrimSpace(id)
			}
		}
	}
	if meta.TmdbID != nil && *meta.TmdbID > 0 && parsed.IDs["tmdb"] == "" {
		parsed.IDs["tmdb"] = strconv.Itoa(int(*meta.TmdbID))
	}
	if meta.ImdbID != nil && strings.TrimSpace(*meta.ImdbID) != "" && parsed.IDs["imdb"] == "" {
		parsed.IDs["imdb"] = strings.TrimSpace(*meta.ImdbID)
	}
	for kind, id := range meta.ExternalIDs {
		if strings.TrimSpace(parsed.IDs[kind]) == "" && strings.TrimSpace(id) != "" {
			parsed.IDs[kind] = strings.TrimSpace(id)
		}
	}
	// Title 兜底：若归一化后 Title/OriginalTitle 都为空，用 items.name
	if parsed.Title == "" && parsed.OriginalTitle == "" {
		parsed.Title = meta.Name
	}
	return parsed
}

type parsedNameCandidate struct {
	Source string
	Raw    string
	Parsed scraper.ParsedName
}

func collectParsedNameCandidates(meta *scrapeItemMeta, mode scraper.ParseMode) []parsedNameCandidate {
	candidates := make([]parsedNameCandidate, 0, 4)
	seen := map[string]struct{}{}
	add := func(source, raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		key := strings.ToLower(raw)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		candidates = append(candidates, parsedNameCandidate{
			Source: source,
			Raw:    raw,
			Parsed: scraper.Parse(raw, mode),
		})
	}

	add("item_name", meta.Name)
	if meta.FilePath != nil && strings.TrimSpace(*meta.FilePath) != "" {
		fp := strings.TrimSpace(*meta.FilePath)
		add("file_basename", filepath.Base(fp))
		add("parent_folder", filepath.Base(filepath.Dir(fp)))
		add("grandparent_folder", filepath.Base(filepath.Dir(filepath.Dir(fp))))
	}
	return candidates
}

func pickPrimaryParsedCandidate(candidates []parsedNameCandidate, itemType string) scraper.ParsedName {
	if len(candidates) == 0 {
		return scraper.ParsedName{IDs: make(map[string]string)}
	}
	best := candidates[0]
	bestScore := scoreParsedNameCandidate(best, itemType)
	for _, cand := range candidates[1:] {
		if score := scoreParsedNameCandidate(cand, itemType); score > bestScore {
			best = cand
			bestScore = score
		}
	}
	parsed := best.Parsed
	if parsed.IDs == nil {
		parsed.IDs = make(map[string]string)
	}
	return parsed
}

func scoreParsedNameCandidate(c parsedNameCandidate, itemType string) int {
	score := 0
	if len(c.Parsed.IDs) > 0 {
		score += 100
	}
	if c.Parsed.Year != nil {
		score += 30
	}
	if title := primaryParsedTitle(c.Parsed); title != "" {
		score += 20
		if !scraper.IsWeakTitle(title) {
			score += 25
		} else {
			score -= 40
		}
	}
	if c.Parsed.Title != "" && c.Parsed.OriginalTitle != "" {
		score += 10
	}
	if c.Parsed.Season != nil || c.Parsed.Episode != nil {
		score -= 20
	}
	switch c.Source {
	case "item_name":
		score += 18
	case "file_basename":
		score += 12
	case "parent_folder":
		score += 16
	case "grandparent_folder":
		score += 8
	}
	if itemType == "Movie" && c.Source == "parent_folder" {
		score += 10
	}
	if itemType == "Series" && c.Source == "file_basename" {
		score += 12
	}
	return score
}

func primaryParsedTitle(p scraper.ParsedName) string {
	if s := strings.TrimSpace(p.Title); s != "" {
		return s
	}
	return strings.TrimSpace(p.OriginalTitle)
}

func buildSearchSeeds(candidates []parsedNameCandidate, primary scraper.ParsedName) []scraper.SearchSeed {
	if len(candidates) == 0 {
		return nil
	}
	out := make([]scraper.SearchSeed, 0, len(candidates))
	seen := map[string]struct{}{}
	add := func(c parsedNameCandidate) {
		seed := scraper.SearchSeed{
			Source:        c.Source,
			Title:         strings.TrimSpace(c.Parsed.Title),
			OriginalTitle: strings.TrimSpace(c.Parsed.OriginalTitle),
			Year:          c.Parsed.Year,
		}
		title := primaryParsedTitle(c.Parsed)
		seed.Weak = title == "" || scraper.IsWeakTitle(title)
		key := strings.ToLower(seed.Source + "|" + seed.Title + "|" + seed.OriginalTitle)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, seed)
	}

	primaryKey := strings.ToLower(primary.Title + "|" + primary.OriginalTitle)
	for _, cand := range candidates {
		if strings.ToLower(cand.Parsed.Title+"|"+cand.Parsed.OriginalTitle) == primaryKey {
			add(cand)
			break
		}
	}
	for _, cand := range candidates {
		add(cand)
	}
	return out
}

func mergeProviderIDs(dst map[string]string, raw []byte) {
	if len(raw) == 0 || dst == nil {
		return
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	for key, value := range payload {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			continue
		}
		switch v := value.(type) {
		case string:
			if s := strings.TrimSpace(v); s != "" {
				dst[key] = s
			}
		case float64:
			if v > 0 {
				dst[key] = strconv.FormatInt(int64(v), 10)
			}
		}
	}
}

func upsertExternalIDs(ctx context.Context, pool *pgxpool.Pool, itemID string, ids []externalIDRecord) {
	if pool == nil || len(ids) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(ids))
	providerMap := make(map[string]string, len(ids))
	for _, rec := range ids {
		provider := strings.ToLower(strings.TrimSpace(rec.Provider))
		value := strings.TrimSpace(rec.Value)
		if provider == "" || value == "" {
			continue
		}
		if _, ok := seen[provider]; ok {
			continue
		}
		seen[provider] = struct{}{}
		providerMap[provider] = value
		_, err := pool.Exec(ctx,
			`INSERT INTO item_external_ids (item_id, provider, external_id, updated_at)
			 VALUES ($1::uuid, $2, $3, NOW())
			 ON CONFLICT (item_id, provider)
			 DO UPDATE SET external_id = EXCLUDED.external_id,
			               updated_at = EXCLUDED.updated_at`,
			itemID, provider, value)
		if err != nil {
			slog.Warn("[Scraper] upsert item_external_ids failed", "item_id", itemID, "provider", provider, "error", err)
			continue
		}
	}
	if len(providerMap) == 0 {
		return
	}
	if raw, err := json.Marshal(providerMap); err == nil {
		_, err = pool.Exec(ctx,
			"UPDATE items SET provider_ids = $1::jsonb, updated_at = NOW() WHERE id = $2::uuid",
			string(raw), itemID)
		if err != nil {
			slog.Warn("[Scraper] update provider_ids failed", "item_id", itemID, "error", err)
		}
	}
}

func replaceIdentifyCandidates(ctx context.Context, pool *pgxpool.Pool, itemID string, candidates []scraper.ScoredCandidate) error {
	if _, err := pool.Exec(ctx, "DELETE FROM identify_candidates WHERE item_id = $1::uuid", itemID); err != nil {
		return err
	}
	for _, cand := range candidates {
		payload, _ := json.Marshal(map[string]interface{}{
			"provider":       cand.Provider,
			"provider_id":    cand.ProviderID,
			"external_ids":   cand.ExternalIDs,
			"original_title": cand.OriginalTitle,
			"source":         cand.Source,
			"popularity":     cand.Popularity,
			"poster_url":     cand.PosterURL,
			"adult_content":  cand.AdultContent,
			"adult_reasons":  cand.AdultReasons,
			"certifications": cand.Certifications,
		})
		var year interface{}
		if cand.Year != nil {
			year = *cand.Year
		}
		_, err := pool.Exec(ctx,
			`INSERT INTO identify_candidates (item_id, provider, external_id, title, year, poster_url, score, payload)
			 VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
			itemID,
			cand.Provider,
			cand.ProviderID,
			cand.Title,
			year,
			strings.TrimSpace(cand.PosterURL),
			float32(cand.Score),
			string(payload),
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func ListIdentifyCandidates(ctx context.Context, pool *pgxpool.Pool, itemID string) ([]identifyCandidateRecord, error) {
	rows, err := pool.Query(ctx,
		`SELECT id::text, item_id::text, provider, external_id, COALESCE(title, ''), year, COALESCE(poster_url, ''), COALESCE(score, 0), payload, created_at
		   FROM identify_candidates
		  WHERE item_id = $1::uuid
		  ORDER BY score DESC, created_at DESC`,
		itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []identifyCandidateRecord
	for rows.Next() {
		var rec identifyCandidateRecord
		var payload []byte
		if err := rows.Scan(&rec.ID, &rec.ItemID, &rec.Provider, &rec.ExternalID, &rec.Title, &rec.Year, &rec.PosterURL, &rec.Score, &payload, &rec.CreatedAt); err != nil {
			return nil, err
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &rec.Payload)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func buildIdentifyFailureDetail(
	parsed scraper.ParsedName,
	candidates []scraper.ScoredCandidate,
	threshold float64,
	providers []string,
	autoApply bool,
	adultFilterEnabled bool,
) identifyFailureDetail {
	attempts := scraper.BuildSearchAttempts(parsed)
	searchAttempts := make([]identifyFailureSearchAttempt, 0, len(attempts))
	for _, a := range attempts {
		if strings.TrimSpace(a.Query) == "" {
			continue
		}
		searchAttempts = append(searchAttempts, identifyFailureSearchAttempt{
			Source: a.Source,
			Query:  a.Query,
			Year:   a.Year,
		})
	}

	detail := identifyFailureDetail{
		Stage:              "identify",
		Threshold:          roundFloat(threshold, 3),
		AutoApply:          autoApply,
		AdultFilterEnabled: adultFilterEnabled,
		Providers:          append([]string(nil), providers...),
		Parsed: identifyFailureParsed{
			Title:         parsed.Title,
			OriginalTitle: parsed.OriginalTitle,
			Year:          parsed.Year,
			IDs:           cloneStringMap(parsed.IDs),
			MediaHint:     parsed.MediaHint,
			Junk:          append([]string(nil), parsed.Junk...),
		},
		SearchAttempts:  searchAttempts,
		CandidatesTotal: len(candidates),
		Candidates:      make([]identifyFailureCandidateRecord, 0, len(candidates)),
	}
	if len(candidates) == 0 {
		detail.Reason = "no candidate returned by providers"
		return detail
	}

	bestScore := roundFloat(candidates[0].Score, 3)
	detail.BestScore = &bestScore
	detail.Reason = fmt.Sprintf("best score %.3f below threshold %.3f", candidates[0].Score, threshold)
	for _, c := range candidates {
		detail.Candidates = append(detail.Candidates, identifyFailureCandidateRecordFromScored(c, false))
	}
	return detail
}

func buildAdultBlockedDetail(
	stage string,
	reason string,
	parsed scraper.ParsedName,
	cfg scraper.RuntimeConfig,
	providers []string,
	ident *scraper.Identity,
	candidates []scraper.ScoredCandidate,
	blocked []scraper.AdultBlockedCandidate,
) identifyFailureDetail {
	searchAttempts := make([]identifyFailureSearchAttempt, 0)
	if strings.EqualFold(strings.TrimSpace(stage), "identify") {
		for _, a := range scraper.BuildSearchAttempts(parsed) {
			if strings.TrimSpace(a.Query) == "" {
				continue
			}
			searchAttempts = append(searchAttempts, identifyFailureSearchAttempt{
				Source: a.Source,
				Query:  a.Query,
				Year:   a.Year,
			})
		}
	}
	detail := identifyFailureDetail{
		Stage:              strings.TrimSpace(stage),
		Reason:             strings.TrimSpace(reason),
		Threshold:          roundFloat(cfg.ConfidenceThreshold, 3),
		AutoApply:          cfg.AutoApply,
		AdultFilterEnabled: cfg.AdultContentFilterEnabled,
		Providers:          append([]string(nil), providers...),
		Parsed: identifyFailureParsed{
			Title:         parsed.Title,
			OriginalTitle: parsed.OriginalTitle,
			Year:          parsed.Year,
			IDs:           cloneStringMap(parsed.IDs),
			MediaHint:     parsed.MediaHint,
			Junk:          append([]string(nil), parsed.Junk...),
		},
		SearchAttempts:         searchAttempts,
		CandidatesTotal:        len(candidates),
		BlockedCandidatesTotal: len(blocked),
		Candidates:             make([]identifyFailureCandidateRecord, 0, len(candidates)),
		BlockedCandidates:      make([]identifyFailureCandidateRecord, 0, len(blocked)),
	}
	if ident != nil {
		detail.Matched = &identifyFailureMatched{
			Provider:    ident.Provider,
			ProviderID:  ident.ProviderID,
			Source:      ident.Source,
			Score:       roundFloat(ident.Score, 3),
			ExternalIDs: cloneStringMap(ident.ExternalIDs),
		}
	}
	for _, cand := range candidates {
		detail.Candidates = append(detail.Candidates, identifyFailureCandidateRecordFromScored(cand, false))
	}
	for _, item := range blocked {
		detail.BlockedCandidates = append(detail.BlockedCandidates, identifyFailureCandidateRecord{
			Provider:       item.Provider,
			ProviderID:     item.ProviderID,
			Title:          item.Title,
			OriginalTitle:  item.OriginalTitle,
			Year:           item.Year,
			Score:          roundFloat(item.Score, 3),
			Popularity:     roundFloat(item.Popularity, 3),
			Source:         item.Source,
			ExternalIDs:    cloneStringMap(item.ExternalIDs),
			PosterURL:      strings.TrimSpace(item.PosterURL),
			Blocked:        true,
			AdultReasons:   append([]string(nil), item.AdultReasons...),
			Certifications: append([]string(nil), item.Certifications...),
		})
	}
	return detail
}

func identifyFailureCandidateRecordFromScored(c scraper.ScoredCandidate, blocked bool) identifyFailureCandidateRecord {
	return identifyFailureCandidateRecord{
		Provider:       c.Provider,
		ProviderID:     c.ProviderID,
		Title:          c.Title,
		OriginalTitle:  c.OriginalTitle,
		Year:           c.Year,
		Score:          roundFloat(c.Score, 3),
		Popularity:     roundFloat(c.Popularity, 3),
		Source:         c.Source,
		ExternalIDs:    cloneStringMap(c.ExternalIDs),
		PosterURL:      strings.TrimSpace(c.PosterURL),
		Blocked:        blocked,
		AdultReasons:   append([]string(nil), c.AdultReasons...),
		Certifications: append([]string(nil), c.Certifications...),
	}
}

// logScrapeFailureDetail 打印识别/填充失败时的完整诊断。
func logScrapeFailureDetail(itemID string, detail identifyFailureDetail) {
	slog.Info("[Scrape] failed",
		"item_id", itemID,
		"stage", detail.Stage,
		"parsed_title", detail.Parsed.Title,
		"parsed_original", detail.Parsed.OriginalTitle,
		"parsed_year", formatYear(detail.Parsed.Year),
		"parsed_ids", detail.Parsed.IDs,
		"providers", detail.Providers,
		"threshold", detail.Threshold,
		"adult_filter_enabled", detail.AdultFilterEnabled,
		"matched", detail.Matched,
		"search_attempts", detail.SearchAttempts,
		"candidates_total", detail.CandidatesTotal,
		"blocked_candidates_total", detail.BlockedCandidatesTotal,
		"top_candidates", detail.Candidates,
		"blocked_candidates", detail.BlockedCandidates,
		"reason", detail.Reason)
}
