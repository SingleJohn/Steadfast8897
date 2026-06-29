package source

import (
	"encoding/json"
	"encoding/xml"
	"html"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var cdataPattern = regexp.MustCompile(`(?s)^<!\[CDATA\[(.*)\]\]>$`)

type cmsResponse struct {
	Code      int             `json:"code"`
	Msg       string          `json:"msg"`
	Page      cmsInt          `json:"page"`
	PageCount cmsInt          `json:"pagecount"`
	Total     cmsInt          `json:"total"`
	List      []cmsVOD        `json:"list"`
	Class     []cmsCategory   `json:"class"`
	Filters   json.RawMessage `json:"filters"`
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

type cmsXMLRSS struct {
	List  cmsXMLList  `xml:"list"`
	Class cmsXMLClass `xml:"class"`
}

type cmsXMLList struct {
	Page        cmsXMLInt     `xml:"page,attr"`
	PageCount   cmsXMLInt     `xml:"pagecount,attr"`
	RecordCount cmsXMLInt     `xml:"recordcount,attr"`
	Total       cmsXMLInt     `xml:"total,attr"`
	Videos      []cmsXMLVideo `xml:"video"`
}

type cmsXMLClass struct {
	Items []cmsXMLCategory `xml:"ty"`
}

type cmsXMLCategory struct {
	ID   string `xml:"id,attr"`
	Name string `xml:",chardata"`
}

type cmsXMLVideo struct {
	Last        string   `xml:"last"`
	ID          string   `xml:"id"`
	TypeID      string   `xml:"tid"`
	Name        string   `xml:"name"`
	TypeName    string   `xml:"type"`
	DT          string   `xml:"dt"`
	Note        string   `xml:"note"`
	Pic         string   `xml:"pic"`
	Year        string   `xml:"year"`
	Area        string   `xml:"area"`
	Lang        string   `xml:"lang"`
	Actor       string   `xml:"actor"`
	Director    string   `xml:"director"`
	Description string   `xml:"des"`
	Content     string   `xml:"content"`
	Remarks     string   `xml:"remarks"`
	PlayFrom    string   `xml:"vod_play_from"`
	PlayURL     string   `xml:"vod_play_url"`
	DL          cmsXMLDL `xml:"dl"`
}

type cmsXMLDL struct {
	Items []cmsXMLDD `xml:"dd"`
}

type cmsXMLDD struct {
	Flag  string `xml:"flag,attr"`
	Value string `xml:",chardata"`
}

type cmsXMLInt int

func (i *cmsXMLInt) UnmarshalXMLAttr(attr xml.Attr) error {
	value, _ := strconv.Atoi(cleanCMSValue(attr.Value))
	*i = cmsXMLInt(value)
	return nil
}

func (i cmsXMLInt) Int() int {
	return int(i)
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
	v.applyJSONAliases(raw)
	return nil
}

func (v *cmsVOD) applyJSONAliases(raw map[string]any) {
	if v == nil || len(raw) == 0 {
		return
	}
	if v.VodID.String() == "" {
		v.VodID = cmsString(firstCMSRawString(raw, "id", "vid"))
	}
	if cleanCMSValue(v.VodName) == "" {
		v.VodName = firstCMSRawString(raw, "name", "title")
	}
	if v.TypeID.String() == "" {
		v.TypeID = cmsString(firstCMSRawString(raw, "tid", "cid", "class_id"))
	}
	if cleanCMSValue(v.TypeName) == "" {
		v.TypeName = firstCMSRawString(raw, "type", "class", "category", "category_name")
	}
	if cleanCMSValue(v.VodPic) == "" {
		v.VodPic = firstCMSRawString(raw, "pic", "img", "image", "cover", "poster")
	}
	if v.VodYear.String() == "" {
		v.VodYear = cmsString(firstCMSRawString(raw, "year", "date", "vod_pubdate"))
	}
	if cleanCMSValue(v.VodArea) == "" {
		v.VodArea = firstCMSRawString(raw, "area", "region")
	}
	if cleanCMSValue(v.VodLang) == "" {
		v.VodLang = firstCMSRawString(raw, "lang", "language")
	}
	if cleanCMSValue(v.VodActor) == "" {
		v.VodActor = firstCMSRawString(raw, "actor", "actors", "stars")
	}
	if cleanCMSValue(v.VodDirector) == "" {
		v.VodDirector = firstCMSRawString(raw, "director", "directors")
	}
	if cleanCMSValue(v.VodContent) == "" {
		v.VodContent = firstCMSRawString(raw, "content", "des", "desc", "description", "summary")
	}
	if cleanCMSValue(v.VodRemarks) == "" {
		v.VodRemarks = firstCMSRawString(raw, "remarks", "remark", "note")
	}
	if cleanCMSValue(v.VodPlayFrom) == "" {
		v.VodPlayFrom = firstCMSRawString(raw, "play_from", "from")
	}
	if cleanCMSValue(v.VodPlayURL) == "" {
		v.VodPlayURL = firstCMSRawString(raw, "play_url", "url", "urls", "vod_url")
	}
}

func (r *cmsResponse) UnmarshalJSON(data []byte) error {
	type alias cmsResponse
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if out.Page.Int() == 0 {
		out.Page = rawCMSInt(raw, "pg", "page_index")
	}
	if out.PageCount.Int() == 0 {
		out.PageCount = rawCMSInt(raw, "page_count", "pagecount")
	}
	if out.Total.Int() == 0 {
		out.Total = rawCMSInt(raw, "recordcount", "record_count", "count")
	}
	if len(out.Class) == 0 {
		out.Class = rawCMSCategories(raw, "classes", "types")
	}
	if len(out.List) == 0 {
		out.List = rawCMSList(raw, "vod", "videos")
	}
	if len(out.Filters) == 0 {
		out.Filters = rawCMSMessage(raw, "filter")
	}
	*r = cmsResponse(out)
	return nil
}

func (c *cmsCategory) UnmarshalJSON(data []byte) error {
	type alias cmsCategory
	var out alias
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	c.TypeID = out.TypeID
	c.TypeName = out.TypeName
	if c.TypeID.String() == "" {
		c.TypeID = cmsString(firstCMSRawString(raw, "id", "tid", "cid", "class_id"))
	}
	if cleanCMSValue(c.TypeName) == "" {
		c.TypeName = firstCMSRawString(raw, "name", "title", "type", "type_name")
	}
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

func rawCMSInt(raw map[string]json.RawMessage, keys ...string) cmsInt {
	for _, key := range keys {
		data, ok := raw[key]
		if !ok || len(data) == 0 {
			continue
		}
		var value cmsInt
		if err := json.Unmarshal(data, &value); err == nil && value.Int() != 0 {
			return value
		}
	}
	return 0
}

func rawCMSCategories(raw map[string]json.RawMessage, keys ...string) []cmsCategory {
	for _, key := range keys {
		data, ok := raw[key]
		if !ok || len(data) == 0 {
			continue
		}
		var rows []cmsCategory
		if err := json.Unmarshal(data, &rows); err == nil && len(rows) > 0 {
			return rows
		}
	}
	return nil
}

func rawCMSList(raw map[string]json.RawMessage, keys ...string) []cmsVOD {
	for _, key := range keys {
		data, ok := raw[key]
		if !ok || len(data) == 0 {
			continue
		}
		var rows []cmsVOD
		if err := json.Unmarshal(data, &rows); err == nil && len(rows) > 0 {
			return rows
		}
	}
	return nil
}

func rawCMSMessage(raw map[string]json.RawMessage, keys ...string) json.RawMessage {
	for _, key := range keys {
		data, ok := raw[key]
		if !ok || len(data) == 0 || string(data) == "null" {
			continue
		}
		return append(json.RawMessage(nil), data...)
	}
	return nil
}

func firstCMSRawString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case string:
			if s := cleanCMSValue(v); s != "" {
				return s
			}
		case json.Number:
			if s := cleanCMSValue(v.String()); s != "" {
				return s
			}
		case float64:
			if v != 0 {
				return strconv.FormatFloat(v, 'f', -1, 64)
			}
		case int:
			if v != 0 {
				return strconv.Itoa(v)
			}
		}
	}
	return ""
}

func parseCMSXML(data []byte, out *cmsResponse) error {
	var payload cmsXMLRSS
	if err := xml.Unmarshal(data, &payload); err != nil {
		return err
	}
	out.Code = 1
	out.Msg = "XML CMS"
	out.Page = cmsInt(payload.List.Page.Int())
	out.PageCount = cmsInt(payload.List.PageCount.Int())
	total := payload.List.Total.Int()
	if total == 0 {
		total = payload.List.RecordCount.Int()
	}
	out.Total = cmsInt(total)
	out.Filters = nil
	out.Class = make([]cmsCategory, 0, len(payload.Class.Items))
	for _, item := range payload.Class.Items {
		id := cleanCMSValue(item.ID)
		name := cleanCMSValue(item.Name)
		if id == "" || name == "" {
			continue
		}
		out.Class = append(out.Class, cmsCategory{TypeID: cmsString(id), TypeName: name})
	}
	out.List = make([]cmsVOD, 0, len(payload.List.Videos))
	for _, item := range payload.List.Videos {
		vod := item.toCMSVOD()
		if vod.VodID.String() == "" && cleanCMSValue(vod.VodName) == "" {
			continue
		}
		out.List = append(out.List, vod)
	}
	return nil
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
	if _, ok := raw["provider_format"]; !ok {
		raw["provider_format"] = "json_cms"
	}
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

func (v cmsXMLVideo) toCMSVOD() cmsVOD {
	playFrom, playURL := v.playFields()
	raw := map[string]any{"provider_format": "xml_cms"}
	addRaw := func(key, value string) {
		if value = cleanCMSValue(value); value != "" {
			raw[key] = value
		}
	}
	addRaw("last", v.Last)
	addRaw("id", v.ID)
	addRaw("tid", v.TypeID)
	addRaw("name", v.Name)
	addRaw("type", v.TypeName)
	addRaw("dt", v.DT)
	addRaw("note", v.Note)
	addRaw("pic", v.Pic)
	addRaw("year", v.Year)
	addRaw("area", v.Area)
	addRaw("lang", v.Lang)
	addRaw("actor", v.Actor)
	addRaw("director", v.Director)
	addRaw("des", v.Description)
	addRaw("content", v.Content)
	addRaw("vod_play_from", playFrom)
	addRaw("vod_play_url", playURL)
	return cmsVOD{
		Raw:         raw,
		VodID:       cmsString(cleanCMSValue(v.ID)),
		VodName:     cleanCMSValue(v.Name),
		TypeID:      cmsString(cleanCMSValue(v.TypeID)),
		TypeName:    cleanCMSValue(v.TypeName),
		VodPic:      cleanCMSValue(v.Pic),
		VodYear:     cmsString(cleanCMSValue(v.Year)),
		VodArea:     cleanCMSValue(v.Area),
		VodLang:     cleanCMSValue(v.Lang),
		VodActor:    cleanCMSValue(v.Actor),
		VodDirector: cleanCMSValue(v.Director),
		VodContent:  firstCMSValue(v.Description, v.Content),
		VodRemarks:  firstCMSValue(v.Note, v.Remarks),
		VodPlayFrom: playFrom,
		VodPlayURL:  playURL,
	}
}

func (v cmsXMLVideo) playFields() (string, string) {
	playFrom := cleanCMSValue(v.PlayFrom)
	playURL := cleanCMSValue(v.PlayURL)
	if len(v.DL.Items) > 0 {
		lineNames := make([]string, 0, len(v.DL.Items))
		lineURLs := make([]string, 0, len(v.DL.Items))
		dtNames := splitCMSLineNames(v.DT)
		for idx, item := range v.DL.Items {
			rawURL := cleanCMSValue(item.Value)
			if rawURL == "" {
				continue
			}
			lineName := cleanCMSValue(item.Flag)
			if lineName == "" && idx < len(dtNames) {
				lineName = dtNames[idx]
			}
			if lineName == "" {
				lineName = "线路" + strconv.Itoa(idx+1)
			}
			lineNames = append(lineNames, lineName)
			lineURLs = append(lineURLs, rawURL)
		}
		if len(lineURLs) > 0 {
			return strings.Join(lineNames, "$$$"), strings.Join(lineURLs, "$$$")
		}
	}
	if playFrom == "" {
		playFrom = strings.Join(splitCMSLineNames(v.DT), "$$$")
	}
	return playFrom, playURL
}

func splitCMSLineNames(value string) []string {
	value = cleanCMSValue(value)
	if value == "" {
		return nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '$' || r == ',' || r == '/' || r == '|' || r == '，'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = cleanCMSValue(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func firstCMSValue(values ...string) string {
	for _, value := range values {
		if value = cleanCMSValue(value); value != "" {
			return value
		}
	}
	return ""
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

// parseModeForCMSURL 只做粗分类:真正的解析推迟到播放时由 ResolvePlayPipeline 处理。
//   - 视频直链(.m3u8/.mp4/...)        → direct
//   - 其余 http(s) 网页/未知地址        → resolver(交给 parses 解析器 + 网页嗅探)
//   - magnet/ed2k/thunder              → magnet
//   - 其余                              → unsupported
func parseModeForCMSURL(rawURL string) string {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return "unsupported"
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "magnet:"), strings.HasPrefix(lower, "ed2k:"), strings.HasPrefix(lower, "thunder:"):
		return "magnet"
	case isDirectVideoURL(raw):
		return "direct"
	case isHTTPURL(raw):
		return "resolver"
	default:
		return "unsupported"
	}
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
