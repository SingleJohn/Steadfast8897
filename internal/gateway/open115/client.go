// Package open115 is a minimal client for the 115 Open Platform APIs that fyms
// gateway needs to build 302 download redirects. It is a clean-room re-implementation
// based on the public yuque.com/115yun/open documentation; it does not import or
// derive from any AGPL-licensed SDK.
package open115

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

const (
	apiBaseURL = "https://proapi.115.com"
	apiAuthURL = "https://passportapi.115.com"

	apiRefreshToken    = apiAuthURL + "/open/refreshToken"
	apiFsGetFolderInfo = apiBaseURL + "/open/folder/get_info"
	apiFsGetFiles      = apiBaseURL + "/open/ufile/files"
	apiFsDownURL       = apiBaseURL + "/open/ufile/downurl"
)

// TokenUpdater is invoked whenever access/refresh tokens are rotated.
// Implementations should persist the new tokens.
type TokenUpdater func(accessToken, refreshToken string)

// Client is a minimal 115 Open API client.
type Client struct {
	mu           sync.Mutex
	accessToken  string
	refreshToken string
	httpc        *http.Client
	onTokenChange TokenUpdater
}

// New creates a Client with the given initial tokens.
func New(accessToken, refreshToken string, onTokenChange TokenUpdater) *Client {
	return &Client{
		accessToken:   accessToken,
		refreshToken:  refreshToken,
		httpc:         &http.Client{Timeout: 30 * time.Second},
		onTokenChange: onTokenChange,
	}
}

// SetTokens updates tokens in-memory (no callback fired).
func (c *Client) SetTokens(accessToken, refreshToken string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accessToken = accessToken
	c.refreshToken = refreshToken
}

// AccessToken returns the current access token (thread-safe).
func (c *Client) AccessToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.accessToken
}

// genericResp matches the standard 115 API envelope.
type genericResp struct {
	State   bool            `json:"state"`
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// authedRequest performs an HTTP request with the access token, automatically
// retrying once after a token refresh on 401 / no-auth errors.
func (c *Client) authedRequest(ctx context.Context, method, endpoint string, query url.Values, form url.Values, ua string) (json.RawMessage, error) {
	resp, err := c.doRequest(ctx, method, endpoint, query, form, ua)
	if err == nil {
		return resp, nil
	}
	if !isAuthError(err) {
		return nil, err
	}
	if rerr := c.RefreshToken(ctx); rerr != nil {
		return nil, fmt.Errorf("refresh token: %w", rerr)
	}
	return c.doRequest(ctx, method, endpoint, query, form, ua)
}

func (c *Client) doRequest(ctx context.Context, method, endpoint string, query url.Values, form url.Values, ua string) (json.RawMessage, error) {
	full := endpoint
	if len(query) > 0 {
		if strings.Contains(full, "?") {
			full += "&" + query.Encode()
		} else {
			full += "?" + query.Encode()
		}
	}

	var body io.Reader
	if method == http.MethodPost && len(form) > 0 {
		body = bytes.NewBufferString(form.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, full, body)
	if err != nil {
		return nil, err
	}
	if method == http.MethodPost && len(form) > 0 {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if tok := c.AccessToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errAuth{code: resp.StatusCode, msg: string(raw)}
	}

	var g genericResp
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, fmt.Errorf("decode response (%s): %w; body=%s", endpoint, err, truncate(string(raw), 200))
	}
	if !g.State {
		// 40140116 = no auth (token expired/revoked)
		if g.Code == 40140116 || strings.Contains(strings.ToLower(g.Message), "no auth") {
			return nil, errAuth{code: g.Code, msg: g.Message}
		}
		return nil, fmt.Errorf("115 api error: code=%d msg=%s", g.Code, g.Message)
	}
	return g.Data, nil
}

type errAuth struct {
	code int
	msg  string
}

func (e errAuth) Error() string { return fmt.Sprintf("115 auth error: code=%d msg=%s", e.code, e.msg) }

func isAuthError(err error) bool {
	_, ok := err.(errAuth)
	return ok
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// RefreshToken exchanges the refresh token for a new access/refresh pair.
func (c *Client) RefreshToken(ctx context.Context) error {
	c.mu.Lock()
	rt := c.refreshToken
	c.mu.Unlock()
	if rt == "" {
		return fmt.Errorf("refresh token empty")
	}
	form := url.Values{"refresh_token": {rt}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiRefreshToken, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var g genericResp
	if err := json.Unmarshal(raw, &g); err != nil {
		return fmt.Errorf("decode refresh response: %w; body=%s", err, truncate(string(raw), 200))
	}
	if !g.State {
		return fmt.Errorf("refresh failed: code=%d msg=%s", g.Code, g.Message)
	}
	var data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(g.Data, &data); err != nil {
		return fmt.Errorf("decode refresh data: %w", err)
	}
	if data.AccessToken == "" {
		return fmt.Errorf("refresh response missing access_token")
	}
	c.mu.Lock()
	c.accessToken = data.AccessToken
	if data.RefreshToken != "" {
		c.refreshToken = data.RefreshToken
	}
	at, rtNew := c.accessToken, c.refreshToken
	cb := c.onTokenChange
	c.mu.Unlock()
	if cb != nil {
		cb(at, rtNew)
	}
	return nil
}

// FolderInfo is the subset of get_info we use.
type FolderInfo struct {
	FileID       string `json:"file_id"`
	FileName     string `json:"file_name"`
	FileCategory string `json:"file_category"` // "1"=file, "0"=folder
	PickCode     string `json:"pick_code"`
	Sha1         string `json:"sha1"`
	Size         string `json:"size"`
}

// GetFolderInfoByPath looks up a node (file or folder) by absolute 115 path.
// 115 accepts absolute paths like "/电影/foo.mkv".
func (c *Client) GetFolderInfoByPath(ctx context.Context, path string) (*FolderInfo, error) {
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	form := url.Values{"path": {path}}
	data, err := c.authedRequest(ctx, http.MethodPost, apiFsGetFolderInfo, nil, form, "")
	if err != nil {
		return nil, err
	}
	var fi FolderInfo
	if err := json.Unmarshal(data, &fi); err != nil {
		return nil, fmt.Errorf("decode folder info: %w", err)
	}
	return &fi, nil
}

// GetFolderInfoByID looks up a folder by file_id.
func (c *Client) GetFolderInfoByID(ctx context.Context, fileID string) (*FolderInfo, error) {
	q := url.Values{"file_id": {fileID}}
	data, err := c.authedRequest(ctx, http.MethodGet, apiFsGetFolderInfo, q, nil, "")
	if err != nil {
		return nil, err
	}
	var fi FolderInfo
	if err := json.Unmarshal(data, &fi); err != nil {
		return nil, fmt.Errorf("decode folder info: %w", err)
	}
	return &fi, nil
}

// FileEntry is the subset of /open/ufile/files we use.
type FileEntry struct {
	FID      string `json:"fid"`
	Pid      string `json:"pid"`
	Fc       string `json:"fc"` // "0"=folder, "1"=file
	Fn       string `json:"fn"`
	PickCode string `json:"pc"`
	Sha1     string `json:"sha1"`
}

// ListChildren lists direct children of cid (one page).
func (c *Client) ListChildren(ctx context.Context, cid string, offset, limit int64) ([]FileEntry, error) {
	q := url.Values{
		"cid":      {cid},
		"limit":    {strconv.FormatInt(limit, 10)},
		"offset":   {strconv.FormatInt(offset, 10)},
		"show_dir": {"1"},
	}
	data, err := c.authedRequest(ctx, http.MethodGet, apiFsGetFiles, q, nil, "")
	if err != nil {
		return nil, err
	}
	var entries []FileEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("decode file list: %w", err)
	}
	return entries, nil
}

// DownloadURL fetches a time-limited HTTP download URL for a pick_code.
// 115 binds the URL to the requesting User-Agent, so the same UA must be
// used by the final client. Returns the URL and the optional file size.
func (c *Client) DownloadURL(ctx context.Context, pickCode, ua string) (string, error) {
	if pickCode == "" {
		return "", fmt.Errorf("empty pick_code")
	}
	form := url.Values{"pick_code": {pickCode}}
	data, err := c.authedRequest(ctx, http.MethodPost, apiFsDownURL, nil, form, ua)
	if err != nil {
		return "", err
	}
	// Response is a map keyed by file_id; pick the first entry's URL.
	var m map[string]struct {
		FileName string `json:"file_name"`
		FileSize int64  `json:"file_size"`
		PickCode string `json:"pick_code"`
		URL      struct {
			URL string `json:"url"`
		} `json:"url"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return "", fmt.Errorf("decode downurl: %w", err)
	}
	for _, v := range m {
		if v.URL.URL != "" {
			return v.URL.URL, nil
		}
	}
	return "", fmt.Errorf("downurl response had no url")
}
