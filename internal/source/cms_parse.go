package source

import (
	"encoding/json"
	"html"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var cdataPattern = regexp.MustCompile(`(?s)^<!\[CDATA\[(.*)\]\]>$`)

type cmsResponse struct {
	Code      int           `json:"code"`
	Msg       string        `json:"msg"`
	Page      cmsInt        `json:"page"`
	PageCount cmsInt        `json:"pagecount"`
	Total     cmsInt        `json:"total"`
	List      []cmsVOD      `json:"list"`
	Class     []cmsCategory `json:"class"`
}

type cmsCategory struct {
	TypeID   cmsString `json:"type_id"`
	TypeName string    `json:"type_name"`
}

type cmsVOD struct {
	Raw         map[string]any `json:"-"`
	VodID       cmsString      `json:"vod_id"`
	VodName     string         `json:"vod_name"`
	TypeID      cmsString      `json:"type_id"`
	TypeName    string         `json:"type_name"`
	VodPic      string         `json:"vod_pic"`
	VodYear     cmsString      `json:"vod_year"`
	VodArea     string         `json:"vod_area"`
	VodLang     string         `json:"vod_lang"`
	VodActor    string         `json:"vod_actor"`
	VodDirector string         `json:"vod_director"`
	VodContent  string         `json:"vod_content"`
	VodRemarks  string         `json:"vod_remarks"`
	VodPlayFrom string         `json:"vod_play_from"`
	VodPlayURL  string         `json:"vod_play_url"`
}

func (v *cmsVOD) UnmarshalJSON(data []byte) error {
	type alias cmsVOD
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*v = cmsVOD(out)
	v.Raw = cleanRawMap(raw)
	return nil
}

type cmsString string

func (s *cmsString) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*s = cmsString(text)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err == nil {
		*s = cmsString(number.String())
		return nil
	}
	*s = ""
	return nil
}

func (s cmsString) String() string {
	return cleanCMSValue(string(s))
}

type cmsInt int

func (i *cmsInt) UnmarshalJSON(data []byte) error {
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*i = cmsInt(n)
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		value, _ := strconv.Atoi(strings.TrimSpace(text))
		*i = cmsInt(value)
		return nil
	}
	*i = 0
	return nil
}

func (i cmsInt) Int() int {
	return int(i)
}

func parseCMSPage(api string, payload cmsResponse, detailLoaded bool) *ProviderPage {
	items := make([]SourceItemSnapshot, 0, len(payload.List))
	for _, vod := range payload.List {
		item := parseCMSItem(api, vod, detailLoaded)
		if item.SourceItemID == "" || item.Title == "" {
			continue
		}
		items = append(items, item)
	}
	return &ProviderPage{
		Page:      payload.Page.Int(),
		PageCount: payload.PageCount.Int(),
		Total:     payload.Total.Int(),
		Items:     items,
	}
}

func parseCMSItem(api string, vod cmsVOD, detailLoaded bool) SourceItemSnapshot {
	typeName := cleanCMSValue(vod.TypeName)
	area := cleanCMSValue(vod.VodArea)
	kind := NormalizeCMSKind(typeName)
	itemType := sourceItemTypeForKind(kind, vod.VodPlayURL)
	raw := vod.Raw
	if raw == nil {
		raw = map[string]any{}
	}
	raw["provider_format"] = "json_cms"
	if categoryID := vod.TypeID.String(); categoryID != "" {
		raw["type_id"] = categoryID
	}
	return SourceItemSnapshot{
		SourceItemID:   vod.VodID.String(),
		ItemType:       itemType,
		Title:          cleanCMSValue(vod.VodName),
		Year:           parseCMSYear(vod.VodYear.String()),
		Region:         NormalizeCMSRegion(area),
		Area:           stringPtrOrNil(area),
		Language:       stringPtrOrNil(cleanCMSValue(vod.VodLang)),
		CategoryID:     stringPtrOrNil(vod.TypeID.String()),
		CategoryName:   stringPtrOrNil(typeName),
		NormalizedKind: kind,
		PosterURL:      stringPtrOrNil(normalizeCMSImageURL(api, cleanCMSValue(vod.VodPic))),
		Remarks:        stringPtrOrNil(cleanCMSValue(vod.VodRemarks)),
		Summary:        stringPtrOrNil(cleanCMSValue(vod.VodContent)),
		Directors:      splitCMSPeople(vod.VodDirector),
		Actors:         splitCMSPeople(vod.VodActor),
		ProviderIDs:    map[string]any{"cms_vod_id": vod.VodID.String()},
		Raw:            raw,
		DetailLoaded:   detailLoaded,
	}
}

func splitCMSPlaySources(playFrom, playURL string) []PlaySourceSnapshot {
	lineNames := strings.Split(cleanCMSValue(playFrom), "$$$")
	lineURLs := strings.Split(cleanCMSValue(playURL), "$$$")
	out := make([]PlaySourceSnapshot, 0)
	for lineIdx, rawEpisodes := range lineURLs {
		rawEpisodes = strings.TrimSpace(rawEpisodes)
		if rawEpisodes == "" {
			continue
		}
		lineName := "线路" + strconv.Itoa(lineIdx+1)
		if lineIdx < len(lineNames) {
			if name := cleanCMSValue(lineNames[lineIdx]); name != "" {
				lineName = name
			}
		}
		episodes := strings.Split(rawEpisodes, "#")
		for epIdx, rawEpisode := range episodes {
			title, rawURL := splitCMSEpisode(rawEpisode, epIdx+1)
			if rawURL == "" {
				continue
			}
			episodeNumber := int32(epIdx + 1)
			episodeKey := "E" + strconv.Itoa(epIdx+1)
			out = append(out, PlaySourceSnapshot{
				LineName:      lineName,
				EpisodeTitle:  title,
				EpisodeKey:    episodeKey,
				EpisodeNumber: &episodeNumber,
				RawURL:        rawURL,
				ParseMode:     parseModeForCMSURL(rawURL),
				Flag:          stringPtrOrNil(lineName),
				ResolverPayload: map[string]any{
					"raw_play_from": cleanCMSValue(playFrom),
					"raw_play_url":  cleanCMSValue(playURL),
					"line_index":    lineIdx + 1,
					"episode_index": epIdx + 1,
				},
				SortOrder: int32(lineIdx*1000 + epIdx + 1),
			})
		}
	}
	return out
}

func splitCMSEpisode(raw string, ordinal int) (string, string) {
	raw = cleanCMSValue(raw)
	if raw == "" {
		return "", ""
	}
	title, rawURL, ok := strings.Cut(raw, "$")
	if !ok {
		title = ""
		rawURL = raw
	}
	title = cleanCMSValue(title)
	rawURL = cleanCMSValue(rawURL)
	if title == "" {
		title = "第" + strconv.Itoa(ordinal) + "集"
	}
	return title, rawURL
}

func parseModeForCMSURL(rawURL string) string {
	lower := strings.ToLower(strings.TrimSpace(rawURL))
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		if strings.Contains(lower, ".m3u8") || strings.Contains(lower, ".mp4") {
			return "direct"
		}
	}
	return "unsupported"
}

func cleanCMSValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if matches := cdataPattern.FindStringSubmatch(value); len(matches) == 2 {
		value = matches[1]
	}
	value = html.UnescapeString(value)
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.TrimSpace(value)
	return value
}

func cleanRawMap(raw map[string]any) map[string]any {
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if s, ok := value.(string); ok {
			out[key] = cleanCMSValue(s)
			continue
		}
		out[key] = value
	}
	return out
}

func normalizeCMSImageURL(api, imageURL string) string {
	imageURL = cleanCMSValue(imageURL)
	if imageURL == "" {
		return ""
	}
	if strings.HasPrefix(imageURL, "//") {
		if base, err := url.Parse(api); err == nil && base.Scheme != "" {
			return base.Scheme + ":" + imageURL
		}
		return "https:" + imageURL
	}
	parsed, err := url.Parse(imageURL)
	if err == nil && parsed.IsAbs() {
		return parsed.String()
	}
	base, err := url.Parse(api)
	if err != nil {
		return imageURL
	}
	if strings.HasPrefix(imageURL, "/") {
		base.Path = imageURL
		base.RawQuery = ""
		return base.String()
	}
	base.Path = path.Join(path.Dir(base.Path), imageURL)
	base.RawQuery = ""
	return base.String()
}

func parseCMSYear(value string) *int32 {
	value = cleanCMSValue(value)
	if len(value) >= 4 {
		value = value[:4]
	}
	year, err := strconv.Atoi(value)
	if err != nil || year <= 0 {
		return nil
	}
	out := int32(year)
	return &out
}

func splitCMSPeople(value string) []string {
	value = cleanCMSValue(value)
	if value == "" {
		return nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '/' || r == '、' || r == '，'
	})
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = cleanCMSValue(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func sourceItemTypeForKind(kind, playURL string) string {
	if kind == "movie" {
		parts := splitCMSPlaySources("", playURL)
		if len(parts) <= 1 {
			return "Movie"
		}
	}
	return "Series"
}

func stringPtrOrNil(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
