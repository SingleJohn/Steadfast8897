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
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"fyms/internal/repository"
)

const (
	cspRuntimeDefaultTimeout = 30 * time.Second
	cspRuntimeOutputMaxBytes = 2 << 20
	cspRuntimeHTTPMaxBytes   = 4 << 20
	cspRuntimeMaxConcurrent  = 2
)

type CSPRuntimeManager struct {
	repo      *repository.SourceRepository
	client    *http.Client
	artifacts *CSPArtifactManager
	dataDir   string
	javaPath  string
	sidecar   string
	workDir   string
	sem       chan struct{}
	limiters  map[int64]*rate.Limiter
	mu        sync.Mutex
	logger    *slog.Logger
}

func NewCSPRuntimeManager(repo *repository.SourceRepository, client *http.Client, dataDir string) *CSPRuntimeManager {
	if client == nil {
		client = http.DefaultClient
	}
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "data"
	}
	workDir := filepath.Join(dataDir, "source-runtime", "csp", "work")
	if abs, err := filepath.Abs(workDir); err == nil {
		workDir = abs
	}
	return &CSPRuntimeManager{
		repo:      repo,
		client:    client,
		artifacts: NewCSPArtifactManager(repo, client, dataDir),
		dataDir:   dataDir,
		workDir:   workDir,
		sem:       make(chan struct{}, sourceRuntimeConcurrency("FYMS_CSP_RUNTIME_CONCURRENCY", cspRuntimeMaxConcurrent)),
		limiters:  map[int64]*rate.Limiter{},
		logger:    SourceLogger("provider"),
	}
}

func (m *CSPRuntimeManager) Start(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("CSP runtime manager 未初始化")
	}
	javaPath, err := exec.LookPath("java")
	if err != nil {
		return fmt.Errorf("未找到 java 可执行文件: %w", err)
	}
	sidecar, err := resolveCSPSidecarJar()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.workDir, 0755); err != nil {
		return err
	}
	m.javaPath = javaPath
	m.sidecar = sidecar
	m.logger.InfoContext(ctx, "CSP runtime ready", "log_target", "provider", "java", javaPath, "sidecar", sidecar)
	return nil
}

func (m *CSPRuntimeManager) Run(ctx context.Context, req CSPRuntimeRequest) (*CSPRuntimeResponse, error) {
	if m == nil {
		return nil, fmt.Errorf("CSP runtime manager 未初始化")
	}
	start := time.Now()
	req = normalizeCSPRuntimeRequest(req)
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 || timeout > cspRuntimeDefaultTimeout {
		timeout = cspRuntimeDefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// 运行时不可用(无 java/sidecar)时立即失败,不占用有限的 worker 槽位,也避免无谓地下载 DEX artifact。
	if strings.TrimSpace(m.javaPath) == "" || strings.TrimSpace(m.sidecar) == "" {
		if err := m.Start(runCtx); err != nil {
			resp := m.errorResponse(start, req, err, "runtime_unavailable")
			m.maybeRecordInvocation(ctx, req, resp)
			return resp, nil
		}
	}
	select {
	case m.sem <- struct{}{}:
		defer func() { <-m.sem }()
	case <-runCtx.Done():
		err := fmt.Errorf("CSP runtime 等待 worker 超时: %w", runCtx.Err())
		resp := m.errorResponse(start, req, err, "timeout")
		m.maybeRecordInvocation(ctx, req, resp)
		return nil, err
	}
	artifact, err := m.artifacts.Fetch(runCtx, req)
	if err != nil {
		resp := m.errorResponse(start, req, err, ErrorType(err))
		m.maybeRecordInvocation(ctx, req, resp)
		return nil, err
	}
	artifact, err = m.ensureLocalArtifact(runCtx, req, artifact)
	if err != nil {
		resp := m.errorResponse(start, req, err, ErrorType(err))
		resp.Artifact = artifact
		m.maybeRecordInvocation(ctx, req, resp)
		return nil, err
	}
	if err := ensureCSPArtifactTrusted(artifact); err != nil {
		resp := m.errorResponse(start, req, err, "untrusted_artifact")
		resp.Artifact = artifact
		m.maybeRecordInvocation(ctx, req, resp)
		return resp, nil
	}
	resp := &CSPRuntimeResponse{
		RuntimeKind: CSPRuntimeKindJVM,
		BaseURL:     req.ConfigBaseURL,
		API:         req.API,
		Method:      req.Method,
		Artifact:    artifact,
		DurationMs:  time.Since(start).Milliseconds(),
	}
	result, dex2jar, logs, pid, err := m.runSidecar(runCtx, req, artifact)
	if err != nil {
		result.OK = false
		result.Method = req.Method
		result.ClassName = cspClassName(req.API)
		result.Error = err.Error()
		result.ErrorType = ErrorType(err)
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		result.OK = false
		result.Error = "CSP runtime 调用超时，worker 已终止"
		result.ErrorType = "timeout"
	}
	resp.Dex2Jar = dex2jar
	resp.OK = result.OK
	resp.Result = result
	resp.Data = result.Data
	resp.Logs = logs
	resp.WorkerPID = pid
	resp.DurationMs = time.Since(start).Milliseconds()
	m.maybeRecordInvocation(ctx, req, resp)
	return resp, nil
}

func (m *CSPRuntimeManager) runSidecar(ctx context.Context, req CSPRuntimeRequest, artifact CSPRuntimeArtifact) (CSPSidecarResult, CSPDex2JarResult, []string, int, error) {
	artifactPath, err := filepath.Abs(artifact.Path)
	if err != nil {
		return CSPSidecarResult{}, CSPDex2JarResult{}, nil, 0, err
	}
	workDir, err := filepath.Abs(m.providerWorkDir(req))
	if err != nil {
		return CSPSidecarResult{}, CSPDex2JarResult{}, nil, 0, err
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return CSPSidecarResult{}, CSPDex2JarResult{}, nil, 0, err
	}
	payload := map[string]any{
		"type":         "run",
		"className":    cspClassName(req.API),
		"method":       req.Method,
		"args":         req.Args,
		"baseUrl":      req.ConfigBaseURL,
		"extend":       req.Ext,
		"providerKey":  req.ProviderKey,
		"artifactPath": artifactPath,
		"workDir":      workDir,
	}
	cmd := exec.CommandContext(ctx, m.javaPath,
		"-Dfile.encoding=UTF-8",
		"-Dsun.stdout.encoding=UTF-8",
		"-Dsun.stderr.encoding=UTF-8",
		"-jar", m.sidecar)
	cmd.Dir = workDir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return CSPSidecarResult{}, CSPDex2JarResult{}, nil, 0, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return CSPSidecarResult{}, CSPDex2JarResult{}, nil, 0, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &limitWriter{w: &stderr, limit: cspRuntimeOutputMaxBytes}
	if err := cmd.Start(); err != nil {
		return CSPSidecarResult{}, CSPDex2JarResult{}, nil, 0, err
	}
	pid := cmd.Process.Pid
	writer := &jsonLineWriter{w: stdin}
	if err := writer.Write(payload); err != nil {
		return CSPSidecarResult{}, CSPDex2JarResult{}, nil, pid, err
	}
	reader := bufio.NewScanner(stdout)
	reader.Buffer(make([]byte, 64*1024), cspRuntimeOutputMaxBytes)
	logs := []string{}
	var result *CSPSidecarResult
	var dex2jar CSPDex2JarResult
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
				return CSPSidecarResult{}, dex2jar, logs, pid, err
			}
		case "log":
			if strings.TrimSpace(msg.Message) != "" {
				logs = append(logs, msg.Message)
			}
		case "result":
			parsed, parsedDex := parseCSPSidecarResult(req.Method, msg.Result, msg.DurationMs)
			result = &parsed
			dex2jar = parsedDex
			resultReceived = true
		}
		if resultReceived {
			break
		}
	}
	if err := reader.Err(); err != nil {
		return CSPSidecarResult{}, dex2jar, logs, pid, err
	}
	if resultReceived {
		_ = stdin.Close()
	}
	waitErr := waitForWorkerExit(cmd, resultReceived)
	if stderr.Len() > 0 {
		logs = append(logs, splitRuntimeLogs(stderr.String())...)
	}
	if result == nil && waitErr != nil {
		return CSPSidecarResult{}, dex2jar, logs, pid, waitErr
	}
	if result == nil {
		return CSPSidecarResult{}, dex2jar, logs, pid, fmt.Errorf("CSP runtime worker 无结果")
	}
	return *result, dex2jar, logs, pid, waitErr
}

func (m *CSPRuntimeManager) ensureLocalArtifact(ctx context.Context, req CSPRuntimeRequest, artifact CSPRuntimeArtifact) (CSPRuntimeArtifact, error) {
	path, err := filepath.Abs(strings.TrimSpace(artifact.Path))
	if err != nil {
		return artifact, err
	}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		artifact.Path = path
		return artifact, nil
	}
	refetched, err := m.artifacts.Fetch(ctx, req)
	if err != nil {
		return artifact, fmt.Errorf("artifact 本地文件缺失且重新下载失败: %w", err)
	}
	path, err = filepath.Abs(strings.TrimSpace(refetched.Path))
	if err != nil {
		return refetched, err
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return refetched, fmt.Errorf("artifact 本地文件不存在: %s", path)
	}
	refetched.Path = path
	return refetched, nil
}

func ensureCSPArtifactTrusted(artifact CSPRuntimeArtifact) error {
	trust := strings.ToLower(strings.TrimSpace(artifact.TrustStatus))
	if trust == "verified" || trust == "trusted" {
		return nil
	}
	return fmt.Errorf("csp_dex_jar artifact 未校验或未信任，管理员确认后才允许加载")
}

func (m *CSPRuntimeManager) providerWorkDir(req CSPRuntimeRequest) string {
	key := strings.TrimSpace(req.ProviderKey)
	if key == "" && req.ProviderID != nil {
		key = fmt.Sprintf("provider-%d", *req.ProviderID)
	}
	if key == "" {
		key = "diagnostic"
	}
	return filepath.Join(m.workDir, safeRuntimePathPart(key))
}

func (m *CSPRuntimeManager) handleBridgeHTTP(ctx context.Context, providerID *int64, id string, in jsHTTPBridgeReq) map[string]any {
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
		httpReq.Header.Set("User-Agent", "FYMS-CSP-Runtime/1.0")
	}
	resp, err := m.client.Do(httpReq)
	if err != nil {
		return map[string]any{"type": "http_response", "id": id, "ok": false, "error": err.Error()}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, cspRuntimeHTTPMaxBytes+1))
	if err != nil {
		return map[string]any{"type": "http_response", "id": id, "ok": false, "error": err.Error()}
	}
	if len(raw) > cspRuntimeHTTPMaxBytes {
		return map[string]any{"type": "http_response", "id": id, "ok": false, "error": "响应体超过 CSP runtime 上限"}
	}
	headers := map[string]string{}
	for key, values := range resp.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}
	out := map[string]any{
		"type":       "http_response",
		"id":         id,
		"ok":         true,
		"status":     resp.StatusCode,
		"headers":    headers,
		"bodyBase64": base64.StdEncoding.EncodeToString(raw),
		"bodyBytes":  len(raw),
		"durationMs": time.Since(start).Milliseconds(),
	}
	if text, charsetName, ok := decodeCSPHTTPText(raw, resp.Header.Get("Content-Type")); ok {
		out["bodyText"] = text
		out["charset"] = charsetName
	}
	return out
}

func (m *CSPRuntimeManager) wait(providerID int64) *rate.Limiter {
	m.mu.Lock()
	defer m.mu.Unlock()
	limiter := m.limiters[providerID]
	if limiter == nil {
		limiter = rate.NewLimiter(rate.Every(500*time.Millisecond), 2)
		m.limiters[providerID] = limiter
	}
	return limiter
}

func (m *CSPRuntimeManager) errorResponse(start time.Time, req CSPRuntimeRequest, err error, errorType string) *CSPRuntimeResponse {
	if errorType == "" {
		errorType = ErrorType(err)
	}
	return &CSPRuntimeResponse{
		OK:          false,
		RuntimeKind: CSPRuntimeKindJVM,
		BaseURL:     req.ConfigBaseURL,
		API:         req.API,
		Method:      req.Method,
		Result: CSPSidecarResult{
			OK:        false,
			Method:    req.Method,
			ClassName: cspClassName(req.API),
			Error:     err.Error(),
			ErrorType: errorType,
		},
		DurationMs: time.Since(start).Milliseconds(),
	}
}

func (m *CSPRuntimeManager) recordInvocation(ctx context.Context, req CSPRuntimeRequest, resp *CSPRuntimeResponse) {
	if m == nil || m.repo == nil || resp == nil {
		return
	}
	status := "ok"
	var errorType *string
	var errorMessage *string
	var workerPID *int32
	engineOK := resp.OK
	if !resp.OK {
		status = "error"
		if resp.Result.ErrorType != "" {
			v := resp.Result.ErrorType
			errorType = &v
		}
		if resp.Result.Error != "" {
			v := sanitizeRuntimeAuditError(resp.Result.Error)
			errorMessage = &v
		}
	}
	if resp.WorkerPID > 0 {
		v := int32(resp.WorkerPID)
		workerPID = &v
	}
	artifactIDs := []int64{}
	if resp.Artifact.ID > 0 {
		artifactIDs = append(artifactIDs, resp.Artifact.ID)
	}
	raw := jsonBytes(map[string]any{
		"provider_key_hash": URLHash(req.ProviderKey),
		"base_hash":         URLHash(req.ConfigBaseURL),
		"api":               req.API,
		"artifact_sha256":   resp.Artifact.SHA256,
		"dex2jar_ok":        resp.Dex2Jar.OK,
		"log_count":         len(resp.Logs),
	}, "{}")
	if _, err := m.repo.CreateRuntimeInvocation(ctx, repository.SourceRuntimeInvocationCreate{
		ProviderID:   req.ProviderID,
		RuntimeKind:  CSPRuntimeKindJVM,
		Method:       cspDefaultString(req.Method, "unknown"),
		Status:       status,
		ErrorType:    errorType,
		ErrorMessage: errorMessage,
		DurationMS:   resp.DurationMs,
		EngineOK:     &engineOK,
		WorkerPID:    workerPID,
		ArtifactIDs:  artifactIDs,
		URLHash:      stringPtrOrNil(URLHash(req.Spider)),
		Raw:          raw,
	}); err != nil {
		m.logger.WarnContext(ctx, "record CSP runtime invocation failed", "log_target", "provider", "error", err)
	}
}

func (m *CSPRuntimeManager) maybeRecordInvocation(ctx context.Context, req CSPRuntimeRequest, resp *CSPRuntimeResponse) {
	if req.SkipAudit {
		return
	}
	m.recordInvocation(context.WithoutCancel(ctx), req, resp)
}

func normalizeCSPRuntimeRequest(req CSPRuntimeRequest) CSPRuntimeRequest {
	if strings.TrimSpace(req.ConfigBaseURL) == "" {
		req.ConfigBaseURL = defaultDRPYBaseURL
	}
	if strings.TrimSpace(req.Spider) == "" {
		req.Spider = "./jar/fan.txt;md5;6c4ab3a9d232164c75534f9060506ee5"
	}
	if strings.TrimSpace(req.API) == "" {
		req.API = "csp_SixV"
	}
	if strings.TrimSpace(req.Ext) == "" && strings.EqualFold(strings.TrimSpace(req.API), "csp_SixV") {
		req.Ext = "https://www.xb6v.com/"
	}
	if strings.TrimSpace(req.Method) == "" {
		req.Method = CSPRuntimeMethodHome
	}
	req.Method = strings.ToLower(strings.TrimSpace(req.Method))
	if req.Args == nil {
		req.Args = map[string]any{}
	}
	return req
}

func cspClassName(api string) string {
	api = strings.TrimSpace(api)
	api = strings.TrimPrefix(api, "csp_")
	if api == "" {
		api = "SixV"
	}
	return "com.github.catvod.spider." + api
}

func parseCSPSidecarResult(fallbackMethod string, raw json.RawMessage, durationMs int64) (CSPSidecarResult, CSPDex2JarResult) {
	result := CSPSidecarResult{Method: fallbackMethod, DurationMs: durationMs}
	dex2jar := CSPDex2JarResult{Tool: "de.femtopedia.dex2jar:dex-translator"}
	if len(raw) == 0 {
		result.Error = "CSP runtime 空结果"
		result.ErrorType = "empty_result"
		dex2jar.Error = result.Error
		dex2jar.ErrorType = result.ErrorType
		return result, dex2jar
	}
	var wrapper struct {
		OK             bool            `json:"ok"`
		Method         string          `json:"method"`
		ClassName      string          `json:"className"`
		Data           json.RawMessage `json:"data"`
		Error          string          `json:"error"`
		ErrorType      string          `json:"errorType"`
		DurationMs     int64           `json:"durationMs"`
		AndroidStubs   []string        `json:"androidStubs"`
		CatVodStubs    []string        `json:"catVodStubs"`
		NetworkBridge  string          `json:"networkBridge"`
		UnsupportedAPI []string        `json:"unsupportedApi"`
		Dex2Jar        struct {
			OK         bool   `json:"ok"`
			Tool       string `json:"tool"`
			InputPath  string `json:"inputPath"`
			OutputPath string `json:"outputPath"`
			DurationMs int64  `json:"durationMs"`
			Error      string `json:"error"`
			ErrorType  string `json:"errorType"`
		} `json:"dex2jar"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		result.Error = "解析 CSP runtime 输出失败: " + err.Error()
		result.ErrorType = "decode_failed"
		result.Data = raw
		dex2jar.Error = result.Error
		dex2jar.ErrorType = result.ErrorType
		return result, dex2jar
	}
	result.OK = wrapper.OK
	result.Method = cspDefaultString(wrapper.Method, fallbackMethod)
	result.ClassName = wrapper.ClassName
	result.Data = wrapper.Data
	result.Error = wrapper.Error
	result.ErrorType = wrapper.ErrorType
	result.DurationMs = wrapper.DurationMs
	result.AndroidStubs = wrapper.AndroidStubs
	result.CatVodStubs = wrapper.CatVodStubs
	result.NetworkBridge = wrapper.NetworkBridge
	result.UnsupportedAPI = wrapper.UnsupportedAPI
	dex2jar = CSPDex2JarResult{
		OK:         wrapper.Dex2Jar.OK,
		Tool:       cspDefaultString(wrapper.Dex2Jar.Tool, "de.femtopedia.dex2jar:dex-translator"),
		InputPath:  wrapper.Dex2Jar.InputPath,
		OutputPath: wrapper.Dex2Jar.OutputPath,
		DurationMs: wrapper.Dex2Jar.DurationMs,
		Error:      wrapper.Dex2Jar.Error,
		ErrorType:  wrapper.Dex2Jar.ErrorType,
	}
	if result.DurationMs == 0 {
		result.DurationMs = durationMs
	}
	return result, dex2jar
}

func resolveCSPSidecarJar() (string, error) {
	candidates := []string{
		filepath.Join("runtime", "csp-sidecar", "fyms-csp-sidecar-all.jar"),
		filepath.Join("runtime", "csp-sidecar", "build", "libs", "fyms-csp-sidecar-all.jar"),
		filepath.Join("..", "runtime", "csp-sidecar", "fyms-csp-sidecar-all.jar"),
		filepath.Join("..", "runtime", "csp-sidecar", "build", "libs", "fyms-csp-sidecar-all.jar"),
		filepath.Join("/app", "runtime", "csp-sidecar", "fyms-csp-sidecar-all.jar"),
		filepath.Join("/app", "runtime", "csp-sidecar", "build", "libs", "fyms-csp-sidecar-all.jar"),
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
	return "", fmt.Errorf("未找到 CSP sidecar fat jar，请先运行 runtime/csp-sidecar/build.ps1")
}

func splitRuntimeLogs(parts ...string) []string {
	logs := []string{}
	for _, part := range parts {
		for _, line := range strings.Split(strings.TrimSpace(part), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				logs = append(logs, line)
			}
		}
	}
	return logs
}

func cspDefaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

var unsafeRuntimePathPart = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safeRuntimePathPart(value string) string {
	value = strings.Trim(unsafeRuntimePathPart.ReplaceAllString(value, "_"), "._-")
	if value == "" {
		return "default"
	}
	return value
}
