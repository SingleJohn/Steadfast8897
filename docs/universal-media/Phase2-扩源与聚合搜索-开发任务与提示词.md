# Phase 2 扩源与聚合搜索 开发任务与提示词

> 配合 [Phase1-在线优先-实施规划.md](./Phase1-在线优先-实施规划.md)（总纲）与
> [技术债与遗留项](./技术债与遗留项.md) 使用。延续 Phase 1 的 Codex 目标模式工作法。

## 范围（已拍板）

Phase 2 = 三条工作流 + 前置技术债清理：

1. **XML CMS 扩源 + 真实 TVBox 配置硬化**：支持 `at/xml` 格式 MacCMS（JSON/XML 自动识别），用真实多站配置端到端跑通并修真实问题。
2. **线路健康排序 + 多线路失败切换**：复用 `success/failure/latency` 做排序，播放失败自动切下一条线路。
3. **跨来源聚合搜索（Federated）**：并发搜全部 provider，归一/去重/排序，接入 Emby 搜索与 Web 前端。

**不做**（留后续）：DRPY JS / CSP sidecar（Phase 3）、AList 目录源、网盘搜索、parse=1 解析器。

延续 Phase 1 不变约束：**在线内容绝不写 items**；存储分离、出口统一；确定性 UUID；代理仅字节中转+header 注入、永不转码。

## §A 共享前置约束

整段沿用 [Phase1-开发任务与提示词.md](./Phase1-开发任务与提示词.md) 的「§A 共享前置约束」（允许 go build/npm build、禁运行服务与测试、中文、防大文件、迁移只增、不污染 items、复用现有模式、路由双注册、按代码边界增量提交），**全程持续生效**。每个任务投喂时把那段 §A 一起带上。

Phase 2 补充：
- 新增来源能力前，先按 TD-2 拆分 `source_repository.go`，新查询加到拆分后的对应文件，不得继续堆大文件。
- 聚合搜索/XML 解析的所有外部请求复用 per-provider limiter（Phase 1 `provider_runtime.go`）与 `ValidateOutboundURL`。

## §B 任务顺序与依赖

```
T10 技术债清理(拆repo + SSRF重定向 + JSONB内联)
      │
      ▼
T11 XML CMS 扩源(JSON/XML 自动识别)
      │
      ├───────────────┐
      ▼               ▼
T12 线路健康排序+失败切换   （T11 完成后）
      │               │
      └──────┬────────┘
             ▼
T13 跨来源聚合搜索(后端：并发/归一/去重/排序/入库)
             │
             ▼
T14 真实 TVBox 配置硬化联调  【人工里程碑：需你提供真实配置 + 客户端验证】
             │
             ▼
T15 聚合搜索接入 Emby + Web 前端  【人工里程碑：客户端/UI 验证】
```

执行：T10→T13 可由 Codex 连续自驱（代码完成 + 构建绿 + 可用 API 自查）；**T14、T15 是人工里程碑**，需要真实外部配置与客户端肉眼验证，到点停下交接。

---

## T10 — 技术债清理（前置）

**依赖**：无　**完成判定**：`source_repository.go` 拆分后均 < 800 行且 go build 通过；代理重定向重校验；admin DTO 的 JSONB 字段内联输出。

**提示词**
```
目标：清理 docs/universal-media/技术债与遗留项.md 的 TD-2 / TD-1 / TD-5，为 Phase 2 加新查询做准备。

1. TD-2（先做）：拆分 internal/repository/source_repository.go（当前约 1419 行）。按域拆成多个同 package 文件，
   如 source_config_repository.go / source_provider_repository.go / source_item_repository.go /
   source_play_repository.go / source_view_repository.go（GC 已在 source_gc_repository.go）。
   保持类型 SourceRepository 与所有导出方法签名不变（方法挂到同一 receiver，分文件即可），调用方零改动。
2. TD-1：internal/handlers/media/source_play.go 的代理 http.Client 增加 CheckRedirect，
   对每一跳重定向 target 调 source.ValidateOutboundURL 校验，拒绝则中断；限制最大重定向次数（如 5）。
3. TD-5：source_repository.go 系列里 JSONB 列（raw_config/ext/categories/headers/capabilities/filter 等）
   扫描用 json.RawMessage 而非 []byte，使 admin DTO 内联输出 JSON 而非 base64；
   确认 GET /SourceConfigs、/SourceProviders、/Library/SourceViews 返回的这些字段是内联 JSON。
（可选）TD-3/TD-4 若顺手可一并做：DialContext 校验实连 IP、设置 Transport.ResponseHeaderTimeout。

完成判定：拆分后各文件 < 800 行、go build ./... 通过；重定向到内网被拒；上述 admin 端点 JSONB 字段不再是 base64。
```

实际落点：拆分 `internal/repository/source_repository.go` 为 `source_types.go`、`source_config_repository.go`、`source_provider_repository.go`、`source_item_repository.go`、`source_play_repository.go`、`source_view_repository.go`、`source_resolve_repository.go`、`source_scan_repository.go`（既有 GC 仍在 `source_gc_repository.go`），各文件均 < 800 行；JSONB 返回字段改为 `json.RawMessage`，admin DTO 可内联 JSON；`internal/handlers/media/source_play.go` 增加每跳重定向 `ValidateOutboundURL` 校验、5 跳上限与 `ResponseHeaderTimeout`。构建：`go build ./...` 通过。T10 commit 范围：fe883b3。

---

## T11 — XML CMS 扩源（JSON/XML 自动识别）

**依赖**：T10　**完成判定**：对 `at/xml` 的 MacCMS api 能列分类/搜索/详情/拆线路并入库，归一与 JSON 路径一致。

**提示词**
```
目标：扩展 Phase 1 的 CMS Provider，支持 MacCMS 的 XML 格式（type=0 / api 形如 .../provide/vod/at/xml/）。
仅 XML，不做 DRPY/CSP/网盘。参照 docs/universal-media/tvbox-source-research/04-VOD-CMS采集.md 的 XML 样本。

参照现有：internal/source/cms_provider.go、cms_parse.go、normalize.go、ingest.go（Phase 1 JSON 实现）。

实现：
1. 格式自动识别：按 api 路径/响应 Content-Type/响应首字符（'<' vs '{'）判定 JSON 还是 XML，复用同一 Provider 接口。
2. XML 解析（rss/list/video，class/ty）：
   <list page/pagecount/...>，<video> 内 id/tid/name/type/dt/note/pic/year/area/actor/director/des/...，
   <class><ty id="">名称</ty>；CDATA 清理；HTML entity 清理；图片 URL 规范化。
   分类来自 <class><ty>；列表/详情字段映射到与 JSON 相同的内部模型。
3. 播放线路：XML 详情里的播放字段（同 vod_play_from/vod_play_url 语义，可能在 dt/dl 节点）按
   $$$ 拆线路 → # 拆集 → 第一个 $ 拆标题/URL 规则拆成 source_play_sources。
4. 归一与入库：完全复用 normalize.go（normalized_kind/region）与 ingest.go 的 upsert(ON CONFLICT public_uuid)，
   XML 与 JSON 走同一套下游，不另写一套入库。剧集仍按总纲 §4.1：Series=1 条 source_item，分集=play_sources。

约束：绝不写 items；外部请求带 timeout、per-provider limiter、错误归一化。

完成判定：给一个真实 at/xml CMS api 能跑通 分类→搜索→详情→拆线路→入库，字段/归一与 JSON 一致（go build 通过；运行期我验证）。
```

实际落点：`internal/source/cms_provider.go` 将 CMS 请求改为 JSON/XML 自动识别（api 路径、Content-Type、响应首字符），出站仍走 `ValidateOutboundURL` 与 provider runtime limiter；`internal/source/cms_parse.go` 新增 MacCMS XML `<rss>/<list>/<class>/<video>/<dl><dd>` 解析，CDATA/HTML entity 清理由既有 `cleanCMSValue` 复用，XML 字段映射到既有 `cmsResponse/cmsVOD`，播放字段统一成 `vod_play_from/vod_play_url` 后复用 `splitCMSPlaySources`、`normalize.go` 与 `ingest.go`；`internal/source/tvbox_importer.go` 将 `provide/vod/at/xml` 纳入 `native_cms`，不再标 runtime_required。未写 items、未引入 sidecar、未编造真实站点运行数据。构建：`go build ./...` 通过。T11 commit 范围：496c9b9。

---

## T12 — 线路健康排序 + 多线路失败切换

**依赖**：T10　**完成判定**：PlaybackInfo 的在线 MediaSources 按线路健康排序；代理播放失败时自动切换该集的下一条线路。

**提示词**
```
目标：让在线播放在多线路下稳定——按健康排序 + 失败自动切换。复用 Phase 1 已有的
source_play_sources.success_count/failure_count/avg_latency_ms/health_status 与回写逻辑。

参照现有：internal/handlers/media/source_playback.go（在线 PlaybackInfo MediaSources）、
source_play.go（/SourcePlay 代理与 MarkPlaySourceSuccess/Failure）、repository/source_repository.go。

实现：
1. 线路排序：同一条目/同一集的多条 source_play_sources，PlaybackInfo 输出 MediaSources 时按
   health_status(健康优先) + 成功率 + avg_latency_ms 排序；direct 可用的排前，unsupported/parse_required 排后或不输出。
2. 失败自动切换（代理层）：/SourcePlay 解析或上游拉流失败时，自动按排序尝试【同 source_item + 同 episode_key】的
   下一条线路，全部失败才返回 502；每次尝试都回写对应线路 success/failure/latency。
   注意：已开始向客户端写响应体后不能再切换（只能在首字节前切换），实现时先探测可用线路再开始 io.Copy。
3. 健康衰减（可选）：连续失败超阈值的线路标记 health_status=unhealthy，排序自动靠后；恢复成功后回升。

约束：仅字节中转 + header 注入，不转码；切换逻辑不得阻塞过久（每条线路带短超时探测）。

完成判定：构造一条坏线路 + 一条好线路，/SourcePlay 能跳过坏的播好的；PlaybackInfo 顺序健康优先（go build 通过；运行期我验证）。
```

实际落点：`internal/repository/source_play_repository.go` 为 `ListPlaySourcesForItem` 增加健康排序（health_status、direct/unknown 优先、成功率、avg_latency_ms），新增 `ListPlayableAlternatives` 查询同 source_item + episode_key 的候选线路，并在连续失败达到阈值后标记 `unhealthy`；`internal/handlers/media/source_play.go` 在 `/SourcePlay/{playSourceUUID}/stream` 中按入口线路优先、后续健康排序候选逐条 resolve/探测上游响应头，只有 2xx/3xx 响应才写客户端并开始 `io.Copy`，首字节前失败会自动切换下一条，已写出后不再切换。未转码、仍仅字节中转 + header 注入。构建：`go build ./...` 通过。T12 commit 范围：156e73a。

---

## T13 — 跨来源聚合搜索（后端）

**依赖**：T11　**完成判定**：一个搜索接口能并发搜所有 enabled provider，合并/去重/排序返回，并把结果入 source_items 缓存；单源故障不影响整体。

**提示词**
```
目标：实现跨来源聚合搜索后端。并发搜全部 enabled+searchable provider，归一/合并/去重/排序，结果入 source_items。

参照现有：internal/source/cms_provider.go 的 Search、provider_runtime.go 的 limiter、ingest.go 的 upsert、
normalize.go；internal/services 的并发/超时模式。

实现：
1. 聚合编排：对所有 effective_enabled 且 searchable 的 provider 并发调用 Search(keyword)；
   每个 provider 各自 timeout + per-provider limiter + panic/错误隔离（单源失败只记日志，不影响其他）。
   总体设并发上限与整体超时。
2. 归一/去重/排序：结果走 normalize.go；按 (normalized_title + year) 或 provider_ids 去重合并（同一片聚合多来源线路）；
   排序按标题匹配度 + provider 健康 + 年份。返回分组结果（每条带其所属 provider 列表）。
3. 入库：搜索命中的条目 upsert 进 source_items（轻量快照，ON CONFLICT public_uuid），便于后续打开详情/绑定；
   绝不写 items。
4. 后端路由（双注册）：POST /SourceSearch（body: {keyword, limit}），返回归一聚合结果 + 错误明细（哪个源失败/超时）。
   结构化日志记录 provider_id/action=federated_search/latency/status/hit_count。

约束：不写 items；外部请求全部经 limiter + ValidateOutboundURL（如涉及出站）；敏感信息不入日志。

完成判定：多 provider 下 POST /SourceSearch 能并发返回合并去重结果，单源故障被隔离（go build 通过；运行期我验证）。
```

---

## T14 — 真实 TVBox 配置硬化联调【人工里程碑】

**依赖**：T11、T12、T13　**完成判定**：用真实多站 TVBox 配置端到端跑通分类/搜索/聚合搜索/详情/播放，修掉真实环境暴露的解析/编码/相对路径/健康问题。

**提示词**
```
目标：用真实 TVBox 配置（我会提供 URL 或 JSON）对 Phase 1+2 的来源链路做硬化联调，修真实问题，不新增大功能。

步骤：
1. 导入我提供的真实配置，确认 JSON+XML CMS 站点被正确识别为可用 provider，其余（csp/js/py/live/parser）标 runtime_required。
2. 逐站探活 + 抽样 分类/搜索/详情/拆线路/播放；用聚合搜索跑几个关键词。
3. 针对真实暴露的问题修复（典型：相对 api/图片 URL 解析、GBK/编码、CDATA/实体、分页字段差异、
   反爬停放页/TLS EOF 的容错归一、健康状态误判、线路拆分边界）。修复按边界小步提交。
4. 区分「协议支持/站点可用/数据质量」三层，把不可用站点的失败归一化展示，不影响其他站点与本地库。

约束：只修不扩；不写 items；不引入 sidecar。

完成判定（人工里程碑）：产出一份联调报告（哪些站可用/不可用及原因、修了哪些解析问题），
我在客户端实测：真实站点的在线库可浏览、可搜索、可播放。我确认后再放行 T15。
```

---

## T15 — 聚合搜索接入 Emby + Web 前端【人工里程碑】

**依赖**：T13、T14　**完成判定**：Web 来源中心有聚合搜索 UI；Emby 客户端搜索能看到在线结果（按设计的命名空间，不污染本地）。

**提示词**
```
目标：把 T13 的聚合搜索接入 Web 前端来源中心，并按设计暴露到 Emby 搜索。

参照现有：web/src 的来源中心页面与组件（Phase 1 T8）、Emby 搜索出口（/Search/Hints 与 /Items?SearchTerm 的现有实现）。

实现：
1. Web 来源中心：新增「聚合搜索」面板，调 POST /SourceSearch，展示合并结果（标题/年份/来源数/海报）、
   每源成功失败明细、可点开详情/线路。错误按归一化展示（源名/动作/耗时/错误类型）。
2. Emby 搜索接入（按总纲存储分离、出口统一）：在 /Search/Hints 或 /Items?SearchTerm 的结果里，
   追加来自 source_items 的在线结果，Id 用 source item public_uuid，与本地结果区分（不混淆、不写 items）；
   仅返回已入库的 source_items（聚合搜索已把命中写入），避免每次 Emby 搜索都现场打外部源。
   是否对 Emby 暴露在线搜索结果做成可配置开关，默认开启但可关。
3. 保持本地搜索行为零变化；在线结果点开走 Phase 1 的在线详情/PlaybackInfo/代理播放链路。

约束：不污染 items；不动本地搜索既有逻辑；前端按领域组件拆分，避免大文件。

完成判定（人工里程碑）：Web 能聚合搜索并展示；Emby 客户端搜索能看到在线条目并播放（go build/npm build 通过；我在客户端验证）。
```

---

## §C 总目标提示词（自驱 T10~T13，里程碑停 T14/T15）

```
【总目标】按 docs/universal-media/Phase2-扩源与聚合搜索-开发任务与提示词.md 顺序实现 Phase 2。
先读该文件「范围/§A/§B」与 T10~T15，以及总纲 docs/universal-media/Phase1-在线优先-实施规划.md 与
docs/universal-media/技术债与遗留项.md。§A 全部约束（含 Phase 2 补充）持续生效。

【执行】严格按 T10→T11→T12→T13→（人工里程碑 T14）→（人工里程碑 T15）顺序，一次一个任务：
- 每任务只做本任务范围；收尾保证 go build ./...（改前端再 npm run build）通过；按代码/功能边界增量提交（中文、每个 commit 能 build）；
  在该任务末尾追加「实际落点」并记 commit 范围。
- 连续自驱 T10、T11、T12、T13。

【T14 人工里程碑】完成 T13 且构建绿后停下，告诉我「需要真实 TVBox 配置以进入 T14 硬化联调」，
等我提供配置与确认。不要自行编造外部站点数据。

【T15 人工里程碑】T14 硬化通过、我确认后才做 T15；T15 完成后停下等我客户端/UI 验证。

【冲突与边界】发现与总纲冲突或会违反 §A（污染 items、动本地主链路、写测试、启动服务、引入 sidecar）即停下说明，
不擅改总纲、不绕约束、不扩范围。每任务用中文汇报：改了什么、构建是否通过、已知未覆盖点。
```

---

## 备注

- T10 先清债再扩源，避免 `source_repository.go` 越堆越大。
- T14/T15 是人工里程碑：T14 需要你给真实配置并在客户端验证，T15 需要 UI/客户端验证——届时可像 Phase 1 那样让我用 API 帮你复检。
- Phase 3 预告：DRPY JS / CSP sidecar（QuickJS/dex runtime + RPC 契约 + 沙箱安全）、AList 目录源、网盘搜索、parse=1 解析器。
