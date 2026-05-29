// Package proxy provides a unified download-link proxy for 115 and 123 cloud drives.
package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/gateway/open115"
	"fyms/internal/proxy/pan123"
)

// ProxyAccount is a database row.
type ProxyAccount struct {
	ID        string          `json:"id"`
	Alias     string          `json:"alias"`
	Type      string          `json:"type"` // "115_open" | "pan123"
	Config    json.RawMessage `json:"config"`
	Enabled   bool            `json:"enabled"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Open115Config is the JSON config for 115_open accounts.
type Open115Config struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	RootFolderID string `json:"root_folder_id,omitempty"`
}

// Pan123Config is the JSON config for pan123 accounts.
type Pan123Config struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	AccessToken  string `json:"access_token,omitempty"`
	TokenExpiry  string `json:"token_expires_at,omitempty"`
}

// LinkResult is the result of a download link resolution.
type LinkResult struct {
	URL       string `json:"url"`
	Alias     string `json:"alias"`
	Type      string `json:"type"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// Service manages proxy accounts and resolves download links.
type Service struct {
	pool *pgxpool.Pool

	mu          sync.RWMutex
	clients115  map[string]*open115.Client
	clients123  map[string]*pan123.Client
	pathCache   map[string]pathCacheEntry   // "alias|path" → pickcode/fileID
	urlCache    map[string]urlCacheEntry    // "alias|path|ua" → download URL
}

type pathCacheEntry struct {
	pickCode string // 115
	fileID   int64  // 123
}

type urlCacheEntry struct {
	url    string
	expiry time.Time
}

// NewService creates a new proxy service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{
		pool:       pool,
		clients115: make(map[string]*open115.Client),
		clients123: make(map[string]*pan123.Client),
		pathCache:  make(map[string]pathCacheEntry),
		urlCache:   make(map[string]urlCacheEntry),
	}
}

// --- Account CRUD ---

func (s *Service) ListAccounts(ctx context.Context) ([]ProxyAccount, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT id, alias, type, config, enabled, created_at, updated_at FROM proxy_accounts ORDER BY alias")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []ProxyAccount
	for rows.Next() {
		var a ProxyAccount
		if err := rows.Scan(&a.ID, &a.Alias, &a.Type, &a.Config, &a.Enabled, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, nil
}

func (s *Service) GetAccount(ctx context.Context, id string) (*ProxyAccount, error) {
	var a ProxyAccount
	err := s.pool.QueryRow(ctx,
		"SELECT id, alias, type, config, enabled, created_at, updated_at FROM proxy_accounts WHERE id = $1::uuid", id,
	).Scan(&a.ID, &a.Alias, &a.Type, &a.Config, &a.Enabled, &a.CreatedAt, &a.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("账号不存在")
	}
	return &a, err
}

func (s *Service) CreateAccount(ctx context.Context, alias, typ string, config json.RawMessage) (*ProxyAccount, error) {
	var a ProxyAccount
	err := s.pool.QueryRow(ctx,
		`INSERT INTO proxy_accounts (alias, type, config) VALUES ($1, $2, $3)
		 RETURNING id, alias, type, config, enabled, created_at, updated_at`,
		alias, typ, config,
	).Scan(&a.ID, &a.Alias, &a.Type, &a.Config, &a.Enabled, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	s.invalidateClient(alias)
	return &a, nil
}

func (s *Service) UpdateAccount(ctx context.Context, id string, alias, typ string, config json.RawMessage, enabled bool) (*ProxyAccount, error) {
	// Get old alias to invalidate cache
	var oldAlias string
	_ = s.pool.QueryRow(ctx, "SELECT alias FROM proxy_accounts WHERE id = $1::uuid", id).Scan(&oldAlias)

	var a ProxyAccount
	err := s.pool.QueryRow(ctx,
		`UPDATE proxy_accounts SET alias=$1, type=$2, config=$3, enabled=$4, updated_at=NOW()
		 WHERE id = $5::uuid
		 RETURNING id, alias, type, config, enabled, created_at, updated_at`,
		alias, typ, config, enabled, id,
	).Scan(&a.ID, &a.Alias, &a.Type, &a.Config, &a.Enabled, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	s.invalidateClient(oldAlias)
	s.invalidateClient(alias)
	return &a, nil
}

func (s *Service) DeleteAccount(ctx context.Context, id string) error {
	var alias string
	_ = s.pool.QueryRow(ctx, "SELECT alias FROM proxy_accounts WHERE id = $1::uuid", id).Scan(&alias)
	_, err := s.pool.Exec(ctx, "DELETE FROM proxy_accounts WHERE id = $1::uuid", id)
	if err == nil {
		s.invalidateClient(alias)
	}
	return err
}

func (s *Service) invalidateClient(alias string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients115, alias)
	delete(s.clients123, alias)
	// Clear path/url caches for this alias
	prefix := alias + "|"
	for k := range s.pathCache {
		if strings.HasPrefix(k, prefix) {
			delete(s.pathCache, k)
		}
	}
	for k := range s.urlCache {
		if strings.HasPrefix(k, prefix) {
			delete(s.urlCache, k)
		}
	}
}

// AliasExists checks if a proxy account alias exists (fast, for NoRoute).
func (s *Service) AliasExists(ctx context.Context, alias string) bool {
	var count int64
	_ = s.pool.QueryRow(ctx,
		"SELECT count(*) FROM proxy_accounts WHERE alias = $1 AND enabled = TRUE", alias,
	).Scan(&count)
	return count > 0
}

// --- Download Link Resolution ---

// ResolveLink resolves a download link for the given alias and relative path.
func (s *Service) ResolveLink(ctx context.Context, alias, relPath, userAgent string) (*LinkResult, error) {
	var account ProxyAccount
	err := s.pool.QueryRow(ctx,
		"SELECT id, alias, type, config, enabled FROM proxy_accounts WHERE alias = $1",
		alias,
	).Scan(&account.ID, &account.Alias, &account.Type, &account.Config, &account.Enabled)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("账号别名 %q 不存在", alias)
	}
	if err != nil {
		return nil, err
	}
	if !account.Enabled {
		return nil, fmt.Errorf("账号 %q 已禁用", alias)
	}

	// Check URL cache
	urlKey := alias + "|" + relPath + "|" + userAgent
	s.mu.RLock()
	if entry, ok := s.urlCache[urlKey]; ok && time.Now().Before(entry.expiry) {
		s.mu.RUnlock()
		return &LinkResult{URL: entry.url, Alias: alias, Type: account.Type}, nil
	}
	s.mu.RUnlock()

	var downloadURL string
	switch account.Type {
	case "115_open":
		downloadURL, err = s.resolve115(ctx, alias, relPath, userAgent, account.Config)
	case "pan123":
		downloadURL, err = s.resolve123(ctx, alias, relPath, account.Config)
	default:
		return nil, fmt.Errorf("不支持的账号类型: %s", account.Type)
	}
	if err != nil {
		return nil, err
	}

	// Cache URL (2 hours for 115, 30 min for 123)
	ttl := 30 * time.Minute
	if account.Type == "115_open" {
		ttl = 110 * time.Minute
	}
	s.mu.Lock()
	s.urlCache[urlKey] = urlCacheEntry{url: downloadURL, expiry: time.Now().Add(ttl)}
	s.mu.Unlock()

	return &LinkResult{
		URL:       downloadURL,
		Alias:     alias,
		Type:      account.Type,
		ExpiresAt: time.Now().Add(ttl).UTC().Format(time.RFC3339),
	}, nil
}

func (s *Service) resolve115(ctx context.Context, alias, relPath, userAgent string, configRaw json.RawMessage) (string, error) {
	client := s.getOrCreate115Client(alias, configRaw)
	if client == nil {
		return "", fmt.Errorf("115 账号配置无效")
	}

	path := "/" + strings.TrimLeft(relPath, "/")

	// Check path cache
	pathKey := alias + "|" + relPath
	s.mu.RLock()
	entry, hasCached := s.pathCache[pathKey]
	s.mu.RUnlock()

	pickCode := entry.pickCode
	if !hasCached || pickCode == "" {
		fi, err := client.GetFolderInfoByPath(ctx, path)
		if err != nil {
			return "", fmt.Errorf("115 路径解析失败: %w", err)
		}
		if fi.PickCode == "" {
			return "", fmt.Errorf("115 路径无 pick_code (可能是目录)")
		}
		pickCode = fi.PickCode
		s.mu.Lock()
		s.pathCache[pathKey] = pathCacheEntry{pickCode: pickCode}
		s.mu.Unlock()
	}

	urlStr, err := client.DownloadURL(ctx, pickCode, userAgent)
	if err != nil {
		// Clear cache on error
		s.mu.Lock()
		delete(s.pathCache, pathKey)
		s.mu.Unlock()
		return "", fmt.Errorf("115 获取直链失败: %w", err)
	}
	return urlStr, nil
}

func (s *Service) resolve123(ctx context.Context, alias, relPath string, configRaw json.RawMessage) (string, error) {
	client := s.getOrCreate123Client(alias, configRaw)
	if client == nil {
		return "", fmt.Errorf("123 账号配置无效")
	}

	// Check path cache
	pathKey := alias + "|" + relPath
	s.mu.RLock()
	entry, hasCached := s.pathCache[pathKey]
	s.mu.RUnlock()

	fileID := entry.fileID
	if !hasCached || fileID == 0 {
		var err error
		fileID, err = client.ResolvePathToFileID(ctx, relPath)
		if err != nil {
			return "", fmt.Errorf("123 路径解析失败: %w", err)
		}
		s.mu.Lock()
		s.pathCache[pathKey] = pathCacheEntry{fileID: fileID}
		s.mu.Unlock()
	}

	urlStr, err := client.GetDirectLinkURL(ctx, fileID)
	if err != nil {
		s.mu.Lock()
		delete(s.pathCache, pathKey)
		s.mu.Unlock()
		return "", fmt.Errorf("123 获取直链失败: %w", err)
	}
	return urlStr, nil
}

func (s *Service) getOrCreate115Client(alias string, configRaw json.RawMessage) *open115.Client {
	s.mu.RLock()
	if c, ok := s.clients115[alias]; ok {
		s.mu.RUnlock()
		return c
	}
	s.mu.RUnlock()

	var cfg Open115Config
	if err := json.Unmarshal(configRaw, &cfg); err != nil || cfg.AccessToken == "" {
		return nil
	}

	pool := s.pool
	aid := alias
	client := open115.New(cfg.AccessToken, cfg.RefreshToken, func(at, rt string) {
		// Persist refreshed tokens
		newCfg := Open115Config{AccessToken: at, RefreshToken: rt, RootFolderID: cfg.RootFolderID}
		raw, _ := json.Marshal(newCfg)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		pool.Exec(ctx, "UPDATE proxy_accounts SET config = $1, updated_at = NOW() WHERE alias = $2", raw, aid)
	})

	s.mu.Lock()
	s.clients115[alias] = client
	s.mu.Unlock()
	return client
}

func (s *Service) getOrCreate123Client(alias string, configRaw json.RawMessage) *pan123.Client {
	s.mu.RLock()
	if c, ok := s.clients123[alias]; ok {
		s.mu.RUnlock()
		return c
	}
	s.mu.RUnlock()

	var cfg Pan123Config
	if err := json.Unmarshal(configRaw, &cfg); err != nil || cfg.ClientID == "" {
		return nil
	}

	var client *pan123.Client
	if cfg.AccessToken != "" && cfg.TokenExpiry != "" {
		if t, err := time.Parse(time.RFC3339, cfg.TokenExpiry); err == nil {
			client = pan123.NewWithToken(cfg.ClientID, cfg.ClientSecret, cfg.AccessToken, t)
		}
	}
	if client == nil {
		client = pan123.New(cfg.ClientID, cfg.ClientSecret)
	}

	s.mu.Lock()
	s.clients123[alias] = client
	s.mu.Unlock()
	return client
}
