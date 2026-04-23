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
	DefaultThreshold  = 0.72
	cacheTTLSuccess   = 7 * 24 * time.Hour
	cacheTTLEmpty     = 24 * time.Hour
	candidateTopN     = 5
	maxSearchAttempts = 10
	nearThresholdGap  = 0.08
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
		candidates, err := m.searchCached(ctx, t, a)
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
		if best.score >= m.threshold-nearThresholdGap && m.passesSecondaryConfirm(candidates, parsed, best) {
			return &Identity{
				Provider:    m.provider.Name(),
				ProviderID:  best.cand.ProviderID,
				ExternalIDs: cloneIDs(best.cand.ExternalIDs),
				Score:       best.score,
				Source:      a.Source + "+secondary_confirm",
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
		candidates, err := m.searchCached(ctx, t, a)
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
	Source         string
	Query          string
	Year           *int32
	PreferOriginal bool
}

// BuildSearchAttempts 返回识别阶段会依次尝试的搜索 query 序列。
func BuildSearchAttempts(p ParsedName) []SearchAttempt {
	out := make([]SearchAttempt, 0, maxSearchAttempts)
	seen := map[string]struct{}{}

	appendAttempt := func(source, query string, year *int32, preferOriginal bool) bool {
		query = strings.TrimSpace(query)
		if query == "" || len(out) >= maxSearchAttempts {
			return false
		}
		key := strings.ToLower(query)
		if year != nil {
			key += "|" + strconv.FormatInt(int64(*year), 10)
		}
		if preferOriginal {
			key += "|orig"
		}
		if _, ok := seen[key]; ok {
			return false
		}
		seen[key] = struct{}{}
		out = append(out, SearchAttempt{Source: source, Query: query, Year: year, PreferOriginal: preferOriginal})
		return true
	}

	appendSeed := func(seed SearchSeed) {
		if len(out) >= maxSearchAttempts {
			return
		}
		year := seed.Year
		if year == nil {
			year = p.Year
		}
		if seed.OriginalTitle != "" {
			appendAttempt(seed.Source+"_orig_title+year", seed.OriginalTitle, year, true)
		}
		if seed.Title != "" && seed.Title != seed.OriginalTitle {
			appendAttempt(seed.Source+"_title+year", seed.Title, year, false)
		}
		full := strings.TrimSpace(strings.Join(compactStrings(seed.Title, seed.OriginalTitle), " "))
		if full != "" && full != seed.Title && full != seed.OriginalTitle {
			appendAttempt(seed.Source+"_full_bilingual", full, nil, false)
		}
		if year != nil {
			if seed.OriginalTitle != "" {
				appendAttempt(seed.Source+"_orig_title_no_year", seed.OriginalTitle, nil, true)
			}
			if seed.Title != "" && seed.Title != seed.OriginalTitle {
				appendAttempt(seed.Source+"_title_no_year", seed.Title, nil, false)
			}
		}
	}

	seeds := prioritizeSearchSeeds(p)
	weakSeeds := make([]SearchSeed, 0, len(seeds))
	if len(p.SearchSeeds) > 0 {
		for _, seed := range seeds {
			if seed.Weak {
				weakSeeds = append(weakSeeds, seed)
				continue
			}
			appendSeed(seed)
		}
		if len(out) == 0 {
			for _, seed := range weakSeeds {
				appendSeed(seed)
			}
		}
	} else {
		appendSeed(SearchSeed{
			Source:        "parsed",
			Title:         p.Title,
			OriginalTitle: p.OriginalTitle,
			Year:          p.Year,
		})
	}

	if len(out) == 0 {
		return out
	}
	primary := out[0].Query
	if token := firstCoreToken(primary); token != "" && token != primary {
		appendAttempt("first_token", token, nil, false)
	}
	return out
}

func prioritizeSearchSeeds(p ParsedName) []SearchSeed {
	if len(p.SearchSeeds) == 0 {
		return nil
	}
	seeds := append([]SearchSeed(nil), p.SearchSeeds...)
	sort.SliceStable(seeds, func(i, j int) bool {
		return searchSeedPriority(seeds[i], p) < searchSeedPriority(seeds[j], p)
	})
	return seeds
}

func searchSeedPriority(seed SearchSeed, p ParsedName) int {
	priority := 10
	if seed.Weak {
		priority += 40
	}
	switch seed.Source {
	case "item_name":
		priority -= 3
	case "file_basename":
		priority -= 2
	case "parent_folder":
		priority -= 1
	}
	switch strings.ToLower(strings.TrimSpace(p.MediaHint)) {
	case "anime":
		if seed.OriginalTitle != "" && seed.OriginalTitle != seed.Title {
			priority -= 8
		}
	case "series":
		if seed.Source == "item_name" || seed.Source == "file_basename" {
			priority -= 3
		}
	case "movie":
		if seed.Year != nil {
			priority -= 2
		}
	}
	return priority
}

func firstCoreToken(s string) string {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func compactStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (m *Matcher) searchCached(ctx context.Context, t MediaType, attempt SearchAttempt) ([]Candidate, error) {
	key := searchCacheKey(m.provider.Name(), t, attempt.Query, attempt.Year, attempt.PreferOriginal)
	var cached []Candidate
	if m.cache != nil && m.cache.GetJSON(ctx, key, &cached) {
		return cached, nil
	}
	q := Query{Year: attempt.Year, Hint: attempt.Source}
	if attempt.PreferOriginal {
		q.OriginalTitle = attempt.Query
	} else {
		q.Title = attempt.Query
	}
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

func searchCacheKey(provider string, t MediaType, query string, year *int32, preferOriginal bool) string {
	yr := ""
	if year != nil {
		yr = strconv.FormatInt(int64(*year), 10)
	}
	mode := "title"
	if preferOriginal {
		mode = "orig"
	}
	h := sha1.Sum([]byte(strings.ToLower(query) + "|" + yr + "|" + mode))
	return fmt.Sprintf("scraper:search:%s:%s:%s", provider, strings.ToLower(string(t)), hex.EncodeToString(h[:]))
}

type scoredCandidate struct {
	cand  Candidate
	score float64
}

func pickBest(cands []Candidate, p ParsedName) *scoredCandidate {
	ranked := rankCandidates(cands, p, 1)
	if len(ranked) == 0 {
		return nil
	}
	return &ranked[0]
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
	score := 0.55*titleSim + 0.30*year + 0.15*pop + mediaHintBonus(c, p, titleSim, year)
	if score > 1 {
		return 1
	}
	return score
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
	s = strings.TrimSpace(strings.ToLower(normalizeCompareSymbols(s)))
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
	out := make([]string, 0, 6)
	appendVariant := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range out {
			if existing == value {
				return
			}
		}
		out = append(out, value)
	}

	appendVariant(base)
	appendVariant(strings.ReplaceAll(base, " ", ""))
	if withArabic := normalizeChineseNumerals(base); withArabic != "" {
		appendVariant(withArabic)
		appendVariant(strings.ReplaceAll(withArabic, " ", ""))
		if withRomanArabic := normalizeRomanNumerals(withArabic); withRomanArabic != "" {
			appendVariant(withRomanArabic)
			appendVariant(strings.ReplaceAll(withRomanArabic, " ", ""))
		}
	}
	if withRomanArabic := normalizeRomanNumerals(base); withRomanArabic != "" {
		appendVariant(withRomanArabic)
		appendVariant(strings.ReplaceAll(withRomanArabic, " ", ""))
	}
	return out
}

func normalizeCompareSymbols(s string) string {
	if s == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"：", " ", ":", " ",
		"·", " ", "・", " ", "•", " ",
		"．", " ", ".", " ",
		"—", " ", "–", " ", "-", " ", "_", " ",
		"/", " ", "\\", " ",
		"（", " ", "）", " ", "(", " ", ")", " ",
		"【", " ", "】", " ", "[", " ", "]", " ",
		"“", " ", "”", " ", "\"", " ",
		"‘", " ", "’", " ", "'", " ", "`", " ",
		"＋", " ", "+", " ",
		"＆", " ", "&", " ",
	)
	return replacer.Replace(s)
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

func normalizeRomanNumerals(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}
	changed := false
	for i, part := range parts {
		if arabic, ok := romanTokenToArabic(part); ok {
			parts[i] = arabic
			changed = true
		}
	}
	if !changed {
		return s
	}
	return strings.Join(parts, " ")
}

func romanTokenToArabic(token string) (string, bool) {
	token = strings.ToUpper(strings.TrimSpace(token))
	if token == "" || len(token) > 6 {
		return "", false
	}
	for _, r := range token {
		if !strings.ContainsRune("IVXLCDM", r) {
			return "", false
		}
	}
	value := romanToInt(token)
	if value <= 0 || value > 30 || intToRoman(value) != token {
		return "", false
	}
	return strconv.Itoa(value), true
}

func romanToInt(s string) int {
	values := map[rune]int{'I': 1, 'V': 5, 'X': 10, 'L': 50, 'C': 100, 'D': 500, 'M': 1000}
	total := 0
	prev := 0
	for i := len(s) - 1; i >= 0; i-- {
		v := values[rune(s[i])]
		if v < prev {
			total -= v
		} else {
			total += v
			prev = v
		}
	}
	return total
}

func intToRoman(v int) string {
	if v <= 0 {
		return ""
	}
	type roman struct {
		value int
		text  string
	}
	table := []roman{
		{1000, "M"}, {900, "CM"}, {500, "D"}, {400, "CD"},
		{100, "C"}, {90, "XC"}, {50, "L"}, {40, "XL"},
		{10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"},
	}
	var out strings.Builder
	for _, item := range table {
		for v >= item.value {
			out.WriteString(item.text)
			v -= item.value
		}
	}
	return out.String()
}

func mediaHintBonus(c Candidate, p ParsedName, titleSim, yearFit float64) float64 {
	switch strings.ToLower(strings.TrimSpace(p.MediaHint)) {
	case "anime":
		orig := specificTitleSimilarity(p.OriginalTitle, c.OriginalTitle, c.Title)
		switch {
		case orig >= 0.93 && yearFit >= 0.5:
			return 0.05
		case orig >= 0.86:
			return 0.03
		}
	case "series":
		if titleSim >= 0.92 && yearFit >= 0.5 {
			return 0.03
		}
	case "movie":
		if titleSim >= 0.92 && yearFit >= 1.0 {
			return 0.02
		}
	}
	return 0
}

func specificTitleSimilarity(source string, targets ...string) float64 {
	avs := titleCompareVariants(source)
	if len(avs) == 0 {
		return 0
	}
	best := 0.0
	for _, target := range targets {
		for _, a := range avs {
			for _, b := range titleCompareVariants(target) {
				if s := jaroWinkler(a, b); s > best {
					best = s
				}
			}
		}
	}
	return best
}

func rankCandidates(cands []Candidate, p ParsedName, limit int) []scoredCandidate {
	if len(cands) == 0 || limit == 0 {
		return nil
	}
	top := cands
	if len(top) > candidateTopN {
		top = top[:candidateTopN]
	}
	ranked := make([]scoredCandidate, 0, len(top))
	for _, c := range top {
		if strings.TrimSpace(c.ProviderID) == "" {
			continue
		}
		ranked = append(ranked, scoredCandidate{cand: c, score: scoreCandidate(c, p)})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})
	if limit > 0 && len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked
}

func (m *Matcher) passesSecondaryConfirm(cands []Candidate, p ParsedName, best *scoredCandidate) bool {
	if best == nil {
		return false
	}
	titleSim := bestTitleSimilarity(best.cand, p)
	yearFit := yearScore(best.cand.Year, p.Year)
	if hasMatchingExternalID(best.cand.ExternalIDs, p.IDs) {
		return true
	}

	ranked := rankCandidates(cands, p, 2)
	secondBest := 0.0
	if len(ranked) > 1 {
		secondBest = ranked[1].score
	}
	margin := best.score - secondBest

	switch {
	case titleSim >= 0.96 && yearFit >= 0.5:
		return true
	case titleSim >= 0.92 && yearFit >= 1.0 && margin >= 0.04:
		return true
	case strings.EqualFold(strings.TrimSpace(p.MediaHint), "anime") &&
		specificTitleSimilarity(p.OriginalTitle, best.cand.OriginalTitle, best.cand.Title) >= 0.93 &&
		yearFit >= 0.5 && margin >= 0.03:
		return true
	default:
		return false
	}
}

func hasMatchingExternalID(candidateIDs, parsedIDs map[string]string) bool {
	if len(candidateIDs) == 0 || len(parsedIDs) == 0 {
		return false
	}
	for kind, parsedID := range parsedIDs {
		parsedID = strings.TrimSpace(parsedID)
		if parsedID == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(candidateIDs[kind]), parsedID) {
			return true
		}
	}
	return false
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
