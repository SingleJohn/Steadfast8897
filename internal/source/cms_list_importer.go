package source

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fyms/internal/repository"
)

const (
	cmsListConfigMaxBytes       = 8 << 20
	CMSListFormatAuto           = "auto"
	CMSListFormatLibreTV        = "libretv_settings"
	CMSListFormatCSV            = "csv"
	CMSListFormatTXT            = "txt"
	CMSListFormatJSON           = "json"
	CMSListSourceType           = "cms_list"
	defaultCMSListSearchAction  = "videolist"
	defaultCMSListDetailAction  = "detail"
	defaultCMSListCategoryQuery = "list"
)

type CMSListImporter struct {
	repo *repository.SourceRepository
}

type ImportCMSListInput struct {
	Name           string
	SourceURL      string
	RawText        []byte
	Format         string
	DefaultEnabled bool
	ImportedBy     *string
}

type ImportCMSListResult struct {
	Config    *repository.SourceConfigImport `json:"config"`
	Providers []repository.SourceProvider    `json:"providers"`
	Accepted  int                            `json:"accepted"`
	Skipped   int                            `json:"skipped"`
}

type loadedCMSListConfig struct {
	Entries        []cmsListEntry
	ContentSHA256  string
	SourceURL      *string
	DetectedFormat string
	RawConfig      map[string]any
}

type cmsListEntry struct {
	Key     string
	Name    string
	URL     string
	Detail  string
	IsAdult *bool
	Raw     map[string]any
}

func NewCMSListImporter(repo *repository.SourceRepository) *CMSListImporter {
	return &CMSListImporter{repo: repo}
}

func (i *CMSListImporter) Import(ctx context.Context, in ImportCMSListInput) (*ImportCMSListResult, error) {
	if i == nil || i.repo == nil {
		return nil, fmt.Errorf("CMS 源清单 importer 缺少 repository")
	}
	loaded, err := loadCMSListConfig(ctx, in)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "CMS 源清单"
	}
	rawConfig := loaded.RawConfig
	if rawConfig == nil {
		rawConfig = map[string]any{}
	}
	rawConfig["_detected_format"] = loaded.DetectedFormat
	config, err := i.repo.UpsertConfigImport(ctx, repository.SourceConfigImportUpsert{
		SourceType:    CMSListSourceType,
		Name:          name,
		SourceURL:     loaded.SourceURL,
		ContentSHA256: loaded.ContentSHA256,
		RawConfig:     jsonBytes(rawConfig, "{}"),
		ImportStatus:  "active",
		Enabled:       true,
		ImportedBy:    in.ImportedBy,
	})
	if err != nil {
		return nil, err
	}
	sourceKeys := make([]string, 0, len(loaded.Entries))
	providers := make([]repository.SourceProvider, 0, len(loaded.Entries))
	accepted := 0
	skipped := 0
	for _, entry := range loaded.Entries {
		def, ok := providerDefinitionFromCMSListEntry(entry, in.DefaultEnabled, loaded.DetectedFormat)
		if !ok {
			skipped++
			continue
		}
		sourceKeys = append(sourceKeys, def.SourceKey)
		provider, err := i.repo.UpsertProviderBySourceKey(ctx, def.toUpsert(config.ID))
		if err != nil {
			return nil, err
		}
		providers = append(providers, *provider)
		accepted++
	}
	if err := i.repo.SupersedeConfigImportsForSourceKeys(ctx, CMSListSourceType, config.ID, sourceKeys); err != nil {
		return nil, err
	}
	return &ImportCMSListResult{Config: config, Providers: providers, Accepted: accepted, Skipped: skipped}, nil
}

func loadCMSListConfig(ctx context.Context, in ImportCMSListInput) (*loadedCMSListConfig, error) {
	raw := in.RawText
	var sourceURL *string
	if len(raw) == 0 {
		u := strings.TrimSpace(in.SourceURL)
		if u == "" {
			return nil, fmt.Errorf("CMS 源清单缺少 URL 或内容")
		}
		if err := ValidateOutboundURL(ctx, u); err != nil {
			return nil, err
		}
		sourceURL = &u
		reqCtx, cancel := context.WithTimeout(ctx, defaultCMSTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
		if err != nil {
			return nil, fmt.Errorf("创建 CMS 源清单请求失败: %w", err)
		}
		req.Header.Set("Accept", "application/json, text/csv, text/plain;q=0.9, */*;q=0.8")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("拉取 CMS 源清单失败: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("CMS 源清单返回异常状态: %d", resp.StatusCode)
		}
		raw, err = io.ReadAll(io.LimitReader(resp.Body, cmsListConfigMaxBytes))
		if err != nil {
			return nil, fmt.Errorf("读取 CMS 源清单失败: %w", err)
		}
	}
	raw = trimCMSResponsePrefix(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("CMS 源清单为空")
	}
	format := normalizeCMSListFormat(in.Format)
	entries, detected, rawConfig, err := parseCMSListEntries(raw, format)
	if err != nil {
		return nil, err
	}
	entries = dedupeCMSListEntries(entries)
	sum := sha256.Sum256(raw)
	return &loadedCMSListConfig{
		Entries:        entries,
		ContentSHA256:  hex.EncodeToString(sum[:]),
		SourceURL:      sourceURL,
		DetectedFormat: detected,
		RawConfig:      rawConfig,
	}, nil
}

func parseCMSListEntries(raw []byte, format string) ([]cmsListEntry, string, map[string]any, error) {
	formats := []string{format}
	if format == CMSListFormatAuto {
		formats = []string{CMSListFormatLibreTV, CMSListFormatJSON, CMSListFormatCSV, CMSListFormatTXT}
	}
	var lastErr error
	for _, candidate := range formats {
		var (
			entries []cmsListEntry
			rawConf map[string]any
			err     error
		)
		switch candidate {
		case CMSListFormatLibreTV:
			entries, rawConf, err = parseLibreTVSettings(raw)
		case CMSListFormatJSON:
			entries, rawConf, err = parseCMSListJSON(raw)
		case CMSListFormatCSV:
			entries, rawConf, err = parseCMSListCSV(raw)
		case CMSListFormatTXT:
			entries, rawConf, err = parseCMSListTXT(raw)
		default:
			err = fmt.Errorf("不支持的 CMS 源清单格式: %s", candidate)
		}
		if err == nil && len(entries) > 0 {
			return entries, candidate, rawConf, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, "", nil, lastErr
	}
	return nil, "", nil, fmt.Errorf("CMS 源清单未解析出可用源")
}

func parseLibreTVSettings(raw []byte) ([]cmsListEntry, map[string]any, error) {
	var payload struct {
		Name       string         `json:"name"`
		Time       any            `json:"time"`
		CfgVer     string         `json:"cfgVer"`
		Data       map[string]any `json:"data"`
		Hash       string         `json:"hash"`
		IsAdultAll any            `json:"isAdultAll"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil, err
	}
	if payload.Data == nil {
		return nil, nil, fmt.Errorf("LibreTV settings 缺少 data")
	}
	customRaw, ok := payload.Data["customAPIs"]
	if !ok {
		return nil, nil, fmt.Errorf("LibreTV settings 缺少 customAPIs")
	}
	customText, ok := customRaw.(string)
	if !ok || strings.TrimSpace(customText) == "" {
		return nil, nil, fmt.Errorf("LibreTV customAPIs 不是字符串 JSON")
	}
	entries, err := parseCMSAPIArray([]byte(customText), CMSListFormatLibreTV)
	if err != nil {
		return nil, nil, err
	}
	rawConf := map[string]any{
		"name":           payload.Name,
		"time":           payload.Time,
		"cfgVer":         payload.CfgVer,
		"hash":           payload.Hash,
		"isAdultAll":     payload.IsAdultAll,
		"selectedAPIs":   payload.Data["selectedAPIs"],
		"customAPICount": len(entries),
	}
	return entries, rawConf, nil
}

func parseCMSListJSON(raw []byte) ([]cmsListEntry, map[string]any, error) {
	if entries, err := parseCMSAPIArray(raw, CMSListFormatJSON); err == nil && len(entries) > 0 {
		return entries, map[string]any{"format": CMSListFormatJSON, "shape": "array"}, nil
	}
	var payload struct {
		Sources map[string]map[string]any `json:"sources"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil, err
	}
	if len(payload.Sources) == 0 {
		return nil, nil, fmt.Errorf("JSON CMS 源清单缺少 sources")
	}
	entries := make([]cmsListEntry, 0, len(payload.Sources))
	for key, rawSource := range payload.Sources {
		entry := cmsListEntry{
			Key:    strings.TrimSpace(key),
			Name:   stringFromMap(rawSource, "name", "title"),
			URL:    stringFromMap(rawSource, "url", "api"),
			Detail: stringFromMap(rawSource, "detail"),
			Raw:    cleanRawMap(rawSource),
		}
		if value, ok := boolFromAny(rawSource["isAdult"]); ok {
			entry.IsAdult = &value
		} else if value, ok := boolFromAny(rawSource["is_adult"]); ok {
			entry.IsAdult = &value
		}
		entries = append(entries, entry)
	}
	return entries, map[string]any{"format": CMSListFormatJSON, "shape": "sources"}, nil
}

func parseCMSAPIArray(raw []byte, format string) ([]cmsListEntry, error) {
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	entries := make([]cmsListEntry, 0, len(items))
	for _, item := range items {
		entry := cmsListEntry{
			Name:   stringFromMap(item, "name", "title"),
			URL:    stringFromMap(item, "url", "api"),
			Detail: stringFromMap(item, "detail"),
			Raw:    cleanRawMap(item),
		}
		if value, ok := boolFromAny(item["isAdult"]); ok {
			entry.IsAdult = &value
		} else if value, ok := boolFromAny(item["is_adult"]); ok {
			entry.IsAdult = &value
		}
		if format == CMSListFormatLibreTV {
			entry.Raw["_source_format"] = CMSListFormatLibreTV
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func parseCMSListCSV(raw []byte) ([]cmsListEntry, map[string]any, error) {
	reader := csv.NewReader(strings.NewReader(string(raw)))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(records) == 0 {
		return nil, nil, fmt.Errorf("CSV CMS 源清单为空")
	}
	start := 0
	header := map[string]int{}
	if looksLikeCMSListHeader(records[0]) {
		start = 1
		for idx, col := range records[0] {
			header[strings.ToLower(strings.TrimSpace(col))] = idx
		}
	}
	entries := make([]cmsListEntry, 0, len(records)-start)
	for _, record := range records[start:] {
		entry := cmsListEntry{
			Name:   csvValue(record, header, 0, "name", "title"),
			URL:    csvValue(record, header, 1, "url", "api"),
			Detail: csvValue(record, header, 2, "detail"),
			Raw:    map[string]any{"record": record},
		}
		if rawAdult := csvValue(record, header, 3, "is_adult", "isAdult", "adult"); rawAdult != "" {
			if value, ok := boolFromString(rawAdult); ok {
				entry.IsAdult = &value
			}
		}
		entries = append(entries, entry)
	}
	return entries, map[string]any{"format": CMSListFormatCSV, "has_header": start == 1}, nil
}

func parseCMSListTXT(raw []byte) ([]cmsListEntry, map[string]any, error) {
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	isAdult := false
	hasType := false
	entries := make([]cmsListEntry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "type:") {
			hasType = true
			value := strings.TrimSpace(strings.TrimPrefix(line, line[:strings.Index(line, ":")+1]))
			isAdult = strings.EqualFold(value, "adult")
			continue
		}
		name, api, ok := splitCMSListTextLine(line)
		if !ok {
			continue
		}
		adult := isAdult
		entries = append(entries, cmsListEntry{
			Name:    name,
			URL:     api,
			IsAdult: &adult,
			Raw:     map[string]any{"line": line},
		})
	}
	if len(entries) == 0 {
		return nil, nil, fmt.Errorf("TXT CMS 源清单未解析出可用源")
	}
	return entries, map[string]any{"format": CMSListFormatTXT, "has_type": hasType}, nil
}

func providerDefinitionFromCMSListEntry(entry cmsListEntry, enabled bool, format string) (ProviderDefinition, bool) {
	api := normalizeCMSListURL(entry.URL)
	if api == "" {
		return ProviderDefinition{}, false
	}
	if parsed, err := url.ParseRequestURI(api); err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ProviderDefinition{}, false
	}
	name := strings.TrimSpace(entry.Name)
	if name == "" {
		name = api
	}
	raw := entry.Raw
	if raw == nil {
		raw = map[string]any{}
	}
	raw["_source_format"] = format
	ext := map[string]any{
		"detail":      strings.TrimSpace(entry.Detail),
		"is_adult":    entry.IsAdult,
		"search_ac":   defaultCMSListSearchAction,
		"detail_ac":   defaultCMSListDetailAction,
		"category_ac": defaultCMSListCategoryQuery,
	}
	return ProviderDefinition{
		SourceKey:    cmsListSourceKey(api),
		Name:         name,
		ProviderKind: "cms_vod",
		RuntimeKind:  "native_cms",
		API:          api,
		Ext:          ext,
		Categories:   []string{},
		Headers:      map[string]any{},
		Capabilities: map[string]any{
			"categories":    true,
			"search":        true,
			"detail":        true,
			"resolve_play":  true,
			"cms_list":      true,
			"source_format": format,
		},
		TimeoutMS:    int32(defaultCMSTimeout / time.Millisecond),
		Enabled:      enabled,
		Visible:      true,
		Searchable:   true,
		HealthStatus: "unknown",
		RawSite:      raw,
		Usable:       true,
	}, true
}

func normalizeCMSListFormat(format string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	switch format {
	case "", CMSListFormatAuto:
		return CMSListFormatAuto
	case "libretv", "libretv-settings":
		return CMSListFormatLibreTV
	case CMSListFormatLibreTV, CMSListFormatCSV, CMSListFormatTXT, CMSListFormatJSON:
		return format
	default:
		return format
	}
}

func normalizeCMSListURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return raw
	}
	parsed.Fragment = ""
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String()
}

func cmsListSourceKey(api string) string {
	sum := sha1.Sum([]byte(normalizeCMSListURL(api)))
	return "cms_" + hex.EncodeToString(sum[:])[:16]
}

func dedupeCMSListEntries(entries []cmsListEntry) []cmsListEntry {
	seen := map[string]bool{}
	out := make([]cmsListEntry, 0, len(entries))
	for _, entry := range entries {
		key := normalizeCMSListURL(entry.URL)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, entry)
	}
	return out
}

func stringFromMap(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func boolFromAny(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		return boolFromString(v)
	case float64:
		return v != 0, true
	case int:
		return v != 0, true
	default:
		return false, false
	}
}

func boolFromString(raw string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes", "y", "adult":
		return true, true
	case "false", "0", "no", "n", "normal":
		return false, true
	default:
		if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
			return n != 0, true
		}
		return false, false
	}
}

func looksLikeCMSListHeader(record []string) bool {
	matches := 0
	for _, col := range record {
		switch strings.ToLower(strings.TrimSpace(col)) {
		case "name", "title", "url", "api", "detail", "is_adult", "isadult", "adult":
			matches++
		}
	}
	return matches >= 2
}

func csvValue(record []string, header map[string]int, fallback int, keys ...string) string {
	for _, key := range keys {
		if idx, ok := header[strings.ToLower(key)]; ok && idx >= 0 && idx < len(record) {
			return strings.TrimSpace(record[idx])
		}
	}
	if len(header) == 0 && fallback >= 0 && fallback < len(record) {
		return strings.TrimSpace(record[fallback])
	}
	return ""
}

func splitCMSListTextLine(line string) (string, string, bool) {
	for _, sep := range []string{",", "，"} {
		if idx := strings.Index(line, sep); idx > 0 {
			name := strings.TrimSpace(line[:idx])
			api := strings.TrimSpace(line[idx+len(sep):])
			return name, api, name != "" && api != ""
		}
	}
	fields := strings.Fields(line)
	if len(fields) >= 2 {
		return strings.Join(fields[:len(fields)-1], " "), fields[len(fields)-1], true
	}
	return "", "", false
}
