# Phase 5 FongMi 兼容语义补强设计与 Codex 提示词

> 本文用于后续 Codex 目标模式执行。目标是让 FYMS 的 TVBox / FongMi 兼容语义从“能跑部分 Provider 方法”提升为“按 FongMi 客户端口径理解配置、首页、分类、探活、解析器与审计”。
> 本 Phase 会触达 JS/CSP/CMS Provider 语义，必须严格小步推进；不改本地 Emby 主链路，不写 `items`，不启动服务，不写 tests。

---

## 1. 调研基线

调研对象：

- `FongMi/TV` 当前源码快照：`79e87d3`，提交日期 `2026-06-25`。
- 重点代码：
  - `app/src/main/java/com/fongmi/android/tv/api/config/VodConfig.java`
  - `app/src/main/java/com/fongmi/android/tv/bean/Site.java`
  - `app/src/main/java/com/fongmi/android/tv/api/SiteApi.java`
  - `app/src/main/java/com/fongmi/android/tv/api/loader/BaseLoader.java`
  - `app/src/main/java/com/fongmi/android/tv/api/loader/JarLoader.java`
  - `app/src/main/java/com/fongmi/android/tv/api/loader/JsLoader.java`
  - `app/src/main/java/com/fongmi/android/tv/bean/Result.java`
  - `app/src/main/java/com/fongmi/android/tv/bean/Vod.java`
  - `app/src/main/java/com/fongmi/android/tv/bean/Class.java`
  - `app/src/main/java/com/fongmi/android/tv/player/parse/ParseJob.java`

核心结论：

1. FongMi 不是把 TVBox JSON 简单映射为接口，而是实现了一层兼容语义：
   - 配置级 `spider` 是默认 jar；
   - 每个 `site` 是运行单元；
   - loader 按 `api` 分派 `cms/json/xml`、`csp_*`、`.js`、`.py`；
   - `Result` 统一承载 `class/list/filters/url/header/msg`；
   - UI 首页、分类、详情、播放、解析器有不同口径。
2. Spider/CSP 首页由两段组成：
   - `homeContent(true)`：主要用于 `class`、`filters`，有时也带 `list`；
   - `homeVideoContent()`：主要用于首页海报墙 `list`。
3. FongMi 如果 `homeVideoContent()` 有 `list`，会用它覆盖 `homeContent()` 的首页列表；移动端还会因为首页 `list` 非空自动增加一个 `home` 类型 tab。
4. `sites[].categories` 不是独立分类来源，而是对 `homeContent().class[]` 的名称白名单过滤。
5. `searchable`、`quickSearch`、`changeable`、`hide`、`indexs`、`timeout`、`header`、`style`、`playUrl`、`click` 等字段会影响 UI 或调用语义。
6. FongMi 播放链路会综合 `playerContent`、`parse`、`playUrl`、`header`、全局 `flags/parses`、站点 `click` 与 WebView/JSON/mix 解析器。FYMS 不应一次性照搬，必须拆阶段。

---

## 2. FYMS 当前差距

FYMS 已有基础：

- `source_config_imports` 保存 TVBox / CMS 配置包。
- `source_providers` 保存单站点 provider，`source_key` 稳定复用。
- `source_runtime_artifacts` 保存 JS/CSP runtime artifact。
- `source_parsers` 保存 TVBox parses。
- `source_runtime_invocations` 保存运行审计。
- `ProviderRuntimeManager` 已按 `native_cms`、`js_node_drpy`、`csp_dex` 分派。
- JS/CSP runtime 均采用 sidecar 形态，出站经 Go SSRF 校验与限流。

主要差距：

1. Provider `HealthCheck` 当前基本等价于 `Categories()`，容易把“`homeContent` 空但 `homeVideoContent` 有海报”的源误判失败。
2. CSP sidecar 当前没有正式暴露 `homeVideoContent()` 调用；JS sidecar / JSProvider 也需要核对是否有同等首页列表语义。
3. 后台缺少 FongMi 口径诊断：看不到 `homeContent`、`homeVideoContent`、`class/list/filters` 的分项结果。
4. TVBox 配置里的站点能力字段部分已落库，但调用层和 UI 没有完整消费。
5. CMS / XML / JSON 的 FongMi 式补图、分类白名单、filters 挂载语义仍不完整。
6. ParserResolver 目前是全局解析器顺序，尚未支持 FongMi 的 parse flag / playUrl / click / mix parser 复杂语义。

---

## 3. 目标模型

新增一层 Provider 兼容语义，不改变 FYMS 的核心媒体模型：

```text
TVBox Config
  -> Source Config Import
  -> Provider
      -> Runtime Adapter
          -> HomeProfile
             - categories from homeContent.class
             - filters from homeContent.filters
             - home items from homeContent.list + homeVideoContent.list
          -> Category/Search/Detail
          -> Play Resolve
  -> Source Items / Play Sources
  -> Source Library Views / Emby compatible exits
```

核心原则：

1. 在线内容继续只写 `source_items/source_play_sources`，不写 `items`。
2. `homeContent` 与 `homeVideoContent` 分开记录、分开报错、按 FongMi 规则合并展示。
3. 探活状态拆分为分项健康，不再用单个“分类是否非空”代表 Provider 是否可用。
4. `sites[].categories` 是过滤策略，不是上游事实来源。
5. Runtime invocation / audit 继续只记录 hash、摘要、错误类型，不记录敏感 URL/token/cookie/header 明文。
6. 播放解析器复杂语义单独阶段推进，不在首页/探活阶段顺手改播放链路。

---

## 4. 建议任务顺序

```text
FM1 FongMi 兼容诊断 API
  - 后台可看到 homeContent/homeVideoContent/search/category/detail 的分项诊断
  - 不改变 ProviderRuntimeManager 现有生产行为

FM2 HomeProfile 抽象与 homeVideoContent 接入
  - CSP sidecar 支持 homeVideoContent
  - JS/CMS 对齐 HomeProfile
  - Provider 增加 HomeProfile/首页列表能力

FM3 Provider 健康状态重定义
  - runtime/category/home/search/play_ready 分项探活
  - 批量探活与批量启停支持按分项状态过滤

FM4 TVBox 站点能力字段消费
  - categories 白名单、hide/indexs/searchable/quickSearch/changeable/header/style/timeout
  - 后台和聚合搜索按能力字段工作

FM5 CMS/FongMi 归一补强
  - XML/JSON/CMS 分类、filters、首页 list、补图、详情回填
  - 保持 native_cms 简洁，不引入 runtime sidecar

FM6 Parser 兼容语义设计与最小落地
  - parse flag/playUrl/click/json parser/mix parser 的 FYMS 侧策略
  - 不做 WebView 嗅探，不引入 Android 模拟器
```

FM1 是只读诊断，风险最低，应先做。FM2 起会改变运行时语义，必须在单独任务里完成并构建通过。FM6 涉及播放链路，必须最后单独设计、单独实现。

---

## 5. 数据与 API 设计草案

### 5.1 HomeProfile 内部结构

建议在 `internal/source` 增加内部 DTO：

```go
type ProviderHomeProfile struct {
    ProviderID int64
    RuntimeKind string
    Categories []ProviderCategory
    Filters map[string]any
    HomeItems []ProviderItem
    Sources ProviderHomeSources
}

type ProviderHomeSources struct {
    HomeContent ProviderRuntimeSlice
    HomeVideoContent ProviderRuntimeSlice
}

type ProviderRuntimeSlice struct {
    Method string
    OK bool
    ErrorType string
    ErrorMessage string
    CategoriesCount int
    FiltersCount int
    ItemsCount int
    DurationMS int64
}
```

说明：

- `ProviderHomeProfile` 不一定立刻落库，可先作为 runtime 结果和诊断 DTO。
- 如果后续要缓存首页海报，可复用 `source_items` 入库，但必须明确 `last_seen_at` 与 GC 语义。
- `Filters` 先保留 JSON，不急着全量建表；已有预留表可后续补落库。

### 5.2 诊断 API

建议新增：

```text
POST /SourceProviders/:id/Diagnose
GET  /SourceProviders/:id/HomeProfile
```

`Diagnose` 建议参数：

```json
{
  "methods": ["home", "homeVideo", "category", "search"],
  "categoryId": "1",
  "keyword": "test",
  "timeoutMs": 30000
}
```

返回只读摘要：

```json
{
  "provider_id": 1,
  "provider_name": "示例",
  "runtime_kind": "csp_dex",
  "overall_status": "partial_ok",
  "home": {
    "ok": true,
    "categories_count": 0,
    "filters_count": 0,
    "items_count": 0,
    "error_type": "",
    "message": ""
  },
  "home_video": {
    "ok": true,
    "items_count": 24,
    "error_type": "",
    "message": ""
  }
}
```

### 5.3 健康状态

短期不建议破坏 `source_providers.health_status`，可在 `capabilities` 或 `raw` 摘要里增加分项，或者新增后续迁移：

```text
source_provider_health_checks
  provider_id
  check_kind        -- runtime/home/category/search/play
  status            -- ok/partial/unhealthy/error/unknown
  error_type
  message
  metrics jsonb
  checked_at
```

若新增表，必须作为独立任务，避免把 FM1 只读诊断变成大迁移。

---

## 6. 风险与边界

1. `homeVideoContent` 成功不代表分类可浏览成功；UI 必须展示 `partial_ok`，不要伪装成全绿。
2. `homeContent` 空字符串在 FongMi 里会被解析成 empty Result，不一定是致命错误；FYMS 诊断也应区分 `empty_result` 与 `runtime_error`。
3. CSP dex 是不可信代码，继续走 JVM sidecar + artifact trust + Go HTTP bridge，不能为了兼容绕过 SSRF。
4. FongMi WebView 嗅探、Android App 环境、签名相关能力不纳入 FYMS 服务端实现。
5. Parser 语义会影响播放链路，不能在 FM1-FM5 顺手更改 `/SourcePlay` 的核心行为。
6. 所有管理 UI 都必须脱敏 URL/header/cookie/token。

---

## 7. §A 共享前置约束

```text
你在为 FYMS 开发。FYMS 是 Go + Gin + PostgreSQL(pgx/v5) + Redis + Vue3 的 Emby 兼容媒体服务器，前端通过 //go:embed 嵌入。

本次任务权威文档是：
- docs/universal-media/Phase5-FongMi兼容语义补强-设计与提示词.md
- docs/universal-media/Phase4-CSP运行时-开发任务与提示词.md
- docs/universal-media/Phase3-JS运行时与解析器-开发任务与提示词.md
- docs/universal-media/SourceCenter-目标模式重构与提示词.md

硬性约束：
1. 允许并要求跑编译与构建校验：
   - 后端改动后必须运行 go build ./...，确保编译成功。
   - 前端改动后必须运行 cd web && npm run build，确保构建成功。
   - 同时改后端和前端时两个都要跑。
   - 构建不绿不算完成，必须先修到绿。
2. 禁止：go run / 启动服务、go test 或任何测试、npm run dev、写测试文件或测试代码。
3. 在线内容绝不写入 items 表，不改现有 items/users/libraries/persons 主键与既有 Emby 主链路。
4. 触达 JS/CSP/CMS runtime 语义时，必须小步提交，并在任务实际落点写清楚改变了哪个 method、哪个 adapter、哪个 API。
5. 数据库迁移只新增 migrations/NNN_*.sql，序号 = 现有最大号 + 1；已执行迁移文件不改。
6. 防止大文件：单文件接近 800 行或职责混杂时，拆到同 package / 同目录下按领域命名的文件。
7. 敏感信息不明文展示或入日志：URL/token/cookie/header 只能脱敏或 hash。
8. 不引入 Android 模拟器、Robolectric、WebView 嗅探或浏览器解析作为服务端 runtime 依赖。
9. 不把 AList/网盘搜索扩大进本 Phase；遇到 cloud_share/magnet/unsupported 继续归一化提示。
10. 提交必须按功能边界增量提交，禁止大 commit：
    - 每完成一个内聚单元就提交一次。
    - 每个 commit 信息用中文。
    - 提交前用 git diff --cached --name-only 核对只 stage 当前任务文件。
    - 不 stage 无关脏文件、构建产物或用户未要求提交的文件。

完成每个任务后用中文汇报：
- 改了哪些文件；
- 新增/调整哪些 API、runtime method、DTO、页面、字段或迁移；
- go build ./... 是否通过；
- cd web && npm run build 是否通过；
- commit 列表；
- 已知未覆盖点和需要用户手动验证的点。
```

---

## 8. 总目标提示词

```text
【总目标】按 docs/universal-media/Phase5-FongMi兼容语义补强-设计与提示词.md 顺序补强 FYMS 对 FongMi/TVBox 的兼容语义。

先完整阅读：
1. docs/universal-media/Phase5-FongMi兼容语义补强-设计与提示词.md
2. docs/universal-media/Phase4-CSP运行时-开发任务与提示词.md 中 CSP runtime 当前落点
3. docs/universal-media/Phase3-JS运行时与解析器-开发任务与提示词.md 中 JS runtime / Parser 当前落点
4. docs/universal-media/SourceCenter-目标模式重构与提示词.md 中来源中心管理和审计口径
5. 当前代码：
   - internal/source/provider.go
   - internal/source/provider_runtime.go
   - internal/source/csp_provider.go
   - internal/source/csp_runtime_manager.go
   - runtime/csp-sidecar/src/fyms/csp/CSPProbe.java
   - internal/source/js_provider.go
   - runtime/js-sidecar/sidecar.mjs
   - internal/source/cms_provider.go
   - internal/source/tvbox_config.go
   - internal/source/tvbox_importer.go
   - internal/handlers/admin/source_provider_handlers.go
   - internal/handlers/admin/source_runtime_handlers.go
   - web/src/components/source-center/*
   - web/src/api/source.ts

§A 共享前置约束全程生效。

【执行方式】
严格按 FM1 → FM2 → FM3 → FM4 → FM5 → FM6 顺序推进，一次只做一个任务。每个任务内部可以按更小代码边界拆 commit。

每个任务收尾必须：
1. go build ./... 通过。
2. 如果改了 web/，cd web && npm run build 通过。
3. 按功能边界提交中文 commit，禁止大 commit。
4. 在本文对应任务末尾追加“实际落点”，写清文件、API、runtime method、构建结果和 commit hash。

【边界】
本 Phase 可以按任务触达 JS/CSP/CMS runtime 语义，但必须只做当前任务明示的 method 和 adapter。
不写 tests，不启动服务，不改本地 Emby 主链路，不写 items。
如果发现必须改播放解析链路才能完成 FM1-FM5，先停下给设计说明，不要擅自改 /SourcePlay 或 ParserResolver。
```

---

## FM1 - FongMi 兼容诊断 API

**目标**：新增只读诊断能力，让管理员能看到 FongMi 口径下 `homeContent` / `homeVideoContent` / 分类 / 搜索的分项结果。FM1 不改变现有 Provider 生产行为和探活判定。

**建议 commit 边界**：

1. 后端诊断 DTO 与 runtime 编排。
2. admin route / handler。
3. 前端 Source Center 诊断入口。

**提示词**

```text
目标：实现 Provider 的 FongMi 兼容诊断 API，不改变现有 HealthCheck/Categories/Search 生产语义。

范围：
1. 新增后端只读 API：
   - POST /SourceProviders/:id/Diagnose
   - 入参支持 methods、category_id、keyword、timeout_ms。
   - methods 至少支持 home、homeVideo、category、search。
2. 诊断结果要分项返回：
   - method
   - status: ok/empty/error/unsupported/skipped
   - error_type/message
   - latency_ms
   - categories_count
   - filters_count
   - items_count
   - sample_items 脱敏摘要，最多 5 条。
3. FM1 不改 ProviderRuntimeManager.HealthCheck，不改 source_providers.health_status 判定。
4. 对尚不支持的 runtime method 返回 unsupported，不报 500。
5. Runtime invocation 继续走既有审计；敏感 URL/header/cookie/token 不明文。
6. 前端在 Provider 表或 drawer 增加“兼容诊断”入口，展示分项结果，解释：
   - FongMi 首页海报墙可能来自 homeVideoContent；
   - homeContent 空不一定代表源坏；
   - 分类、首页、搜索应分开判断。

完成判定：
- go build ./... 通过。
- 如果改 web/，cd web && npm run build 通过。
- 至少两个中文 commit：后端诊断 API、前端诊断入口。
```

**实际落点**：
- 文件：
  - `internal/source/provider_diagnose.go`
  - `internal/handlers/admin/source_provider_handlers.go`
  - `internal/handlers/admin/source_routes.go`
  - `web/src/api/source.ts`
  - `web/src/composables/useSourceProviders.ts`
  - `web/src/composables/useSourceCenter.ts`
  - `web/src/components/source-center/SourceProviderPanel.vue`
  - `web/src/pages/SourceCenterPage.vue`
- API：
  - 新增 `POST /SourceProviders/:id/Diagnose`。
  - 入参支持 `methods/category_id/keyword/source_item_id/detail_id/timeout_ms`。
  - 返回 `provider_id/provider_name/source_key/runtime_kind/provider_kind/overall_status/duration_ms/results[]`；每个分项包含 `method/status/error_type/message/latency_ms/categories_count/filters_count/items_count/sample_items`。
- runtime method / adapter：
  - FM1 只做只读诊断，不改变 `ProviderRuntimeManager.HealthCheck`、`Search`、`Detail`、`Categories` 的生产语义。
  - `home`：JS 走 `JSRuntimeMethodHome`，CSP 走 `CSPRuntimeMethodHome`，CMS 走 native CMS `ac=list` 分类/首页口径，统一统计 `class/filters/list`。
  - `category`：直接调用当前 Provider `Category`，只汇总摘要，不走 ingestor，不写 `source_items`。
  - `search`：直接调用当前 Provider `Search`，只汇总摘要，不走 ingestor，不写 `source_items`。
  - `detail`：仅在请求提供 `source_item_id/detail_id` 时只读调用 Provider `Detail`，否则返回 `skipped`。
  - `homeVideo/homeVideoContent`：FM1 返回 `unsupported` 并说明 FM2 正式接入，不在本任务新增 sidecar method。
- 前端：
  - Source Center Provider 表增加“诊断”入口。
  - 诊断结果区展示 FongMi 分项状态、耗时、class/filter/list 计数、样例条目摘要，并提示 `homeVideoContent` 与 `homeContent` 的 FongMi 语义差异。
  - 诊断不会刷新 Provider 探活状态，也不会写在线缓存。
- 构建：
  - `go build ./...` 通过。
  - `cd web && npm run build` 通过；保留既有 ArtPlayer CommonJS warning。
- Commit：
  - `3cd440cd` 新增FongMi兼容诊断API。
  - `cc961f35` 来源中心增加FongMi诊断入口。

---

## FM2 - HomeProfile 抽象与 homeVideoContent 接入

**目标**：按 FongMi 语义正式接入首页 Profile。CSP 支持 `homeVideoContent()`；JS/CMS 对齐统一 HomeProfile；首页列表可入库到 `source_items`，但不写 `items`。

**建议 commit 边界**：

1. Provider interface / HomeProfile 内部 DTO。
2. CSP sidecar method + CSPProvider 接入。
3. JS/CMS 对齐 HomeProfile。
4. HomeProfile API / 前端展示。

**提示词**

```text
目标：实现 ProviderHomeProfile，并正式支持 FongMi 的 homeVideoContent 语义。

范围：
1. internal/source 增加 HomeProfile 内部 DTO 与 Provider 可选能力。
2. CSP sidecar 增加 method 映射：
   - homeVideo
   - homeVideoContent
   调用 Spider.homeVideoContent()。
3. CSPProvider.HomeProfile：
   - 调 homeContent(true) 拿 class/filters/list；
   - 调 homeVideoContent() 拿 list；
   - 如果 homeVideo list 非空，按 FongMi 规则作为首页列表；
   - class/filters 仍以 homeContent 为准。
4. JSProvider/CMSProvider 对齐 HomeProfile：
   - JS 如已有 home 返回 list/class，则映射为 homeContent 结果；
   - CMS 首页列表按现有 home/category 能力保守映射，不强行伪造 homeVideo。
5. 新增只读 API：
   - GET /SourceProviders/:id/HomeProfile
   返回 categories、filters 摘要、home_items 摘要、sources 分项结果。
6. 可选择把 HomeProfile 的 home_items 通过 Ingestor 写入 source_items，但必须满足：
   - 只写 source_items/source_play_sources；
   - 不写 items；
   - last_seen_at/GC 语义清晰；
   - 失败不影响分类返回。

完成判定：
- go build ./... 通过。
- 如改 web/，cd web && npm run build 通过。
- 提交拆分为 sidecar method、provider HomeProfile、API/UI 三类，不要大 commit。
```

**实际落点**：
- 文件：
  - `internal/source/provider_home_profile.go`
  - `internal/source/provider_runtime.go`
  - `internal/source/csp_runtime_types.go`
  - `internal/source/csp_provider.go`
  - `internal/source/js_provider.go`
  - `internal/source/cms_provider.go`
  - `internal/source/provider_diagnose.go`
  - `runtime/csp-sidecar/src/fyms/csp/CSPProbe.java`
  - `internal/handlers/admin/source_provider_handlers.go`
  - `internal/handlers/admin/source_routes.go`
  - `web/src/api/source.ts`
  - `web/src/composables/useSourceProviders.ts`
  - `web/src/composables/useSourceCenter.ts`
  - `web/src/components/source-center/SourceProviderPanel.vue`
  - `web/src/pages/SourceCenterPage.vue`
- API：
  - 新增 `GET /SourceProviders/:id/HomeProfile`。
  - 返回 `provider_id/runtime_kind/categories/filters/filters_count/home_items/home_item_source/sources`。
  - `sources.home_content` 与 `sources.home_video_content` 分开返回 `method/status/ok/error_type/error_message/categories_count/filters_count/items_count/duration_ms`。
  - API 为只读运行画像，不走 `SourceIngestor`，不写 `source_items`，不写 `items`。
- runtime method / adapter：
  - 新增 `CSPRuntimeMethodHomeVideo = "homeVideo"`。
  - CSP sidecar `CSPProbe.callSpider` 支持 `homeVideo` / `homeVideoContent` alias，调用 `Spider.homeVideoContent()`。
  - `CSPProvider.HomeProfile` 分别调用 `homeContent(true)` 与 `homeVideoContent()`；`class/filters` 以 `homeContent` 为准；若 `homeVideoContent.list` 非空，则按 FongMi 规则作为最终 `home_items`，否则使用 `homeContent.list`。两个分项独立记录失败，只有两者都失败时整体返回错误。
  - `JSProvider.HomeProfile` 将现有 `home` 返回映射为 `homeContent`，不伪造独立 `homeVideoContent`。
  - `CMSProvider.HomeProfile` 使用 native CMS `ac=list` 口径保守映射 `class/list`，`homeVideoContent` 标记为 unsupported。
  - `ProviderRuntimeManager.HomeProfile` 使用可选接口 `HomeProfiler` 编排，只读调用 Provider，不改变 `HealthCheck/Search/Detail/Categories` 生产语义。
  - FM1 诊断中的 `homeVideo/homeVideoContent` 已随 FM2 对 CSP 正式接入；JS/CMS 仍返回 unsupported 说明。
- 前端：
  - Source Center Provider 表新增“首页”操作。
  - 首页画像区展示运行态、最终首页列表来源、class/filter/home items 数量、`homeContent` / `homeVideoContent` 分项状态与样例条目。
  - UI 明确该操作为 read-only，不写在线缓存。
- 构建：
  - `go build ./...` 通过。
  - `cd web && npm run build` 通过；保留既有 ArtPlayer CommonJS warning。
- Commit：
  - `c376cdc1` 接入CSP首页视频诊断。
  - `c3bc3ef3` 新增Provider首页画像抽象。
  - `7693ee35` 来源中心展示首页画像。

---

## FM3 - Provider 健康状态重定义

**目标**：把探活从单一 `Categories()` 判定改为分项健康模型，避免“首页有海报但分类为空”的 Provider 被误判失败。

**建议 commit 边界**：

1. 后端健康 DTO / 可选迁移。
2. HealthCheck 编排与批量探活返回。
3. 前端筛选和批量操作。

**提示词**

```text
目标：重定义 Provider 探活口径，支持 runtime/home/category/search/play_ready 分项健康。

范围：
1. 设计并实现健康结果 DTO：
   - runtime_status
   - home_status
   - category_status
   - search_status
   - play_ready_status
   - overall_status
   - message
   - checked_at
2. 可选新增 migration source_provider_health_checks；如果不新增表，则先把分项摘要存入 provider capabilities/raw health summary，但必须说明取舍。
3. ProviderRuntimeManager.HealthCheck 改为：
   - runtime 能调用即 runtime ok；
   - homeContent/homeVideoContent 任一有可用首页信息则 home ok/partial；
   - category 单独看 class 是否非空；
   - search 只在 searchable=true 且给定轻量关键词时检查，失败不必拖垮整体。
4. BatchHealthCheck 返回分项健康，单个 provider 失败不影响整批。
5. GET /SourceProviders 支持按分项状态过滤，至少支持：
   - health_status
   - home_status
   - category_status
   - runtime_status
6. 前端 Provider 页支持按状态过滤和批量：
   - 把首页可用全部启用；
   - 把 runtime/category/home 明确失败的全部禁用；
   - 二次确认说明筛选条件和数量。

完成判定：
- go build ./... 通过。
- cd web && npm run build 通过。
- 中文 commit 按迁移/后端探活/前端筛选批量拆分。
```

**实际落点**：

- 后端分项健康模型落在 `internal/source/provider_health.go` 与 `internal/source/provider_runtime.go`：
  - 新增 `ProviderHealthSummary` / `ProviderHealthMethodSummary`，字段包含 `runtime_status`、`home_status`、`category_status`、`search_status`、`play_ready_status`、`overall_status`、`message`、`checked_at`。
  - `ProviderRuntimeManager.HealthCheck` 不再只用 `Categories()` 判定，优先通过 `HomeProfile()` 汇总 `homeContent` / `homeVideoContent`；首页任一方法有可用首页信息则 `home_status=ok/partial`，分类单独按 `class` 非空判定，`searchable=true` 时用轻量关键词 `test` 做搜索探活且搜索失败只影响分项。
  - `play_ready_status` 保持 `skipped`，未触达 `/SourcePlay` 或 `ParserResolver`。
- 存储取舍：本任务未新增 `source_provider_health_checks` migration；分项摘要写入 `source_providers.capabilities.health`，避免污染上游 `raw_site`，后续如需历史趋势再单独建检查历史表。
- Repository / API 落点：
  - `internal/repository/source_provider_repository.go` 新增 `UpdateProviderHealthSummary()`，同步更新 `health_status`、`last_check_at`、`last_error`、`categories` 与 `capabilities.health`。
  - `GET /SourceProviders` 支持 `health_status`、`runtime_status`、`home_status`、`category_status` 过滤。
  - `POST /SourceProviders/BatchHealthCheck` 返回分项状态，单 Provider 失败不影响整批。
- 前端落点：
  - `web/src/api/source.ts` 补充分项健康类型与列表过滤参数。
  - `web/src/composables/useSourceProviders.ts` / `useSourceCenter.ts` 接入服务端分项筛选状态。
  - `web/src/components/source-center/SourceProviderPanel.vue` 展示 runtime/home/category/search/play_ready 标签，增加 Runtime/首页/分类健康筛选，并提供“启用首页可用”“停用明确失败”批量动作。
  - `web/src/pages/SourceCenterPage.vue` 完成筛选状态绑定。
- 构建结果：
  - `go build ./...` 通过。
  - `cd web && npm run build` 通过；仅保留 Vite 对 `artplayer` 依赖 CommonJS `module` 变量的既有 warning。
- Commits：
  - `82f22a50` 重定义Provider分项探活。
  - `14839717` 来源中心支持分项健康筛选。

---

## FM4 - TVBox 站点能力字段消费

**目标**：让 TVBox 配置字段进入 FYMS 的实际管理和调用语义。

**提示词**

```text
目标：消费 TVBox sites 字段，使 FYMS 管理和调用更接近 FongMi。

范围：
1. 核对并补齐 TVBox importer 字段：
   - hide
   - indexs
   - searchable
   - quickSearch
   - changeable
   - categories
   - header
   - timeout
   - style
   - playUrl
   - click
2. categories 作为 homeContent.class 的名称白名单，不作为真实分类替代。
3. 聚合搜索尊重 searchable/quickSearch。
4. Provider 列表默认隐藏 hide=1，但提供“显示隐藏站点”筛选。
5. header 进入对应 Provider 请求和播放解析上下文，但敏感 header 不明文显示。
6. style 仅作为 UI 展示元数据保存，不影响 Emby 核心模型。

完成判定：
- go build ./... 通过。
- 如改 web/，cd web && npm run build 通过。
- 每个字段消费点写清实际落点。
```

**实际落点**：

---

## FM5 - CMS/FongMi 归一补强

**目标**：补齐 native CMS 在 FongMi 口径下的分类、filters、首页、详情补图与字段回填，不引入 sidecar。

**提示词**

```text
目标：增强 native CMS Provider 的 FongMi 兼容行为。

范围：
1. CMS 首页/分类解析支持：
   - class
   - filters
   - list
   - page/pagecount/total
2. 对 XML/JSON 的字段别名做保守兼容，复用现有 cms_parse.go，不写 ad hoc string parser。
3. 支持分类白名单过滤。
4. 支持首页 list 缺 poster 时按 FongMi fetchPic 思路批量 detail 补图，但必须：
   - 有超时和数量上限；
   - 失败只降级，不拖垮首页；
   - 不写 items。
5. 详情回填继续只写 source_items/source_play_sources。

完成判定：
- go build ./... 通过。
- 如改 web/，cd web && npm run build 通过。
- 不新增测试，不启动服务。
```

**实际落点**：

---

## FM6 - Parser 兼容语义设计与最小落地

**目标**：把 FongMi 的 parse/playUrl/flag/click 语义设计清楚，并只落服务端可控的最小能力。不做 WebView 嗅探。

**提示词**

```text
目标：设计并最小落地 Parser 兼容语义，补齐 parse=1 的服务端可控路径。

范围：
1. 先输出设计说明，再改代码：
   - TVBox parse type 0/1/2/3/4 在 FYMS 的支持矩阵；
   - flag 匹配策略；
   - playUrl=json:/parse: 前缀策略；
   - click/header 如何传递；
   - 不支持 WebView 嗅探的降级说明。
2. 代码只落最小安全路径：
   - type=1 JSON parser URL 模板；
   - flag 过滤；
   - header 传递；
   - parse 结果 URL 继续 ValidateOutboundURL。
3. type=0 WebView、type=3 mix、type=4 super parse 如无法服务端安全实现，保持 unsupported，不伪装。
4. 前端解析器页展示支持矩阵与 unsupported 原因。

完成判定：
- go build ./... 通过。
- cd web && npm run build 通过。
- 设计说明与代码 commit 分开。
```

**实际落点**：
