# Phase 3 JS 运行时与解析器 开发任务与提示词

> 配合总纲 [Phase1-在线优先-实施规划.md](./Phase1-在线优先-实施规划.md) 与
> [技术债与遗留项](./技术债与遗留项.md) 使用。延续 Codex 目标模式工作法。

## 范围（已拍板）

Phase 3 = **DRPY JS 运行时 + parse=1 解析器**（覆盖 0821 配置里 41 个 `.js` 站 + 跨站解析线路）。

**明确不做**：
- **CSP JAR/dex 运行时** → 推 **Phase 4**（需模拟 Android 宿主 + dex 加载，难度极高，单独立项）。
- **AList 目录源 / 网盘搜索** → 永久排除（来源本身不稳定，不值得投入）。

**运行时形态（in-process cgo QuickJS vs 独立 sidecar）= 先由 T16 PoC 验证后再定**，不预设。

延续不变约束：**在线内容绝不写 items**；存储分离、出口统一；确定性 UUID；代理仅字节中转+header 注入、永不转码；外部出站经 SSRF 校验 + per-provider limiter。

## §A 共享前置约束

整段沿用 [Phase1-开发任务与提示词.md](./Phase1-开发任务与提示词.md) 的「§A 共享前置约束」，**全程生效**，每个任务带上。

Phase 3 补充（运行不可信脚本，安全是一等公民）：
- **沙箱**：JS 运行时禁止任意文件系统访问；网络出站必须经 `source.ValidateOutboundURL` + per-provider limiter；单次调用强制超时；响应体大小上限；脚本异常/卡死不得拖垮 FYMS Core。
- **artifact 信任**：drpy 引擎与规则 js 视为 `source_runtime_artifacts`，下载落 `data/source-runtime/js/`，记录 md5/sha256；无 hash 的远程脚本默认 `unverified`，需管理员确认。
- **runtime 失败隔离**：runtime 不健康只影响对应 provider，归一化错误，不影响本地库与已可用 CMS。
- 数据层启用既有预留：`runtime_kind`(js_quickjs)、`source_runtime_artifacts`、`source_parsers`、`source_runtime_invocations`（审计）。

## §B 任务顺序

```
T16 DRPY PoC（可行性验证 + 运行时形态定夺）   【人工里程碑 / 闸门：决定后续形态，PoC 不通过不往下铺】
      │  （PoC 结论出来后，下面 T17~T20 的提示词据此定稿）
      ▼
T17 JS 运行时完整化 + 沙箱 + 生命周期 + artifact 管理
      ▼
T18 JS Provider 接入统一 Provider 接口（home/category/detail/search/play → 复用 normalize/ingest/play）
      ▼
T19 parse=1 解析器（source_parsers：导入/管理/解析；默认禁用，管理员逐个启用）
      ▼
T20 前端启停·健康·搜索测试 + parser 管理 UI + runtime 调用审计(source_runtime_invocations)
```

T17~T20 是 outline，**待 T16 PoC 定下运行时形态后再逐个定稿提示词**（in-process 与 sidecar 的 T17/T18 写法差异很大）。

---

## T16 — DRPY PoC（可行性验证 + 形态定夺）【闸门】

**依赖**：Phase 2 完成　**完成判定**：见末尾——能用真实 drpy2.min.js + 一个真实规则跑通 home/search/detail/play，并产出形态建议。

**提示词**
```
目标：做 DRPY JS 运行时的最小可行性 PoC，回答"FYMS 能否跑通 TVBox 的 DRPY JS 规则"，并给出运行时形态建议。
不求完整，只求证伪。§A 约束 + Phase 3 安全补充持续生效；不写 items、不扩范围到 T17+。

背景：0821 配置里 js 站形如 api="./lib/drpy2.min.js", ext="./js/360影视.js"，相对 base_url
(https://tvboxconfig.singlelovely.cn/gao/) 解析。DRPY 规则是为 QuickJS 写的，依赖一批宿主函数
(req/网络、pdfh/pdfa/pd HTML 解析、jsonpath、base64/md5/crypto、local 存储、编码转换、console 等)。

PoC 步骤：
1. 选引擎：优先尝试 in-process QuickJS(cgo，如 github.com/buke/quickjs-go 之类)——DRPY 原生面向 QuickJS，最忠实。
   若 cgo 与本项目静态构建/交叉编译冲突，或宿主函数实现成本过高，记录原因，作为"改用独立 sidecar(quickjs/node 进程)"的依据。
2. 拉取 artifact：按相对路径解析并下载 drpy2.min.js 与一个规则(建议 ./js/360影视.js)，下载经 ValidateOutboundURL。
3. 实现"最小可用"宿主桥：只补足让 360影视 规则跑起来所必需的宿主函数(req 走 Go HTTP + SSRF + limiter；
   pdfh/pdfa 等 HTML 解析；jsonpath；base64/md5；local 用内存 map)。缺哪个补哪个，记录清单。
4. 跑通四件：home(分类)、search(搜索一个关键词)、detail(取一条详情)、play(解析一条播放地址)。
5. 把结果通过一个【临时可访问入口】暴露出来供人工验证(例如一个仅管理员的 POST /SourceRuntime/TestJS
   {configBaseUrl, engine, rule, method, args} 返回 {ok,data,logs,durationMs})——因为我无法自行启动服务，
   需要我重启后用 API 验证。该入口是 PoC 专用，可后续移除/收编。
6. 全程单次调用超时、禁文件访问、网络经 SSRF、脚本异常不崩主进程。

完成判定（人工里程碑，go build 通过 + 我重启后用 API 验证）：
- /SourceRuntime/TestJS 对 360影视 规则能返回 home 分类、search 命中、detail 详情、play 可播放地址(或明确的 parse 标记)。
- 产出 PoC 报告：用了哪个引擎、cgo/静态构建影响、最小宿主函数清单与缺口、DRPY 各方法成功/失败情况、
  以及【运行时形态建议：in-process cgo QuickJS vs 独立 sidecar】及理由。
- 然后停下，告诉我"DRPY PoC 完成，待形态定夺"，不要进入 T17。
```

实际落点：新增 `internal/source/drpy_poc.go`，实现 T16 临时 DRPY PoC runner：按 `configBaseUrl + engine/rule` 下载真实 `drpy2.min.js` 与 `360影视.js` 到 `data/source-runtime/js/`，记录 md5/sha256 与 `unverified` trust，单次调用超时、artifact 下载经 `ValidateOutboundURL`，通过临时 Node sidecar 脚本返回 `home/search/detail/play` 四方法结果、日志、engine import 探测、cgo/静态构建影响、宿主函数清单与形态建议；新增 `internal/handlers/admin/source_runtime_handlers.go` 并在 `internal/handlers/admin/source_routes.go` 双注册管理员接口 `POST /SourceRuntime/TestJS`。未新增迁移/表，未写 `items`，未接 Provider 正式链路，未启动服务，未进入 T17。构建：`go build ./...` 通过。T16 commit 范围：19207ca。

---

## T17~T20 — Outline（PoC 后定稿）

> 下列为方向，提示词在 T16 PoC 定下形态后据此细化。

- **T17 运行时完整化 + 沙箱 + 生命周期 + artifact 管理**
  补齐 DRPY 宿主函数全集；runtime 按 site key 实例化、空闲超时销毁、配置/MD5 变更重建；
  沙箱(无 fs、网络 allowlist/SSRF、单次超时、响应大小上限、并发上限)；
  `source_runtime_artifacts` 落地(下载/校验/trust/本地缓存目录 data/source-runtime/js/)。

- **T18 JS Provider 接入统一接口**
  runtime_kind=js_quickjs 的 provider 通过运行时实现 Categories/Search/Category/Detail/ResolvePlay，
  输出复用 normalize.go/ingest.go/play 链路与聚合搜索；导入时 js 站从 runtime_required 转为可用。

- **T19 parse=1 解析器**
  `source_parsers` 落地(导入 TVBox `parses`：type 0/1 URL 模板)；播放线路 parse_mode=resolver 时按配置解析器解析；
  默认全部禁用、管理员逐个启用；解析出站经 SSRF + 超时；解析结果进 Redis 短 TTL(复用 Phase 1)。

- **T20 前端 + 审计**
  来源中心支持 js provider 启停/健康/搜索测试、parser 管理 UI；
  `source_runtime_invocations` 记录 runtime 调用(provider_id/method/duration/status/error_type，敏感信息脱敏)。

---

## §C 总目标提示词（先只跑到 T16 闸门）

```
【总目标】按 docs/universal-media/Phase3-JS运行时与解析器-开发任务与提示词.md 执行 Phase 3。
先读该文件「范围/§A/§B」与 T16，以及总纲与技术债文档。§A 全部约束(含 Phase 3 安全补充)持续生效。

【执行】本轮只做 T16 DRPY PoC：按代码/功能边界增量提交(中文、每个 commit 能 go build)，
完成后追加「实际落点」与 commit 范围；保证 go build ./... 通过。

【T16 闸门】完成 PoC 且构建绿后停下，产出 PoC 报告(引擎/cgo 影响/宿主函数清单与缺口/四方法成败/形态建议)，
明确告诉我"DRPY PoC 完成，待形态定夺"，不要进入 T17。T17~T20 的提示词需等我据 PoC 结论定稿后再发。

【冲突与边界】发现与总纲冲突或会违反 §A(污染 items、动本地主链路、写测试、自行启动服务、把 CSP/AList/网盘纳入)
即停下说明，不擅改、不绕约束、不扩范围。中文汇报：改了什么、构建是否通过、已知缺口。
```

---

## 备注

- T16 是整个 Phase 3 的命门：DRPY 能否在 FYMS 跑通、以什么形态跑，决定 T17~T20 的全部写法。和 Phase 1 的 T4 同性质——先证伪再铺开。
- cgo 抉择要点：若项目坚持 `CGO_ENABLED=0` 静态构建/交叉编译，则 in-process cgo QuickJS 不可行，应走 sidecar；PoC 必须明确回答这点。
- 安全红线：运行不可信第三方 JS，沙箱/超时/SSRF/artifact 校验是硬约束，不是可选项。
- CSP(38 站) 在 Phase 4 单独规划；AList/网盘已永久排除。
