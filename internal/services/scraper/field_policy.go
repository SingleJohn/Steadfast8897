package scraper

import "strings"

type MergedDetails struct {
	Provider      string
	ProviderID    string
	ExternalIDs   map[string]string
	Platforms     []string
	Title         string
	OriginalTitle string
	Overview      string
	Tagline       string
	Year          *int32
	Premiered     string
	Rating        *float64
	Genres        []string
	Studios       []string
	Actors        []Actor
	Directors     []string
	PosterURLs    []string
	BackdropURLs  []string
	SeasonPosters map[int32]string
}

// FieldPolicy 描述每个字段的 provider 优先顺序。Provider 名小写。
// 空列表代表"按传入 Details 顺序"(即 Aggregator 的 Priority)。
type FieldPolicy struct {
	Overview      []string
	Title         []string
	OriginalTitle []string
	Tagline       []string
	Premiered     []string
	Year          []string
	Rating        []string
	Actors        []string
	Poster        []string
	Backdrop      []string
	SeasonPoster  []string
}

// DefaultFieldPolicy 返回方案 §5.3 的默认字段优先级策略。
func DefaultFieldPolicy() FieldPolicy {
	return FieldPolicy{
		Overview:      []string{"tmdb", "douban", "bangumi", "tvdb"},
		Title:         []string{"tmdb", "tvdb", "douban", "bangumi"},
		OriginalTitle: []string{"tmdb", "tvdb", "imdb"},
		Tagline:       []string{"tmdb", "tvdb"},
		Premiered:     []string{"tmdb", "tvdb", "bangumi"},
		Year:          []string{"tmdb", "tvdb", "bangumi", "douban"},
		Rating:        []string{"douban", "tmdb", "bangumi", "tvdb"},
		Actors:        []string{"tmdb", "tvdb"},
		Poster:        []string{"tvdb", "tmdb", "fanart"},
		Backdrop:      []string{"fanart", "tmdb", "tvdb"},
		SeasonPoster:  []string{"tvdb", "tmdb", "fanart"},
	}
}

// MergeDetails 保留原 API,内部调用 MergeDetailsWithPolicy(DefaultFieldPolicy)。
// 所有外部调用都不用改签名;Aggregator.Fill 用带 policy 版本。
func MergeDetails(primary *Identity, details ...*Details) *MergedDetails {
	return MergeDetailsWithPolicy(DefaultFieldPolicy(), primary, details...)
}

// MergeDetailsWithPolicy 按 policy 为每个字段独立选择来源。
// 优先级规则:
//  1. policy.<Field> 的 provider 顺序里,遍历找第一个该 provider 的 Details 且字段非空
//  2. policy 该字段为空或未命中,按传入 details 顺序找第一个非空值(行为等价 M4.5 旧版)
//
// 聚合字段(Genres / Platforms / Studios / Directors / ExternalIDs)保持 union 语义不变。
// SeasonPosters 是一张 map,按策略 provider 顺序取第一个非空 map,内部 key 冲突以先到为准。
func MergeDetailsWithPolicy(policy FieldPolicy, primary *Identity, details ...*Details) *MergedDetails {
	merged := &MergedDetails{}
	if primary != nil {
		merged.Provider = primary.Provider
		merged.ProviderID = primary.ProviderID
		merged.ExternalIDs = cloneStringMap(primary.ExternalIDs)
	}
	if merged.ExternalIDs == nil {
		merged.ExternalIDs = map[string]string{}
	}

	byProvider := indexByProvider(details)

	// 标量字符串
	merged.Title = pickString(policy.Title, byProvider, details, func(d *Details) string { return d.Title })
	merged.OriginalTitle = pickString(policy.OriginalTitle, byProvider, details, func(d *Details) string { return d.OriginalTitle })
	merged.Overview = pickString(policy.Overview, byProvider, details, func(d *Details) string { return d.Overview })
	merged.Tagline = pickString(policy.Tagline, byProvider, details, func(d *Details) string { return d.Tagline })
	merged.Premiered = pickString(policy.Premiered, byProvider, details, func(d *Details) string { return d.Premiered })

	// 标量值
	merged.Year = pickYear(policy.Year, byProvider, details)
	merged.Rating = pickRating(policy.Rating, byProvider, details)

	// 图片(list 按策略取首个非空列表)
	merged.PosterURLs = pickStringSlice(policy.Poster, byProvider, details, func(d *Details) []string { return d.PosterURLs })
	merged.BackdropURLs = pickStringSlice(policy.Backdrop, byProvider, details, func(d *Details) []string { return d.BackdropURLs })
	merged.SeasonPosters = pickSeasonPosters(policy.SeasonPoster, byProvider, details)

	// Actors 按策略取首个非空 list
	merged.Actors = pickActors(policy.Actors, byProvider, details)

	// 聚合字段(union)
	for _, d := range details {
		if d == nil {
			continue
		}
		mergeExternalIDs(merged, d)
		merged.Genres = appendUniqueStrings(merged.Genres, d.Genres...)
		merged.Platforms = appendUniqueStrings(merged.Platforms, d.Platforms...)
		merged.Studios = appendUniqueStrings(merged.Studios, d.Studios...)
		merged.Directors = appendUniqueStrings(merged.Directors, d.Directors...)
	}

	// primary 的 Provider/ProviderID 若为空,退回任一 Details 的
	if merged.Provider == "" || merged.ProviderID == "" {
		for _, d := range details {
			if d == nil {
				continue
			}
			if merged.Provider == "" {
				merged.Provider = d.Provider
			}
			if merged.ProviderID == "" {
				merged.ProviderID = d.ProviderID
			}
			if merged.Provider != "" && merged.ProviderID != "" {
				break
			}
		}
	}
	return merged
}

func indexByProvider(details []*Details) map[string]*Details {
	out := make(map[string]*Details, len(details))
	for _, d := range details {
		if d == nil {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(d.Provider))
		if name == "" {
			continue
		}
		if _, ok := out[name]; !ok {
			out[name] = d
		}
	}
	return out
}

func pickString(order []string, byProvider map[string]*Details, all []*Details, get func(*Details) string) string {
	for _, name := range order {
		d, ok := byProvider[strings.ToLower(name)]
		if !ok || d == nil {
			continue
		}
		if s := strings.TrimSpace(get(d)); s != "" {
			return s
		}
	}
	for _, d := range all {
		if d == nil {
			continue
		}
		if s := strings.TrimSpace(get(d)); s != "" {
			return s
		}
	}
	return ""
}

func pickYear(order []string, byProvider map[string]*Details, all []*Details) *int32 {
	for _, name := range order {
		d, ok := byProvider[strings.ToLower(name)]
		if !ok || d == nil || d.Year == nil {
			continue
		}
		v := *d.Year
		return &v
	}
	for _, d := range all {
		if d == nil || d.Year == nil {
			continue
		}
		v := *d.Year
		return &v
	}
	return nil
}

func pickRating(order []string, byProvider map[string]*Details, all []*Details) *float64 {
	for _, name := range order {
		d, ok := byProvider[strings.ToLower(name)]
		if !ok || d == nil || d.Rating == nil {
			continue
		}
		v := *d.Rating
		return &v
	}
	for _, d := range all {
		if d == nil || d.Rating == nil {
			continue
		}
		v := *d.Rating
		return &v
	}
	return nil
}

func pickStringSlice(order []string, byProvider map[string]*Details, all []*Details, get func(*Details) []string) []string {
	for _, name := range order {
		d, ok := byProvider[strings.ToLower(name)]
		if !ok || d == nil {
			continue
		}
		if xs := get(d); len(xs) > 0 {
			return append([]string(nil), xs...)
		}
	}
	for _, d := range all {
		if d == nil {
			continue
		}
		if xs := get(d); len(xs) > 0 {
			return append([]string(nil), xs...)
		}
	}
	return nil
}

func pickActors(order []string, byProvider map[string]*Details, all []*Details) []Actor {
	for _, name := range order {
		d, ok := byProvider[strings.ToLower(name)]
		if !ok || d == nil {
			continue
		}
		if len(d.Actors) > 0 {
			return append([]Actor(nil), d.Actors...)
		}
	}
	for _, d := range all {
		if d == nil {
			continue
		}
		if len(d.Actors) > 0 {
			return append([]Actor(nil), d.Actors...)
		}
	}
	return nil
}

func pickSeasonPosters(order []string, byProvider map[string]*Details, all []*Details) map[int32]string {
	take := func(d *Details) map[int32]string {
		if d == nil || len(d.SeasonPosters) == 0 {
			return nil
		}
		out := make(map[int32]string, len(d.SeasonPosters))
		for k, v := range d.SeasonPosters {
			if vv := strings.TrimSpace(v); vv != "" {
				out[k] = vv
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	for _, name := range order {
		d, ok := byProvider[strings.ToLower(name)]
		if !ok {
			continue
		}
		if m := take(d); m != nil {
			return m
		}
	}
	for _, d := range all {
		if m := take(d); m != nil {
			return m
		}
	}
	return nil
}

func mergeExternalIDs(merged *MergedDetails, d *Details) {
	if merged.ExternalIDs == nil {
		merged.ExternalIDs = make(map[string]string)
	}
	for k, v := range d.ExternalIDs {
		if vv := strings.TrimSpace(v); vv != "" && merged.ExternalIDs[k] == "" {
			merged.ExternalIDs[k] = vv
		}
	}
}

func appendUniqueStrings(base []string, values ...string) []string {
	seen := make(map[string]struct{}, len(base)+len(values))
	out := make([]string, 0, len(base)+len(values))
	for _, v := range base {
		vv := strings.TrimSpace(v)
		if vv == "" {
			continue
		}
		key := strings.ToLower(vv)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, vv)
	}
	for _, v := range values {
		vv := strings.TrimSpace(v)
		if vv == "" {
			continue
		}
		key := strings.ToLower(vv)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, vv)
	}
	return out
}

func cloneStringMap(src map[string]string) map[string]string {
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
