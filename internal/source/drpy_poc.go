package source

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultDRPYBaseURL = "https://tvboxconfig.singlelovely.cn/gao/"
	defaultDRPYEngine  = "./lib/drpy2.min.js"
	defaultDRPYRule    = "./js/360影视.js"

	drpyArtifactMaxBytes = 2 << 20
	drpyOutputMaxBytes   = 2 << 20
	drpyDefaultTimeout   = 20 * time.Second
)

type DRPYPoCRequest struct {
	ConfigBaseURL string         `json:"configBaseUrl"`
	Engine        string         `json:"engine"`
	Rule          string         `json:"rule"`
	Method        string         `json:"method"`
	Args          map[string]any `json:"args"`
	TimeoutMs     int            `json:"timeoutMs"`
}

type DRPYPoCArtifact struct {
	Kind   string `json:"kind"`
	URL    string `json:"url"`
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	MD5    string `json:"md5"`
	SHA256 string `json:"sha256"`
	Trust  string `json:"trust"`
}

type DRPYPoCMethodResult struct {
	OK         bool            `json:"ok"`
	Method     string          `json:"method"`
	EngineOK   bool            `json:"engineOk,omitempty"`
	EngineNote string          `json:"engineNote,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
	Error      string          `json:"error,omitempty"`
	DurationMs int64           `json:"durationMs"`
}

type DRPYPoCReport struct {
	Engine                  string   `json:"engine"`
	RuntimeShape            string   `json:"runtimeShape"`
	CGOImpact               string   `json:"cgoImpact"`
	HostFunctions           []string `json:"hostFunctions"`
	KnownGaps               []string `json:"knownGaps"`
	MethodSummary           string   `json:"methodSummary"`
	Recommendation          string   `json:"recommendation"`
	RecommendationRationale []string `json:"recommendationRationale"`
}

type DRPYPoCResponse struct {
	OK         bool                  `json:"ok"`
	Engine     string                `json:"engine"`
	BaseURL    string                `json:"baseUrl"`
	Artifacts  []DRPYPoCArtifact     `json:"artifacts"`
	Results    []DRPYPoCMethodResult `json:"results"`
	Logs       []string              `json:"logs"`
	DurationMs int64                 `json:"durationMs"`
	Report     DRPYPoCReport         `json:"report"`
}

type DRPYPoCRunner struct {
	client      *http.Client
	artifactDir string
}

func NewDRPYPoCRunner(client *http.Client, dataDir string) *DRPYPoCRunner {
	if client == nil {
		client = http.DefaultClient
	}
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "data"
	}
	return &DRPYPoCRunner{
		client:      client,
		artifactDir: filepath.Join(dataDir, "source-runtime", "js"),
	}
}

func (r *DRPYPoCRunner) Run(ctx context.Context, req DRPYPoCRequest) (*DRPYPoCResponse, error) {
	start := time.Now()
	req = normalizeDRPYPoCRequest(req)
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 || timeout > drpyDefaultTimeout {
		timeout = drpyDefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	baseURL, err := url.Parse(req.ConfigBaseURL)
	if err != nil {
		return nil, fmt.Errorf("解析 configBaseUrl 失败: %w", err)
	}
	engineURL, err := resolveDRPYURL(baseURL, req.Engine)
	if err != nil {
		return nil, err
	}
	ruleURL, err := resolveDRPYURL(baseURL, req.Rule)
	if err != nil {
		return nil, err
	}

	engineArtifact, engineCode, err := r.fetchArtifact(runCtx, "engine", engineURL)
	if err != nil {
		return nil, err
	}
	ruleArtifact, ruleCode, err := r.fetchArtifact(runCtx, "rule", ruleURL)
	if err != nil {
		return nil, err
	}

	nodePath, err := exec.LookPath("node")
	if err != nil {
		return &DRPYPoCResponse{
			OK:        false,
			Engine:    "node-sidecar",
			BaseURL:   req.ConfigBaseURL,
			Artifacts: []DRPYPoCArtifact{engineArtifact, ruleArtifact},
			Results: []DRPYPoCMethodResult{{
				OK:     false,
				Method: req.Method,
				Error:  "未找到 node 可执行文件，无法运行 sidecar PoC",
			}},
			DurationMs: time.Since(start).Milliseconds(),
			Report:     drpyPoCReport("node-sidecar", []string{"node runtime missing"}),
		}, nil
	}

	payload := map[string]any{
		"engineCode": string(engineCode),
		"ruleCode":   string(ruleCode),
		"method":     req.Method,
		"args":       req.Args,
		"baseUrl":    req.ConfigBaseURL,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	scriptPath, err := writeDRPYSandboxScript(r.artifactDir)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(runCtx, nodePath, scriptPath)
	cmd.Stdin = bytes.NewReader(payloadJSON)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitWriter{w: &stdout, limit: drpyOutputMaxBytes}
	cmd.Stderr = &limitWriter{w: &stderr, limit: drpyOutputMaxBytes}
	runStart := time.Now()
	runErr := cmd.Run()
	durationMs := time.Since(runStart).Milliseconds()
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		runErr = fmt.Errorf("DRPY PoC 调用超时")
	}

	result := parseDRPYSandboxOutput(req.Method, stdout.Bytes(), stderr.String(), durationMs, runErr)
	resp := &DRPYPoCResponse{
		OK:         result.OK,
		Engine:     "node-sidecar",
		BaseURL:    req.ConfigBaseURL,
		Artifacts:  []DRPYPoCArtifact{engineArtifact, ruleArtifact},
		Results:    []DRPYPoCMethodResult{result},
		DurationMs: time.Since(start).Milliseconds(),
		Report:     drpyPoCReport("node-sidecar", nil),
	}
	if stderr.Len() > 0 {
		resp.Logs = append(resp.Logs, strings.Split(strings.TrimSpace(stderr.String()), "\n")...)
	}
	if logs := extractDRPYLogs(result.Data); len(logs) > 0 {
		resp.Logs = append(resp.Logs, logs...)
	}
	return resp, nil
}

func normalizeDRPYPoCRequest(req DRPYPoCRequest) DRPYPoCRequest {
	if strings.TrimSpace(req.ConfigBaseURL) == "" {
		req.ConfigBaseURL = defaultDRPYBaseURL
	}
	if strings.TrimSpace(req.Engine) == "" {
		req.Engine = defaultDRPYEngine
	}
	if strings.TrimSpace(req.Rule) == "" {
		req.Rule = defaultDRPYRule
	}
	if strings.TrimSpace(req.Method) == "" {
		req.Method = "all"
	}
	req.Method = strings.ToLower(strings.TrimSpace(req.Method))
	if req.Args == nil {
		req.Args = map[string]any{}
	}
	return req
}

func resolveDRPYURL(base *url.URL, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("artifact 路径为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("解析 artifact URL 失败: %w", err)
	}
	if !u.IsAbs() {
		u = base.ResolveReference(u)
	}
	return u.String(), nil
}

func (r *DRPYPoCRunner) fetchArtifact(ctx context.Context, kind, rawURL string) (DRPYPoCArtifact, []byte, error) {
	if err := ValidateOutboundURL(ctx, rawURL); err != nil {
		return DRPYPoCArtifact{}, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return DRPYPoCArtifact{}, nil, err
	}
	req.Header.Set("User-Agent", "FYMS-DRPY-PoC/1.0")
	resp, err := r.client.Do(req)
	if err != nil {
		return DRPYPoCArtifact{}, nil, fmt.Errorf("下载 %s artifact 失败: %w", kind, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DRPYPoCArtifact{}, nil, fmt.Errorf("下载 %s artifact 返回异常状态: %d", kind, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, drpyArtifactMaxBytes+1))
	if err != nil {
		return DRPYPoCArtifact{}, nil, err
	}
	if len(body) > drpyArtifactMaxBytes {
		return DRPYPoCArtifact{}, nil, fmt.Errorf("%s artifact 超过大小上限", kind)
	}
	artifact, err := r.saveArtifact(kind, rawURL, body)
	if err != nil {
		return DRPYPoCArtifact{}, nil, err
	}
	return artifact, body, nil
}

func (r *DRPYPoCRunner) saveArtifact(kind, rawURL string, body []byte) (DRPYPoCArtifact, error) {
	if err := os.MkdirAll(r.artifactDir, 0755); err != nil {
		return DRPYPoCArtifact{}, err
	}
	sha := sha256.Sum256(body)
	md5sum := md5.Sum(body)
	name := sanitizeArtifactName(kind + "-" + filepath.Base(urlPath(rawURL)))
	if name == kind+"-" || name == kind+"-." {
		name = kind + ".js"
	}
	path := filepath.Join(r.artifactDir, hex.EncodeToString(sha[:8])+"-"+name)
	if err := os.WriteFile(path, body, 0644); err != nil {
		return DRPYPoCArtifact{}, err
	}
	return DRPYPoCArtifact{
		Kind:   kind,
		URL:    rawURL,
		Path:   path,
		Bytes:  int64(len(body)),
		MD5:    hex.EncodeToString(md5sum[:]),
		SHA256: hex.EncodeToString(sha[:]),
		Trust:  "unverified",
	}, nil
}

func urlPath(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Path
}

func sanitizeArtifactName(name string) string {
	name = strings.TrimSpace(name)
	replacer := strings.NewReplacer("\\", "_", "/", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	name = replacer.Replace(name)
	if len([]rune(name)) > 80 {
		rs := []rune(name)
		name = string(rs[len(rs)-80:])
	}
	return name
}

func parseDRPYSandboxOutput(method string, stdout []byte, stderr string, durationMs int64, runErr error) DRPYPoCMethodResult {
	result := DRPYPoCMethodResult{Method: method, DurationMs: durationMs}
	if runErr != nil {
		result.Error = strings.TrimSpace(runErr.Error() + "\n" + stderr)
		return result
	}
	out := bytes.TrimSpace(stdout)
	if len(out) == 0 {
		result.Error = "DRPY PoC 无输出"
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
		Data    json.RawMessage `json:"data"`
		Error   string          `json:"error"`
		Results json.RawMessage `json:"results"`
		Logs    []string        `json:"logs"`
	}
	if err := json.Unmarshal(out, &wrapper); err != nil {
		result.Error = "解析 DRPY PoC 输出失败: " + err.Error()
		result.Data = json.RawMessage(out)
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
	return result
}

func extractDRPYLogs(raw json.RawMessage) []string {
	var obj map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	logs, _ := obj["logs"].([]any)
	out := make([]string, 0, len(logs))
	for _, item := range logs {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func drpyPoCReport(engine string, extraGaps []string) DRPYPoCReport {
	gaps := []string{
		"in-process cgo QuickJS 与 Dockerfile/scripts 的 CGO_ENABLED=0 Linux 静态构建目标冲突",
		"PoC 入口未落 source_runtime_artifacts/source_runtime_invocations 表，T17 需要正式化",
		"Node sidecar 仍需进程池、并发隔离、依赖打包和容器运行时约束",
		"360影视可用的最小桥不等于 DRPY 宿主函数全集，复杂规则还缺 OCR/RSA/完整 cheerio/html parser/proxy/sniffer",
	}
	gaps = append(gaps, extraGaps...)
	return DRPYPoCReport{
		Engine:       engine,
		RuntimeShape: "临时 Node sidecar 进程；每次调用独立进程，禁止文件访问能力注入，仅通过受控 HTTP bridge 出站",
		CGOImpact:    "项目 Dockerfile 与 scripts/build-linux-amd64.ps1 均使用 CGO_ENABLED=0 GOOS=linux；直接引入 cgo QuickJS 会破坏当前静态构建/交叉编译链路。",
		HostFunctions: []string{
			"req/request/fetch: 通过 Node fetch 发起，PoC 层由 Go 先对 artifact 下载做 ValidateOutboundURL；正式形态需把每次运行时出站也回调 Go 校验",
			"jsonpath/json expression: 360影视 home/search/category 使用轻量 JSON path 读取",
			"base64/md5: Node Buffer/crypto 可满足 PoC",
			"local: 当前 PoC 使用进程内 Map，调用结束即丢弃",
			"console/log/print: 收集到响应 logs",
			"urljoin/buildUrl/urlDeal: PoC 内置最小实现",
		},
		KnownGaps:      gaps,
		MethodSummary:  "通过 /SourceRuntime/TestJS method=all 可一次返回 home/search/detail/play 四段结果；engineOk/engineNote 表示真实 drpy2.min.js 在 sidecar 内的加载/初始化状态，results 表示 360 规则最小桥的四方法结果。",
		Recommendation: "建议 T17 走独立 sidecar（Node 优先，QuickJS sidecar 可作为瘦身方案评估），暂不走 in-process cgo QuickJS。",
		RecommendationRationale: []string{
			"保持 FYMS Core 的 CGO_ENABLED=0 静态构建不变。",
			"第三方 JS 卡死或崩溃时只杀 sidecar，不拖垮 Core。",
			"DRPY engine 是 ES module 且含远程/asset import，Node 的兼容成本低于 Go 内嵌 runtime。",
			"sidecar 更容易做进程级网络、CPU、内存与超时隔离。",
			"代价是部署多一个运行时，后续需要明确容器内 Node/QuickJS 打包策略。",
		},
	}
}

func writeDRPYSandboxScript(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "drpy-poc-runner.mjs")
	return path, os.WriteFile(path, []byte(drpySandboxScript), 0644)
}

type limitWriter struct {
	w     io.Writer
	limit int
	n     int
}

func (w *limitWriter) Write(p []byte) (int, error) {
	originalLen := len(p)
	if w.limit <= 0 || w.n >= w.limit {
		return originalLen, nil
	}
	remain := w.limit - w.n
	if len(p) > remain {
		p = p[:remain]
	}
	n, err := w.w.Write(p)
	w.n += n
	if err != nil {
		return n, err
	}
	return originalLen, nil
}

const drpySandboxScript = `
import vm from 'node:vm';
import crypto from 'node:crypto';

const input = JSON.parse(await readStdin());
const logs = [];
const started = Date.now();
const engineProbe = probeEngine(input.engineCode || '');

function logLine(...args) {
  logs.push(args.map(v => typeof v === 'string' ? v : JSON.stringify(v)).join(' '));
}

function makeURL(base, path) {
  try { return new URL(path || '', base || 'https://tvboxconfig.singlelovely.cn/gao/').toString(); }
  catch { return String(path || ''); }
}

function buildUrl(raw, obj = {}) {
  const u = new URL(raw);
  for (const [k, v] of Object.entries(obj || {})) u.searchParams.set(k, String(v));
  return u.toString();
}

function getPath(obj, path) {
  if (!path) return obj;
  const parts = String(path).replace(/^\$?\./, '').split('.').filter(Boolean);
  let cur = obj;
  for (const part of parts) {
    if (cur == null) return undefined;
    cur = cur[part];
  }
  return cur;
}

function firstValue(obj, expr) {
  for (const key of String(expr || '').split('||')) {
    const trimmed = key.trim();
    if (trimmed.includes('+')) {
      const values = trimmed.split('+').map(part => getPath(obj, part.trim())).filter(v => v !== undefined && v !== null && String(v) !== '');
      if (values.length) return values.join('$');
    }
    const v = getPath(obj, trimmed);
    if (v !== undefined && v !== null && String(v) !== '') return v;
  }
  return '';
}

function mapJSONList(root, ruleExpr) {
  const parts = String(ruleExpr || '').replace(/^json:/, '').split(';');
  const list = getPath(root, parts[0]) || [];
  if (!Array.isArray(list)) return [];
  return list.map(item => ({
    vod_id: [firstValue(item, parts[4]), firstValue(item, parts[5])].filter(Boolean).join('$') || String(firstValue(item, parts[4]) || ''),
    vod_name: String(firstValue(item, parts[1]) || ''),
    vod_pic: String(firstValue(item, parts[2]) || ''),
    vod_remarks: String(firstValue(item, parts[3]) || ''),
    type_name: String(firstValue(item, parts[3]) || ''),
    vod_content: String(firstValue(item, parts[5]) || ''),
    raw: item,
  }));
}

async function req(raw, options = {}) {
  const headers = Object.assign({'User-Agent': MOBILE_UA}, options.headers || {});
  const resp = await fetch(raw, { method: options.method || 'GET', headers, redirect: 'follow' });
  if (!resp.ok) throw new Error('HTTP ' + resp.status + ' ' + raw);
  return await resp.text();
}

async function loadJSON(raw, options = {}) {
  return JSON.parse(await req(raw, options));
}

const MOBILE_UA = 'Mozilla/5.0 (Linux; Android 11; Pixel 5) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/90.0.4430.91 Mobile Safari/537.36';
const PC_UA = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/95.0.4638.54 Safari/537.36';
const local = new Map();

const context = vm.createContext({
  console: { log: (...args) => logLine(...args), error: (...args) => logLine(...args), warn: (...args) => logLine(...args) },
  print: (...args) => logLine(...args),
  log: (...args) => logLine(...args),
  Buffer,
  URL,
  URLSearchParams,
  crypto,
  MOBILE_UA,
  PC_UA,
  UA: 'Mozilla/5.0',
  local,
  request: req,
  fetch: req,
  req,
  buildUrl,
  urljoin: makeURL,
  urljoin2: makeURL,
  base64Encode: text => Buffer.from(String(text), 'utf8').toString('base64'),
  base64Decode: text => Buffer.from(String(text), 'base64').toString('utf8'),
  md5: text => crypto.createHash('md5').update(String(text)).digest('hex'),
  setItem: (k, v) => local.set(k, v),
  getItem: (k, fallback = '') => local.has(k) ? local.get(k) : fallback,
  clearItem: k => local.delete(k),
  pdfh: () => '',
  pdfa: () => [],
  pd: () => '',
  cheerio: {
    jp: (path, obj) => getPath(obj, path),
    jinja2: (tpl) => tpl,
  },
});

const ruleCode = input.ruleCode.replace(/\bvar\s+rule\s*=/, 'globalThis.rule =');
vm.runInContext(ruleCode, context, { timeout: 5000 });
const rule = context.rule || {};

async function home() {
  const classes = [];
  const names = String(rule.class_name || '').split('&');
  const ids = String(rule.class_url || '').split('&');
  for (let i = 0; i < Math.min(names.length, ids.length); i++) {
    if (names[i] && ids[i]) classes.push({ type_id: ids[i], type_name: names[i] });
  }
  return { class: classes };
}

async function category(args) {
  const tid = String(args.tid || args.id || '1');
  const pg = String(args.pg || args.page || '1');
  const url = String(rule.url || '').replaceAll('fyclass', tid).replaceAll('fypage', pg);
  const json = await loadJSON(url, { headers: rule.headers || {} });
  return { list: mapJSONList(json, rule['一级']), page: Number(pg) || 1 };
}

async function search(args) {
  const keyword = String(args.keyword || args.wd || '三体');
  const pg = String(args.pg || args.page || '1');
  const url = String(rule.searchUrl || '').replaceAll('**', encodeURIComponent(keyword)).replaceAll('fypage', pg);
  const json = await loadJSON(url, { headers: rule.headers || {} });
  return { list: mapJSONList(json, rule['搜索']), page: Number(pg) || 1 };
}

async function detail(args) {
  let id = String(args.id || '');
  if (!id) {
    const sr = await search({ keyword: args.keyword || '三体', page: 1 });
    id = sr.list?.[0]?.vod_id || '';
  }
  const [fyclass, fyid] = id.includes('$') ? id.split('$') : ['', id];
  const detailURL = String(rule.detailUrl || '').replaceAll('fyclass', fyclass).replaceAll('fyid', fyid);
  const data = await loadJSON(detailURL, { headers: rule.headers || {} });
  const item = data.data || {};
  const vod = {
    vod_id: id,
    vod_name: item.title || item.name || '',
    vod_pic: item.cdncover || item.cover || '',
    type_name: Array.isArray(item.moviecategory) ? item.moviecategory.join(',') : '',
    vod_area: Array.isArray(item.area) ? item.area.join(',') : '',
    vod_director: Array.isArray(item.director) ? item.director.join(',') : '',
    vod_actor: Array.isArray(item.actor) ? item.actor.join(',') : '',
    vod_content: item.description || '',
    vod_play_from: '',
    vod_play_url: '',
    raw: item,
  };
  const play = {};
  for (const site of item.playlink_sites || []) {
    const values = [];
    if (item.allupinfo && item.allupinfo[site]) {
      const total = Number(item.allupinfo[site]) || 0;
      const end = Math.min(total, 20);
      if (end > 0) {
        const epURL = buildUrl(detailURL, { start: 1, end, site });
        const epJSON = await loadJSON(epURL, { headers: rule.headers || {} });
        const rows = epJSON.data?.allepidetail?.[site] || epJSON.data?.defaultepisode || [];
        for (const row of rows) values.push(String(row.playlink_num || row.period || row.name || '') + '$' + String(row.url || row.default_url || ''));
      }
    } else if (item.playlinksdetail?.[site]) {
      const row = item.playlinksdetail[site];
      values.push(String(row.sort || '') + '$' + String(row.default_url || row.url || ''));
    }
    if (values.length) play[site] = values.join('#');
  }
  vod.vod_play_from = Object.keys(play).join('$$$');
  vod.vod_play_url = Object.values(play).join('$$$');
  return { list: [vod] };
}

async function play(args) {
  let id = String(args.url || args.id || '');
  if (!id) {
    const d = await detail(args);
    const first = d.list?.[0]?.vod_play_url?.split('$$$')?.[0]?.split('#')?.[0] || '';
    id = first.includes('$') ? first.split('$').slice(1).join('$') : first;
  }
  id = decodeURIComponent(id).split('?')[0];
  return { parse: /\.(m3u8|mp4|m4a)$/i.test(id) ? 0 : 1, jx: /\.(m3u8|mp4|m4a)$/i.test(id) ? 0 : 1, url: id };
}

async function runOne(method, args) {
  const t = Date.now();
  try {
    const data = await ({home, category, search, detail, play}[method])(args || {});
    return { ok: true, method, durationMs: Date.now() - t, data };
  } catch (e) {
    return { ok: false, method, durationMs: Date.now() - t, error: e?.message || String(e) };
  }
}

const method = String(input.method || 'all').toLowerCase();
const methods = method === 'all' ? ['home', 'search', 'detail', 'play'] : [method];
const results = [];
for (const m of methods) {
  if (!['home', 'category', 'search', 'detail', 'play'].includes(m)) {
    results.push({ ok: false, method: m, error: 'unsupported method' });
  } else {
    results.push(await runOne(m, input.args || {}));
  }
}

const ok = results.every(r => r.ok);
process.stdout.write(JSON.stringify({ ok, method, engine: engineProbe, results, logs, durationMs: Date.now() - started }));

function probeEngine(code) {
  if (!code) return { ok: false, stage: 'empty', error: 'engine artifact empty' };
  const imports = [...code.matchAll(/import\s+(?:[^'"]+from\s*)?["']([^"']+)["']/g)].map(m => m[1]);
  const needsHost = [];
  for (const name of ['req', 'request', 'pdfh', 'pdfa', 'pd', 'CryptoJS', 'cheerio', 'getItem', 'setItem']) {
    if (new RegExp('\\b' + name + '\\b').test(code)) needsHost.push(name);
  }
  if (imports.length > 0) {
    return {
      ok: false,
      stage: 'esm-import',
      error: 'engine contains unresolved imports: ' + imports.join(', '),
      imports,
      needsHost,
    };
  }
  try {
    const stripped = code.replace(/export\s+default\s+/, 'globalThis.__drpyDefault = ');
    vm.runInContext(stripped, context, { timeout: 5000 });
    return { ok: !!context.__drpyDefault, stage: 'vm-load', needsHost };
  } catch (e) {
    return { ok: false, stage: 'vm-load', error: e?.message || String(e), needsHost };
  }
}

function readStdin() {
  return new Promise((resolve, reject) => {
    let data = '';
    process.stdin.setEncoding('utf8');
    process.stdin.on('data', chunk => data += chunk);
    process.stdin.on('end', () => resolve(data));
    process.stdin.on('error', reject);
  });
}
`
