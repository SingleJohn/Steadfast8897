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
	"runtime"
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
	dex2jar   string
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
	return &CSPRuntimeManager{
		repo:      repo,
		client:    client,
		artifacts: NewCSPArtifactManager(repo, client, dataDir),
		dataDir:   dataDir,
		workDir:   filepath.Join(dataDir, "source-runtime", "csp", "work"),
		sem:       make(chan struct{}, cspRuntimeMaxConcurrent),
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
	dex2jar, err := resolveDex2JarTool()
	if err != nil {
		return err
	}
	sidecar, err := resolveCSPSidecarClasses(ctx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.workDir, 0755); err != nil {
		return err
	}
	m.javaPath = javaPath
	m.dex2jar = dex2jar
	m.sidecar = sidecar
	m.logger.InfoContext(ctx, "CSP runtime PoC ready", "log_target", "provider", "java", javaPath, "dex2jar", dex2jar, "sidecar", sidecar)
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
	select {
	case m.sem <- struct{}{}:
		defer func() { <-m.sem }()
	case <-runCtx.Done():
		err := fmt.Errorf("CSP runtime 等待 worker 超时: %w", runCtx.Err())
		resp := m.errorResponse(start, req, err, "timeout")
		m.recordInvocation(context.WithoutCancel(ctx), req, resp)
		return nil, err
	}
	if strings.TrimSpace(m.javaPath) == "" || strings.TrimSpace(m.dex2jar) == "" || strings.TrimSpace(m.sidecar) == "" {
		if err := m.Start(runCtx); err != nil {
			resp := m.errorResponse(start, req, err, "runtime_unavailable")
			m.recordInvocation(context.WithoutCancel(ctx), req, resp)
			return resp, nil
		}
	}
	artifact, err := m.artifacts.Fetch(runCtx, req)
	if err != nil {
		resp := m.errorResponse(start, req, err, ErrorType(err))
		m.recordInvocation(context.WithoutCancel(ctx), req, resp)
		return nil, err
	}
	converted, dexLogs := m.convertDexJar(runCtx, artifact)
	logs := append([]string{}, dexLogs...)
	resp := &CSPRuntimeResponse{
		RuntimeKind: CSPRuntimeKindJVMPoC,
		BaseURL:     req.ConfigBaseURL,
		API:         req.API,
		Method:      req.Method,
		Artifact:    artifact,
		Dex2Jar:     converted,
		DurationMs:  time.Since(start).Milliseconds(),
	}
	if !converted.OK {
		resp.OK = false
		resp.Result = CSPSidecarResult{OK: false, Method: req.Method, ClassName: cspClassName(req.API), Error: converted.Error, ErrorType: converted.ErrorType}
		resp.Logs = logs
		m.recordInvocation(context.WithoutCancel(ctx), req, resp)
		return resp, nil
	}
	result, sidecarLogs, pid, err := m.runSidecar(runCtx, req, converted.OutputPath)
	logs = append(logs, sidecarLogs...)
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
	resp.OK = result.OK
	resp.Result = result
	resp.Data = result.Data
	resp.Logs = logs
	resp.WorkerPID = pid
	resp.DurationMs = time.Since(start).Milliseconds()
	m.recordInvocation(context.WithoutCancel(ctx), req, resp)
	return resp, nil
}

func (m *CSPRuntimeManager) convertDexJar(ctx context.Context, artifact CSPRuntimeArtifact) (CSPDex2JarResult, []string) {
	start := time.Now()
	output := filepath.Join(m.workDir, artifact.SHA256[:16]+"-classes.jar")
	result := CSPDex2JarResult{
		OK:         false,
		Tool:       m.dex2jar,
		InputPath:  artifact.Path,
		OutputPath: output,
	}
	if _, err := os.Stat(output); err == nil {
		result.OK = true
		result.DurationMs = time.Since(start).Milliseconds()
		return result, []string{"dex2jar 命中已转换缓存"}
	}
	if err := os.MkdirAll(m.workDir, 0755); err != nil {
		result.Error = err.Error()
		result.ErrorType = ErrorType(err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}
	args := dex2jarArgs(m.dex2jar, artifact.Path, output)
	cmd := exec.CommandContext(ctx, m.dex2jar, args...)
	cmd.Dir = m.workDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitWriter{w: &stdout, limit: cspRuntimeOutputMaxBytes}
	cmd.Stderr = &limitWriter{w: &stderr, limit: cspRuntimeOutputMaxBytes}
	err := cmd.Run()
	logs := splitRuntimeLogs(stdout.String(), stderr.String())
	result.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		result.Error = "dex2jar 转换失败: " + err.Error()
		result.ErrorType = ErrorType(err)
		return result, logs
	}
	if _, err := os.Stat(output); err != nil {
		result.Error = "dex2jar 未生成输出 jar: " + err.Error()
		result.ErrorType = "missing_output"
		return result, logs
	}
	result.OK = true
	return result, logs
}

func (m *CSPRuntimeManager) runSidecar(ctx context.Context, req CSPRuntimeRequest, classJar string) (CSPSidecarResult, []string, int, error) {
	classpath := m.sidecar + string(os.PathListSeparator) + classJar
	payload := map[string]any{
		"type":      "run",
		"className": cspClassName(req.API),
		"method":    req.Method,
		"args":      req.Args,
		"baseUrl":   req.ConfigBaseURL,
	}
	cmd := exec.CommandContext(ctx, m.javaPath, "-cp", classpath, "fyms.csp.CSPProbe")
	cmd.Dir = m.workDir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return CSPSidecarResult{}, nil, 0, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return CSPSidecarResult{}, nil, 0, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &limitWriter{w: &stderr, limit: cspRuntimeOutputMaxBytes}
	if err := cmd.Start(); err != nil {
		return CSPSidecarResult{}, nil, 0, err
	}
	pid := cmd.Process.Pid
	writer := &jsonLineWriter{w: stdin}
	if err := writer.Write(payload); err != nil {
		return CSPSidecarResult{}, nil, pid, err
	}
	reader := bufio.NewScanner(stdout)
	reader.Buffer(make([]byte, 64*1024), cspRuntimeOutputMaxBytes)
	logs := []string{}
	var result *CSPSidecarResult
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
				return CSPSidecarResult{}, logs, pid, err
			}
		case "log":
			if strings.TrimSpace(msg.Message) != "" {
				logs = append(logs, msg.Message)
			}
		case "result":
			parsed := parseCSPSidecarResult(req.Method, msg.Result, msg.DurationMs)
			result = &parsed
			resultReceived = true
		}
		if resultReceived {
			break
		}
	}
	if err := reader.Err(); err != nil {
		return CSPSidecarResult{}, logs, pid, err
	}
	if resultReceived {
		_ = stdin.Close()
	}
	waitErr := waitForWorkerExit(cmd, resultReceived)
	if stderr.Len() > 0 {
		logs = append(logs, splitRuntimeLogs(stderr.String())...)
	}
	if result == nil && waitErr != nil {
		return CSPSidecarResult{}, logs, pid, waitErr
	}
	if result == nil {
		return CSPSidecarResult{}, logs, pid, fmt.Errorf("CSP runtime worker 无结果")
	}
	return *result, logs, pid, waitErr
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
		RuntimeKind: CSPRuntimeKindJVMPoC,
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
		RuntimeKind:  CSPRuntimeKindJVMPoC,
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

func parseCSPSidecarResult(fallbackMethod string, raw json.RawMessage, durationMs int64) CSPSidecarResult {
	result := CSPSidecarResult{Method: fallbackMethod, DurationMs: durationMs}
	if len(raw) == 0 {
		result.Error = "CSP runtime 空结果"
		result.ErrorType = "empty_result"
		return result
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
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		result.Error = "解析 CSP runtime 输出失败: " + err.Error()
		result.ErrorType = "decode_failed"
		result.Data = raw
		return result
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
	if result.DurationMs == 0 {
		result.DurationMs = durationMs
	}
	return result
}

func resolveDex2JarTool() (string, error) {
	candidates := []string{
		"d2j-dex2jar",
		"d2j-dex2jar.sh",
		"d2j-dex2jar.bat",
		filepath.Join("tools", "dex2jar", "d2j-dex2jar.bat"),
		filepath.Join("tools", "dex2jar", "d2j-dex2jar.sh"),
		filepath.Join("runtime", "csp-sidecar", "tools", "d2j-dex2jar.bat"),
		filepath.Join("runtime", "csp-sidecar", "tools", "d2j-dex2jar.sh"),
	}
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				return candidate, nil
			}
			return abs, nil
		}
	}
	return "", fmt.Errorf("未找到 dex2jar 工具 d2j-dex2jar")
}

func dex2jarArgs(tool, input, output string) []string {
	args := []string{"--force", "--output", output, input}
	if runtime.GOOS == "windows" && strings.HasSuffix(strings.ToLower(tool), ".bat") {
		args = []string{"--force", "--output", output, input}
	}
	return args
}

func resolveCSPSidecarClasses(ctx context.Context) (string, error) {
	candidates := []string{
		filepath.Join("runtime", "csp-sidecar", "classes"),
		filepath.Join("..", "runtime", "csp-sidecar", "classes"),
		filepath.Join("/app", "runtime", "csp-sidecar", "classes"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				return candidate, nil
			}
			return abs, nil
		}
	}
	if compiled, err := compileCSPSidecarClasses(ctx); err == nil {
		return compiled, nil
	}
	return "", fmt.Errorf("未找到 CSP sidecar classes runtime/csp-sidecar/classes")
}

func compileCSPSidecarClasses(ctx context.Context) (string, error) {
	srcDir := filepath.Join("runtime", "csp-sidecar", "src")
	if info, err := os.Stat(srcDir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("未找到 CSP sidecar 源码目录")
	}
	javac, err := exec.LookPath("javac")
	if err != nil {
		return "", fmt.Errorf("未找到 javac，无法编译 CSP sidecar classes")
	}
	outDir := filepath.Join("runtime", "csp-sidecar", "classes")
	sources := []string{}
	if err := filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".java") {
			sources = append(sources, path)
		}
		return nil
	}); err != nil {
		return "", err
	}
	if len(sources) == 0 {
		return "", fmt.Errorf("CSP sidecar 源码为空")
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", err
	}
	args := append([]string{"-encoding", "UTF-8", "-d", outDir}, sources...)
	cmd := exec.CommandContext(ctx, javac, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &limitWriter{w: &stderr, limit: cspRuntimeOutputMaxBytes}
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("编译 CSP sidecar classes 失败: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	abs, err := filepath.Abs(outDir)
	if err != nil {
		return outDir, nil
	}
	return abs, nil
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
