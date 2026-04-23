package scraper

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var ErrNoMatch = errors.New("scraper: no match")

const (
	DefaultThreshold = 0.72
	cacheTTLSuccess  = 7 * 24 * time.Hour
	cacheTTLEmpty    = 24 * time.Hour
	candidateTopN    = 5
)

// Matcher 负责把 ParsedName 映射到一个 provider 内部 ID。
// 它按 (ID 直达 → 带打分的搜索 → 降级链) 的顺序尝试，命中阈值即返回。
type Matcher struct {
	provider  Provider
	cache     Cache
	threshold float64
}

func NewMatcher(p Provider, c Cache) *Matcher {
	return &Matcher{provider: p, cache: c, threshold: DefaultThreshold}
}

// WithThreshold 覆盖默认阈值（用于后台设置）。
func (m *Matcher) WithThreshold(t float64) *Matcher {
	if t > 0 && t <= 1 {
		m.threshold = t
	}
	return m
}

// TryIDDirect 只做 ID 直达(本 provider 自己的 ID + 通过 FindByExternalID 解析常见跨源 ID),
// 不做 Search。Aggregator 做并发识别时用这个快路径。
func (m *Matcher) TryIDDirect(ctx context.Context, parsed ParsedName) (*Identity, error) {
	if id := m.tryOwnIDDirect(parsed); id != nil {
		return id, nil
	}
	for _, kind := range []string{"imdb", "tmdb", "tvdb", "bangumi", "douban"} {
		if kind == m.provider.Name() {
			continue
		}
		id, err := m.tryExternalID(ctx, parsed, kind)
		if err == nil && id != nil {
			id.Provider = m.provider.Name()
			return id, nil
		}
	}
	return nil, nil
}

// Identify 按置信度下降的顺序尝试识别。调用方已知 item 类型（Movie/Series）。
func (m *Matcher) Identify(ctx context.Context, parsed ParsedName, t MediaType) (*Identity, error) {
	if id, err := m.TryIDDirect(ctx, parsed); err == nil && id != nil {
		return id, nil
	}

	attempts := BuildSearchAttempts(parsed)
	for _, a := range attempts {
		if strings.TrimSpace(a.Query) == "" {
			continue
		}
		candidates, err := m.searchCached(ctx, t, a.Query, a.Year)
		if err != nil {
			slog.Debug("[Matcher] search error", "query", a.Query, "error", err)
			continue
		}
		best := pickBest(candidates, parsed)
		if best == nil {
			continue
		}
		slog.Debug("[Matcher] candidate",
			"provider", m.provider.Name(), "source", a.Source, "query", a.Query, "provider_id", best.cand.ProviderID,
			"title", best.cand.Title, "score", best.score)
		if best.score >= m.threshold {
			return &Identity{
				Provider:    m.provider.Name(),
				ProviderID:  best.cand.ProviderID,
				ExternalIDs: cloneIDs(best.cand.ExternalIDs),
				Score:       best.score,
				Source:      a.Source,
			}, nil
		}
	}
	return nil, ErrNoMatch
}

func (m *Matcher) Candidates(ctx context.Context, parsed ParsedName, t MediaType) ([]ScoredCandidate, error) {
	attempts := BuildSearchAttempts(parsed)
	seen := make(map[string]struct{})
	out := make([]ScoredCandidate, 0, candidateTopN)
	for _, a := range attempts {
		if strings.TrimSpace(a.Query) == "" {
			continue
		}
		candidates, err := m.searchCached(ctx, t, a.Query, a.Year)
		if err != nil {
			continue
		}
		limit := len(candidates)
		if limit > candidateTopN {
			limit = candidateTopN
		}
		for _, cand := range candidates[:limit] {
			if strings.TrimSpace(cand.ProviderID) == "" {
				continue
			}
			key := m.provider.Name() + ":" + cand.ProviderID
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, ScoredCandidate{
				Provider:      m.provider.Name(),
				ProviderID:    cand.ProviderID,
				ExternalIDs:   cloneIDs(cand.ExternalIDs),
				Title:         cand.Title,
				OriginalTitle: cand.OriginalTitle,
				Year:          cand.Year,
				Score:         scoreCandidate(cand, parsed),
				Popularity:    cand.Popularity,
				PosterURL:     cand.PosterURL,
				Source:        a.Source,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Score > out[j].Score
	})
	if len(out) > candidateTopN {
		out = out[:candidateTopN]
	}
	return out, nil
}

func (m *Matcher) tryOwnIDDirect(p ParsedName) *Identity {
	name := m.provider.Name()
	v, ok := p.IDs[name]
	if !ok {
		return nil
	}
	id := strings.TrimSpace(v)
	if id == "" || id == "0" {
		return nil
	}
	return &Identity{
		Provider:    name,
		ProviderID:  id,
		ExternalIDs: map[string]string{name: id},
		Score:       1.0,
		Source:      "id_direct_" + name,
	}
}

func (m *Matcher) tryExternalID(ctx context.Context, p ParsedName, kind string) (*Identity, error) {
	id := strings.TrimSpace(p.IDs[kind])
	if id == "" {
		return nil, nil
	}
	key := fmt.Sprintf("scraper:find:%s:%s:%s", m.provider.Name(), kind, strings.ToLower(id))
	var cached string
	if m.cache != nil && m.cache.GetJSON(ctx, key, &cached) && strings.TrimSpace(cached) != "" {
		return &Identity{
			Provider:    m.provider.Name(),
			ProviderID:  cached,
			ExternalIDs: map[string]string{kind: id},
			Score:       1.0,
			Source:      "id_direct_" + kind,
		}, nil
	}
	providerID, err := m.provider.FindByExternalID(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	providerID = strings.TrimSpace(providerID)
	if providerID == "" || providerID == "0" {
		return nil, nil
	}
	if m.cache != nil {
		m.cache.SetJSON(ctx, key, providerID, cacheTTLSuccess)
	}
	return &Identity{
		Provider:    m.provider.Name(),
		ProviderID:  providerID,
		ExternalIDs: map[string]string{kind: id},
		Score:       1.0,
		Source:      "id_direct_" + kind,
	}, nil
}

type SearchAttempt struct {
	Source string
	Query  string
	Year   *int32
}

// BuildSearchAttempts 返回识别阶段会依次尝试的搜索 query 序列。
func BuildSearchAttempts(p ParsedName) []SearchAttempt {
	var out []SearchAttempt
	if p.OriginalTitle != "" {
		out = append(out, SearchAttempt{"orig_title+year", p.OriginalTitle, p.Year})
	}
	if p.Title != "" && p.Title != p.OriginalTitle {
		out = append(out, SearchAttempt{"title+year", p.Title, p.Year})
	}
	if p.Year != nil {
		if p.OriginalTitle != "" {
			out = append(out, SearchAttempt{"orig_title_no_year", p.OriginalTitle, nil})
		}
		if p.Title != "" && p.Title != p.OriginalTitle {
			out = append(out, SearchAttempt{"title_no_year", p.Title, nil})
		}
	}
	if len(out) == 0 {
		return out
	}
	// 最后兜底：主 query 的首个 token（对抗 `Name.EXTRA.STUFF` 残留噪声）
	primary := out[0].Query
	if token := firstCoreToken(primary); token != "" && token != primary {
		out = append(out, SearchAttempt{"first_token", token, nil})
	}
	return out
}

func firstCoreToken(s string) string {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func (m *Matcher) searchCached(ctx context.Context, t MediaType, query string, year *int32) ([]Candidate, error) {
	key := searchCacheKey(m.provider.Name(), t, query, year)
	var cached []Candidate
	if m.cache != nil && m.cache.GetJSON(ctx, key, &cached) {
		return cached, nil
	}
	q := Query{Title: query, Year: year}
	candidates, err := m.provider.Search(ctx, t, q)
	if err != nil {
		return nil, err
	}
	if m.cache != nil {
		ttl := cacheTTLSuccess
		if len(candidates) == 0 {
			ttl = cacheTTLEmpty
		}
		m.cache.SetJSON(ctx, key, candidates, ttl)
	}
	return candidates, nil
}

func searchCacheKey(provider string, t MediaType, query string, year *int32) string {
	yr := ""
	if year != nil {
		yr = strconv.FormatInt(int64(*year), 10)
	}
	h := sha1.Sum([]byte(strings.ToLower(query) + "|" + yr))
	return fmt.Sprintf("scraper:search:%s:%s:%s", provider, strings.ToLower(string(t)), hex.EncodeToString(h[:]))
}

type scoredCandidate struct {
	cand  Candidate
	score float64
}

func pickBest(cands []Candidate, p ParsedName) *scoredCandidate {
	if len(cands) == 0 {
		return nil
	}
	top := cands
	if len(top) > candidateTopN {
		top = top[:candidateTopN]
	}
	var best *scoredCandidate
	for _, c := range top {
		if strings.TrimSpace(c.ProviderID) == "" {
			continue
		}
		s := scoreCandidate(c, p)
		if best == nil || s > best.score {
			cc := c
			best = &scoredCandidate{cand: cc, score: s}
		}
	}
	return best
}

func cloneIDs(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		if vv := strings.TrimSpace(v); vv != "" {
			dst[k] = vv
		}
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}

func scoreCandidate(c Candidate, p ParsedName) float64 {
	titleSim := bestTitleSimilarity(c, p)
	year := yearScore(c.Year, p.Year)
	pop := popularityScore(c.Popularity)
	return 0.55*titleSim + 0.30*year + 0.15*pop
}

func bestTitleSimilarity(c Candidate, p ParsedName) float64 {
	pairs := [][2]string{
		{p.Title, c.Title},
		{p.OriginalTitle, c.OriginalTitle},
		{p.Title, c.OriginalTitle},
		{p.OriginalTitle, c.Title},
	}
	var best float64
	for _, pair := range pairs {
		avs := titleCompareVariants(pair[0])
		bvs := titleCompareVariants(pair[1])
		for _, a := range avs {
			for _, b := range bvs {
				if a == "" || b == "" {
					continue
				}
				if s := jaroWinkler(a, b); s > best {
					best = s
				}
			}
		}
	}
	return best
}

func yearScore(a, b *int32) float64 {
	if a == nil || b == nil {
		return 0.5
	}
	diff := int32(0)
	if *a > *b {
		diff = *a - *b
	} else {
		diff = *b - *a
	}
	switch {
	case diff == 0:
		return 1.0
	case diff == 1:
		return 0.6
	case diff == 2:
		return 0.2
	default:
		return 0
	}
}

func popularityScore(pop float64) float64 {
	if pop <= 0 {
		return 0
	}
	v := pop / 50.0
	if v > 1 {
		v = 1
	}
	return v
}

// normalizeForCompare 小写 + 剥非字母数字 + 保留 CJK。
func normalizeForCompare(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(normalizeDigitRune(r))
		case r == ' ':
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func titleCompareVariants(s string) []string {
	base := normalizeForCompare(s)
	if base == "" {
		return nil
	}
	out := []string{base}
	if withArabic := normalizeChineseNumerals(base); withArabic != "" && withArabic != base {
		out = append(out, withArabic)
	}
	return out
}

func normalizeDigitRune(r rune) rune {
	if r >= '０' && r <= '９' {
		return '0' + (r - '０')
	}
	return r
}

func normalizeChineseNumerals(s string) string {
	if s == "" {
		return ""
	}
	var out strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); {
		if !isChineseNumeralRune(runes[i]) {
			out.WriteRune(runes[i])
			i++
			continue
		}
		j := i
		for j < len(runes) && isChineseNumeralRune(runes[j]) {
			j++
		}
		token := string(runes[i:j])
		if arabic, ok := chineseNumeralTokenToArabic(token); ok {
			out.WriteString(arabic)
		} else {
			out.WriteString(token)
		}
		i = j
	}
	return out.String()
}

func isChineseNumeralRune(r rune) bool {
	_, ok := chineseDigitValue[r]
	if ok {
		return true
	}
	_, ok = chineseUnitValue[r]
	return ok
}

var chineseDigitValue = map[rune]int{
	'零': 0, '〇': 0,
	'一': 1, '壹': 1,
	'二': 2, '贰': 2, '两': 2, '俩': 2,
	'三': 3, '叁': 3,
	'四': 4, '肆': 4,
	'五': 5, '伍': 5,
	'六': 6, '陆': 6,
	'七': 7, '柒': 7,
	'八': 8, '捌': 8,
	'九': 9, '玖': 9,
}

var chineseUnitValue = map[rune]int{
	'十': 10, '拾': 10,
	'百': 100, '佰': 100,
	'千': 1000, '仟': 1000,
	'万': 10000, '萬': 10000,
}

func chineseNumeralTokenToArabic(token string) (string, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	if !strings.ContainsAny(token, "十拾百佰千仟万萬") {
		var out strings.Builder
		for _, r := range token {
			v, ok := chineseDigitValue[r]
			if !ok {
				return "", false
			}
			out.WriteString(strconv.Itoa(v))
		}
		return out.String(), true
	}

	total := 0
	section := 0
	number := 0
	for _, r := range token {
		if v, ok := chineseDigitValue[r]; ok {
			number = v
			continue
		}
		unit, ok := chineseUnitValue[r]
		if !ok {
			return "", false
		}
		if unit < 10000 {
			if number == 0 {
				number = 1
			}
			section += number * unit
			number = 0
			continue
		}
		if number == 0 && section == 0 {
			section = 1
		}
		total += (section + number) * unit
		section = 0
		number = 0
	}
	total += section + number
	return strconv.Itoa(total), true
}

// jaroWinkler 在 rune 上实现的 Jaro-Winkler 相似度。
func jaroWinkler(a, b string) float64 {
	ra := []rune(a)
	rb := []rune(b)
	la := len(ra)
	lb := len(rb)
	if la == 0 && lb == 0 {
		return 1
	}
	if la == 0 || lb == 0 {
		return 0
	}

	matchDist := max(la, lb)/2 - 1
	if matchDist < 0 {
		matchDist = 0
	}
	aMatches := make([]bool, la)
	bMatches := make([]bool, lb)
	matches := 0
	for i := 0; i < la; i++ {
		start := i - matchDist
		if start < 0 {
			start = 0
		}
		end := i + matchDist + 1
		if end > lb {
			end = lb
		}
		for j := start; j < end; j++ {
			if bMatches[j] || ra[i] != rb[j] {
				continue
			}
			aMatches[i] = true
			bMatches[j] = true
			matches++
			break
		}
	}
	if matches == 0 {
		return 0
	}

	transpositions := 0
	k := 0
	for i := 0; i < la; i++ {
		if !aMatches[i] {
			continue
		}
		for !bMatches[k] {
			k++
		}
		if ra[i] != rb[k] {
			transpositions++
		}
		k++
	}

	m := float64(matches)
	jaro := (m/float64(la) + m/float64(lb) + (m-float64(transpositions)/2)/m) / 3

	prefix := 0
	for i := 0; i < la && i < lb && i < 4; i++ {
		if ra[i] != rb[i] {
			break
		}
		prefix++
	}
	return jaro + float64(prefix)*0.1*(1-jaro)
}
