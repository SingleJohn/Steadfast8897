# 来源中心目标模式重构与 Codex 提示词

> 本文用于后续 Codex 目标模式执行。目标是重构来源中心的管理信息架构和页面功能，重点提升配置、Provider、在线虚拟库、解析器、审计的可管理性与可用性。
> 本轮明确不改 JS/CSP/CMS 运行时核心执行逻辑；如遇到必须触达运行时语义的需求，先停下说明。

---

## 目标架构

来源中心按四层组织：

1. **配置 Config**
   - TVBox/CMS 源清单导入包，是顶层管理单元。
   - 配置下挂 Provider、Parser、runtime artifact、导入状态与统计。
   - 配置启停影响其下 Provider/Parser 的 effective 状态。

2. **Provider**
   - 单个站点或数据源运行单元。
   - 负责探活、分类、搜索、详情、播放线路发现。
   - effective enabled = `source_config_imports.enabled && source_providers.enabled`。

3. **在线虚拟库 Source Library View**
   - 面向 Emby/客户端展示的在线库视图。
   - 基于 `source_library_views` 组织 `source_items`，不写 `items`。
   - 现有 `provider_ids` 支持不同在线库选择不同 Provider。

4. **审计 Audit**
   - 独立页面管理运行调用、错误、artifact 信任、Provider 探活结果。
   - 列表只展示脱敏摘要，敏感 URL/token/cookie 不明文外泄。

---

## 产品页面规划

来源中心改为后台子导航，而不是把所有 panel 纵向堆在一页。

1. **总览**
   - 展示配置数、Provider 总数、启用数、可用/失败/未探活数、在线库暴露数、最近错误。
   - 主操作：导入配置、批量探活、进入审计。

2. **配置**
   - 左侧配置列表，右侧配置详情。
   - 详情展示该配置下 Provider、Parser、导入来源、更新时间、启用状态、统计摘要。
   - 支持配置启停、删除影响预览、级联删除。

3. **Provider**
   - 表格管理全部 Provider。
   - 支持按配置、运行时、健康状态、启用状态、关键词筛选。
   - 支持批量启用/停用、批量探活、单个分类查看、单站搜索测试。
   - 分类 drawer 解释其用途：站点栏目、可用于站点浏览和在线库组织辅助。

4. **在线库**
   - 做成“库构建器”：
     - 选择库类型/维度；
     - 选择匹配值或多匹配值；
     - 选择 Provider 范围；
     - 设置封面、排序、是否暴露给 Emby；
     - 保存前预览命中数量。
   - Provider 选择必须可自由多选。
   - 解析器本轮先明确为全局解析器；不要假装库级解析器已经生效。

5. **解析器**
   - 独立管理 `source_parsers`。
   - 显示所属配置、类型、启用、信任状态、最后检查、错误。
   - 如需 Provider 级或库级解析器策略，另开设计任务。

6. **审计**
   - 独立页面。
   - 支持按时间、Provider、method、status、error_type、runtime_kind 筛选。
   - Runtime artifact 信任可以作为审计页 tab。

---

## 当前代码事实

- 前端入口：
  - `web/src/pages/SourceCenterPage.vue`
  - `web/src/composables/useSourceCenter.ts`
  - `web/src/api/source.ts`
  - `web/src/components/source-center/*`
- 后端 admin 路由：
  - `internal/handlers/admin/source_routes.go`
  - `internal/handlers/admin/source_handlers.go`
  - `internal/handlers/admin/source_runtime_handlers.go`
- Source repository：
  - `internal/repository/source_config_repository.go`
  - `internal/repository/source_provider_repository.go`
  - `internal/repository/source_parser_repository.go`
  - `internal/repository/source_runtime_invocation_repository.go`
  - `internal/repository/source_view_repository.go`
- 关键表：
  - `source_config_imports`
  - `source_providers`
  - `source_parsers`
  - `source_runtime_artifacts`
  - `source_runtime_invocations`
  - `source_library_views`
  - `source_items`
  - `source_play_sources`
- 重要边界：
  - `source_providers.config_id` 当前是 `ON DELETE SET NULL`，配置级联删除需要显式事务逻辑，不能假设数据库自动 cascade。
  - `source_library_views.provider_ids` 已支持在线库按 Provider 过滤。
  - `ParserResolver` 当前按全局启用 Parser 顺序尝试，不带在线库上下文。

---

## §A 共享前置约束（每个任务都要带上）

```text
你在为 FYMS 开发。FYMS 是 Go + Gin + PostgreSQL(pgx/v5) + Redis + Vue3 的 Emby 兼容媒体服务器，前端通过 //go:embed 嵌入。

本次任务权威文档是：
- docs/universal-media/SourceCenter-目标模式重构与提示词.md
- 相关背景参考 docs/universal-media/Phase1-在线优先-实施规划.md
- 相关背景参考 docs/universal-media/元数据融合与媒体库组织.md

硬性约束：
1. 允许并要求跑编译与构建校验：
   - 后端改动后必须运行 go build ./...，确保编译成功。
   - 前端改动后必须运行 cd web && npm run build，确保构建成功。
   - 同时改后端和前端时两个都要跑。
   - 构建不绿不算完成，必须先修到绿。
2. 仍然禁止：go run / 启动服务、go test 或任何测试、npm run dev、写测试文件或测试代码。运行期功能由用户手动验证。
3. 不改 JS/CSP/CMS 运行时核心执行逻辑。本轮只做管理 API、页面重构、审计查询、配置/Provider/在线库管理。若发现必须改运行时才能完成，先停下说明。
4. 在线内容绝不写入 items 表，不改现有 items/users/libraries/persons 主键与既有 Emby 主链路。
5. 数据库迁移只新增 migrations/NNN_*.sql，序号 = 现有最大号 + 1；已执行迁移文件不改；迁移使用 IF NOT EXISTS 或可重复执行写法。
6. 防止大文件：单文件接近 800 行或职责混杂时，拆到同 package / 同目录下按领域命名的文件。不要继续把逻辑堆进大入口文件。
7. 前端使用 Vue3 Composition API，复用现有 Naive UI / 项目组件风格；调用 $frontend-design 与 $web-design-guidelines 的设计口径，保证实用、清晰、美观、可访问。
8. 危险操作必须有影响预览和二次确认，尤其是删除配置、级联删除 Provider/Parser/source_items/play_sources。
9. 敏感信息不明文展示或入日志：URL/token/cookie/header 只能脱敏或 hash。
10. 提交必须按功能边界增量提交，禁止最后攒一个大 commit：
    - 每完成一个内聚单元就提交一次。
    - 每个 commit 信息用中文，说明功能边界。
    - 每个 commit 自身必须 go build ./... 通过；若该 commit 涉及前端，也必须 cd web && npm run build 通过。
    - 提交前用 git diff --cached --name-only 核对只 stage 当前任务文件，不 stage 无关脏文件。
    - 不要把后端 API、前端大重构、审计页、在线库构建器全部塞进一个 commit。

完成每个任务后用中文汇报：
- 改了哪些文件；
- 新增/调整哪些 API、页面、字段或迁移；
- go build ./... 是否通过；
- cd web && npm run build 是否通过；
- commit 列表；
- 已知未覆盖点和需要用户手动验证的点。
```

---

## §B 任务顺序

```text
SC1 后端配置层管理 API
  - 配置影响预览
  - 配置级联删除
  - 配置详情聚合统计

SC2 后端 Provider 批量管理 API
  - Provider 列表筛选
  - 批量启停
  - 批量探活
  - 分类语义返回与错误归一

SC3 后端审计查询增强
  - runtime invocation 筛选扩展
  - artifact 信任管理入口整理
  - summary-first DTO

SC4 前端信息架构重构
  - 来源中心子导航
  - 总览/配置/Provider/在线库/解析器/审计页面骨架
  - URL tab/filter 状态

SC5 在线库构建器
  - Provider 多选
  - 维度发现与命中预览
  - 暴露给 Emby、排序、封面操作
  - 明确 Parser 为全局策略

SC6 前端体验与可访问性收口
  - 批量操作确认
  - 空状态/错误状态/加载状态
  - 表格筛选/分页
  - 视觉 polish 与 Web Interface Guidelines 审查
```

---

## §C 总目标提示词

```text
【总目标】按 docs/universal-media/SourceCenter-目标模式重构与提示词.md 顺序重构 FYMS 来源中心管理功能。

先完整阅读：
1. docs/universal-media/SourceCenter-目标模式重构与提示词.md
2. docs/universal-media/Phase1-在线优先-实施规划.md 中 Source Bridge / 在线虚拟库相关章节
3. docs/universal-media/元数据融合与媒体库组织.md 中媒体库组织原则
4. 当前代码：
   - internal/handlers/admin/source_routes.go
   - internal/handlers/admin/source_handlers.go
   - internal/handlers/admin/source_runtime_handlers.go
   - internal/repository/source_*_repository.go
   - web/src/pages/SourceCenterPage.vue
   - web/src/composables/useSourceCenter.ts
   - web/src/api/source.ts
   - web/src/components/source-center/*

§A 共享前置约束全程生效。

【执行方式】
严格按 SC1 → SC2 → SC3 → SC4 → SC5 → SC6 顺序推进，一次只做一个任务。每个任务内部可以按更小代码边界拆 commit。

每个任务收尾必须：
1. go build ./... 通过。
2. 如果改了 web/，cd web && npm run build 通过。
3. 按功能边界提交中文 commit，禁止大 commit。
4. 在本文对应任务末尾追加“实际落点”，写清文件、API、构建结果和 commit hash。

【边界】
本轮不改 JS/CSP/CMS 运行时核心逻辑，不写 tests，不启动服务。
如果某需求必须改变运行时语义，例如“在线库级解析器真正生效”，先停下给设计说明，不要擅自改播放链路。
```

---

## SC1 - 后端配置层管理 API

**目标**：让配置成为一等管理对象，支持详情、影响预览和级联删除。

**建议 commit 边界**：
1. repository 查询与删除事务。
2. admin handler / route。
3. 前端 API 类型声明（如本任务顺手补，不做页面）。

**提示词**

```text
目标：实现来源中心配置层管理 API，不改运行时。

范围：
1. 新增配置详情/影响预览：
   - GET /SourceConfigs/:id
   - GET /SourceConfigs/:id/Impact
   Impact 至少返回 provider_count、parser_count、source_item_count、play_source_count、runtime_artifact_count、runtime_invocation_count。
2. 新增配置级联删除：
   - DELETE /SourceConfigs/:id?confirm=true
   - 必须先有 Impact 能力。
   - 删除逻辑用显式事务，不依赖现有 ON DELETE SET NULL。
   - 删除该配置下 providers/parsers 及它们派生的 source_items/source_play_sources 等可重建数据。
   - runtime_invocations 建议保留审计记录并置空 provider_id，除非现有约束已经自然 SET NULL。
3. 不删除本地 items，不影响本地媒体库。
4. 删除前后都要保持 source_library_views 安全：若某 view.provider_ids 引用了被删除 provider，需要清理这些 id 或在 Impact 中阻止并要求用户确认策略。

完成判定：
- go build ./... 通过。
- 本任务不需要 npm build，除非改了 web/。
- 至少 2 个中文 commit：repository/事务一个，handler/API 一个。
```

**实际落点**：
- 文件：`internal/repository/source_config_repository.go`、`internal/repository/source_types.go`、`internal/handlers/admin/source_config_handlers.go`、`internal/handlers/admin/source_routes.go`。
- API：新增 `GET /SourceConfigs/:id`、`GET /SourceConfigs/:id/Impact`、`DELETE /SourceConfigs/:id?confirm=true`。
- 行为：Impact 返回 Provider、Parser、source_items、source_play_sources、runtime_artifacts、runtime_invocations 与受影响在线库统计；级联删除显式事务删除配置下 Provider/Parser，清理 `source_library_views.provider_ids` 中被删 Provider，保留 runtime invocation 审计并由外键置空 provider。
- 构建：`go build ./...` 通过；本任务未改 `web/`，未运行 `cd web && npm run build`。
- Commit：`98e420d3` 来源配置补充影响统计与级联删除事务；`919c00c6` 来源配置开放详情影响与删除接口。

---

## SC2 - 后端 Provider 批量管理 API

**目标**：Provider 支持筛选、批量启停、批量探活，并把分类语义稳定给前端。

**建议 commit 边界**：
1. repository/list options 扩展。
2. batch health service/handler。
3. API DTO 与错误归一。

**提示词**

```text
目标：增强 Provider 管理 API，不改 Provider 运行时实现。

范围：
1. 扩展 GET /SourceProviders 查询参数：
   - config_id
   - enabled
   - health_status
   - runtime_kind
   - provider_kind
   - keyword
   - limit/offset
2. 新增批量启停：
   - POST /SourceProviders/BatchEnable
   - POST /SourceProviders/BatchDisable
   body 使用 provider_ids。
3. 新增批量探活：
   - POST /SourceProviders/BatchHealthCheck
   - 限制并发，建议 3-5。
   - 单个 provider 失败不能中断整批。
   - 返回每个 provider 的 status、error_type、message、latency_ms、categories_count。
4. 分类接口返回稳定 DTO：
   - id/name/count/source 字段可选。
   - 前端需要能解释分类是“上游站点栏目”，用于站点浏览与在线库组织辅助。
5. 敏感信息仍脱敏，不返回 token/cookie/header 明文。

完成判定：
- go build ./... 通过。
- 本任务不需要 npm build，除非改了 web/。
- 按 repository、handler/API、DTO 小步中文 commit。
```

**实际落点**：
- 文件：`internal/repository/source_provider_repository.go`、`internal/repository/source_types.go`、`internal/repository/source_scan_repository.go`、`internal/handlers/admin/source_provider_handlers.go`、`internal/handlers/admin/source_handlers.go`、`internal/handlers/admin/source_routes.go`。
- API：扩展 `GET /SourceProviders` 查询参数 `config_id/enabled/health_status/runtime_kind/provider_kind/keyword/limit/offset`；新增 `POST /SourceProviders/BatchEnable`、`POST /SourceProviders/BatchDisable`、`POST /SourceProviders/BatchHealthCheck`。
- 行为：批量启停按 `provider_ids` 更新并返回脱敏 Provider DTO；批量探活并发限制为 4，单 Provider 失败不影响整批，返回 `provider_id/provider_name/status/error_type/message/latency_ms/categories_count`；分类接口返回稳定 `id/name/count/source` DTO，并附带“上游站点栏目，可用于站点浏览与在线库组织辅助”说明。
- 安全：Provider 管理响应不返回 `headers/ext/raw_site`，`api` 去除 query/user/fragment，额外返回 `APIHash` 便于定位。
- 构建：`go build ./...` 通过；本任务未改 `web/`，未运行 `cd web && npm run build`。
- Commit：`6673c5cf` 来源Provider支持筛选与批量启停；`247fbc8f` 来源Provider开放批量管理接口。

---

## SC3 - 后端审计查询增强

**目标**：支持独立审计页面需要的筛选和 summary-first 数据。

**建议 commit 边界**：
1. invocation repository 筛选扩展。
2. artifact / invocation DTO 整理。
3. routes / API。

**提示词**

```text
目标：增强来源中心审计 API，供独立审计页面使用。

范围：
1. 扩展 GET /SourceRuntime/Invocations：
   - provider_id
   - method
   - status
   - error_type
   - runtime_kind
   - start_time/end_time
   - limit/offset
2. 返回 summary-first DTO：
   - 列表只放 provider_id/provider_name/runtime_kind/method/status/error_type/duration_ms/invoked_at/url_hash。
   - raw/detail 不在列表大字段里展开。
3. 如需详情，新增 GET /SourceRuntime/Invocations/:id。
4. Artifact 列表保留信任操作，但页面语义归到审计页。
5. 不展示敏感 URL 明文。

完成判定：
- go build ./... 通过。
- 本任务不需要 npm build，除非改了 web/。
- 按查询扩展、DTO/handler 小步中文 commit。
```

**实际落点**：
- 文件：`internal/repository/source_runtime_invocation_repository.go`、`internal/repository/source_types.go`、`internal/handlers/admin/source_runtime_handlers.go`、`internal/handlers/admin/source_routes.go`。
- API：扩展 `GET /SourceRuntime/Invocations` 查询参数 `provider_id/method/status/error_type/runtime_kind/start_time/end_time/limit/offset`；新增 `GET /SourceRuntime/Invocations/:id`。
- 行为：调用列表返回 summary-first DTO，仅包含 `id/provider_id/provider_name/runtime_kind/method/status/error_type/duration_ms/invoked_at/url_hash`；详情接口单独返回 `error_message/engine_ok/worker_pid/artifact_ids/raw`。
- Artifact：`GET /SourceRuntime/Artifacts` 与信任返回改为审计 DTO，保留信任操作所需字段，不返回 `local_path/raw`，`source_url/base_url` 脱敏并提供 `SourceURLHash`。
- 构建：`go build ./...` 通过；本任务未改 `web/`，未运行 `cd web && npm run build`。
- Commit：`be86c1cb` 来源审计调用记录支持筛选与详情查询；`6f9299f5` 来源审计接口返回摘要并补详情。

---

## SC4 - 前端信息架构重构

**目标**：把来源中心从单页 panel 堆叠改为子导航工作台。

**建议 commit 边界**：
1. 路由/页面 shell 与子导航。
2. 数据 composable 拆分。
3. 旧 panel 平滑迁移。

**提示词**

```text
目标：重构 web 来源中心信息架构，优先清晰实用，不改变运行时。

必须使用：
- $frontend-design：设计为后台运维工作台，克制、密集、可扫描，不做营销页，不做大 hero。
- $web-design-guidelines：按最新 Web Interface Guidelines 审查可访问性、状态表达、URL 状态、表单标签、危险操作确认。

范围：
1. SourceCenterPage 改为子导航布局：
   - 总览
   - 配置
   - Provider
   - 在线库
   - 解析器
   - 审计
2. tab/filter 状态尽量进入 URL query，刷新页面不丢当前工作上下文。
3. useSourceCenter.ts 如过大，拆成按领域 composable：
   - useSourceConfigs
   - useSourceProviders
   - useSourceViews
   - useSourceRuntimeAudit
4. 页面要有清晰空状态、加载状态、错误状态。
5. 不要把卡片套卡片；后台页面以表格、分栏、drawer、toolbar 为主。

完成判定：
- go build ./... 通过。
- cd web && npm run build 通过。
- 至少按页面骨架、composable 拆分、旧功能迁移分别中文 commit。
```

**实际落点**：待执行时填写。

---

## SC5 - 在线库构建器

**目标**：让在线虚拟库更好用，并支持不同库自由选择不同 Provider。

**建议 commit 边界**：
1. 后端命中预览 API。
2. 前端在线库构建器 UI。
3. provider 多选与保存联调。

**提示词**

```text
目标：重做在线库管理体验，做成库构建器。

范围：
1. 后端新增或复用预览能力：
   - 输入 dimension/match_value/match_values/provider_ids/filter。
   - 返回 item_count、provider 分布、示例 items。
   - 不写任何数据，只预览。
2. 前端在线库构建器：
   - 选择维度 normalized_kind/region/kind_region/provider/custom。
   - 支持维度值发现。
   - 支持 Provider 多选，并显示每个 Provider 健康状态。
   - 保存前显示命中数量。
   - 支持 enabled/expose_to_emby/sort_order/cover 操作。
3. 文案讲清楚：
   - 在线库是 Emby 可见的组织视图，不是配置，也不是 Provider。
   - Provider 选择会限制该库收录哪些站点的数据。
   - Parser 本轮是全局播放解析器，不做库级解析器生效。
4. 如果发现库级 parser 必须进入播放上下文，停下说明，不要擅改播放链路。

完成判定：
- go build ./... 通过。
- cd web && npm run build 通过。
- 按后端预览 API、前端构建器、保存联调小步中文 commit。
```

**实际落点**：待执行时填写。

---

## SC6 - 前端体验与可访问性收口

**目标**：完成来源中心管理后台的实用性、美观性和可访问性审查。

**建议 commit 边界**：
1. 交互与状态修复。
2. 视觉 polish。
3. guidelines 审查修复。

**提示词**

```text
目标：按 $frontend-design 与 $web-design-guidelines 收口来源中心 UI/UX。

范围：
1. 批量操作必须有明确选择计数、影响说明、二次确认。
2. 删除配置必须展示 Impact 摘要，用户确认后才提交。
3. 表格支持合理分页/筛选/搜索，长列表不卡 UI。
4. 所有图标按钮要有可访问标签或 tooltip。
5. 表单控件有 label，错误提示贴近字段。
6. 页面在窄屏下不能文本溢出或按钮挤压。
7. 审计和 Provider 错误信息要可复制，但敏感字段不明文。
8. 视觉风格：后台运维工作台，信息密度高、层级清楚、色彩克制，避免大面积单色、装饰性渐变和营销式 hero。

完成判定：
- go build ./... 通过。
- cd web && npm run build 通过。
- 输出一次简短 UI 审查结论，列出已修复的问题和仍需人工浏览器验证的点。
- 按交互修复、视觉 polish、审查修复小步中文 commit。
```

**实际落点**：待执行时填写。
