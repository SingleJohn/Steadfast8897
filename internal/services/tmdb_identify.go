package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
	"fyms/internal/services/scraper"
)

type externalIDRecord = repository.ExternalIDRecord

type identifyCandidateRecord = repository.IdentifyCandidateRow

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

type scrapeItemMeta = repository.ScrapeItemMeta

func loadScrapeItemMeta(ctx context.Context, pool *pgxpool.Pool, itemID string) (*scrapeItemMeta, error) {
	return repository.NewBackgroundTaskRepository(pool).LoadScrapeItemMeta(ctx, itemID)
}

// tmdbSetIdentifyAttempted 记录"尝试识别过一次"(不区分成功/失败)。
// Phase 5 前这里还会同时设置 identify_cooldown_until 做整块冷却,现在冷却语义
// 由 scrape_queue.next_run_at + 指数退避接管,attempted_at 仅作诊断/审计。
func tmdbSetIdentifyAttempted(ctx context.Context, pool *pgxpool.Pool, itemID string) {
	if err := repository.NewBackgroundTaskRepository(pool).MarkIdentifyAttempted(ctx, itemID); err != nil {
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
	if err := repository.NewBackgroundTaskRepository(pool).UpsertExternalIDs(ctx, itemID, ids); err != nil {
		slog.Warn("[Scraper] upsert item_external_ids failed", "item_id", itemID, "error", err)
	}
}

func replaceIdentifyCandidates(ctx context.Context, pool *pgxpool.Pool, itemID string, candidates []scraper.ScoredCandidate) error {
	rows := make([]repository.IdentifyCandidateUpsert, 0, len(candidates))
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
		rows = append(rows, repository.IdentifyCandidateUpsert{
			Provider:   cand.Provider,
			ExternalID: cand.ProviderID,
			Title:      cand.Title,
			Year:       year,
			PosterURL:  strings.TrimSpace(cand.PosterURL),
			Score:      cand.Score,
			Payload:    string(payload),
		})
	}
	return repository.NewBackgroundTaskRepository(pool).ReplaceIdentifyCandidates(ctx, itemID, rows)
}

func ListIdentifyCandidates(ctx context.Context, pool *pgxpool.Pool, itemID string) ([]identifyCandidateRecord, error) {
	return repository.NewBackgroundTaskRepository(pool).ListIdentifyCandidates(ctx, itemID)
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
