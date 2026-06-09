package services

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"fyms/internal/models"
)

func fetchDoubanOverview(client *http.Client, name string) *string {
	suggestURL := fmt.Sprintf("https://movie.douban.com/j/subject_suggest?q=%s", url.QueryEscape(name))

	req, err := http.NewRequest(http.MethodGet, suggestURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Referer", "https://movie.douban.com/")

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var results []map[string]interface{}
	if err := json.Unmarshal(body, &results); err != nil || len(results) == 0 {
		return nil
	}

	subjectID, ok := results[0]["id"].(string)
	if !ok || subjectID == "" {
		return nil
	}

	detailURL := fmt.Sprintf("https://movie.douban.com/j/subject_abstract?subject_id=%s", subjectID)
	req2, err := http.NewRequest(http.MethodGet, detailURL, nil)
	if err != nil {
		return nil
	}
	req2.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req2.Header.Set("Referer", "https://movie.douban.com/")

	resp2, err := client.Do(req2)
	if err != nil {
		return nil
	}
	defer resp2.Body.Close()

	body2, err := io.ReadAll(resp2.Body)
	if err != nil {
		return nil
	}

	var detail map[string]interface{}
	if err := json.Unmarshal(body2, &detail); err != nil {
		return nil
	}

	subject, ok := detail["subject"].(map[string]interface{})
	if !ok {
		return nil
	}
	desc, ok := subject["short_description"].(string)
	if !ok || desc == "" {
		return nil
	}

	slog.Debug("[Douban] Got overview from Douban", "name", name)
	return &desc
}

// ========== Scrape item counters ==========
// 方案 C 后不再有 legacy ScrapeTask / ScrapeProgress:
// 全库刮削在 handler 层退化为一次 EnqueueMissingScrapeIdentify 的入队动作,
// 由 ScrapeWorker 消费 scrape_queue 实际执行。下面两个计数函数仍被
// buildEffectiveScrapeProgress 用来填 missing_count / items_total。

var platformAliases = map[string]string{
	"netflix":            "Netflix",
	"hbo":                "HBO",
	"hbo max":            "HBO",
	"max":                "HBO",
	"disney+":            "Disney+",
	"disney plus":        "Disney+",
	"apple tv+":          "Apple TV+",
	"apple tv":           "Apple TV+",
	"amazon":             "Amazon",
	"amazon studios":     "Amazon",
	"amazon prime video": "Amazon",
	"prime video":        "Amazon",
	"hulu":               "Hulu",
	"paramount+":         "Paramount+",
	"paramount plus":     "Paramount+",
	"peacock":            "Peacock",
	"showtime":           "Showtime",
	"starz":              "Starz",
	"crunchyroll":        "Crunchyroll",
	"fx":                 "FX",
	"fx productions":     "FX",
	"abc":                "ABC",
	"nbc":                "NBC",
	"cbs":                "CBS",
	"the cw":             "The CW",
	"bbc":                "BBC",
	"bbc one":            "BBC",
	"bbc two":            "BBC",
	"itv":                "ITV",
}

func canonicalPlatformAlias(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if canonical, ok := platformAliases[strings.ToLower(name)]; ok {
		return canonical
	}
	return models.CanonicalPlatformName(name)
}

func extractPlatformCandidates(details map[string]interface{}, itemType string) []string {
	var candidates []string
	if itemType == "Series" || itemType == "Episode" || itemType == "Season" {
		if networks, ok := details["networks"].([]interface{}); ok {
			for _, n := range networks {
				nm, ok := n.(map[string]interface{})
				if !ok {
					continue
				}
				name, ok := nm["name"].(string)
				if ok && strings.TrimSpace(name) != "" {
					candidates = append(candidates, name)
				}
			}
		}
	}
	if companies, ok := details["production_companies"].([]interface{}); ok {
		for _, c := range companies {
			cm, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			name, ok := cm["name"].(string)
			if ok && strings.TrimSpace(name) != "" {
				candidates = append(candidates, name)
			}
		}
	}
	return candidates
}

// ExtractPlatform extracts a canonical platform name from TMDB details.
func ExtractPlatform(details map[string]interface{}, itemType string) *string {
	for _, candidate := range extractPlatformCandidates(details, itemType) {
		if canonical := canonicalPlatformAlias(candidate); canonical != "" {
			return &canonical
		}
	}
	return nil
}
