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
	Parses    []any          `json:"parses"`
	Lives     []any          `json:"lives"`
	Rules     []any          `json:"rules"`
	Flags     []any          `json:"flags"`
	RawConfig map[string]any `json:"-"`
}

type TVBoxSite struct {
	Key         string         `json:"key"`
	Name        string         `json:"name"`
	Type        *int32         `json:"type"`
	API         string         `json:"api"`
	Ext         map[string]any `json:"ext"`
	Categories  []string       `json:"categories"`
	Searchable  *int32         `json:"searchable"`
	QuickSearch *int32         `json:"quickSearch"`
	Filterable  *int32         `json:"filterable"`
	PlayerType  *int32         `json:"playerType"`
	Timeout     *int32         `json:"timeout"`
	Raw         map[string]any `json:"-"`
}

func (s *TVBoxSite) UnmarshalJSON(data []byte) error {
	type alias TVBoxSite
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*s = TVBoxSite(out)
	s.Raw = raw
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
		return map[string]any{"value": strings.TrimSpace(v)}
	case map[string]any:
		return v
	default:
		return map[string]any{"value": v}
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
