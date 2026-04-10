package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Runtime holds the running state of all source servers and adapters.
type Runtime struct {
	mu             sync.Mutex
	config         *GatewayConfig
	servers        map[string]*http.Server
	adapters       map[string]BackendAdapter
	store          *Store
	logger         *slog.Logger
	recorder       *Recorder
	managementPort int
}

func NewRuntime(store *Store, logger *slog.Logger, managementPort int) *Runtime {
	return &Runtime{
		servers:        map[string]*http.Server{},
		adapters:       map[string]BackendAdapter{},
		store:          store,
		logger:         logger,
		managementPort: managementPort,
	}
}

// Rebuild recreates adapters and reconciles source servers based on new config.
func (rt *Runtime) Rebuild(ctx context.Context, cfg *GatewayConfig) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	oldSources := rt.getSourceMap()

	rt.config = cfg
	rt.adapters = BuildBackendAdapters(cfg.Backends, rt.saveOpen115Tokens)

	if rt.recorder == nil {
		rt.recorder = NewRecorder(rt.store, cfg.Observability, rt.logger)
		go rt.recorder.Run(ctx)
	}

	return rt.reconcileServers(ctx, cfg, oldSources)
}

func (rt *Runtime) getSourceMap() map[string]EmbySourceConfig {
	if rt.config == nil {
		return nil
	}
	m := make(map[string]EmbySourceConfig, len(rt.config.Sources))
	for _, src := range rt.config.Sources {
		m[src.ID] = src
	}
	return m
}

// saveOpen115Tokens persists rotated 115 tokens for the given backend ID
// back into the gateway config. Invoked from the open115 client whenever
// a refresh succeeds.
func (rt *Runtime) saveOpen115Tokens(backendID, accessToken, refreshToken string) {
	rt.mu.Lock()
	if rt.config == nil {
		rt.mu.Unlock()
		return
	}
	updated := false
	for i := range rt.config.Backends {
		b := &rt.config.Backends[i]
		if b.ID != backendID || b.Type != "115_open" || b.Open115 == nil {
			continue
		}
		if b.Open115.AccessToken == accessToken && b.Open115.RefreshToken == refreshToken {
			break
		}
		b.Open115.AccessToken = accessToken
		b.Open115.RefreshToken = refreshToken
		updated = true
		break
	}
	cfgCopy := rt.config
	rt.mu.Unlock()
	if !updated {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rt.store.SaveConfig(ctx, cfgCopy); err != nil {
		rt.logger.Warn("persist 115_open tokens failed", "backend_id", backendID, "error", err)
		return
	}
	rt.logger.Info("persisted refreshed 115_open tokens", "backend_id", backendID)
}

func (rt *Runtime) reconcileServers(ctx context.Context, cfg *GatewayConfig, oldSources map[string]EmbySourceConfig) error {
	desired := map[string]EmbySourceConfig{}
	for _, src := range cfg.Sources {
		if src.Enabled {
			desired[src.ID] = src
		}
	}

	// Stop servers that are no longer desired or have changed
	for id, srv := range rt.servers {
		newSrc, stillDesired := desired[id]
		if !stillDesired {
			rt.logger.Info("stopping source server (removed)", "source_id", id)
			shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			srv.Shutdown(shutdownCtx)
			cancel()
			delete(rt.servers, id)
			continue
		}
		oldSrc, hadOld := oldSources[id]
		if !hadOld || !sourceSpecEqual(oldSrc, newSrc) {
			rt.logger.Info("restarting source server (config changed)", "source_id", id)
			shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			srv.Shutdown(shutdownCtx)
			cancel()
			delete(rt.servers, id)
		}
	}

	// Start new/changed servers
	for id, src := range desired {
		if _, running := rt.servers[id]; running {
			continue
		}
		rt.startSourceServer(src)
	}
	return nil
}

func sourceSpecEqual(a, b EmbySourceConfig) bool {
	if a.ListenHost != b.ListenHost || a.ListenPort != b.ListenPort {
		return false
	}
	if a.StreamPathPrefix != b.StreamPathPrefix {
		return false
	}
	if a.Upstream.Mode != b.Upstream.Mode || a.Upstream.Host != b.Upstream.Host || a.Upstream.BasePath != b.Upstream.BasePath || a.Upstream.ApiKey != b.Upstream.ApiKey {
		return false
	}
	if len(a.Routes) != len(b.Routes) {
		return false
	}
	for i := range a.Routes {
		ra, rb := a.Routes[i], b.Routes[i]
		if ra.ID != rb.ID || ra.Enabled != rb.Enabled || ra.Priority != rb.Priority ||
			ra.PathRuleSetID != rb.PathRuleSetID || ra.PoolID != rb.PoolID || ra.RequireMapping != rb.RequireMapping {
			return false
		}
	}
	return true
}

func (rt *Runtime) startSourceServer(src EmbySourceConfig) {
	var upstreamURL string
	if src.Upstream.Mode == "self" {
		upstreamURL = fmt.Sprintf("http://127.0.0.1:%d", rt.managementPort)
	} else {
		upstreamURL = strings.TrimRight(src.Upstream.Host, "/")
	}

	upstream, err := url.Parse(upstreamURL)
	if err != nil {
		rt.logger.Error("invalid upstream URL", "source_id", src.ID, "error", err)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(upstream)
	origDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		origDirector(req)
		if src.Upstream.Mode != "self" && src.Upstream.BasePath != "" {
			req.URL.Path = strings.TrimRight(src.Upstream.BasePath, "/") + req.URL.Path
		}
		req.Host = upstream.Host
		if src.Upstream.Mode != "self" && src.Upstream.ApiKey != "" {
			q := req.URL.Query()
			q.Set("api_key", src.Upstream.ApiKey)
			req.URL.RawQuery = q.Encode()
		}
	}

	mux := http.NewServeMux()

	streamPrefix := src.StreamPathPrefix
	if streamPrefix == "" {
		streamPrefix = "/stream"
	}
	streamPrefix = strings.TrimRight(streamPrefix, "/")

	mux.HandleFunc(streamPrefix+"/", func(w http.ResponseWriter, r *http.Request) {
		rt.handleDirectStream(w, r, src, streamPrefix)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		rt.handleProxyRequest(w, r, src, proxy)
	})

	addr := fmt.Sprintf("%s:%d", src.ListenHost, src.ListenPort)
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	rt.servers[src.ID] = srv

	go func() {
		rt.logger.Info("starting source server", "source_id", src.ID, "name", src.Name, "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			rt.logger.Error("source server error", "source_id", src.ID, "error", err)
		}
	}()
}

func (rt *Runtime) handleDirectStream(w http.ResponseWriter, r *http.Request, src EmbySourceConfig, prefix string) {
	start := time.Now()
	objectPath := strings.TrimPrefix(r.URL.Path, prefix)

	rt.mu.Lock()
	cfg := rt.config
	adapters := rt.adapters
	rt.mu.Unlock()

	decision := DecideRoute(src.Routes, objectPath)
	if decision == nil {
		http.Error(w, "no matching route", http.StatusNotFound)
		rt.logRequest(src.ID, "proxy", r, http.StatusNotFound, start, "", "", "")
		return
	}

	var ruleSet *PathRuleSetConfig
	if decision.PathRuleSetID != "" {
		ruleSet = FindPathRuleSet(cfg.PathRuleSets, decision.PathRuleSetID)
	}

	objectKey, ok := ResolveObjectKey(objectPath, ruleSet, decision.RequireMapping)
	if !ok {
		http.Error(w, "path mapping required but not matched", http.StatusNotFound)
		rt.logRequest(src.ID, "proxy", r, http.StatusNotFound, start, decision.RouteID, decision.PoolID, "")
		return
	}

	pool := FindPool(cfg.ResourcePools, decision.PoolID)
	if pool == nil {
		http.Error(w, "resource pool not found", http.StatusInternalServerError)
		rt.logRequest(src.ID, "proxy", r, http.StatusInternalServerError, start, decision.RouteID, decision.PoolID, "")
		return
	}

	redirectURL, backendID, err := TryPool(r.Context(), *pool, adapters, objectKey, r.UserAgent())
	if err != nil {
		rt.logger.Error("302 redirect failed", "source_id", src.ID, "object_key", objectKey, "error", err)
		http.Error(w, "redirect failed: "+err.Error(), http.StatusBadGateway)
		rt.logRequest(src.ID, "proxy", r, http.StatusBadGateway, start, decision.RouteID, decision.PoolID, "")
		return
	}

	http.Redirect(w, r, redirectURL, http.StatusFound)
	rt.logRedirect(src.ID, r, start, decision.RouteID, decision.PoolID, objectKey, backendID, redirectURL)
}

func (rt *Runtime) handleProxyRequest(w http.ResponseWriter, r *http.Request, src EmbySourceConfig, proxy *httputil.ReverseProxy) {
	if itemID, ok := matchInterceptItemID(r.URL.Path); ok {
		if r.Method != http.MethodHead && rt.tryResolvePlaybackRoute(w, r, src, itemID) {
			// 302 直链成功
			r.Header.Set("X-Play-Method", "Redirect")
			return
		}
		// 302 失败
		if src.DisableProxyFallback {
			rt.logger.Warn("302 failed and proxy fallback disabled", "source_id", src.ID, "item_id", itemID)
			http.Error(w, "302 直链失败，网关中转已禁用", http.StatusBadGateway)
			return
		}
	}
	// 网关中转
	r.Header.Set("X-Play-Method", "Proxy")
	proxy.ServeHTTP(w, r)
}

// tryResolvePlaybackRoute resolves the real media path and attempts a 302 redirect.
// In "self" mode, queries the local FYMS database directly.
// In "external" mode, fetches from the upstream Emby API.
// Returns true if the redirect was issued, false to fall through to proxy.
func (rt *Runtime) tryResolvePlaybackRoute(w http.ResponseWriter, r *http.Request, src EmbySourceConfig, itemID string) bool {
	start := time.Now()

	mediaSourceID := r.URL.Query().Get("MediaSourceId")
	if mediaSourceID == "" {
		mediaSourceID = r.URL.Query().Get("mediaSourceId")
	}

	var realPath string
	if src.Upstream.Mode == "self" {
		realPath = rt.resolveSelfMediaPathDirect(r.Context(), itemID, mediaSourceID)
		if realPath == "" {
			realPath = rt.resolveSelfMediaPath(r.Context(), r, src, itemID, mediaSourceID)
		}
	} else {
		realPath = rt.resolveRemoteMediaPath(r.Context(), src, itemID, mediaSourceID)
	}
	if realPath == "" {
		return false
	}

	rt.logger.Info("resolved media path", "source_id", src.ID, "item_id", itemID, "real_path", realPath, "mode", src.Upstream.Mode)

	rt.mu.Lock()
	cfg := rt.config
	adapters := rt.adapters
	rt.mu.Unlock()

	decision := DecideRoute(src.Routes, realPath)
	if decision == nil {
		rt.logger.Warn("no route matched for real path", "source_id", src.ID, "real_path", realPath)
		return false
	}

	var ruleSet *PathRuleSetConfig
	if decision.PathRuleSetID != "" {
		ruleSet = FindPathRuleSet(cfg.PathRuleSets, decision.PathRuleSetID)
	}

	objectKey, ok := ResolveObjectKey(realPath, ruleSet, decision.RequireMapping)
	if !ok {
		rt.logger.Warn("path mapping not matched", "source_id", src.ID, "real_path", realPath, "rule_set", decision.PathRuleSetID, "require_mapping", decision.RequireMapping)
		return false
	}

	pool := FindPool(cfg.ResourcePools, decision.PoolID)
	if pool == nil {
		rt.logger.Warn("resource pool not found", "source_id", src.ID, "pool_id", decision.PoolID)
		return false
	}

	redirectURL, backendID, err := TryPool(r.Context(), *pool, adapters, objectKey, r.UserAgent())
	if err != nil {
		rt.logger.Warn("302 redirect failed, falling back to proxy", "source_id", src.ID, "item_id", itemID, "error", err)
		return false
	}

	rt.logger.Info("302 redirect", "source_id", src.ID, "item_id", itemID, "backend", backendID, "object_key", objectKey)
	http.Redirect(w, r, redirectURL, http.StatusFound)
	rt.logRedirect(src.ID, r, start, decision.RouteID, decision.PoolID, objectKey, backendID, redirectURL)
	return true
}

// resolveSelfMediaPath calls the local FYMS management API to resolve the media path.
// This uses the same Items API that external Emby upstream uses, which properly
// resolves .strm files into actual media paths via buildItemMediaSources.
func (rt *Runtime) resolveSelfMediaPath(ctx context.Context, originalReq *http.Request, src EmbySourceConfig, itemID, mediaSourceID string) string {
	apiKey := strings.TrimSpace(src.Upstream.ApiKey)

	localHost := fmt.Sprintf("http://127.0.0.1:%d", rt.managementPort)
	embyAPIURL, ok := buildEmbyAPIURL(localHost, "", itemID, apiKey)
	if !ok {
		rt.logger.Warn("self-mode: failed to build local API URL", "source_id", src.ID)
		return ""
	}

	embyReq, err := http.NewRequestWithContext(ctx, http.MethodGet, embyAPIURL, nil)
	if err != nil {
		rt.logger.Warn("self-mode: failed to create local API request", "source_id", src.ID, "error", err)
		return ""
	}
	copyAuthForLocalResolve(embyReq, originalReq)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(embyReq)
	if err != nil {
		rt.logger.Warn("self-mode: local API request failed", "source_id", src.ID, "error", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		rt.logger.Debug("self-mode: local API returned non-200", "source_id", src.ID, "status", resp.StatusCode)
		return ""
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ""
	}

	type MediaSource struct {
		Id   string `json:"Id"`
		Path string `json:"Path"`
	}
	type Item struct {
		Path         string        `json:"Path"`
		MediaSources []MediaSource `json:"MediaSources"`
	}
	type EmbyResponse struct {
		Items []Item `json:"Items"`
	}

	var embyResp EmbyResponse
	if err := json.Unmarshal(bodyBytes, &embyResp); err != nil {
		rt.logger.Debug("self-mode: local API JSON parse failed", "source_id", src.ID, "error", err)
		return ""
	}
	if len(embyResp.Items) == 0 {
		rt.logger.Debug("self-mode: no items returned from local API", "source_id", src.ID, "item_id", itemID)
		return ""
	}

	item := embyResp.Items[0]
	realPath := strings.TrimSpace(item.Path)
	if len(item.MediaSources) > 0 {
		var selected *MediaSource
		if mediaSourceID != "" {
			for i := range item.MediaSources {
				if item.MediaSources[i].Id == mediaSourceID {
					selected = &item.MediaSources[i]
					break
				}
			}
		}
		if selected == nil {
			selected = &item.MediaSources[0]
		}
		if strings.TrimSpace(selected.Path) != "" {
			realPath = strings.TrimSpace(selected.Path)
		}
	}
	return realPath
}

// resolveRemoteMediaPath fetches the real media path from an external upstream Emby server.
func (rt *Runtime) resolveRemoteMediaPath(ctx context.Context, src EmbySourceConfig, itemID, mediaSourceID string) string {
	up := src.Upstream
	if strings.TrimSpace(up.Host) == "" || strings.TrimSpace(up.ApiKey) == "" {
		return ""
	}

	embyAPIURL, ok := buildEmbyAPIURL(up.Host, up.BasePath, itemID, up.ApiKey)
	if !ok {
		rt.logger.Warn("invalid upstream config for Emby API", "source_id", src.ID)
		return ""
	}

	embyReq, err := http.NewRequestWithContext(ctx, http.MethodGet, embyAPIURL, nil)
	if err != nil {
		rt.logger.Warn("failed to create Emby API request", "source_id", src.ID, "error", err)
		return ""
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(embyReq)
	if err != nil {
		rt.logger.Warn("Emby API request failed, falling back to proxy", "source_id", src.ID, "error", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		rt.logger.Debug("Emby API returned non-200, falling back to proxy", "source_id", src.ID, "status", resp.StatusCode)
		return ""
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ""
	}

	type MediaSource struct {
		Id   string `json:"Id"`
		Path string `json:"Path"`
	}
	type Item struct {
		Path         string        `json:"Path"`
		MediaSources []MediaSource `json:"MediaSources"`
	}
	type EmbyResponse struct {
		Items []Item `json:"Items"`
	}

	var embyResp EmbyResponse
	if err := json.Unmarshal(bodyBytes, &embyResp); err != nil {
		rt.logger.Debug("Emby API JSON parse failed", "source_id", src.ID, "error", err)
		return ""
	}
	if len(embyResp.Items) == 0 {
		return ""
	}

	item := embyResp.Items[0]
	realPath := strings.TrimSpace(item.Path)
	if len(item.MediaSources) > 0 {
		var selected *MediaSource
		if mediaSourceID != "" {
			for i := range item.MediaSources {
				if item.MediaSources[i].Id == mediaSourceID {
					selected = &item.MediaSources[i]
					break
				}
			}
		}
		if selected == nil {
			selected = &item.MediaSources[0]
		}
		if strings.TrimSpace(selected.Path) != "" {
			realPath = strings.TrimSpace(selected.Path)
		}
	}
	return realPath
}

// resolveSelfMediaPathDirect queries the database directly to resolve the real media path,
// bypassing the HTTP API. This avoids authentication issues and reduces latency since
// the Gateway shares the same database pool as the main FYMS server.
func (rt *Runtime) resolveSelfMediaPathDirect(ctx context.Context, itemID, mediaSourceID string) string {
	pool := rt.store.Pool()

	var filePath string

	if mediaSourceID != "" {
		err := pool.QueryRow(ctx,
			"SELECT file_path FROM media_versions WHERE id = $1::uuid", mediaSourceID).Scan(&filePath)
		if err == nil && filePath != "" {
			resolved := resolveStrmContent(filePath)
			rt.logger.Debug("resolveSelfMediaPathDirect: found by mediaSourceID", "msid", mediaSourceID, "raw", filePath, "resolved", resolved)
			return resolved
		}
	}

	err := pool.QueryRow(ctx,
		`SELECT file_path FROM media_versions WHERE item_id = $1::uuid
		 ORDER BY is_primary DESC, created_at ASC LIMIT 1`, itemID).Scan(&filePath)
	if err == nil && filePath != "" {
		resolved := resolveStrmContent(filePath)
		rt.logger.Debug("resolveSelfMediaPathDirect: found by item media_version", "itemID", itemID, "raw", filePath, "resolved", resolved)
		return resolved
	}

	var fp *string
	err = pool.QueryRow(ctx,
		"SELECT file_path FROM items WHERE id = $1::uuid", itemID).Scan(&fp)
	if err == nil && fp != nil && *fp != "" {
		resolved := resolveStrmContent(*fp)
		rt.logger.Debug("resolveSelfMediaPathDirect: found by item.file_path", "itemID", itemID, "raw", *fp, "resolved", resolved)
		return resolved
	}

	rt.logger.Debug("resolveSelfMediaPathDirect: no path found", "itemID", itemID, "msid", mediaSourceID)
	return ""
}

// resolveStrmContent reads a .strm file and returns the first non-empty, non-comment line.
// For non-.strm paths, returns the path unchanged.
func resolveStrmContent(filePath string) string {
	if !strings.HasSuffix(strings.ToLower(filePath), ".strm") {
		return filePath
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return filePath
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return filePath
}

// matchInterceptItemID extracts the Emby item ID from playback URLs.
// Handles optional /emby/ prefix and patterns: /videos/{id}/stream, /audio/{id}/stream,
// /audio/{id}/universal, /items/{id}/download, /sync/jobitems/{id}/file.
func matchInterceptItemID(requestPath string) (string, bool) {
	parts := splitPath(requestPath)
	if len(parts) < 3 {
		return "", false
	}

	offset := 0
	if strings.EqualFold(parts[0], "emby") {
		offset = 1
	}
	if len(parts) <= offset+2 {
		return "", false
	}

	switch strings.ToLower(parts[offset]) {
	case "videos":
		itemID := parts[offset+1]
		action := strings.ToLower(parts[offset+2])
		if strings.HasPrefix(action, "stream") || strings.HasPrefix(action, "original") {
			return itemID, true
		}
	case "audio":
		itemID := parts[offset+1]
		action := strings.ToLower(parts[offset+2])
		if strings.HasPrefix(action, "stream") || strings.HasPrefix(action, "universal") {
			return itemID, true
		}
	case "items":
		itemID := parts[offset+1]
		action := strings.ToLower(parts[offset+2])
		if strings.HasPrefix(action, "download") {
			return itemID, true
		}
	case "sync":
		if len(parts) <= offset+3 {
			return "", false
		}
		if !strings.EqualFold(parts[offset+1], "jobitems") {
			return "", false
		}
		if !strings.EqualFold(parts[offset+3], "file") {
			return "", false
		}
		return parts[offset+2], true
	}
	return "", false
}

func splitPath(p string) []string {
	raw := strings.Split(p, "/")
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func buildEmbyAPIURL(host, basePath, itemID, apiKey string) (string, bool) {
	base := strings.TrimRight(strings.TrimSpace(host), "/")
	if base == "" {
		return "", false
	}
	if bp := strings.TrimSpace(basePath); bp != "" {
		base = base + "/" + strings.Trim(bp, "/")
	}
	parsed, err := url.Parse(base + "/Items")
	if err != nil {
		return "", false
	}
	q := parsed.Query()
	q.Set("Ids", itemID)
	q.Set("Fields", "Path,MediaSources")
	if strings.TrimSpace(apiKey) != "" {
		q.Set("api_key", apiKey)
	}
	parsed.RawQuery = q.Encode()
	return parsed.String(), true
}

func copyAuthForLocalResolve(dst, src *http.Request) {
	if dst == nil || src == nil {
		return
	}
	for _, header := range []string{
		"Authorization",
		"X-Emby-Authorization",
		"X-Emby-Token",
		"X-MediaBrowser-Token",
	} {
		if v := strings.TrimSpace(src.Header.Get(header)); v != "" {
			dst.Header.Set(header, v)
		}
	}
	if dst.URL == nil || src.URL == nil {
		return
	}
	srcQuery := src.URL.Query()
	dstQuery := dst.URL.Query()
	for _, key := range []string{"api_key", "ApiKey"} {
		if v := strings.TrimSpace(srcQuery.Get(key)); v != "" {
			dstQuery.Set(key, v)
		}
	}
	dst.URL.RawQuery = dstQuery.Encode()
}

func (rt *Runtime) logRequest(sourceID, tag string, r *http.Request, status int, start time.Time, routeID, poolID, backendID string) {
	if rt.recorder == nil {
		return
	}
	rt.recorder.Record(RequestLog{
		Tag:             tag,
		SourceID:        sourceID,
		ClientIP:        r.RemoteAddr,
		Method:          r.Method,
		Path:            r.URL.Path,
		Query:           r.URL.RawQuery,
		Status:          status,
		LatencyMs:       time.Since(start).Milliseconds(),
		UserAgent:       r.UserAgent(),
		Referer:         r.Referer(),
		RouteID:         routeID,
		PoolID:          poolID,
		RedirectBackend: backendID,
	})
}

func (rt *Runtime) logRedirect(sourceID string, r *http.Request, start time.Time, routeID, poolID, objectKey, backendID, redirectURL string) {
	if rt.recorder == nil {
		return
	}
	rt.recorder.Record(RequestLog{
		Tag:              "proxy",
		SourceID:         sourceID,
		ClientIP:         r.RemoteAddr,
		Method:           r.Method,
		Path:             r.URL.Path,
		Query:            r.URL.RawQuery,
		Status:           http.StatusFound,
		LatencyMs:        time.Since(start).Milliseconds(),
		UserAgent:        r.UserAgent(),
		Referer:          r.Referer(),
		RouteID:          routeID,
		PoolID:           poolID,
		ObjectKey:        objectKey,
		RedirectBackend:  backendID,
		RedirectLocation: redirectURL,
	})
}

// Shutdown stops all source servers.
func (rt *Runtime) Shutdown(ctx context.Context) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for id, srv := range rt.servers {
		rt.logger.Info("shutting down source server", "source_id", id)
		srv.Shutdown(ctx)
	}
	rt.servers = map[string]*http.Server{}
	if rt.recorder != nil {
		rt.recorder.Flush(ctx)
	}
}
