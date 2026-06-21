# Phase 1 开发任务与 Codex 提示词

> 配合 [Phase1-在线优先-实施规划.md](./Phase1-在线优先-实施规划.md)（下称「总纲」）使用。
> 面向 Codex 目标（agentic）模式：每个任务一段自包含提示词，按顺序串行执行。
> 串行原因：多任务都会改动路由注册、`AppState`、共享 handler 文件，串行可避免合并冲突。

---

## 使用方法

1. 每次执行**一个任务**：把下面「§A 共享前置约束」整段 + 该任务的「提示词」拼在一起，投给 Codex。
2. Codex 跑完后，你本地编译 / IDE 诊断 / 手动验证「完成判定」。通过再进入下一个任务。
3. **T4 跑完后必须做 P0 PoC 校验**（见 T4），这一步决定整个形态是否成立——不通过不要继续往后铺。

---

## §A 共享前置约束（每个任务都要带上）

```
你在为 FYMS 开发。FYMS 是 Go + Gin + PostgreSQL(pgx/v5) + Redis + Vue3 的 Emby 兼容媒体服务器，
前端通过 //go:embed 嵌入。本次开发的权威设计文档是
docs/universal-media/Phase1-在线优先-实施规划.md（务必先完整阅读，下称「总纲」），
背景参考 docs/universal-media/架构草案.md 与 docs/universal-media/元数据融合与媒体库组织.md。

硬性约束（违反即不合格）：
1. 允许跑编译与构建来自校验：用 go build ./... 确认编译通过；改动前端时 cd web && npm run build 确认构建成功
   （Go 通过 //go:embed 嵌入 web/dist，改前端后必须能构建）。每个任务结束前必须保证编译与构建是绿的，不绿不算完成。
   仍然禁止：go run / 启动服务、go test 或任何测试、npm run dev、以及写测试文件或测试代码。运行期功能由我手动验证。
2. 不要写测试文件、不要写测试代码。
3. 所有注释、提交说明、与我的交流一律用中文。
4. 防止大文件：保持文件职责单一。单文件接近 800 行或职责开始混杂时，拆到同 package 下按领域命名的文件。
   路由注册、请求解析、业务编排、SQL、DTO/映射、外部客户端、后台任务状态尽量分文件。
   不要因为「改动方便」继续把逻辑堆进 library.go / compat.go / tmdb.go / item.go 等入口文件。
5. 数据库迁移：只新增 migrations/NNN_*.sql（序号 = 现有最大号+1），用 IF NOT EXISTS 保证幂等；
   已执行过的迁移文件一律不改。
6. 新增在线来源能力，绝不改动现有 items/users/libraries/persons 的主键与既有 Emby 主链路；
   在线内容绝不写入 items 表（总纲 §2.1 存储分离、出口统一）。
7. 复用现有模式而非另造：参照 internal/models/platform.go（虚拟库）、internal/services/tmdb.go
   的 sharedTmdbLimiter（限流）、internal/repository/* 的 repository 写法、internal/handlers/* 的分域 handler。
8. 所有对外路由按现有约定双注册（根分组 + /emby 前缀），与现有 handler 一致。
9. 提交（commit）按代码/功能边界增量提交，禁止最后攒一个大 commit：每完成一个内聚单元就提交一次，例如
   「每张迁移一个 commit」「数据访问层一个、UUID 工具一个、ResolveEntity 一个」「一组路由一个、对应 DTO 映射一个」。
   每个 commit 信息用中文写明边界（如 feat(source): 新增 source_items 迁移）；保证每个 commit 自身能 go build ./... 通过，
   便于按边界回滚与 review。不要把多个不相关改动塞进同一个 commit，更不要整任务/跨任务一次性提交。

完成后用中文简述：改了哪些文件、新增哪些路由/表/函数、有哪些已知未覆盖点。不要自行扩大范围到本任务之外。
```

---

## §B 任务依赖与顺序

```
T1 迁移(6表)
      │
      ▼
T2 数据访问层 + UUID 工具 + ResolveEntity 分派
      │
      ├──────────────┐
      ▼              ▼
T3 Emby 读出口      T4 播放(Resolve+代理+SSRF)     ← T3+T4 完成后做 P0 PoC（手工 seed 数据）
      │              │
      └──────┬───────┘
             ▼
T5 JSON CMS Provider + 入库归一
             │
             ▼
T6 TVBox 配置导入 + Provider 启停 + 限流 + 健康检查
             │
             ▼
T7 虚拟在线库 CRUD/管理(后端，对标 platform_libraries)
             │
             ▼
T8 前端：来源中心 + 虚拟库管理 UI
             │
             ▼
T9 source_items 缓存 GC + 来源维度可观测性日志
```

---

## §C 总目标提示词（一次性按顺序推动 T1~T9）

> 适合 Codex 目标（agentic）模式：投一次，让它按顺序自驱。
> 但有一个**人工闸门在 T4**——客户端能否看到/播放在线内容只能由人在 Infuse/Emby 上验证，Codex 无法自证，
> 所以总目标里强制要求「做到 T4 代码完成且构建绿，然后停下交接 PoC，不要擅自继续 T5」。
> 即：实际是「自动推到 T4 → 人工 PoC → 再放行 T5~T9」。把下面整段投给 Codex 即可。

```
【总目标】按 docs/universal-media/Phase1-开发任务与提示词.md 顺序实现 FYMS Universal Media Phase 1。
先完整阅读：该文件的「§A 共享前置约束」「§B 顺序」与 T1~T9 各任务，以及权威设计
docs/universal-media/Phase1-在线优先-实施规划.md（总纲）。§A 的全部硬性约束在整个过程持续生效。

【执行方式】严格按 T1 → T2 → T3 → T4 → (人工闸门) → T5 → T6 → T7 → T8 → T9 顺序，一次做一个任务：
1. 进入任务前，把该任务的「目标/要求/完成判定」读清；只做该任务范围内的事，不提前做后续任务。
2. 任务收尾前必须保证 go build ./... 通过；若改了前端，cd web && npm run build 也要成功。构建不绿不算完成，先修再继续。
3. 提交遵守 §A 第 9 条：任务内按代码/功能边界多次增量提交（每个内聚单元一次、中文信息、每个 commit 都能 go build），
   不要把整个任务攒成一个大 commit。任务完成时确保改动已全部提交，并在本文件该任务末尾追加一行
   「实际落点：<改了哪些文件/新增哪些路由·表·函数 + 本任务的 commit 范围>」。
4. 然后再进入下一个任务，直到遇到下面的闸门。

【T4 人工闸门（强制停止点）】完成 T4 且构建绿后，不要继续 T5。改为：
- 产出一份「P0 PoC 交接说明」：包含手工 seed 的 SQL（1 个 expose 的虚拟在线库 + 1 部电影 source_item(direct m3u8)
  + 1 部多集剧集：1 条 series source_item + 若干 source_play_sources），以及让我在 Infuse / Emby 官方 App 上逐项验证的清单
  （能在 View 看到 → 电影直接播 → 剧集逐集导航并播 → 经 /SourcePlay 代理播放成功 → 进度/已看回写 source_user_item_data）。
- 然后停下并明确告诉我「等待 T4 PoC 人工验证结果」。我确认通过后会再发指令放行 T5~T9。

【放行后】我回复「PoC 通过，继续」时，再按同样方式顺序完成 T5 → T9（每个任务构建绿、追加实际落点、单独提交）。

【冲突与边界】任何时刻若发现总纲描述与现有代码冲突、或某要求会违反 §A 约束（尤其：污染 items 表、
改动现有 items/users/libraries/persons 主键与既有 Emby 主链路、写测试、启动服务），立即停下，在产出说明里指出冲突，
不要自行擅改总纲、不要绕过约束、不要扩大范围。每个任务结束用中文汇报：改了什么、构建是否通过、有哪些已知未覆盖点。
```

---

## T1 — 第一批 migration（6 张表）

**依赖**：无　**完成判定**：6 个迁移文件存在、SQL 与总纲 §5 一致、序号正确、幂等。

**提示词**
```
目标：按总纲 §5（5.1~5.6）与 §5.7，新增第一批 6 张表的迁移文件。

要求：
- 在 migrations/ 下新增 6 个文件，序号接现有最大号之后（先列出 migrations/ 确认最大号再编号），
  每张表一个文件，文件名形如 NNN_source_config_imports.sql 等：
  source_config_imports / source_providers / source_items / source_play_sources /
  source_user_item_data / source_library_views。
- 字段、类型、默认值、注释枚举、UNIQUE 约束、索引（含各 public_uuid 唯一索引、
  idx_source_items_kind_region / _provider_seen / _title）严格照总纲 §5 抄。
- 全部用 CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS。
- 外键 REFERENCES items(id)/users(id) 按总纲写明的 ON DELETE 行为。
- 不创建总纲 §6 列为「延后」的任何表（categories/filters/bindings/runtime_artifacts/resolve_cache 等）。

完成判定：6 文件齐全、序号连续正确、SQL 合法可被 database.RunMigrations 顺序执行；go build ./... 通过（迁移在服务启动时执行，本任务不启动服务）。
```

实际落点：新增 migrations/067_source_config_imports.sql、068_source_providers.sql、069_source_items.sql、070_source_play_sources.sql、071_source_user_item_data.sql、072_source_library_views.sql；新增 6 张 source_* 核心表、public_uuid 唯一索引与 source_items 查询索引；T1 commit 范围：c12c4f1..b2d9fd7。

---

## T2 — 数据访问层 + UUID 工具 + ResolveEntity 分派

**依赖**：T1　**完成判定**：新表的 models/repository CRUD 可编译；UUID 工具与 `PlatformVirtualID` 同法；`ResolveEntity` 按 §3.1 分派且本地热路径单查询。

**提示词**
```
目标：为第一批 6 张表建立数据访问层，并实现总纲 §3 的确定性 UUID 工具与 §3.1 的 ResolveEntity 分派。

1. 新建在线来源 package 骨架 internal/source/（Source Bridge 的家），数据访问可放 internal/models +
   internal/repository（沿用现有 raw SQL + repository 模式，参照 platform.go / *_repository.go）。
2. UUID 工具（参照 models.PlatformVirtualID = uuid.NewSHA1(namespace, dimension+"\x00"+matchValue)）：
   为 source_item / source_play_source / source_library_view / episode 各定义独立 namespace 常量，
   按总纲 §3 表与 §4.1、§5.7 的种子定义生成 public_uuid：
   - source_item:  NewSHA1(sourceItemNamespace, siteKey + "\x00" + source_item_id)   // 注意用 siteKey，不混 config_id
   - play_source:  NewSHA1(playSourceNamespace, sourceItemUUID + "\x00" + lineName + "\x00" + episodeKey)
   - source_view:  NewSHA1(sourceLibNamespace, dimension + "\x00" + matchValue)
   - episode:      NewSHA1(episodeNamespace, sourceItemUUID + "\x00" + episodeKey)
3. 各表的 CRUD / upsert：source_items 与 source_play_sources 一律 upsert ON CONFLICT (public_uuid)（§5.7）；
   source_providers 以 source_key 为稳定定位键 upsert。
4. 实现 ResolveEntity(ctx, id string) -> 判别联合 {Kind, LocalUUID|SourceItemID|SourceViewID}（§3.1）：
   - 能 parse UUID：先查 items(id) 命中即返回 local_item（保持本地热路径单查询）；
     落空依次查 platform 虚拟库 → source_items.public_uuid → source_library_views.public_uuid。
   - 纯数字：走现有 emby_id 路径（不要改 models.ResolveToUUID 本身，在新函数里调用/复用它）。
   - 都不命中：返回未找到。
   不要改动 models.ResolveToUUID 与 emby_id 既有两条分支。

完成判定：新表 CRUD + UUID 工具 + ResolveEntity 编译通过（go build ./... 通过）；UUID 生成可在重导入下稳定。
```

---

## T3 — Emby 读出口（source item → Item DTO / 虚拟库 → View / /Items 分派 / Images）

**依赖**：T2　**完成判定**：手工 seed 一条 source_item + 一个 source_library_view 后，`/Users/{}/Views` 能看到在线库、`/Users/{}/Items?ParentId=<viewUUID>` 能列条目、`/Items/{uuid}` 能出详情、`/Items/{uuid}/Images/Primary` 能出图（代理/缓存）。

**提示词**
```
目标：把在线 source item 与虚拟在线库通过 Emby 标准路由暴露给客户端（读路径），不污染 items。

参照现有：internal/handlers/compat/compat_items.go（/Items 查询与 DTO）、
internal/handlers/mediasupport/emby_defaults.go、dto.FormatItemDtoList、
images.go（图片缓存与回退链）、platform 虚拟库在 getUserViews 的挂载方式。

实现：
1. /Users/{}/Views（或现有 getUserViews 等价出口）：把 enabled 且 expose_to_emby=true 的
   source_library_views 作为在线库 View 一起返回，View 的 Id = 其 public_uuid，
   名称用 EffectiveDisplayName 思路（display_name 优先），CollectionType 用视图的 collection_type。
   与现有本地库/平台虚拟库混排（参照 library_display_order 的合并排序思路）。
2. /Items 查询（GET /Users/{}/Items 与 /Items）：当 ParentId 命中某 source_library_view.public_uuid 时，
   按总纲 §4 的「归一列匹配口径」聚合 source_items（WHERE normalized_kind / region，按 dimension 译成 SQL，
   参照 platform 的 virtualDimensionCondition 思路），分页返回包装后的 Emby Item。
3. DTO 包装：写 source item → Emby Item DTO 的映射（独立文件，勿塞进 compat_items.go）。
   Id=public_uuid，Type 按 item_type（Movie/Series），LocationType/IsRemote 等与本地 item 区分。
4. 剧集层级（总纲 §4.1）：Series 详情下 /Items?ParentId=<seriesUUID> 把该 series 的 source_play_sources
   合成为 Episode 子 Item（Episode Id = episode public_uuid）。
5. /Items/{id} 详情、/Items/{id}/Images/*：接 ResolveEntity 分派；source 命中时走在线 DTO 与图片代理/缓存，
   图片缓存键含 provider + source_item + poster_url_hash（总纲 §8 图片小节）。本地命中时保持现状不变。
6. UserData：在线 item 的 UserData 读自 source_user_item_data；本地 item 不变。

约束：所有改动只在 UUID 经 ResolveEntity 判为 source 时生效；本地 item/库路径行为零变化。
不实现写入/播放（T4 负责）；本任务只读。

完成判定：手工往新表 INSERT 一条 source_item + 一个 expose 的 source_library_view，
用 Emby token 调 /Users/{}/Views、/Users/{}/Items?ParentId=、/Items/{uuid}、/Items/{uuid}/Images/Primary 均正常（go build ./... 通过；运行期由我手动验证）。
```

---

## T4 — 播放：Resolve + Redis 短缓存 + 代理端点 + SSRF + 在线 PlaybackInfo　【P0 PoC 检查点】

**依赖**：T2（可与 T3 并行开发，PoC 校验需 T3）　**完成判定**：见末尾 PoC。

**提示词**
```
目标：实现在线来源的播放出口（总纲 §7、§8），并让 PlaybackInfo 对在线 item 返回代理 MediaSource。

参照现有：internal/handlers/media/videos.go 的 getPlaybackInfo、services/tmdb.go 的限流。
注意 internal/gateway/（网盘网关）仅作 302/统计思路参考：它需配置才生效且只改写本地路径，在线源用独立的
/SourcePlay 代理端点、路径天然不与其相交，不要复用它、也不需要主动规避它。

实现：
1. ResolvePlay：给定 source_play_source，调用其 provider 得到最终可播放 URL + headers。
   Phase 1 只处理 parse_mode=direct（含需 header 的 m3u8/mp4）；
   parse=1/magnet/cloud_share/CSP 一律返回「需 runtime，暂不支持」的归一化错误，不报 500。
2. 解析结果写 Redis 短 TTL（5~30min，键含 play_source public_uuid）；过期或播放失败即删，重新 Resolve。
   不要建 PG 缓存表。
3. 代理端点 GET /SourcePlay/{playSourceUUID}/stream（双注册）：
   ResolveEntity/直查得到 play_source → 取/建 Redis 解析结果 → FYMS 服务端拉流并注入 Referer/UA/Cookie → 回传客户端。
   仅字节中转 + header 注入，绝不转码（项目铁律）。失败回写 source_play_sources 的 success/failure/latency。
4. SSRF 防护（总纲 §8）：Resolve 与代理出站前校验目标 IP，拒绝私网段、回环、169.254.0.0/16（含 169.254.169.254）。
   做成可复用函数，放独立文件。
5. PlaybackInfo：getPlaybackInfo 接 ResolveEntity；当 id 判为 source_item/episode 时，
   返回在线 MediaSource（总纲 §7 的字段：Id=play_source public_uuid，Path=/SourcePlay/{uuid}/stream，
   IsRemote=true，SupportsTranscoding=false），多线路 = 多 MediaSource。
   本地 item 的 PlaybackInfo 行为零变化。Path 不暴露解析前真实 URL；非管理员不下发真实 headers/cookie。

完成判定（务必执行 P0 PoC，决定形态是否成立）：
- 手工 seed：1 个 expose 的虚拟在线库 + 1 部电影 source_item(direct m3u8) + 1 部多集剧集
  (1 条 series source_item + 若干 source_play_sources)。
- 用 Infuse 与 Emby 官方 App 验证：能在 View 看到 → 电影直接播 → 剧集逐集导航并播 →
  经 /SourcePlay 代理播放成功 → 进度/已看回写到 source_user_item_data。
- 若客户端不接受在线 View/Item 或代理播放不通，先停下复盘形态，不要继续 T5+。
```

---

## T5 — JSON CMS Provider + 入库归一

**依赖**：T2（数据层）、T4（Resolve 接口形态）　**完成判定**：给定一个 JSON CMS api，能列分类、搜索、详情、拆线路并写入 source_items / source_play_sources，且 normalized_kind/region 归一正确。

**提示词**
```
目标：在 internal/source/ 下实现「JSON 苹果CMS/VOD」原生 Provider，覆盖总纲 §9 Phase 1 的来源能力。
仅 JSON，XML/DRPY/CSP/网盘 全部不做。

参照调研：docs/universal-media/tvbox-source-research/04-VOD-CMS采集.md（字段、query、vod_play_from/url 拆分规则）。

实现：
1. 定义 Provider 接口（参照架构草案 §7，按需精简）：Categories / Search / Category / Detail / ResolvePlay。
   CMS 请求：分类=GET api 读 class[]；搜索=ac=list&wd=&pg=；分类页=ac=list&t=&pg=；详情=ac=detail&ids=。
   api 已带 query 时用 URL builder 合并参数，不要字符串硬拼。
2. 解析：JSON 结构（code/list/class/page/pagecount）；HTML entity/CDATA 清理；图片 URL 规范化；失败归一化。
   vod_play_from/vod_play_url 按「$$$ 拆线路 → # 拆集 → 第一个 $ 拆标题/URL」拆成 source_play_sources，
   标题空用序号兜底；direct m3u8/mp4 标 parse_mode=direct，其余标 parse_required/unsupported。
3. 归一（总纲 §4 / §4.1）：
   - normalized_kind：按 type_name 启发式 → movie/series/anime/variety/documentary/...（映射表放独立文件，便于后续扩展）。
   - region：按 vod_area → CN/HK/TW/US/JP/KR/EU/Foreign/...。
   - raw 保存原始 vod_* 字段。
4. 入库：搜索/分类结果写轻量 source_items 快照；打开详情再补 detail_loaded=true 与 source_play_sources。
   全部走 T2 的 upsert(ON CONFLICT public_uuid)。剧集按 §4.1：Series=1 条 source_item，分集=play_sources。

约束：搜索/详情结果绝不写 items。Provider 的所有外部请求带 timeout 与错误归一化。

完成判定：对一个真实 JSON CMS api（我会提供）能跑通 分类→搜索→详情→拆线路→入库，归一字段正确（go build ./... 通过；运行期由我手动验证）。
```

---

## T6 — TVBox 配置导入 + Provider 启停 + 限流 + 健康检查

**依赖**：T5　**完成判定**：能导入 TVBox 配置 JSON、按 site 解析出 cms_vod provider 入库、site key 维度 upsert/supersede（§5.7 方案 A）、配置包/单 provider 可启停、每 provider 限流、健康检查可用。

**提示词**
```
目标：实现 TVBox 配置导入与 Provider 生命周期管理（总纲 §5.1/§5.2/§5.7 方案 A、§8 限流）。

参照调研：tvbox-source-research/01-配置总览.md（TVBox 字段→FYMS 映射）、06-FYMS迁移实施手册.md（解析顺序）。

实现：
1. TVBox config loader：支持远程 URL 与粘贴 JSON；解析 spider/sites/parses/lives/rules/flags；
   保存原始 JSON 到 source_config_imports.raw_config；算 content_sha256；ext 支持 string/object。
2. site → provider 归一：Phase 1 只接纳 api 指向 provide/vod 的 JSON CMS（provider_kind=cms_vod,
   runtime_kind=native_cms）。其余（csp_*/.js/.py/live/parser）解析出来但落库标记为 runtime_required/不可用，
   不报错、不创建可用 provider（为后续阶段留位）。
3. 导入流程按 §5.7 方案 A：以 (source_type, site key) 为身份。content_sha256 变化时新建/更新 config 行，
   把同 site key 的旧配置置 import_status=superseded；provider 行复用（按 source_key UPDATE 改挂新 config_id），
   不重建、不改变 public_uuid。
4. 启停：effective_enabled = config.enabled AND provider.enabled，在查询/搜索/播放各路径统一生效。
5. per-provider 限流：参照 services/tmdb.go 的 sharedTmdbLimiter，为每个 provider 建独立 rate.Limiter，
   所有外部调用经其 Wait。
6. 健康检查：对 provider 发一次轻量探活（如取分类），写 health_status/last_check_at/last_error。
7. 后端路由（双注册）：
   POST /SourceConfigs/ImportTVBox、GET /SourceConfigs、POST /SourceConfigs/{id}/Enable|Disable、
   GET /SourceProviders、POST /SourceProviders/{id}/Enable|Disable、
   POST /SourceProviders/{id}/HealthCheck、POST /SourceProviders/{id}/Search、GET /SourceProviders/{id}/Categories。
   路由解析/编排/SQL 分文件，勿堆单文件。

完成判定：导入示例 TVBox 配置后 cms_vod provider 正确入库并可启停/探活/搜索；重导入同 site key 不换 ID（go build ./... 通过；运行期由我手动验证）。
```

---

## T7 — 虚拟在线库 CRUD / 管理（后端，对标 platform_libraries）

**依赖**：T3、T5　**完成判定**：能创建/改名/换封面/调序/启停在线虚拟库，维度聚合用归一列，public_uuid 稳定，可控制是否 expose_to_emby。

**提示词**
```
目标：实现在线虚拟库（source_library_views）的后台管理，能力对标现有平台虚拟库（platform_libraries）。

参照现有：internal/models/platform.go 与平台库的后台路由族（POST /Library/Platforms/*：增删、Rename、
Image/Generate、CoverArt、DisplayOrder），内部 coverart registry（/Library/CoverArt/Styles）、
library_display_order 混排。在线库尽量复用同一套 coverart 与排序机制。

实现：
1. CRUD：创建/编辑/删除 source_library_views。dimension ∈ {normalized_kind, region, kind_region, provider, custom}；
   按总纲 §4 匹配口径把 (dimension, match_value/match_values, filter) 译成对 source_items 的聚合 SQL
   （归一维度走 normalized_kind/region 列；custom 用 match_values[]）。
2. 显示名/封面/排序：display_name 自定义名；复用 coverart registry 生成封面（接收 {Style, Options}）、
   清除封面；sort_order 调序并参与与本地库/平台库的混排。
3. expose_to_emby 开关：默认 false；为 true 时才进 T3 的 /Views 出口。
4. 维度值发现（便于前端选值）：提供一个「列出某 dimension 下可用值及计数」的接口
   （参照 platform 的 DiscoverDimensionValues 思路，查 source_items）。
5. 后端路由（双注册）：GET/POST /Library/SourceViews、PUT/DELETE /Library/SourceViews/{id}、
   POST /Library/SourceViews/{id}/Rename、POST /Library/SourceViews/{id}/Image/Generate、
   DELETE /Library/SourceViews/{id}/Image、POST /Library/SourceViews/DisplayOrder。
   命名与现有 /Library/Platforms/* 风格保持一致。

完成判定：能建出「国产电影/国外电影/国产剧/国外剧」及任意自定义库，聚合结果正确、封面与排序可用、
expose 开关控制 Emby 可见性（go build ./... 通过；运行期由我手动验证）。
```

---

## T8 — 前端：来源中心 + 虚拟库管理 UI

**依赖**：T6、T7　**完成判定**：后台可视化导入配置、管理 provider、测试搜索/健康、管理在线虚拟库。

**提示词**
```
目标：在 web/ 前端（Vue3）新增「来源中心」与「在线虚拟库管理」页面，对接 T6/T7 的后端接口。

参照现有：web/ 里平台虚拟库管理页、媒体库管理页的组件与请求封装风格，保持一致的设计语言与 API 调用方式。
遵守项目前端规范（Composition API + <script setup>）。注意项目曾有「多根模板过渡白屏」问题
（见记忆/文档）：弹窗等不要作为根级兄弟节点，要放进单根内。

实现：
1. 来源中心：
   - TVBox 配置导入（URL / 粘贴 JSON），导入结果与解析出的 provider 列表。
   - 配置包列表 + 启停；provider 列表 + 启停 + 健康状态(成功率/最近错误) + 手动探活 + 搜索测试 + 分类查看。
2. 在线虚拟库管理：
   - 列表/新建/编辑/删除；选择 dimension 与值（调维度值发现接口）；自定义显示名；
   - 封面风格选择与生成/清除（复用 CoverArt/Styles 下拉）；拖拽调序；expose_to_emby 开关。
3. 错误展示：来源名 / 动作 / 耗时 / 错误类型 / 可重试建议（对接后端归一化错误）。

约束：不动现有页面既有功能；新页面/组件按领域拆分，避免超大单文件。

完成判定：能在后台完成「导入配置→看到 provider→测试搜索→建虚拟库→出现在 Emby」全流程（go build ./... 通过；运行期由我手动验证）。
```

---

## T9 — source_items 缓存 GC + 来源维度可观测性

**依赖**：T5　**完成判定**：有后台周期任务按 last_seen_at 淘汰过期 source_items；新增 source/provider/resolver 日志分类与关键指标。

**提示词**
```
目标：补齐总纲 §8 的运维项：source_items 缓存 GC 与来源维度可观测性。

参照现有：internal/services/ 的周期任务模式（如 metrics.go / scrape_worker.go 的启动与调度）、
现有分类日志体系。

实现：
1. 缓存 GC：后台周期任务，按 last_seen_at 淘汰长期未被访问/刷新且未被任何虚拟库命中的 source_items
   （连带其 source_play_sources 由外键级联）。阈值可配置，默认给一个保守值；日志记录清理量。
   注意：被 expose 的虚拟库当前可命中的条目不应被误删——以 last_seen_at 为主依据并留足窗口。
2. 可观测性：新增 source / provider / resolver 三个日志分类；
   在搜索/详情/Resolve/健康/代理路径记录 provider_id、action、latency、status、error_type、cache_hit；
   敏感 token/cookie/播放 URL 明文不入日志（只记 hash/脱敏）。

完成判定：GC 任务随服务启动并按周期运行、有清理日志；来源各动作有结构化日志与指标（go build ./... 通过）。
```

---

## 备注

- T1~T4 是关键路径与形态验证；T5~T6 让数据真正进来；T7~T8 是自定义分类的产品化；T9 是运维收尾。
- 若 Codex 在某任务里发现总纲描述与现有代码冲突，应**停下并在产出说明里指出**，不要自行擅改总纲或绕过约束。
- 每完成一个任务，建议在本文件对应任务后追加一行「实际落点」记录（改了哪些文件/路由），方便后续任务衔接与回溯。
