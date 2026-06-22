package source

import (
	"context"
	"fmt"
	"strings"

	"fyms/internal/repository"
)

type TVBoxImporter struct {
	repo *repository.SourceRepository
}

type ImportTVBoxInput struct {
	Name       string
	SourceURL  string
	RawJSON    []byte
	ImportedBy *string
}

type ImportTVBoxResult struct {
	Config    *repository.SourceConfigImport `json:"config"`
	Providers []repository.SourceProvider    `json:"providers"`
	Parsers   []repository.SourceParser      `json:"parsers"`
	Accepted  int                            `json:"accepted"`
	Skipped   int                            `json:"skipped"`
}

func NewTVBoxImporter(repo *repository.SourceRepository) *TVBoxImporter {
	return &TVBoxImporter{repo: repo}
}

func (i *TVBoxImporter) Import(ctx context.Context, in ImportTVBoxInput) (*ImportTVBoxResult, error) {
	if i == nil || i.repo == nil {
		return nil, fmt.Errorf("TVBox importer 缺少 repository")
	}
	loaded, err := LoadTVBoxConfig(ctx, LoadTVBoxConfigInput{
		SourceURL: in.SourceURL,
		RawJSON:   in.RawJSON,
	})
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "TVBox 配置"
	}
	config, err := i.repo.UpsertConfigImport(ctx, repository.SourceConfigImportUpsert{
		SourceType:    "tvbox",
		Name:          name,
		SourceURL:     loaded.SourceURL,
		BaseURL:       loaded.BaseURL,
		ContentSHA256: loaded.ContentSHA256,
		SpiderRef:     stringPtrOrNil(cleanCMSValue(loaded.Config.Spider)),
		RawConfig:     loaded.RawJSON,
		ImportStatus:  "active",
		Enabled:       true,
		ImportedBy:    in.ImportedBy,
	})
	if err != nil {
		return nil, err
	}
	definitions := ProviderDefinitionsFromTVBox(loaded.Config)
	sourceKeys := make([]string, 0, len(definitions))
	for _, def := range definitions {
		sourceKeys = append(sourceKeys, def.SourceKey)
	}
	if err := i.repo.SupersedeConfigImportsForSourceKeys(ctx, "tvbox", config.ID, sourceKeys); err != nil {
		return nil, err
	}
	providers := make([]repository.SourceProvider, 0, len(definitions))
	accepted := 0
	skipped := 0
	for _, def := range definitions {
		provider, err := i.repo.UpsertProviderBySourceKey(ctx, def.toUpsert(config.ID))
		if err != nil {
			return nil, err
		}
		providers = append(providers, *provider)
		if def.Usable {
			accepted++
		} else {
			skipped++
		}
	}
	parserDefs := ParserDefinitionsFromTVBox(loaded.Config, loaded.BaseURL)
	parsers := make([]repository.SourceParser, 0, len(parserDefs))
	for _, def := range parserDefs {
		parser, err := i.repo.UpsertParser(ctx, def.toUpsert(config.ID))
		if err != nil {
			return nil, err
		}
		parsers = append(parsers, *parser)
	}
	return &ImportTVBoxResult{Config: config, Providers: providers, Parsers: parsers, Accepted: accepted, Skipped: skipped}, nil
}

type ProviderDefinition struct {
	SourceKey    string
	Name         string
	ProviderKind string
	RuntimeKind  string
	TVBoxType    *int32
	API          string
	Ext          map[string]any
	Categories   []string
	Headers      map[string]any
	Capabilities map[string]any
	TimeoutMS    int32
	Enabled      bool
	Visible      bool
	Searchable   bool
	HealthStatus string
	LastError    *string
	RawSite      map[string]any
	Usable       bool
}

type ParserDefinition struct {
	Name        string
	ParserType  int32
	URL         string
	BaseURL     *string
	TimeoutMS   int32
	TrustStatus string
	Status      string
	LastError   *string
	Raw         map[string]any
}

func ProviderDefinitionsFromTVBox(cfg TVBoxConfig) []ProviderDefinition {
	out := make([]ProviderDefinition, 0, len(cfg.Sites))
	for _, site := range cfg.Sites {
		key := strings.TrimSpace(site.Key)
		if key == "" {
			continue
		}
		def := ProviderDefinition{
			SourceKey:    key,
			Name:         defaultSnapshotString(site.Name, key),
			ProviderKind: "tvbox_site",
			RuntimeKind:  "runtime_required",
			TVBoxType:    site.Type,
			API:          strings.TrimSpace(site.API),
			Ext:          site.Ext,
			Categories:   site.Categories,
			Headers:      map[string]any{},
			Capabilities: tvboxCapabilities(site),
			TimeoutMS:    tvboxTimeoutMS(site.Timeout),
			Enabled:      false,
			Visible:      false,
			Searchable:   false,
			HealthStatus: "runtime_required",
			LastError:    ptrString("该 TVBox site 需要后续 runtime，Phase 1 暂不可用"),
			RawSite:      site.Raw,
			Usable:       false,
		}
		if isNativeCMSVodSite(site) {
			def.ProviderKind = "cms_vod"
			def.RuntimeKind = "native_cms"
			def.Enabled = true
			def.Visible = true
			def.Searchable = tvboxBool(site.Searchable, true)
			def.HealthStatus = "unknown"
			def.LastError = nil
			def.Usable = true
		} else if isDRPYJSSite(site) {
			def.ProviderKind = "drpy_js"
			def.RuntimeKind = JSRuntimeKindNodeDRPY
			def.Enabled = true
			def.Visible = true
			def.Searchable = tvboxBool(site.Searchable, true)
			def.HealthStatus = "unknown"
			def.LastError = nil
			def.Usable = true
		}
		out = append(out, def)
	}
	return out
}

func ParserDefinitionsFromTVBox(cfg TVBoxConfig, baseURL *string) []ParserDefinition {
	out := make([]ParserDefinition, 0, len(cfg.Parses))
	for _, parser := range cfg.Parses {
		rawURL := strings.TrimSpace(parser.URL)
		if rawURL == "" {
			continue
		}
		parserType := int32(0)
		if parser.Type != nil {
			parserType = *parser.Type
		}
		name := strings.TrimSpace(parser.Name)
		if name == "" {
			name = rawURL
		}
		def := ParserDefinition{
			Name:        name,
			ParserType:  parserType,
			URL:         rawURL,
			BaseURL:     baseURL,
			TimeoutMS:   8000,
			TrustStatus: "unverified",
			Status:      "active",
			Raw:         parser.Raw,
		}
		if parserType == 3 {
			def.LastError = ptrString("TVBox type=3 嗅探解析器暂未接入，已导入但默认禁用")
		}
		out = append(out, def)
	}
	return out
}

func (d ProviderDefinition) toUpsert(configID int64) repository.SourceProviderUpsert {
	return repository.SourceProviderUpsert{
		ConfigID:     &configID,
		SourceKey:    d.SourceKey,
		Name:         d.Name,
		ProviderKind: d.ProviderKind,
		RuntimeKind:  d.RuntimeKind,
		TVBoxType:    d.TVBoxType,
		API:          d.API,
		Ext:          jsonBytes(d.Ext, "{}"),
		Categories:   jsonBytes(d.Categories, "[]"),
		Headers:      jsonBytes(d.Headers, "{}"),
		Capabilities: jsonBytes(d.Capabilities, "{}"),
		TimeoutMS:    d.TimeoutMS,
		Enabled:      d.Enabled,
		Visible:      d.Visible,
		Searchable:   d.Searchable,
		HealthStatus: d.HealthStatus,
		LastError:    d.LastError,
		RawSite:      jsonBytes(d.RawSite, "{}"),
	}
}

func (d ParserDefinition) toUpsert(configID int64) repository.SourceParserUpsert {
	return repository.SourceParserUpsert{
		ConfigID:    &configID,
		SourceType:  "tvbox",
		Name:        d.Name,
		ParserType:  d.ParserType,
		URL:         d.URL,
		BaseURL:     d.BaseURL,
		TimeoutMS:   d.TimeoutMS,
		Enabled:     false,
		TrustStatus: d.TrustStatus,
		Status:      d.Status,
		LastError:   d.LastError,
		Raw:         jsonBytes(d.Raw, "{}"),
	}
}

func isNativeCMSVodSite(site TVBoxSite) bool {
	api := strings.TrimSpace(site.API)
	if api == "" || !strings.Contains(strings.ToLower(api), "provide/vod") {
		return false
	}
	lower := strings.ToLower(api)
	if site.Type != nil && *site.Type != 0 && *site.Type != 1 {
		return false
	}
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func isDRPYJSSite(site TVBoxSite) bool {
	api := strings.ToLower(strings.TrimSpace(site.API))
	if api == "" {
		return false
	}
	if strings.HasSuffix(api, ".js") || strings.Contains(api, "/drpy") {
		return true
	}
	if raw, ok := site.Ext["_path"].(string); ok && strings.HasSuffix(strings.ToLower(strings.TrimSpace(raw)), ".js") {
		return true
	}
	return false
}

func tvboxCapabilities(site TVBoxSite) map[string]any {
	return map[string]any{
		"search":       tvboxBool(site.Searchable, true),
		"quick_search": tvboxBool(site.QuickSearch, false),
		"filter":       tvboxBool(site.Filterable, false),
		"player_type":  int32OrNil(site.PlayerType),
		"categories":   site.Categories,
	}
}

func tvboxBool(value *int32, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value != 0
}

func tvboxTimeoutMS(value *int32) int32 {
	if value == nil || *value <= 0 {
		return 8000
	}
	if *value < 100 {
		return *value * 1000
	}
	return *value
}

func int32OrNil(value *int32) any {
	if value == nil {
		return nil
	}
	return *value
}
