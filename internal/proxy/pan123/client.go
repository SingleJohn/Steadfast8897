// Package pan123 is a minimal client for the 123 Cloud Drive Open API.
// Base URL: https://open-api.123pan.com
package pan123

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const apiBase = "https://open-api.123pan.com"

// Client is a 123 Cloud Drive API client.
type Client struct {
	mu           sync.Mutex
	clientID     string
	clientSecret string
	accessToken  string
	tokenExpiry  time.Time
	httpc        *http.Client
}

// New creates a Client with client_id and client_secret.
func New(clientID, clientSecret string) *Client {
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpc:        &http.Client{Timeout: 30 * time.Second},
	}
}

// NewWithToken creates a Client with a pre-existing access token.
func NewWithToken(clientID, clientSecret, accessToken string, tokenExpiry time.Time) *Client {
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		accessToken:  accessToken,
		tokenExpiry:  tokenExpiry,
		httpc:        &http.Client{Timeout: 30 * time.Second},
	}
}

type apiResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) ensureToken(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry.Add(-60*time.Second)) {
		return nil
	}
	body, _ := json.Marshal(map[string]string{
		"clientID":     c.clientID,
		"clientSecret": c.clientSecret,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/api/v1/access_token", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("获取 access_token 失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var r apiResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("解析 token 响应失败: %w", err)
	}
	if r.Code != 0 {
		return fmt.Errorf("123 token 错误: code=%d msg=%s", r.Code, r.Message)
	}
	var data struct {
		AccessToken string `json:"accessToken"`
		ExpiredAt   string `json:"expiredAt"`
	}
	if err := json.Unmarshal(r.Data, &data); err != nil {
		return fmt.Errorf("解析 token data 失败: %w", err)
	}
	c.accessToken = data.AccessToken
	if t, err := time.Parse(time.RFC3339, data.ExpiredAt); err == nil {
		c.tokenExpiry = t
	} else {
		c.tokenExpiry = time.Now().Add(24 * time.Hour)
	}
	return nil
}

func (c *Client) doRequest(ctx context.Context, method, endpoint string, query url.Values, body interface{}) (json.RawMessage, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}
	fullURL := apiBase + endpoint
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}
	var bodyReader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	token := c.accessToken
	c.mu.Unlock()
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Platform", "open_platform")
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var r apiResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w; body=%s", err, truncate(string(raw), 200))
	}
	if r.Code != 0 {
		return nil, fmt.Errorf("123 API 错误: code=%d msg=%s", r.Code, r.Message)
	}
	return r.Data, nil
}

// FileEntry represents a file or folder from the 123 API.
type FileEntry struct {
	FileID   int64  `json:"fileID"`
	Filename string `json:"filename"`
	Type     int    `json:"type"` // 0=file, 1=folder
	Size     int64  `json:"size"`
	Etag     string `json:"etag"`
	ParentID int64  `json:"parentFileID"`
}

// ListFilesResp is the response from /api/v1/file/list.
type ListFilesResp struct {
	FileList   []FileEntry `json:"dataList"`
	LastFileID int64       `json:"lastFileId"`
	Total      int64       `json:"total"`
}

// ListFiles lists files in a directory.
func (c *Client) ListFiles(ctx context.Context, parentFileID int64, limit int, lastFileID int64) (*ListFilesResp, error) {
	q := url.Values{
		"parentFileId": {strconv.FormatInt(parentFileID, 10)},
		"limit":        {strconv.Itoa(limit)},
	}
	if lastFileID > 0 {
		q.Set("lastFileId", strconv.FormatInt(lastFileID, 10))
	}
	data, err := c.doRequest(ctx, http.MethodGet, "/api/v1/file/list", q, nil)
	if err != nil {
		return nil, err
	}
	var resp ListFilesResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析文件列表失败: %w", err)
	}
	return &resp, nil
}

// GetDirectLinkURL gets a direct download URL for a file.
func (c *Client) GetDirectLinkURL(ctx context.Context, fileID int64) (string, error) {
	q := url.Values{
		"fileId": {strconv.FormatInt(fileID, 10)},
	}
	data, err := c.doRequest(ctx, http.MethodGet, "/api/v1/direct-link/url", q, nil)
	if err != nil {
		return "", err
	}
	var resp struct {
		DownloadURL string `json:"downloadUrl"`
		RedirectURL string `json:"redirectUrl"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("解析直链失败: %w", err)
	}
	u := resp.RedirectURL
	if u == "" {
		u = resp.DownloadURL
	}
	if u == "" {
		return "", fmt.Errorf("123 返回空直链")
	}
	return u, nil
}

// ResolvePathToFileID walks the path from root (parentFileID=0) to find the target file.
// Returns the file ID.
func (c *Client) ResolvePathToFileID(ctx context.Context, relPath string) (int64, error) {
	relPath = strings.Trim(relPath, "/")
	if relPath == "" {
		return 0, fmt.Errorf("空路径")
	}
	parts := strings.Split(relPath, "/")
	parentID := int64(0)
	for i, part := range parts {
		found := false
		var lastID int64
		for {
			resp, err := c.ListFiles(ctx, parentID, 100, lastID)
			if err != nil {
				return 0, fmt.Errorf("列出目录失败 (path=%s): %w", strings.Join(parts[:i+1], "/"), err)
			}
			for _, f := range resp.FileList {
				if strings.EqualFold(f.Filename, part) {
					if i == len(parts)-1 {
						return f.FileID, nil
					}
					if f.Type != 1 {
						return 0, fmt.Errorf("%q 不是目录", strings.Join(parts[:i+1], "/"))
					}
					parentID = f.FileID
					found = true
					break
				}
			}
			if found {
				break
			}
			if resp.LastFileID == 0 || len(resp.FileList) == 0 {
				break
			}
			lastID = resp.LastFileID
		}
		if !found {
			return 0, fmt.Errorf("路径不存在: %s", strings.Join(parts[:i+1], "/"))
		}
	}
	return 0, fmt.Errorf("路径解析异常")
}

// DownloadURL resolves path and returns download URL.
func (c *Client) DownloadURL(ctx context.Context, relPath string) (string, error) {
	fileID, err := c.ResolvePathToFileID(ctx, relPath)
	if err != nil {
		return "", err
	}
	return c.GetDirectLinkURL(ctx, fileID)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
