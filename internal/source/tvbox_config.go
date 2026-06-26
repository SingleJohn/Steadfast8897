package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const tvboxConfigMaxBytes = 8 << 20

type TVBoxConfig struct {
	Spider    string         `json:"spider"`
	Sites     []TVBoxSite    `json:"sites"`
	Parses    []TVBoxParse   `json:"parses"`
	Lives     []any          `json:"lives"`
	Rules     []any          `json:"rules"`
	Flags     []any          `json:"flags"`
	RawConfig map[string]any `json:"-"`
}

type TVBoxSite struct {
	Key         string         `json:"key"`
	Name        string         `json:"name"`
	Type        *int32         `json:"-"`
	API         string         `json:"api"`
	Ext         map[string]any `json:"-"`
	Hide        *int32         `json:"hide"`
	Indexs      []string       `json:"indexs"`
	Categories  []string       `json:"categories"`
	Searchable  *int32         `json:"searchable"`
	QuickSearch *int32         `json:"quickSearch"`
	Filterable  *int32         `json:"filterable"`
	Changeable  *int32         `json:"changeable"`
	PlayerType  *int32         `json:"playerType"`
	Timeout     *int32         `json:"timeout"`
	Header      map[string]any `json:"-"`
	Style       map[string]any `json:"-"`
	PlayURL     string         `json:"playUrl"`
	Click       string         `json:"click"`
	Raw         map[string]any `json:"-"`
}

type TVBoxParse struct {
	Name string         `json:"name"`
	Type *int32         `json:"-"`
	URL  string         `json:"url"`
	Raw  map[string]any `json:"-"`
}

func (p *TVBoxParse) UnmarshalJSON(data []byte) error {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if rawURL, ok := value.(string); ok {
		rawURL = strings.TrimSpace(rawURL)
		*p = TVBoxParse{Name: rawURL, URL: rawURL, Raw: map[string]any{"url": rawURL}}
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		*p = TVBoxParse{Raw: map[string]any{}}
		return nil
	}
	type alias struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*p = TVBoxParse{Name: out.Name, URL: out.URL, Raw: raw}
	if value, ok := raw["type"]; ok {
		p.Type = normalizeTVBoxInt32(value)
	}
	return nil
}

func (s *TVBoxSite) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	type alias struct {
		Key     string `json:"key"`
		Name    string `json:"name"`
		API     string `json:"api"`
		PlayURL string `json:"playUrl"`
		Click   string `json:"click"`
	}
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*s = TVBoxSite{
		Key:     out.Key,
		Name:    out.Name,
		API:     out.API,
		PlayURL: out.PlayURL,
		Click:   out.Click,
	}
	s.Raw = raw
	if value, ok := raw["indexs"]; ok {
		s.Indexs = normalizeTVBoxStringSlice(value)
	}
	if value, ok := raw["categories"]; ok {
		s.Categories = normalizeTVBoxStringSlice(value)
	}
	if value, ok := raw["hide"]; ok {
		s.Hide = normalizeTVBoxInt32(value)
	}
	if value, ok := raw["type"]; ok {
		s.Type = normalizeTVBoxInt32(value)
	}
	if value, ok := raw["searchable"]; ok {
		s.Searchable = normalizeTVBoxInt32(value)
	}
	if value, ok := raw["quickSearch"]; ok {
		s.QuickSearch = normalizeTVBoxInt32(value)
	}
	if value, ok := raw["filterable"]; ok {
		s.Filterable = normalizeTVBoxInt32(value)
	}
	if value, ok := raw["changeable"]; ok {
		s.Changeable = normalizeTVBoxInt32(value)
	}
	if value, ok := raw["playerType"]; ok {
		s.PlayerType = normalizeTVBoxInt32(value)
	}
	if value, ok := raw["timeout"]; ok {
		s.Timeout = normalizeTVBoxInt32(value)
	}
	if value, ok := raw["header"]; ok {
		s.Header = normalizeTVBoxObject(value)
	}
	if value, ok := raw["style"]; ok {
		s.Style = normalizeTVBoxObject(value)
	}
	if ext, ok := raw["ext"]; ok {
		s.Ext = normalizeTVBoxExt(ext)
	}
	return nil
}

type LoadTVBoxConfigInput struct {
	SourceURL string
	RawJSON   []byte
	Client    *http.Client
	Timeout   time.Duration
}

type LoadedTVBoxConfig struct {
	Config        TVBoxConfig
	RawJSON       []byte
	ContentSHA256 string
	BaseURL       *string
	SourceURL     *string
}

func LoadTVBoxConfig(ctx context.Context, in LoadTVBoxConfigInput) (*LoadedTVBoxConfig, error) {
	raw := in.RawJSON
	var sourceURL *string
	if len(raw) == 0 {
		u := strings.TrimSpace(in.SourceURL)
		if u == "" {
			return nil, fmt.Errorf("TVBox 配置缺少 URL 或 JSON")
		}
		if err := ValidateOutboundURL(ctx, u); err != nil {
			return nil, err
		}
		sourceURL = &u
		timeout := in.Timeout
		if timeout <= 0 {
			timeout = defaultCMSTimeout
		}
		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
		if err != nil {
			return nil, fmt.Errorf("创建 TVBox 配置请求失败: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		client := in.Client
		if client == nil {
			client = http.DefaultClient
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("拉取 TVBox 配置失败: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("TVBox 配置返回异常状态: %d", resp.StatusCode)
		}
		raw, err = io.ReadAll(io.LimitReader(resp.Body, tvboxConfigMaxBytes))
		if err != nil {
			return nil, fmt.Errorf("读取 TVBox 配置失败: %w", err)
		}
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("TVBox 配置为空")
	}
	var rawMap map[string]any
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		return nil, fmt.Errorf("解析 TVBox 原始 JSON 失败: %w", err)
	}
	var cfg TVBoxConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("解析 TVBox 配置失败: %w", err)
	}
	cfg.RawConfig = rawMap
	sum := sha256.Sum256(raw)
	return &LoadedTVBoxConfig{
		Config:        cfg,
		RawJSON:       raw,
		ContentSHA256: hex.EncodeToString(sum[:]),
		BaseURL:       baseURLFromSource(sourceURL),
		SourceURL:     sourceURL,
	}, nil
}

func normalizeTVBoxExt(value any) map[string]any {
	switch v := value.(type) {
	case nil:
		return map[string]any{}
	case string:
		if strings.TrimSpace(v) == "" {
			return map[string]any{}
		}
		return map[string]any{"_raw": strings.TrimSpace(v), "_path": strings.TrimSpace(v)}
	case map[string]any:
		return v
	default:
		return map[string]any{"_raw": v}
	}
}

func normalizeTVBoxObject(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if obj, ok := value.(map[string]any); ok {
		return obj
	}
	return map[string]any{"_raw": value}
}

func normalizeTVBoxStringSlice(value any) []string {
	switch v := value.(type) {
	case nil:
		return []string{}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if text, ok := normalizeTVBoxStringSliceItem(item); ok {
				out = append(out, text)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if text := strings.TrimSpace(item); text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return []string{}
		}
		fields := strings.FieldsFunc(v, func(r rune) bool {
			return r == ',' || r == '，' || r == '#'
		})
		out := make([]string, 0, len(fields))
		for _, field := range fields {
			if text := strings.TrimSpace(field); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		if text, ok := normalizeTVBoxStringSliceItem(v); ok {
			return []string{text}
		}
		return []string{}
	}
}

func normalizeTVBoxStringSliceItem(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		v = strings.TrimSpace(v)
		return v, v != ""
	case float64:
		if v == 0 {
			return "", false
		}
		return strings.TrimSpace(fmt.Sprint(v)), true
	case json.Number:
		text := strings.TrimSpace(v.String())
		return text, text != "" && text != "0"
	default:
		return "", false
	}
}

func normalizeTVBoxInt32(value any) *int32 {
	switch v := value.(type) {
	case nil:
		return nil
	case float64:
		out := int32(v)
		return &out
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return nil
		}
		out := int32(n)
		return &out
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return nil
		}
		var n json.Number = json.Number(v)
		parsed, err := n.Int64()
		if err != nil {
			return nil
		}
		out := int32(parsed)
		return &out
	default:
		return nil
	}
}

func baseURLFromSource(sourceURL *string) *string {
	if sourceURL == nil || strings.TrimSpace(*sourceURL) == "" {
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(*sourceURL))
	if err != nil {
		return nil
	}
	u.RawQuery = ""
	u.Fragment = ""
	if idx := strings.LastIndex(u.Path, "/"); idx >= 0 {
		u.Path = u.Path[:idx+1]
	}
	out := u.String()
	return &out
}

func jsonBytes(value any, fallback string) []byte {
	raw, err := json.Marshal(value)
	if err != nil || !json.Valid(raw) {
		return []byte(fallback)
	}
	return raw
}
