package scraper

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type ParseMode int

const (
	ModeAuto ParseMode = iota
	ModeMovie
	ModeSeries
	ModeEpisode
)

type ParsedName struct {
	Title         string
	OriginalTitle string
	Year          *int32
	Season        *int32
	Episode       *int32
	IDs           map[string]string
	MediaHint     string
	Junk          []string
	Quality       QualityTags
}

// QualityTags 聚合从文件名解析出的画质信息。
// 字段取值均为归一化后的短 token(便于前端直接渲染胶囊)。
type QualityTags struct {
	Resolution string // "4k" / "1440p" / "1080p" / "720p" / "sd" / ""
	HDRFormat  string // "hdr10+" / "hdr10" / "dv" / "sdr" / ""
	VideoCodec string // "x265" / "x264" / "av1" / ""
	AudioCodec string // "atmos" / "truehd" / "dts-hd" / "dts" / "ac3" / "eac3" / "flac" / "aac" / ""
	Source     string // "remux" / "bluray" / "bdrip" / "web-dl" / "webrip" / "hdtv" / "dvdrip" / ""
}

// Empty 表示该 QualityTags 所有字段都为空字符串。
func (q QualityTags) Empty() bool {
	return q.Resolution == "" && q.HDRFormat == "" && q.VideoCodec == "" &&
		q.AudioCodec == "" && q.Source == ""
}

var (
	reIDTmdb    = regexp.MustCompile(`\{tmdb-(\d+)\}`)
	reIDImdbFmt = regexp.MustCompile(`\{imdb-(tt\d{7,8})\}`)
	reIDImdbRaw = regexp.MustCompile(`(?i)\btt(\d{7,8})\b`)
	reIDTvdb    = regexp.MustCompile(`\{tvdb-(\d+)\}`)
	reIDBangumi = regexp.MustCompile(`\{(?:bgm|bangumi)-(\d+)\}`)

	reEpSxxExx = regexp.MustCompile(`(?i)\bs(\d{1,2})[\s._-]*e(\d{1,3})\b`)
	reEpNxN    = regexp.MustCompile(`\b(\d{1,2})x(\d{1,3})\b`)
	reEpEP     = regexp.MustCompile(`(?i)\bep[\s._-]*(\d{1,3})\b`)
	reEpBr     = regexp.MustCompile(`\[(\d{1,3})(?:v\d)?\]`)
	reEpCN     = regexp.MustCompile(`第\s*(\d{1,3})\s*[话集回]`)
	reEpHash   = regexp.MustCompile(`#\s*(\d{1,3})\b`)
	reEpDash   = regexp.MustCompile(`[\s._]-\s*(\d{1,3})(?:[\s._v]|$)`)
	reEpPlainE = regexp.MustCompile(`(?i)(?:^|[\s._-])e(\d{1,3})(?:[\s._-]|$)`)

	reSeasonEN = regexp.MustCompile(`(?i)\b(?:season|staffel|saison|serie)[\s._-]*(\d{1,2})\b`)
	reSeasonS  = regexp.MustCompile(`(?i)(?:^|[\s._\[\(-])s(\d{1,2})(?:[\s._\]\)-]|$)`)
	reSeasonCN = regexp.MustCompile(`第\s*(\d{1,3})\s*[季部]`)

	reYearBrace = regexp.MustCompile(`[\(\[【](19\d{2}|20\d{2})[\)\]】]`)
	reYearPlain = regexp.MustCompile(`(?:^|[^\d])(19\d{2}|20\d{2})(?:[^\d]|$)`)

	reBracketTag  = regexp.MustCompile(`\[[^\[\]]*\]`)
	reGroupSuffix = regexp.MustCompile(`-[A-Za-z0-9@._]+$`)
	reSpaces      = regexp.MustCompile(`\s+`)

	reCJK = regexp.MustCompile(`[\p{Han}\p{Hiragana}\p{Katakana}\p{Hangul}]`)

	noiseTokens = []string{
		`720p`, `1080p`, `1440p`, `2160p`, `4k`, `8k`,
		`hdr10\+?`, `hdr`, `dv`, `dolby\.?vision`, `sdr`,
		`bluray`, `blu-ray`, `bdrip`, `bdremux`, `bdmv`,
		`web-?dl`, `webrip`, `hdtv`, `dvdrip`, `dvd`, `remux`,
		`x26[45]`, `h\.?26[45]`, `hevc`, `avc`, `av1`, `10bit`, `8bit`,
		`dts(?:-?hd)?(?:[._-]?ma)?`, `truehd`, `atmos`, `ac3`, `eac3`, `flac`, `aac`,
		`[257]\.1`, `[257]\.0`,
		`chs`, `cht`, `gb`, `big5`, `简体`, `繁体`, `简繁`, `国语`, `粤语`, `双语`, `中英`, `中日`,
		`repack`, `proper`, `extended`, `director'?s?\.?cut`, `unrated`, `uncut`, `theatrical`,
		`complete`, `multi`, `internal`,
	}
	reNoise = regexp.MustCompile(`(?i)\b(?:` + strings.Join(noiseTokens, `|`) + `)\b`)
)

// Parse 解析目录名或文件名，提取可用于识别/刮削的结构化信息。
func Parse(raw string, mode ParseMode) ParsedName {
	p := ParsedName{IDs: make(map[string]string)}
	if raw == "" {
		return p
	}

	work := raw
	if mode == ModeEpisode || mode == ModeMovie {
		if ext := filepath.Ext(work); len(ext) > 1 && len(ext) <= 5 {
			work = strings.TrimSuffix(work, ext)
		}
	}

	work = extractIDs(work, &p)
	work = extractEpisode(work, &p)
	work = extractSeason(work, &p)
	work = extractYear(work, &p)
	work = stripBrackets(work, &p)
	work = stripGroupSuffix(work, &p)
	work = stripNoise(work, &p)

	work = strings.NewReplacer(".", " ", "_", " ").Replace(work)
	work = reSpaces.ReplaceAllString(work, " ")
	work = strings.Trim(work, " -·.,")

	p.Title, p.OriginalTitle = splitBilingual(work)
	p.MediaHint = inferMediaHint(raw, &p)

	return p
}

func extractIDs(work string, p *ParsedName) string {
	if m := reIDTmdb.FindStringSubmatch(work); m != nil {
		p.IDs["tmdb"] = m[1]
		work = strings.Replace(work, m[0], " ", 1)
	}
	if m := reIDImdbFmt.FindStringSubmatch(work); m != nil {
		p.IDs["imdb"] = m[1]
		work = strings.Replace(work, m[0], " ", 1)
	} else if m := reIDImdbRaw.FindStringSubmatch(work); m != nil {
		p.IDs["imdb"] = "tt" + m[1]
		work = strings.Replace(work, m[0], " ", 1)
	}
	if m := reIDTvdb.FindStringSubmatch(work); m != nil {
		p.IDs["tvdb"] = m[1]
		work = strings.Replace(work, m[0], " ", 1)
	}
	if m := reIDBangumi.FindStringSubmatch(work); m != nil {
		p.IDs["bangumi"] = m[1]
		work = strings.Replace(work, m[0], " ", 1)
	}
	return work
}

func extractEpisode(work string, p *ParsedName) string {
	if m := reEpSxxExx.FindStringSubmatch(work); m != nil {
		setInt32(&p.Season, m[1])
		setInt32(&p.Episode, m[2])
		return strings.Replace(work, m[0], " ", 1)
	}
	if m := reEpNxN.FindStringSubmatch(work); m != nil {
		setInt32(&p.Season, m[1])
		setInt32(&p.Episode, m[2])
		return strings.Replace(work, m[0], " ", 1)
	}
	for _, re := range []*regexp.Regexp{reEpEP, reEpCN, reEpBr, reEpHash, reEpDash, reEpPlainE} {
		if m := re.FindStringSubmatch(work); m != nil {
			setInt32(&p.Episode, m[1])
			work = strings.Replace(work, m[0], " ", 1)
			break
		}
	}
	return work
}

func extractSeason(work string, p *ParsedName) string {
	if p.Season != nil {
		return work
	}
	if m := reSeasonCN.FindStringSubmatch(work); m != nil {
		setInt32(&p.Season, m[1])
		return strings.Replace(work, m[0], " ", 1)
	}
	if m := reSeasonEN.FindStringSubmatch(work); m != nil {
		setInt32(&p.Season, m[1])
		return strings.Replace(work, m[0], " ", 1)
	}
	if m := reSeasonS.FindStringSubmatch(work); m != nil {
		setInt32(&p.Season, m[1])
		return strings.Replace(work, m[0], " ", 1)
	}
	return work
}

func extractYear(work string, p *ParsedName) string {
	if m := reYearBrace.FindStringSubmatch(work); m != nil {
		setInt32Ptr(&p.Year, m[1])
		return strings.Replace(work, m[0], " ", 1)
	}
	if m := reYearPlain.FindStringSubmatch(work); m != nil {
		setInt32Ptr(&p.Year, m[1])
		idx := strings.Index(work, m[1])
		if idx >= 0 {
			work = work[:idx] + " " + work[idx+len(m[1]):]
		}
	}
	return work
}

func stripBrackets(work string, p *ParsedName) string {
	tags := reBracketTag.FindAllString(work, -1)
	if len(tags) > 0 {
		p.Junk = append(p.Junk, tags...)
	}
	return reBracketTag.ReplaceAllString(work, " ")
}

func stripGroupSuffix(work string, p *ParsedName) string {
	trimmed := strings.TrimSpace(work)
	if m := reGroupSuffix.FindString(trimmed); m != "" {
		tail := strings.TrimPrefix(m, "-")
		if len(tail) <= 12 && !hasDigitsOnly(tail) {
			p.Junk = append(p.Junk, m)
			trimmed = strings.TrimSuffix(trimmed, m)
			return trimmed
		}
	}
	return work
}

func stripNoise(work string, p *ParsedName) string {
	hits := reNoise.FindAllString(work, -1)
	if len(hits) > 0 {
		p.Junk = append(p.Junk, hits...)
		for _, raw := range hits {
			classifyQualityToken(raw, &p.Quality)
		}
	}
	return reNoise.ReplaceAllString(work, " ")
}

// classifyQualityToken 把单个噪声 token 归一到 QualityTags 的对应字段。
// 同字段多次命中时,按 tokenPriority 给出的 rank 取高值(如 remux > bluray > bdrip)。
func classifyQualityToken(raw string, q *QualityTags) {
	t := strings.ToLower(strings.TrimSpace(raw))
	if t == "" {
		return
	}
	// 统一分隔符
	norm := strings.NewReplacer(".", "", "_", "", "-", "", " ", "").Replace(t)

	// Resolution
	switch {
	case norm == "2160p" || norm == "4k":
		assignIfStronger(&q.Resolution, "4k", resolutionRank)
	case norm == "8k":
		assignIfStronger(&q.Resolution, "8k", resolutionRank)
	case norm == "1440p":
		assignIfStronger(&q.Resolution, "1440p", resolutionRank)
	case norm == "1080p":
		assignIfStronger(&q.Resolution, "1080p", resolutionRank)
	case norm == "720p":
		assignIfStronger(&q.Resolution, "720p", resolutionRank)
	case norm == "480p" || norm == "576p":
		assignIfStronger(&q.Resolution, "sd", resolutionRank)
	}

	// HDR
	switch {
	case norm == "hdr10+":
		assignIfStronger(&q.HDRFormat, "hdr10+", hdrRank)
	case norm == "hdr" || norm == "hdr10":
		assignIfStronger(&q.HDRFormat, "hdr10", hdrRank)
	case norm == "dv" || norm == "dolbyvision":
		assignIfStronger(&q.HDRFormat, "dv", hdrRank)
	case norm == "sdr":
		assignIfStronger(&q.HDRFormat, "sdr", hdrRank)
	}

	// Video codec
	switch {
	case norm == "x265" || norm == "hevc" || norm == "h265":
		assignIfStronger(&q.VideoCodec, "x265", codecRank)
	case norm == "x264" || norm == "avc" || norm == "h264":
		assignIfStronger(&q.VideoCodec, "x264", codecRank)
	case norm == "av1":
		assignIfStronger(&q.VideoCodec, "av1", codecRank)
	}

	// Audio codec (atmos > truehd > dtshd > dts > eac3 > ac3 > flac > aac)
	switch {
	case strings.Contains(norm, "atmos"):
		assignIfStronger(&q.AudioCodec, "atmos", audioRank)
	case strings.Contains(norm, "truehd"):
		assignIfStronger(&q.AudioCodec, "truehd", audioRank)
	case norm == "dtshd" || norm == "dtshdma" || norm == "dts hd":
		assignIfStronger(&q.AudioCodec, "dts-hd", audioRank)
	case norm == "dts":
		assignIfStronger(&q.AudioCodec, "dts", audioRank)
	case norm == "eac3":
		assignIfStronger(&q.AudioCodec, "eac3", audioRank)
	case norm == "ac3":
		assignIfStronger(&q.AudioCodec, "ac3", audioRank)
	case norm == "flac":
		assignIfStronger(&q.AudioCodec, "flac", audioRank)
	case norm == "aac":
		assignIfStronger(&q.AudioCodec, "aac", audioRank)
	}

	// Source (remux 优先于 bluray/bdrip;web-dl 优先于 webrip)
	switch {
	case norm == "remux" || norm == "bdremux":
		assignIfStronger(&q.Source, "remux", sourceRank)
	case norm == "bluray" || norm == "bdmv":
		assignIfStronger(&q.Source, "bluray", sourceRank)
	case norm == "bdrip":
		assignIfStronger(&q.Source, "bdrip", sourceRank)
	case norm == "webdl" || norm == "web dl":
		assignIfStronger(&q.Source, "web-dl", sourceRank)
	case norm == "webrip":
		assignIfStronger(&q.Source, "webrip", sourceRank)
	case norm == "hdtv":
		assignIfStronger(&q.Source, "hdtv", sourceRank)
	case norm == "dvdrip" || norm == "dvd":
		assignIfStronger(&q.Source, "dvdrip", sourceRank)
	}
}

var (
	resolutionRank = map[string]int{"sd": 1, "720p": 2, "1080p": 3, "1440p": 4, "4k": 5, "8k": 6}
	hdrRank        = map[string]int{"sdr": 1, "hdr10": 2, "hdr10+": 3, "dv": 4}
	codecRank      = map[string]int{"x264": 1, "x265": 2, "av1": 3}
	audioRank      = map[string]int{"aac": 1, "flac": 2, "ac3": 3, "eac3": 4, "dts": 5, "dts-hd": 6, "truehd": 7, "atmos": 8}
	sourceRank     = map[string]int{"dvdrip": 1, "hdtv": 2, "webrip": 3, "web-dl": 4, "bdrip": 5, "bluray": 6, "remux": 7}
)

func assignIfStronger(dst *string, candidate string, rank map[string]int) {
	if *dst == "" {
		*dst = candidate
		return
	}
	cur := rank[*dst]
	next := rank[candidate]
	if next > cur {
		*dst = candidate
	}
}

func inferMediaHint(raw string, p *ParsedName) string {
	lowerRaw := strings.ToLower(raw)
	animeMarkers := []string{"anime", "番剧", "动画", "动漫"}
	hasAnimeMarker := false
	for _, m := range animeMarkers {
		if strings.Contains(lowerRaw, m) {
			hasAnimeMarker = true
			break
		}
	}
	bracketCount := 0
	for _, j := range p.Junk {
		if strings.HasPrefix(j, "[") {
			bracketCount++
		}
	}
	if hasAnimeMarker || (bracketCount >= 2 && reCJK.MatchString(raw)) {
		return "anime"
	}
	if p.Season != nil || p.Episode != nil {
		return "series"
	}
	if p.Year != nil {
		return "movie"
	}
	return ""
}

// splitBilingual 按字符类型（CJK / ASCII 字母数字 / 其他）分段，
// CJK 段拼为 Title，ASCII 段拼为 OriginalTitle。
func splitBilingual(s string) (title, original string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	if !reCJK.MatchString(s) {
		return s, ""
	}

	type seg struct {
		text string
		kind int // 1=cjk, 2=ascii, 0=other
	}
	var segs []seg
	var cur strings.Builder
	curKind := 0
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		segs = append(segs, seg{strings.TrimSpace(cur.String()), curKind})
		cur.Reset()
	}
	for _, r := range s {
		k := 0
		switch {
		case reCJK.MatchString(string(r)):
			k = 1
		case isAsciiAlnum(r):
			k = 2
		}
		if k != curKind && cur.Len() > 0 {
			flush()
		}
		if k != 0 || (curKind != 0 && r == ' ') {
			cur.WriteRune(r)
			if k != 0 {
				curKind = k
			}
		}
	}
	flush()

	var cjk, asc []string
	for _, s := range segs {
		t := strings.TrimSpace(s.text)
		if t == "" {
			continue
		}
		switch s.kind {
		case 1:
			cjk = append(cjk, t)
		case 2:
			asc = append(asc, t)
		}
	}
	title = strings.Join(cjk, "")
	original = strings.Join(asc, " ")
	if len([]rune(original)) <= 2 {
		original = ""
	}
	return
}

func isAsciiAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
		r == '\'' || r == ':' || r == '&' || r == '!' || r == '?'
}

func hasDigitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func setInt32(dst **int32, s string) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return
	}
	i := int32(v)
	*dst = &i
}

func setInt32Ptr(dst **int32, s string) {
	setInt32(dst, s)
}
