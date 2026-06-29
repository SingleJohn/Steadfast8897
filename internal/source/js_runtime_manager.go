package source

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"fyms/internal/repository"
)

const (
	jsRuntimeDefaultTimeout = 25 * time.Second
	jsRuntimeOutputMaxBytes = 2 << 20
	jsRuntimeHTTPMaxBytes   = 4 << 20
	jsRuntimeMaxConcurrent  = 4
)

type JSRuntimeManager struct {
	repo      *repository.SourceRepository
	client    *http.Client
	artifacts *JSArtifactManager
	dataDir   string
	nodePath  string
	script    string
	sem       chan struct{}
	limiters  map[int64]*rate.Limiter
	mu        sync.Mutex
	logger    *slog.Logger
}

func NewJSRuntimeManager(repo *repository.SourceRepository, client *http.Client, dataDir string) *JSRuntimeManager {
	if client == nil {
		client = http.DefaultClient
	}
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "data"
	}
	return &JSRuntimeManager{
		repo:      repo,
		client:    client,
		artifacts: NewJSArtifactManager(repo, client, dataDir),
		dataDir:   dataDir,
		sem:       make(chan struct{}, jsRuntimeMaxConcurrent),
		limiters:  map[int64]*rate.Limiter{},
		logger:    SourceLogger("provider"),
	}
}

func (m *JSRuntimeManager) Start(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("JS runtime manager 未初始化")
	}
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return fmt.Errorf("未找到 node 可执行文件: %w", err)
	}
	script, err := resolveJSSidecarScript()
	if err != nil {
		return err
	}
	m.nodePath = nodePath
	m.script = script
	if err := os.MkdirAll(filepath.Join(m.dataDir, "source-runtime", "work"), 0755); err != nil {
		return err
	}
	m.logger.InfoContext(ctx, "JS runtime sidecar ready", "log_target", "provider", "node", nodePath, "script", script)
	return nil
}

func (m *JSRuntimeManager) Run(ctx context.Context, req JSRuntimeRequest) (*JSRuntimeResponse, error) {
	if m == nil {
		return nil, fmt.Errorf("JS runtime manager 未初始化")
	}
	start := time.Now()
	req = normalizeJSRuntimeRequest(req)
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 || timeout > jsRuntimeDefaultTimeout {
		timeout = jsRuntimeDefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case m.sem <- struct{}{}:
		defer func() { <-m.sem }()
	case <-runCtx.Done():
		err := fmt.Errorf("JS runtime 等待 worker 超时: %w", runCtx.Err())
		resp := m.errorResponse(start, req, err, "timeout")
		m.maybeRecordInvocation(ctx, req, resp)
		return nil, err
	}
	if strings.TrimSpace(m.nodePath) == "" || strings.TrimSpace(m.script) == "" {
		if err := m.Start(runCtx); err != nil {
			resp := m.errorResponse(start, req, err, "runtime_unavailable")
			m.maybeRecordInvocation(ctx, req, resp)
			return resp, nil
		}
	}
	artifacts, bodies, err := m.artifacts.FetchPair(runCtx, req)
	if err != nil {
		resp := m.errorResponse(start, req, err, ErrorType(err))
		m.maybeRecordInvocation(ctx, req, resp)
		return nil, err
	}
	payload := map[string]any{
		"type":       "run",
		"engineCode": string(bodies["engine"]),
		"ruleCode":   string(bodies["rule"]),
		"method":     req.Method,
		"args":       req.Args,
		"baseUrl":    req.ConfigBaseURL,
	}
	result, logs, pid, err := m.runWorker(runCtx, req, payload)
	if err != nil {
		result = JSRuntimeMethodResult{
			OK:         false,
			Method:     req.Method,
			Error:      err.Error(),
			ErrorType:  ErrorType(err),
			DurationMs: time.Since(start).Milliseconds(),
		}
	}
	resp := &JSRuntimeResponse{
		OK:          result.OK,
		Engine:      "node-sidecar",
		RuntimeKind: JSRuntimeKindNodeDRPY,
		BaseURL:     req.ConfigBaseURL,
		Artifacts:   artifacts,
		Results:     []JSRuntimeMethodResult{result},
		Logs:        logs,
		DurationMs:  time.Since(start).Milliseconds(),
		WorkerPID:   pid,
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		resp.OK = false
		resp.Results[0].OK = false
		resp.Results[0].Error = "JS runtime 调用超时，worker 已终止"
		resp.Results[0].ErrorType = "timeout"
	}
	m.maybeRecordInvocation(ctx, req, resp)
	return resp, nil
}

func (m *JSRuntimeManager) errorResponse(start time.Time, req JSRuntimeRequest, err error, errorType string) *JSRuntimeResponse {
	if errorType == "" {
		errorType = ErrorType(err)
	}
	return &JSRuntimeResponse{
		OK:          false,
		Engine:      "node-sidecar",
		RuntimeKind: JSRuntimeKindNodeDRPY,
		BaseURL:     req.ConfigBaseURL,
		Results: []JSRuntimeMethodResult{{
			OK:         false,
			Method:     req.Method,
			Error:      err.Error(),
			ErrorType:  errorType,
			DurationMs: time.Since(start).Milliseconds(),
		}},
		DurationMs: time.Since(start).Milliseconds(),
	}
}

func (m *JSRuntimeManager) recordInvocation(ctx context.Context, req JSRuntimeRequest, resp *JSRuntimeResponse) {
	if m == nil || m.repo == nil || resp == nil {
		return
	}
	status := "ok"
	var errorType *string
	var errorMessage *string
	var engineOK *bool
	var workerPID *int32
	method := strings.TrimSpace(req.Method)
	urlHash := URLHash(req.Rule)
	artifactIDs := make([]int64, 0, len(resp.Artifacts))
	for _, artifact := range resp.Artifacts {
		if artifact.ID > 0 {
			artifactIDs = append(artifactIDs, artifact.ID)
		}
	}
	if len(resp.Results) > 0 {
		result := resp.Results[0]
		method = strings.TrimSpace(result.Method)
		engineOK = &result.EngineOK
		if !result.OK {
			status = "error"
			if result.ErrorType != "" {
				v := result.ErrorType
				errorType = &v
			}
			if result.Error != "" {
				v := sanitizeRuntimeAuditError(result.Error)
				errorMessage = &v
			}
		}
	}
	if method == "" {
		method = "unknown"
	}
	if resp.WorkerPID > 0 {
		v := int32(resp.WorkerPID)
		workerPID = &v
	}
	raw := jsonBytes(map[string]any{
		"provider_key_hash": URLHash(req.ProviderKey),
		"base_hash":         URLHash(req.ConfigBaseURL),
		"log_count":         len(resp.Logs),
	}, "{}")
	if _, err := m.repo.CreateRuntimeInvocation(ctx, repository.SourceRuntimeInvocationCreate{
		ProviderID:   req.ProviderID,
		RuntimeKind:  JSRuntimeKindNodeDRPY,
		Method:       method,
		Status:       status,
		ErrorType:    errorType,
		ErrorMessage: errorMessage,
		DurationMS:   resp.DurationMs,
		EngineOK:     engineOK,
		WorkerPID:    workerPID,
		ArtifactIDs:  artifactIDs,
		URLHash:      stringPtrOrNil(urlHash),
		Raw:          raw,
	}); err != nil {
		m.logger.WarnContext(ctx, "record JS runtime invocation failed", "log_target", "provider", "error", err)
	}
}

func (m *JSRuntimeManager) maybeRecordInvocation(ctx context.Context, req JSRuntimeRequest, resp *JSRuntimeResponse) {
	if req.SkipAudit {
		return
	}
	m.recordInvocation(context.WithoutCancel(ctx), req, resp)
}

func sanitizeRuntimeAuditError(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	if strings.Contains(message, "://") {
		return "url_hash:" + URLHash(message)
	}
	if len(message) > 500 {
		return message[:500]
	}
	return message
}

func (m *JSRuntimeManager) runWorker(ctx context.Context, req JSRuntimeRequest, payload map[string]any) (JSRuntimeMethodResult, []string, int, error) {
	cmd := exec.CommandContext(ctx, m.nodePath, m.script)
	cmd.Dir = filepath.Join(m.dataDir, "source-runtime", "work")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return JSRuntimeMethodResult{}, nil, 0, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return JSRuntimeMethodResult{}, nil, 0, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &limitWriter{w: &stderr, limit: jsRuntimeOutputMaxBytes}
	if err := cmd.Start(); err != nil {
		return JSRuntimeMethodResult{}, nil, 0, err
	}
	pid := cmd.Process.Pid
	writer := &jsonLineWriter{w: stdin}
	if err := writer.Write(payload); err != nil {
		return JSRuntimeMethodResult{}, nil, pid, err
	}
	reader := bufio.NewScanner(stdout)
	reader.Buffer(make([]byte, 64*1024), jsRuntimeOutputMaxBytes)
	logs := []string{}
	var result *JSRuntimeMethodResult
	resultReceived := false
	for reader.Scan() {
		line := bytes.TrimSpace(reader.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg struct {
			Type       string          `json:"type"`
			ID         string          `json:"id"`
			Message    string          `json:"message"`
			Request    jsHTTPBridgeReq `json:"request"`
			Result     json.RawMessage `json:"result"`
			DurationMs int64           `json:"durationMs"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			logs = append(logs, string(line))
			continue
		}
		switch msg.Type {
		case "http_request":
			resp := m.handleBridgeHTTP(ctx, req.ProviderID, msg.ID, msg.Request)
			if err := writer.Write(resp); err != nil {
				return JSRuntimeMethodResult{}, logs, pid, err
			}
		case "log":
			if strings.TrimSpace(msg.Message) != "" {
				logs = append(logs, msg.Message)
			}
		case "result":
			parsed := parseJSRuntimeResult(req.Method, msg.Result, msg.DurationMs)
			result = &parsed
			resultReceived = true
			break
		}
		if resultReceived {
			break
		}
	}
	if err := reader.Err(); err != nil {
		return JSRuntimeMethodResult{}, logs, pid, err
	}
	if resultReceived {
		_ = stdin.Close()
	}
	waitErr := waitForWorkerExit(cmd, resultReceived)
	if stderr.Len() > 0 {
		logs = append(logs, strings.Split(strings.TrimSpace(stderr.String()), "\n")...)
	}
	if result == nil && waitErr != nil {
		return JSRuntimeMethodResult{}, logs, pid, waitErr
	}
	if result == nil {
		return JSRuntimeMethodResult{}, logs, pid, fmt.Errorf("JS runtime worker 无结果")
	}
	return *result, logs, pid, waitErr
}

func waitForWorkerExit(cmd *exec.Cmd, resultReceived bool) error {
	if cmd == nil {
		return nil
	}
	if !resultReceived {
		return cmd.Wait()
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(1200 * time.Millisecond):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		err := <-done
		if err != nil {
			return nil
		}
		return nil
	}
}

type jsonLineWriter struct {
	w  io.Writer
	mu sync.Mutex
}

func (w *jsonLineWriter) Write(value any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	_, err = w.w.Write(raw)
	return err
}

type jsHTTPBridgeReq struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

func (m *JSRuntimeManager) handleBridgeHTTP(ctx context.Context, providerID *int64, id string, in jsHTTPBridgeReq) map[string]any {
	start := time.Now()
	if err := ValidateOutboundURL(ctx, in.URL); err != nil {
		return map[string]any{"type": "http_response", "id": id, "ok": false, "error": err.Error()}
	}
	limiterID := int64(0)
	if providerID != nil {
		limiterID = *providerID
	}
	if err := m.wait(limiterID).Wait(ctx); err != nil {
		return map[string]any{"type": "http_response", "id": id, "ok": false, "error": err.Error()}
	}
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if in.Body != "" {
		body = strings.NewReader(in.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, in.URL, body)
	if err != nil {
		return map[string]any{"type": "http_response", "id": id, "ok": false, "error": err.Error()}
	}
	for key, value := range in.Headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			httpReq.Header.Set(key, value)
		}
	}
	if httpReq.Header.Get("User-Agent") == "" {
		httpReq.Header.Set("User-Agent", "FYMS-DRPY-Runtime/1.0")
	}
	resp, err := m.client.Do(httpReq)
	if err != nil {
		return map[string]any{"type": "http_response", "id": id, "ok": false, "error": err.Error()}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, jsRuntimeHTTPMaxBytes+1))
	if err != nil {
		return map[string]any{"type": "http_response", "id": id, "ok": false, "error": err.Error()}
	}
	if len(raw) > jsRuntimeHTTPMaxBytes {
		return map[string]any{"type": "http_response", "id": id, "ok": false, "error": "响应体超过 JS runtime 上限"}
	}
	headers := map[string]string{}
	for key, values := range resp.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}
	return map[string]any{
		"type":       "http_response",
		"id":         id,
		"ok":         true,
		"status":     resp.StatusCode,
		"headers":    headers,
		"bodyBase64": base64.StdEncoding.EncodeToString(raw),
		"bodyBytes":  len(raw),
		"durationMs": time.Since(start).Milliseconds(),
	}
}

func (m *JSRuntimeManager) wait(providerID int64) *rate.Limiter {
	m.mu.Lock()
	defer m.mu.Unlock()
	limiter := m.limiters[providerID]
	if limiter == nil {
		limiter = rate.NewLimiter(rate.Every(500*time.Millisecond), 2)
		m.limiters[providerID] = limiter
	}
	return limiter
}

func parseJSRuntimeResult(fallbackMethod string, raw json.RawMessage, durationMs int64) JSRuntimeMethodResult {
	result := JSRuntimeMethodResult{Method: fallbackMethod, DurationMs: durationMs}
	if len(raw) == 0 {
		result.Error = "JS runtime 空结果"
		result.ErrorType = "empty_result"
		return result
	}
	var wrapper struct {
		OK     bool   `json:"ok"`
		Method string `json:"method"`
		Engine struct {
			OK    bool   `json:"ok"`
			Stage string `json:"stage"`
			Error string `json:"error"`
		} `json:"engine"`
		Data       json.RawMessage `json:"data"`
		Error      string          `json:"error"`
		ErrorType  string          `json:"errorType"`
		Results    json.RawMessage `json:"results"`
		DurationMs int64           `json:"durationMs"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		result.Error = "解析 JS runtime 输出失败: " + err.Error()
		result.ErrorType = "decode_failed"
		result.Data = raw
		return result
	}
	result.OK = wrapper.OK
	if wrapper.Method != "" {
		result.Method = wrapper.Method
	}
	result.EngineOK = wrapper.Engine.OK
	if wrapper.Engine.Error != "" {
		result.EngineNote = strings.TrimSpace(wrapper.Engine.Stage + ": " + wrapper.Engine.Error)
	} else {
		result.EngineNote = wrapper.Engine.Stage
	}
	if len(wrapper.Results) > 0 {
		result.Data = wrapper.Results
	} else {
		result.Data = wrapper.Data
	}
	result.Error = wrapper.Error
	result.ErrorType = wrapper.ErrorType
	if wrapper.DurationMs > 0 {
		result.DurationMs = wrapper.DurationMs
	}
	return result
}

func resolveJSSidecarScript() (string, error) {
	candidates := []string{
		filepath.Join("runtime", "js-sidecar", "sidecar.mjs"),
		filepath.Join("..", "runtime", "js-sidecar", "sidecar.mjs"),
		filepath.Join("/app", "runtime", "js-sidecar", "sidecar.mjs"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				return candidate, nil
			}
			return abs, nil
		}
	}
	return "", fmt.Errorf("未找到 JS sidecar 脚本 runtime/js-sidecar/sidecar.mjs")
}
