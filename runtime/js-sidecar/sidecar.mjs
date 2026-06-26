import vm from 'node:vm';
import crypto from 'node:crypto';
import readline from 'node:readline';
import { Buffer } from 'node:buffer';

const MOBILE_UA = 'Mozilla/5.0 (Linux; Android 11; Pixel 5) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/90.0.4430.91 Mobile Safari/537.36';
const PC_UA = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/95.0.4638.54 Safari/537.36';
const pendingHTTP = new Map();
const local = new Map();
const runtimeTimers = new Set();
let seq = 0;

const rl = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
rl.on('line', line => onLine(line).catch(err => emitResult({
  ok: false,
  method: 'unknown',
  error: err?.message || String(err),
  errorType: 'runtime_error',
})));

async function onLine(line) {
  if (!line.trim()) return;
  const msg = JSON.parse(line);
  if (msg.type === 'http_response') {
    const pending = pendingHTTP.get(msg.id);
    if (pending) {
      pendingHTTP.delete(msg.id);
      msg.ok ? pending.resolve(msg) : pending.reject(new Error(msg.error || 'HTTP bridge failed'));
    }
    return;
  }
  if (msg.type === 'run') {
    const started = Date.now();
    const result = await runRuntime(msg);
    emitResult(result, Date.now() - started);
    process.stdin.destroy();
    clearRuntimeTimers();
    setImmediate(() => process.exit(0));
  }
}

async function runRuntime(input) {
  const logs = [];
  const phase = makePhaseLogger(logs);
  phase('runtime:start', { method: input.method || 'init' });
  const context = createContext(input, logs);
  phase('engine:load:start', { bytes: String(input.engineCode || '').length });
  const engine = await loadEngine(context, input.engineCode || '');
  phase('engine:load:done', { ok: engine.ok, stage: engine.stage, error: engine.error || '', durationMs: engine.durationMs || 0 });
  if (!engine.ok) {
    phase('engine:degrade', { reason: engine.error || engine.stage || 'unknown' });
  }
  phase('rule:load:start', { bytes: String(input.ruleCode || '').length });
  const ruleResult = loadRule(context, input.ruleCode || '');
  phase('rule:load:done', { ok: ruleResult.ok, error: ruleResult.error || '', durationMs: ruleResult.durationMs || 0 });
  if (!ruleResult.ok) {
    return {
      ok: false,
      method: input.method || 'init',
      engine,
      error: ruleResult.error,
      errorType: 'rule_load_failed',
      data: { logs },
    };
  }
  const method = String(input.method || 'init').toLowerCase();
  const methods = method === 'all' ? ['init', 'home', 'search', 'detail', 'play'] : [method];
  const results = [];
  for (const m of methods) {
    phase('dispatch:start', { method: m });
    results.push(await runOne(context, m, input.args || {}, engine));
    const last = results[results.length - 1];
    phase('dispatch:done', { method: m, ok: last.ok, durationMs: last.durationMs, errorType: last.errorType || '' });
  }
  phase('runtime:done', { ok: results.every(r => r.ok), durationMs: Date.now() - phase.startedAt });
  return {
    ok: results.every(r => r.ok),
    method,
    engine,
    results,
    data: { logs },
  };
}

function createContext(input, logs) {
  const baseUrl = input.baseUrl || 'https://tvboxconfig.singlelovely.cn/gao/';
  const siteHeaders = normalizeHeaders(input.headers || {});
  function logLine(...args) {
    const line = args.map(v => typeof v === 'string' ? v : JSON.stringify(v)).join(' ');
    logs.push(line);
    write({ type: 'log', message: line });
  }
  async function request(raw, options = {}) {
    const url = makeURL(baseUrl, raw);
    const headers = Object.assign({ 'User-Agent': MOBILE_UA }, siteHeaders, options.headers || {});
    logLine(`[bridgeHTTP:start] ${options.method || 'GET'} ${safeURLForLog(url)}`);
    const resp = await bridgeHTTP({
      url,
      method: options.method || 'GET',
      headers,
      body: options.body || '',
    });
    logLine(`[bridgeHTTP:done] status=${resp.status || 0} durationMs=${resp.durationMs || 0} bytes=${resp.bodyBytes || 0}`);
    const body = Buffer.from(resp.bodyBase64 || '', 'base64').toString(options.encoding || 'utf8');
    if (options.withHeaders) return { statusCode: resp.status, headers: resp.headers || {}, body };
    return body;
  }
  const sandbox = {
    console: { log: logLine, error: logLine, warn: logLine, info: logLine },
    print: logLine,
    log: logLine,
    Buffer,
    URL,
    URLSearchParams,
    crypto,
    setTimeout: trackedSetTimeout,
    clearTimeout: trackedClearTimeout,
    setInterval: trackedSetInterval,
    clearInterval: trackedClearInterval,
    MOBILE_UA,
    PC_UA,
    UA: MOBILE_UA,
    request,
    req: request,
    fetch: request,
    post: (raw, body, options = {}) => request(raw, { ...options, method: 'POST', body }),
    buildUrl,
    urljoin: makeURL,
    urljoin2: makeURL,
    base64Encode: text => Buffer.from(String(text), 'utf8').toString('base64'),
    base64Decode: text => Buffer.from(String(text), 'base64').toString('utf8'),
    md5: text => crypto.createHash('md5').update(String(text)).digest('hex'),
    rsa: () => { throw new Error('RSA host function is not supported yet'); },
    ocr: () => { throw new Error('OCR host function is not supported yet'); },
    sniffer: () => { throw new Error('sniffer host function is not supported yet'); },
    proxy: () => ({ error: 'proxy host function is not supported yet' }),
    __unsupportedImport: spec => Promise.reject(new Error(`dynamic import is not supported in FYMS sidecar: ${spec}`)),
    setItem: (k, v) => local.set(String(k), String(v)),
    getItem: (k, fallback = '') => local.has(String(k)) ? local.get(String(k)) : fallback,
    clearItem: k => local.delete(String(k)),
    local,
    CryptoJS: makeCryptoJS(),
    cheerio: makeCheerioFacade(),
    pdfh,
    pdfa,
    pd,
    jsp: jsonPath,
    jsonpath: jsonPath,
  };
  sandbox.globalThis = sandbox;
  return vm.createContext(sandbox, { name: 'fyms-drpy-context' });
}

function normalizeHeaders(raw) {
  const out = {};
  for (const [key, value] of Object.entries(raw || {})) {
    const name = String(key || '').trim();
    const text = String(value || '').trim();
    if (name && text) out[name] = text;
  }
  return out;
}

async function runOne(context, method, args, engine) {
  const started = Date.now();
  try {
    const data = await dispatch(context, method, args || {});
    return { ok: true, method, engineOk: engine.ok, engineNote: engine.stage, durationMs: Date.now() - started, data };
  } catch (err) {
    return {
      ok: false,
      method,
      engineOk: engine.ok,
      engineNote: engine.error ? `${engine.stage}: ${engine.error}` : engine.stage,
      durationMs: Date.now() - started,
      error: err?.message || String(err),
      errorType: classifyError(err),
    };
  }
}

async function dispatch(context, method, args) {
  const rule = context.rule || {};
  const candidates = methodFunctionNames(method);
  for (const name of candidates) {
    if (typeof rule[name] === 'function') return await rule[name](...methodArgs(method, args));
    if (context.__engineActive && typeof context[name] === 'function') return await context[name](...methodArgs(method, args));
  }
  if (method === 'init') return { ok: true, rule: rule.title || rule.name || '', host: hostSummary() };
  if (method === 'home') return fallbackHome(rule);
  if (method === 'category') return fallbackCategory(context, rule, args);
  if (method === 'search') return fallbackSearch(context, rule, args);
  if (method === 'detail') return fallbackDetail(context, rule, args);
  if (method === 'play') return fallbackPlay(args);
  if (method === 'proxy') return { error: 'proxy host function is not supported yet' };
  throw new Error(`unsupported method: ${method}`);
}

function loadRule(context, code) {
  const started = Date.now();
  try {
    const transformed = transformModuleCode(String(code || ''))
      .replace(/\bvar\s+rule\s*=/, 'globalThis.rule =')
      .replace(/\blet\s+rule\s*=/, 'globalThis.rule =')
      .replace(/\bconst\s+rule\s*=/, 'globalThis.rule =');
    vm.runInContext(transformed, context, { timeout: 8000 });
    if (!context.rule && context.__defaultExport) context.rule = context.__defaultExport;
    if (!context.rule) return { ok: false, error: '规则未导出 rule 对象', durationMs: Date.now() - started };
    return { ok: true, durationMs: Date.now() - started };
  } catch (err) {
    return { ok: false, error: err?.message || String(err), durationMs: Date.now() - started };
  }
}

async function loadEngine(context, code) {
  const started = Date.now();
  if (!code) return { ok: false, stage: 'empty', error: 'engine artifact empty', durationMs: 0 };
  try {
    const transformed = transformModuleCode(String(code));
    vm.runInContext(transformed, context, { timeout: 1500, microtaskMode: 'afterEvaluate' });
    clearRuntimeTimers();
    return { ok: true, stage: 'vm-load', needsHost: detectHostFunctions(code), durationMs: Date.now() - started };
  } catch (err) {
    clearRuntimeTimers();
    return { ok: false, stage: 'vm-load-degraded', error: err?.message || String(err), needsHost: detectHostFunctions(code), durationMs: Date.now() - started };
  }
}

function transformModuleCode(code) {
  return stripStaticImports(code)
    .replace(/\bimport\s*\(/g, '__unsupportedImport(')
    .replace(/export\s+default\b/g, 'globalThis.__defaultExport = ')
    .replace(/export\s+\{[^}]+\};?/g, '')
    .replace(/export\s+(async\s+function|function|const|let|var|class)\s+/g, '$1 ');
}

function stripStaticImports(code) {
  return String(code || '')
    .replace(/\bimport\s+[^;'"()]+?\s*from\s*['"][^'"]+['"]\s*;?/g, '')
    .replace(/\bimport\s*\{[^}]*\}\s*from\s*['"][^'"]+['"]\s*;?/g, '')
    .replace(/\bimport\s*['"][^'"]+['"]\s*;?/g, '');
}

function fallbackHome(rule) {
  const classes = [];
  const names = String(rule.class_name || '').split('&');
  const ids = String(rule.class_url || '').split('&');
  for (let i = 0; i < Math.min(names.length, ids.length); i++) {
    if (names[i] && ids[i]) classes.push({ type_id: ids[i], type_name: names[i] });
  }
  return { class: classes };
}

async function fallbackCategory(context, rule, args) {
  const tid = String(args.tid || args.id || '1');
  const pg = String(args.pg || args.page || '1');
  const url = String(rule.url || '').replaceAll('fyclass', tid).replaceAll('fypage', pg);
  const json = JSON.parse(await context.req(url, { headers: rule.headers || {} }));
  return { list: mapJSONList(json, rule['一级']), page: Number(pg) || 1 };
}

async function fallbackSearch(context, rule, args) {
  const keyword = String(args.keyword || args.wd || '三体');
  const pg = String(args.pg || args.page || '1');
  const url = String(rule.searchUrl || '').replaceAll('**', encodeURIComponent(keyword)).replaceAll('fypage', pg);
  const json = JSON.parse(await context.req(url, { headers: rule.headers || {} }));
  return { list: mapJSONList(json, rule['搜索']), page: Number(pg) || 1 };
}

async function fallbackDetail(context, rule, args) {
  if (typeof rule.detailUrl !== 'string') throw new Error('规则未提供 detailUrl，且未实现 detail 函数');
  let id = String(args.id || '');
  if (!id) {
    const sr = await fallbackSearch(context, rule, { keyword: args.keyword || '三体', page: 1 });
    id = sr.list?.[0]?.vod_id || '';
  }
  const [fyclass, fyid] = id.includes('$') ? id.split('$') : ['', id];
  const detailURL = String(rule.detailUrl || '').replaceAll('fyclass', fyclass).replaceAll('fyid', fyid);
  const data = JSON.parse(await context.req(detailURL, { headers: rule.headers || {} }));
  return normalizeDetailPayload(id, data, detailURL, rule);
}

function fallbackPlay(args) {
  const id = decodeURIComponent(String(args.url || args.id || '')).split('?')[0];
  return { parse: /\.(m3u8|mp4|m4a)$/i.test(id) ? 0 : 1, jx: /\.(m3u8|mp4|m4a)$/i.test(id) ? 0 : 1, url: id };
}

function normalizeDetailPayload(id, data, detailURL) {
  const item = data.data || data.list?.[0] || data || {};
  const play = normalizePlayFields(item);
  return { list: [{
    vod_id: id,
    vod_name: item.title || item.name || item.vod_name || '',
    vod_pic: item.cdncover || item.cover || item.vod_pic || '',
    type_name: joinMaybe(item.moviecategory || item.type_name),
    vod_area: joinMaybe(item.area || item.vod_area),
    vod_director: joinMaybe(item.director || item.vod_director),
    vod_actor: joinMaybe(item.actor || item.vod_actor),
    vod_content: item.description || item.vod_content || '',
    vod_play_from: play.from,
    vod_play_url: play.url,
    raw: item,
    detail_url: detailURL,
  }] };
}

function normalizePlayFields(item) {
  const directFrom = item.vod_play_from || item.play_from || item.from || item.flag || '';
  const directURL = item.vod_play_url || item.play_url || item.url || item.playUrl || '';
  if (directURL) return { from: String(directFrom || '默认线路'), url: String(directURL) };
  const rows = item.play || item.plays || item.playList || item.playlist || item.urls || item.videos || item.episodes;
  if (!rows) return { from: '', url: '' };
  if (Array.isArray(rows)) {
    const episodes = rows.map((row, idx) => {
      if (typeof row === 'string') {
        return row ? `第${idx + 1}集$${row}` : '';
      }
      const title = row.title || row.name || row.n || `第${idx + 1}集`;
      const rawURL = row.url || row.play_url || row.playUrl || row.u || row.id || '';
      return rawURL ? `${title}$${rawURL}` : '';
    }).filter(Boolean);
    return { from: String(directFrom || '默认线路'), url: episodes.join('#') };
  }
  if (typeof rows === 'object') {
    const from = [];
    const urls = [];
    for (const [lineName, value] of Object.entries(rows)) {
      const normalized = normalizePlayFields({ play: Array.isArray(value) ? value : [value] });
      if (!normalized.url) continue;
      from.push(lineName);
      urls.push(normalized.url);
    }
    return { from: from.join('$$$'), url: urls.join('$$$') };
  }
  return { from: String(directFrom || '默认线路'), url: String(rows || '') };
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

function methodFunctionNames(method) {
  return {
    init: ['init'],
    home: ['home', 'homeVod'],
    category: ['category', 'cate'],
    search: ['search'],
    detail: ['detail'],
    play: ['play'],
    proxy: ['proxy_rule', 'proxy'],
  }[method] || [method];
}

function methodArgs(method, args) {
  if (method === 'category') return [args.tid || args.id || '', args.pg || args.page || 1, args.filter || false, args.extend || {}];
  if (method === 'search') return [args.keyword || args.wd || '', args.quick || false, args.pg || args.page || 1];
  if (method === 'detail') return [args.id ? [args.id] : (args.ids || [])];
  if (method === 'play') return [args.flag || '', args.url || args.id || '', args.flags || []];
  if (method === 'proxy') return [args.params || args];
  return [];
}

function bridgeHTTP(request) {
  const id = String(++seq);
  write({ type: 'http_request', id, request });
  return new Promise((resolve, reject) => {
    pendingHTTP.set(id, { resolve, reject });
    setTimeout(() => {
      if (pendingHTTP.delete(id)) reject(new Error('HTTP bridge timeout'));
    }, 20000);
  });
}

function write(obj) {
  process.stdout.write(JSON.stringify(obj) + '\n');
}

function emitResult(result, durationMs = 0) {
  write({ type: 'result', result: { ...result, durationMs }, durationMs });
}

function makePhaseLogger(logs) {
  const startedAt = Date.now();
  const logger = (stage, fields = {}) => {
    const suffix = Object.entries(fields).map(([k, v]) => `${k}=${String(v)}`).join(' ');
    const line = `[phase] ${stage} t=${Date.now() - startedAt}ms${suffix ? ' ' + suffix : ''}`;
    logs.push(line);
    write({ type: 'log', message: line });
  };
  logger.startedAt = startedAt;
  return logger;
}

function trackedSetTimeout(fn, ms, ...args) {
  const timer = setTimeout(() => {
    runtimeTimers.delete(timer);
    fn(...args);
  }, ms);
  runtimeTimers.add(timer);
  return timer;
}

function trackedClearTimeout(timer) {
  runtimeTimers.delete(timer);
  clearTimeout(timer);
}

function trackedSetInterval(fn, ms, ...args) {
  const timer = setInterval(fn, ms, ...args);
  runtimeTimers.add(timer);
  return timer;
}

function trackedClearInterval(timer) {
  runtimeTimers.delete(timer);
  clearInterval(timer);
}

function clearRuntimeTimers() {
  for (const timer of runtimeTimers) {
    clearTimeout(timer);
    clearInterval(timer);
  }
  runtimeTimers.clear();
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
    if (!trimmed) continue;
    if (trimmed.includes('+')) {
      const values = trimmed.split('+').map(part => getPath(obj, part.trim())).filter(v => v !== undefined && v !== null && String(v) !== '');
      if (values.length) return values.join('$');
    }
    const v = getPath(obj, trimmed);
    if (v !== undefined && v !== null && String(v) !== '') return v;
  }
  return '';
}

function jsonPath(obj, path) {
  return getPath(obj, String(path || '').replace(/^\$/, ''));
}

function pdfh(html, selector) {
  const rows = pdfa(html, selector);
  return rows.length ? stripTags(rows[0]) : '';
}

function pdfa(html, selector) {
  const text = String(html || '');
  if (!selector || selector === '*') return [text];
  const tag = String(selector).match(/[a-zA-Z][\w-]*/)?.[0];
  if (!tag) return [];
  const re = new RegExp(`<${tag}[^>]*>([\\s\\S]*?)<\\/${tag}>`, 'gi');
  return [...text.matchAll(re)].map(m => m[0]);
}

function pd(html, selector, attr) {
  const rows = pdfa(html, selector);
  if (!rows.length) return '';
  if (!attr || attr === 'Text') return stripTags(rows[0]);
  const m = rows[0].match(new RegExp(`${attr}\\s*=\\s*["']([^"']+)["']`, 'i'));
  return m ? m[1] : '';
}

function stripTags(text) {
  return String(text || '').replace(/<[^>]+>/g, '').trim();
}

function makeCheerioFacade() {
  return {
    load: html => selector => ({ text: () => pdfh(html, selector), attr: name => pd(html, selector, name), html: () => pdfa(html, selector)[0] || '' }),
    jp: (path, obj) => getPath(obj, path),
    jinja2: tpl => tpl,
  };
}

function makeCryptoJS() {
  return {
    MD5: text => ({ toString: () => crypto.createHash('md5').update(String(text)).digest('hex') }),
    SHA1: text => ({ toString: () => crypto.createHash('sha1').update(String(text)).digest('hex') }),
    SHA256: text => ({ toString: () => crypto.createHash('sha256').update(String(text)).digest('hex') }),
    enc: {
      Utf8: { parse: text => Buffer.from(String(text), 'utf8'), stringify: buf => Buffer.from(buf).toString('utf8') },
      Base64: { stringify: buf => Buffer.from(buf).toString('base64'), parse: text => Buffer.from(String(text), 'base64') },
    },
  };
}

function joinMaybe(value) {
  if (Array.isArray(value)) return value.join(',');
  return value == null ? '' : String(value);
}

function safeURLForLog(raw) {
  try {
    const u = new URL(raw);
    return `${u.protocol}//${u.host}${u.pathname}`;
  } catch {
    return '[invalid-url]';
  }
}

function classifyError(err) {
  const text = err?.message || String(err);
  if (/timeout/i.test(text)) return 'timeout';
  if (/HTTP\s+[45]\d\d/i.test(text)) return 'site_unavailable';
  if (/not supported/i.test(text)) return 'unsupported';
  return 'runtime_error';
}

function detectHostFunctions(code) {
  const out = [];
  for (const name of ['req', 'request', 'pdfh', 'pdfa', 'pd', 'CryptoJS', 'cheerio', 'getItem', 'setItem', 'ocr', 'sniffer', 'proxy']) {
    if (new RegExp(`\\b${name}\\b`).test(code)) out.push(name);
  }
  return out;
}

function hostSummary() {
  return {
    network: 'go-bridge-ssrf-limited',
    html: 'lightweight-pdfh-pdfa-pd',
    crypto: 'node-crypto-cryptojs-facade',
    unsupported: ['ocr', 'sniffer', 'proxy'],
  };
}
