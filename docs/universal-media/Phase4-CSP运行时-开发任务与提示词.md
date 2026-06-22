# Phase 4 CSP 运行时 开发任务与提示词

> 配合总纲 [Phase1-在线优先-实施规划.md](./Phase1-在线优先-实施规划.md)、[技术债与遗留项](./技术债与遗留项.md) 使用。
> 延续 Phase 3 的 Codex 目标模式工作法。覆盖 0821 配置里 ~38 个 `csp_*` 站。

## 1. CSP 调研结论（关键：不需要 Android 模拟器）

CSP jar 本质是 jar，但**内含 `classes.dex`（Dalvik 字节码），不是标准 JVM `.class`**——普通 `java -jar` 不能直接加载，这是唯一硬障碍。

但它对 Android 的**依赖很浅**：绝大多数 spider 只用 **OkHttp + Jsoup**（纯 Java，JVM/Android 通用）+ 少数 `android.*` 帮手（`TextUtils`/`Uri`/`Base64`/`Log`/`Context`/`SharedPreferences`）。**不需要 WebView/完整 framework**（除少数例外）。铁证：已有人把 FongMi CatVodSpider 移植成**纯 JVM 桌面版**（`Greatwallcorner/CatVodSpider`，for TV-Multiplatform），改动仅是 `TextUtils.join`→`StringUtils.join`、`init()` 去掉 `Context`。

**结论：不需要 Android 模拟器/Robolectric。** 两条轻路径：

| 路径 | 做法 | 跑的是谁 | 取舍 |
|---|---|---|---|
| **A. dex2jar + 薄桩（首选）** | `d2j-dex2jar classes.dex` → `URLClassLoader` 反射 → 提供薄 `android.*` 桩 | **配置里真实 dex jar**（覆盖 38 站） | 跑真实源；个别 R8 混淆/深度 Android API 的跑不了 |
| **B. 源码重编译** | CatVodSpider 源码去 Android 化编译成标准 jar | 自己重编的 jar | 干净；但配置下发的自定义 jar 没源码覆盖不了 |

**采用路径 A**（直接跑站点下发 jar），B 仅作个别高价值站兜底。

**硬骨头少数派**（薄桩搞不定，归一化标"不支持"，不强求）：WebView 取数、OCR、深度 Android API、App 接口加密签名类。

**与 Phase 3 的关系**：CSP runtime 形态 = **JVM sidecar**，和 Phase 3 的 Node sidecar 完全同构（独立进程 + RPC + 网络回调 Go 做 SSRF/限流 + artifact + 审计）。工作量同级，**不是数量级更重**。

参考：FongMi/TV、FongMi/CatVodSpider、Greatwallcorner/CatVodSpider（JVM 移植）、CatVodTVOfficial/CatVodTVJarLoader（反射加载器）。

## 2. 范围（轻量化，已定）

Phase 4 = **CSP dex jar 运行时（JVM sidecar，路径 A：dex2jar + 薄 android 桩）**。

**明确不做**：
- Android 模拟器 / Robolectric / 完整 framework —— 太重，不碰。
- WebView/OCR/深度 Android/App 加密签名类 spider —— 归一化标"不支持"，不强求。
- AList 目录源 / 网盘搜索 —— 永久排除。

**形态已基本确定 = JVM sidecar**（依据调研），但**可行性仍由 T21 PoC 先证伪**（dex2jar + 薄桩能不能真跑通配置 jar），PoC 后再定稿 T22~T24 细节。

延续不变约束：**在线内容绝不写 items**；存储分离、出口统一；确定性 UUID；代理仅字节中转+header 注入、永不转码；外部出站经 SSRF + per-provider limiter。

## §A 共享前置约束

整段沿用 [Phase1-开发任务与提示词.md](./Phase1-开发任务与提示词.md) 的「§A 共享前置约束」，全程生效，每任务带上。

**Phase 4 安全补充（运行不可信 dex/jar，比 JS 更危险）**：
- **JVM 内沙箱已不可靠**（Java 17 弃用、21+ 移除 SecurityManager），所以**靠进程/OS/容器级隔离**：sidecar 以低权限独立进程运行，**不挂载媒体库真实路径**、工作目录隔离、资源/内存上限；容器内只读根文件系统 + 独立用户。
- **网络出站只走 Go 回调**（经 `ValidateOutboundURL` + per-provider limiter）；尽量限制 sidecar 直接开 socket。
- **jar 信任**：dex jar 必须校验 MD5/SHA256（配置 `spider=path;md5;hash` 自带 hash）；无 hash 默认 `unverified`，管理员显式确认才加载。
- runtime/spider 崩溃、卡死、OOM 不得拖垮 FYMS Core；单次调用超时 + worker kill；spider 实例按 provider 隔离，不跨 provider 共享状态。

## §B 任务顺序

```
T21 CSP PoC（dex2jar + JVM 反射 + 薄桩 跑通一个简单 spider）  【闸门：验证路径 A 可行性 + 定形态】
      ▼
T22 JVM sidecar 运行时 + 薄 android 桩 + CatVod 宿主(init/proxy) + 生命周期 + 沙箱 + artifact + Dockerfile 装 JRE  【重里程碑】
      ▼
T23 CSP JAR Provider 接入统一接口 + proxy()/playerContent 处理（复用 normalize/ingest/play/聚合）
      ▼
T24 前端启停·健康·搜索测试 + jar 信任确认 UI + runtime 审计（复用 source_runtime_invocations）
```

每任务停验（需重启 + 真实站点验证）。T22~T24 提示词以下为初稿，**T21 PoC 后据结论微调**。

---

## T21 — CSP PoC（可行性验证 + 形态确认）【闸门】

**依赖**：Phase 3 完成　**完成判定**：能用配置里真实 dex jar 跑通一个简单 spider 的 home/category/detail/search/play，并产出可行性报告。

**提示词**
```
目标：CSP dex jar 运行时最小可行性 PoC，回答"FYMS 能否经 dex2jar + 薄 android 桩在 JVM 跑通 TVBox 的 csp_* spider"。
不求完整，只求证伪。§A + Phase 4 安全补充持续生效；不写 items、不接 Provider 正式链路、不引入 Android 模拟器。

背景：0821 配置 spider="./jar/fan.txt;md5;6c4ab3a9d232164c75534f9060506ee5"，内含 classes.dex，
csp_Xxx 对应 com.github.catvod.spider.Xxx，继承 com.github.catvod.crawler.Spider。
调研结论：依赖浅（OkHttp/Jsoup + 少量 android.* 帮手），不需 Android 模拟器（见本文 §1）。

PoC 步骤：
1. 拉取并校验 spider jar（按 spider=path;md5;hash 解析、相对 base_url 解析、下载经 ValidateOutboundURL、校验 md5）。
2. dex2jar：把 classes.dex 转成标准 class jar（用 dex-tools/d2j-dex2jar 或等价库）。记录转换成功/失败。
3. JVM sidecar（最小）：URLClassLoader 加载转换后 jar，反射实例化一个【简单网页抓取型 spider】
   （调研点名 SixV 或 FirstAid），调用 init/homeContent/categoryContent/detailContent/searchContent/playerContent。
4. 薄 android 桩：只补让该 spider 跑起来必需的 android.* 类（TextUtils/Uri/Base64/Log/空 Context/SharedPreferences），
   缺哪个补哪个，记录清单。CatVod 的 Spider 抽象 + init 宿主能力提供最小实现；网络 req 回调 Go（SSRF+limiter）。
5. 临时入口暴露供人工验证：管理员 POST /SourceRuntime/TestCSP {configBaseUrl, spider, md5, api(csp_Xxx), method, args}
   返回 {ok,data,logs,durationMs}。我重启后用 API 验证。
6. 单次超时、禁文件访问媒体库、脚本异常不崩主进程。

完成判定（go build 通过 + 我重启后 API 验证）：
- /SourceRuntime/TestCSP 对 SixV 或 FirstAid 能返回 home/category/detail/search/play 真实数据（或明确 parse 标记）。
- PoC 报告：dex2jar 是否可行/成功率、必需 android 桩清单与缺口、各方法成败、JRE 进镜像影响、
  以及【形态确认：JVM sidecar】及理由（含 Java 无 SecurityManager → 靠进程/容器隔离的安全方案）。
- 停下告诉我"CSP PoC 完成，待形态确认"，不进 T22。
```

### T21 第一次尝试落点（2026-06-22，已被自包含重做覆盖）

**commit 范围**：`d3b4a7e`（实现 CSP JVM PoC 临时入口）~ `51791e0`（调整 CSP PoC 工具探测顺序）。

> 该版本依赖外部 dex2jar CLI 与运行期 javac，未真正通过 T21 闸门；保留为失败记录。当前有效结论见下方「T21 自包含重做实际落点」。

**代码落点**
- 管理员临时入口：`POST /SourceRuntime/TestCSP`，通过既有 `RegisterSourceRoutes` 自动双注册到根分组与 `/emby` 前缀。
- Go 侧 PoC 管理器：`internal/source/csp_runtime_*`，负责请求归一、spider artifact 下载校验、dex2jar 转换、一次性 JVM worker 调用、超时与并发限制、source_runtime_artifacts/source_runtime_invocations 记录。
- JVM PoC sidecar：`runtime/csp-sidecar/src`，提供 `fyms.csp.CSPProbe` 反射加载转换后 class jar，并补最小 `android.*` 与 CatVod 宿主桩。
- 不接正式 Provider，不写 `items` / `source_items`，不改本地 Emby 主链路，不新增迁移。

**PoC API**
```json
POST /SourceRuntime/TestCSP
{
  "configBaseUrl": "https://tvboxconfig.singlelovely.cn/gao/",
  "spider": "./jar/fan.txt;md5;6c4ab3a9d232164c75534f9060506ee5",
  "api": "csp_SixV",
  "method": "home",
  "args": {},
  "timeoutMs": 30000
}
```

返回结构：`{ok,runtimeKind,baseUrl,api,method,artifact,dex2jar,result,data,logs,durationMs,workerPid}`。

**已实现的安全边界**
- spider jar 下载前走 `ValidateOutboundURL`；只允许 http/https，拒绝内网、回环、链路本地地址。
- `spider=path;md5;hash` 与独立 `md5` 字段均可校验；hash 命中后 artifact 标记 `verified`，无 hash 保持 `unverified`。
- 单次调用有超时，worker 由 `exec.CommandContext` 拉起并在超时后终止；CSP PoC 并发上限为 2。
- PoC 工作目录限定在 `data/source-runtime/csp/work`，artifact 放在 `data/source-runtime/csp/artifacts`；不挂载或暴露媒体库真实路径。
- 审计只记录 provider/base/spider 的 hash、artifact sha256、dex2jar 状态、耗时与错误类型，不写敏感 URL 明文。

**薄桩清单**
- `android.text.TextUtils`
- `android.net.Uri`
- `android.util.Base64`
- `android.util.Log`
- `android.content.Context`
- `android.content.SharedPreferences` / 内存实现
- `com.github.catvod.crawler.Spider`
- `com.github.catvod.net.OkHttp`（CatVod 常见网络包装，经 Go stdin/stdout 回调，复用 SSRF + per-provider limiter）

**已知缺口 / 待人工验证**
- 本机缺 `javac`，因此未能在开发环境预编译 `runtime/csp-sidecar/classes`；Go 构建不受影响，实际 API 调用时若目标环境有 JDK 会自动编译 classes，否则返回 `runtime_unavailable` 并提示缺 `javac/classes`。
- 本机未安装 `d2j-dex2jar`，实际 API 调用在 jar 下载校验成功后会返回 dex2jar 工具缺失；目标环境需提供 `d2j-dex2jar` / `d2j-dex2jar.bat` / `d2j-dex2jar.sh`。
- 若 spider 直接依赖自带 `okhttp3` 或深度 Android API，PoC 无法强制接管网络，可能返回 `missing_stub` / `runtime_error`；这正是 T21 用于确认 T22 宿主桥范围的证伪点。
- WebView、OCR、深度 Android API、App 加密签名类仍按原计划标记不支持，不在 T21 扩展。

**第一次尝试结论**
- 不通过 T21 闸门：运行期依赖 `javac` 和外部 `d2j-dex2jar`，且无法证明 spider 已真实加载执行。
- 已在 `2d99b1d` / `76a2005f` 改为自包含 fat jar + sidecar 内 dex2jar 库 API + Go 网络桥。

### T21 自包含重做实际落点（2026-06-22）

**commit 范围**：`2d99b1d`（重做 CSP PoC 为自包含 sidecar）。

**重做原因**
- 上一版卡在外部 `d2j-dex2jar.bat/.sh` 与运行期 `javac`，spider 未真正加载执行，不能视为 T21 过闸门。
- 运行时现编译 classes 不适合生产路径；T21 重做后将 sidecar 构建前移，运行期只依赖 JRE。

**自包含落点**
- `runtime/csp-sidecar` 改为 Gradle wrapper 工程，`build.ps1` 一键执行 `clean shadowJar`，产物为 `runtime/csp-sidecar/build/libs/fyms-csp-sidecar-all.jar`。
- sidecar fat jar 内含：
  - `fyms.csp.CSPProbe` JSON-lines RPC 入口；
  - CatVod 宿主桩与 android 薄桩；
  - dex2jar 库依赖 `de.femtopedia.dex2jar:dex-translator:2.4.13`；
  - 常见纯 Java 依赖 `jsoup:1.17.2`、`org.json:20240303`；
  - `okhttp3` 最小兼容桥接层，`OkHttpClient.newCall().execute()` 会发 JSON-lines `http_request` 给 Go，不在 sidecar 内裸连。
- Go 侧改为 `java -jar <fat-jar>`，payload 传入 `artifactPath/workDir/className/method/args/ext`；不再查找或调用外部 `d2j-dex2jar`，不再运行期 `javac`。
- Dockerfile 增加 Gradle 构建阶段生成 sidecar fat jar；运行镜像只安装 `openjdk-21-jre-headless`，不需要 JDK。

**dex2jar 库化方案**
- sidecar 内部从 spider jar 中抽取 `classes.dex` 到隔离工作目录；
- 调用 `com.googlecode.d2j.dex.Dex2jar.from(dexFile).skipDebug(true).reUseReg(true).to(outputJar)` 生成标准 class jar；
- 由同一个 JVM 进程 `URLClassLoader` 反射加载 `com.github.catvod.spider.SixV`。

**薄桩清单（本轮真实补齐）**
- `android.text.TextUtils`
- `android.net.Uri`
- `android.util.Base64`
- `android.util.Log`
- `android.app.Application`
- `android.content.Context`
- `android.content.SharedPreferences` / 内存实现
- `android.view.ViewGroup.LayoutParams`
- `com.github.catvod.crawler.Spider`
- `com.github.catvod.net.OkHttp`
- `okhttp3` 桥接子集：`OkHttpClient` / `Request` / `Response` / `ResponseBody` / `Headers` / `FormBody` / `MediaType` / `Dns` / `Dispatcher` / `Call`

**本地证据**
- `runtime/csp-sidecar/build.ps1` 成功产出 fat jar。
- `go build ./...` 通过。
- 使用本地已校验 `fan.txt` artifact 与 `ext=https://www.xb6v.com/`，执行 `java -jar runtime/csp-sidecar/build/libs/fyms-csp-sidecar-all.jar` 并用 JSON-lines 模拟 Go HTTP bridge，已证实：
  - `home`：成功返回 SixV 分类，如 `国剧/日韩剧/欧美剧/喜剧片/...`；
  - `category`：成功返回真实列表，如 `她的直拳法则[全集]`、`樊笼[全集]` 等；
  - `detail`：成功返回真实详情与 `vod_play_url`，含 magnet 播放项；
  - `play`：对 magnet 播放 id 成功返回 `{parse:0,url:"magnet:..."}`；
  - `search`：方法执行成功，关键词 `庆余年` 当前站点返回空 `list`，不是 runtime 缺失。
  - 网络桥修正后，sidecar 已输出 `http_request`，等待 Go/模拟桥回写 `http_response` 后继续执行；返回 `networkBridge=okhttp3-go-bridge`。

**仍需人工 API 验证**
- 本地未启动 FYMS 服务；需重启后通过 `POST /SourceRuntime/TestCSP` 验证 Go HTTP 入口、artifact 下载校验、审计落库与 sidecar RPC 全链路。
- 若其它 CSP spider 使用 T21 尚未覆盖的 OkHttp API 或其它网络库，会返回缺类/缺方法；T22 继续扩展桥接子集或加容器网络策略。

**路径修复（2026-06-22）**
- Go 传给 JVM sidecar 的 `artifactPath` 与 `workDir` 已统一转为 `filepath.Abs` 后的绝对路径，避免 sidecar cwd 与 FYMS cwd 不一致导致 `NoSuchFileException`。
- `CSPArtifactManager` 保存 artifact 时写入绝对 `local_path`；从旧 DB 记录读出相对路径时也会在返回 DTO 前绝对化。
- 调用 sidecar 前会 `os.Stat` 校验 artifact 文件存在；若 DB/缓存记录命中但本地文件缺失，会重新下载并再次校验，不只信 `source_runtime_artifacts.local_path`。
- 本地确认 `data/source-runtime/csp/artifacts/...-csp-fan.txt` 与 `data/source-runtime/csp/work` 均可解析为 Windows 绝对路径。
- 本次补强：`source_runtime_artifacts` 命中缓存时先把 `local_path` 绝对化并校验本地文件与配置 hash；文件缺失、路径失效或 hash 不匹配时放弃缓存并重新下载。sidecar 内部也会把 `artifactPath` normalize 为绝对路径后再打开，错误返回绝对路径，便于定位。

**阶段结论**
- T21 形态确认：JVM sidecar 可行，且应采用“构建期 fat jar + 运行期 JRE + sidecar 内 dex2jar 库 API”的自包含形态。
- 生产安全仍按 Phase4 补充执行：不信任 dex/jar，不依赖 JVM 内沙箱；靠独立进程、工作目录隔离、容器低权限/资源限制、单次超时 kill、artifact hash 信任与 Go 侧网络校验/限流。

---

## T22 — JVM sidecar 运行时 + 薄桩 + 宿主 + 生命周期 + artifact【重里程碑】

**依赖**：T21　**完成判定**：真实 fan.txt 内多个 csp spider 能加载并跑通；JRE 进镜像；崩溃自愈、超时 kill；artifact 校验 hash。

**提示词（PoC 后微调）**
```
目标：把 T21 PoC 升级为正式 JVM sidecar 运行时。形态=独立 JVM sidecar，JRE 打进 FYMS Docker 镜像。
§A + Phase 4 安全补充持续生效；不写 items、不接 Provider 正式链路(T23)。

1. sidecar 工程（如 runtime/jvm-sidecar/，Java/Kotlin + Gradle）：提供 RPC（stdin/stdout JSON-lines 或本地 socket）：
   loadJar/init/home/category/detail/search/play/proxy/destroy。
   实现 CatVod Spider 抽象层（com.github.catvod.crawler.Spider 等宿主兼容类）+ init 宿主能力。
2. dex2jar + 加载：jar 下载/校验 → dex2jar → URLClassLoader 反射实例化 csp_Xxx；className 解析默认
   csp_Xxx→com.github.catvod.spider.Xxx，找不到返回 class_not_found，不猜近似类名。
3. 薄 android 桩：补全 spider 常用 android.* 子集（TextUtils/Uri/Base64/Log/Context/SharedPreferences/MimeTypeMap…）；
   OCR/WebView/深度 API 留桩归一化"不支持"。
4. 宿主桥：网络 req/OkHttp 出站回调 Go（ValidateOutboundURL + per-provider limiter，单一真相）；
   proxy() 宿主代理入口（见 T23）；siteKey 写入 spider 供 Proxy.getUrl 生成稳定地址。
   **【T21 实测必修】字符集**：大量站是 GBK（如 SixV/xb6v.com）——req 桥必须按站点 charset（Content-Type charset / HTML meta / 自动探测）正确转 UTF-8，
   像 CatVod 的 OkHttp 那样。T21 实测 SixV home 的 type_name 乱码、category list 为空，根因就是 GBK 当 UTF-8 处理；
   修后 type_name 应正常、category 列表应非空。
5. 生命周期：FYMS 启动拉起 sidecar 并监督（崩溃自愈）；spider 实例按 (providerKey + jar md5) 缓存、空闲销毁、
   md5 变更重建；单次调用超时 kill；并发上限；实例隔离不跨 provider 共享 extend/cookie。
6. 沙箱（§A 安全补充）：低权限进程、不挂媒体库、工作目录隔离 data/source-runtime/csp/{providerKey}、
   资源/内存上限；JVM 无 SecurityManager 故靠进程/容器隔离。
7. artifact：复用 source_runtime_artifacts（kind=csp_dex_jar），下载/校验 md5·sha256/trust_status，
   无 hash 默认 unverified。
8. Docker：装精简 JRE（如 temurin-jre-headless）+ sidecar 产物；本地无 Docker 回退 PATH 的 java。

完成判定（go build + sidecar 构建通过 + 我重启后 API 验证）：
- 真实 fan.txt 内 ≥1 个 csp spider（SixV/FirstAid 等）能加载并跑通 home/search/detail/play；
- jar md5 校验、崩溃自愈、超时 kill、网络经 Go SSRF+limiter 均生效；artifact 落库带 hash。
- 停下告诉我"T22 完成待验"，不进 T23。
```

---

## T23 — CSP JAR Provider 接入 + proxy() 处理

**依赖**：T22　**完成判定**：csp_* 站从 runtime_required 转为可用 provider，经 sidecar 跑通搜索/详情/播放；proxy() 型播放可用。

**提示词（PoC 后微调）**
```
目标：把 runtime_kind=csp_dex 的 provider 接入统一 Provider 接口，处理 CSP 特有的 proxy()/playerContent。
§A + Phase 4 安全持续生效；不写 items。

1. 实现 CSPProvider，经 T22 sidecar 实现 Categories/Search/Category/Detail/ResolvePlay；
   ProviderRuntimeManager 按 runtime_kind 分派（native_cms→CMS，js_node_drpy→JS，csp_dex→CSP）。
2. 字段归一/入库/拆线路复用 normalize.go/ingest.go/play 链路；剧集按总纲 §4.1；聚合搜索自动纳入 csp provider。
3. proxy() 处理：部分 spider playerContent 返回指向宿主代理的 URL，需由 sidecar 的 proxy(Object[]) 动态生成
   MPD/M3U8/转发（如 Bili DASH）。FYMS 播放出口对接：/SourcePlay 命中 csp proxy 线路时经 sidecar proxy 取流/取直链，
   仍走 Go 字节中转 + SSRF，不转码。
4. 导入：csp_* 站绑定其 spider jar artifact，从 runtime_required 翻为可用 provider（jar 未确认信任则保持禁用/待确认）。
5. parse=1 复用 Phase 3 解析器；magnet/cloud_share/不支持类归一化标记，不崩。

完成判定（go build + 我重启后 API/客户端验证）：
- 一个真实 csp 站作为可用 provider 出现，搜索/详情/播放跑通；proxy() 型线路能播；
- 客户端浏览经 Phase 3 的 detail 按需入库出分集/线路。停下告诉我"T23 完成待验"。
```

---

## T24 — 前端 + jar 信任 UI + 审计

**依赖**：T23　**完成判定**：来源中心支持 csp provider 与 jar 信任管理；csp runtime 调用入审计。

**提示词**
```
目标：前端接入 + 安全/可观测收尾。§A + 安全持续生效；不写 items；前端按领域组件拆分，注意多根模板过渡白屏。

1. 来源中心：csp provider 启停/健康/搜索测试（复用既有 UI）；
   jar 信任管理：展示 artifact 的 md5/sha256/trust_status，提供"确认信任"操作（unverified→verified 才允许加载）。
2. 审计：csp runtime 调用入 source_runtime_invocations（providerKey/method/duration/status/error_type；敏感不入明文）。
3. 错误归一化展示（来源名/动作/耗时/错误类型/可重试建议）；不支持类 spider 明确标注。

完成判定（go build + npm run build + 我验证）：来源中心可管 csp 源与 jar 信任；csp 调用入审计；
不支持类有清晰标注。停下告诉我"T24 完成待验"。
```

---

### T22/T23/T24 实际落点（2026-06-22）

**commit 范围**
- T22 runtime 安全边界：`45f69ae`（正式化 CSP JVM runtime 安全边界）。
- T23 Provider 接入：`5cb92a8`（接入 CSP Provider 统一来源链路）。
- T24 前端信任 UI：`28ffbd2`（补充 CSP artifact 信任管理界面）。
- T21/T22 路径防御补强：`5ced6a9`（修复 CSP sidecar 工作目录创建）。
- T23 proxy 播放桥接补齐：`5f2f827`（补齐 CSP proxy 播放桥接）。
- T21/T22 artifact 路径防御：`60c2b5f`（补强 CSP sidecar 绝对路径防御）。
- T23 proxy 字节体处理：`808b837`（补强 CSP proxy 字节体处理）。
- T24 artifact 信任 UI：已补强 `verified|trusted` 可信状态展示，仅 `unverified` 保留确认信任操作。
- T21/T22 CSP 编码修复：Go HTTP bridge 在 JSON marshal 前完成文本响应转码，sidecar CatVod OkHttp 桩优先读取 `bodyText`。

**T22 落点**
- CSP runtime kind 正式定为 `csp_dex`，继续采用独立 JVM worker 形态；每次调用由 `exec.CommandContext` 拉起，单次超时由上下文 kill，崩溃/卡死不拖垮 FYMS Core。
- 工作目录从全局 `data/source-runtime/csp/work` 下沉为按 providerKey/providerID 隔离的子目录；artifact/workDir 继续传绝对路径。
- sidecar 启动前显式创建 provider 隔离工作目录，避免 `cmd.Dir` 指向未创建目录导致 Windows/相对路径类失败。
- Go HTTP bridge 在 SSRF 校验与 per-provider limiter 后读取响应，并按 `Content-Type charset` / HTML meta 自动转 UTF-8 后传给 sidecar，修复 SixV/xb6v.com 等 GBK 站点乱码问题。
- CSP HTTP bridge 增加文本响应判定与多级编码探测：`Content-Type charset`、HTML meta、BOM、GB18030/Big5 启发式、`DetermineEncoding`；二进制响应只保留 `bodyBase64`，避免把非 UTF-8 字节直接交给 Go `json.Marshal`。
- sidecar 的 `com.github.catvod.net.OkHttp` 兼容桩改为优先使用 Go bridge 返回的 UTF-8 `bodyText`，防止 SixV 等 spider 经过旧 `bodyBase64` UTF-8 解码路径重新乱码。
- artifact 信任门槛收紧：`trust_status=verified|trusted` 才允许加载；无 hash 的 `unverified` 默认拒绝，管理员可通过信任接口确认。
- sidecar 补 `proxy` 方法入口和 unsupported API 清单；为 fan.txt/Bili proxy 类加载补齐轻量签名桩（Activity/AlertDialog/Intent/PackageManager/Handler/View/ImageView 等）与纯 Java Gson 依赖。WebView/OCR/深度 Android/App 签名仍按不支持类归一化，不引入 Android 模拟器。

**T23 落点**
- 新增 `internal/source/csp_provider.go`，实现 `CSPProvider` 的 Categories/Search/Category/Detail/ResolvePlay，调用正式 CSP runtime。
- `ProviderRuntimeManager` 增加 `WithCSPRuntime`，按 `provider_kind=tvbox_site + runtime_kind=csp_dex` 分派 CSP Provider。
- TVBox 导入识别 `api=csp_*` 的站点为可用 CSP provider；仍由 runtime artifact 信任门槛阻止未校验 jar 真正加载。
- 搜索、详情、分集/线路入库继续复用 `normalize.go` / `ingest.go` / `splitCMSPlaySources`，在线内容仍只写 `source_items/source_play_sources`，不写 `items`。
- `/SourcePlay` 与按需详情加载注入 CSP runtime；`playerContent` 的 direct HTTP 线路可走 Go 字节代理，`parse=1` 交解析器，magnet/cloud_share/不支持类清晰报错不崩。
- CSP proxy 线路闭环：识别 `parse_mode=proxy`、`proxy://`、`fyms-csp-proxy://` 与本地宿主 proxy URL 后，不直连本地地址，而是回调 sidecar `proxy()`；Go 侧支持 CatVod 常见 `Object[]{status, contentType, headers, body}`、JSON `{url,headers}` 与内联 body 结果，再统一经 `/SourcePlay` 输出。
- CSP proxy 边角补强：支持 sidecar normalize 后的 `Object[]{..., {bodyBase64}}` 字节体；`/SourcePlay` 只缓存可校验外部 URL，不缓存 proxy 内联 body。
- 本地 sidecar 诊断确认 Bili proxy 已进入 Go 网络桥协议并发出 `http_request`，后续由 FYMS 运行期 bridge 回写 `http_response` 继续执行；未启动 FYMS 服务做端到端 API 验证。

**T24 落点**
- 新增管理员接口 `POST /SourceRuntime/Artifacts/:id/Trust`，将 artifact 置为 `trusted` 并写 `verified_at`；路由继续按根分组与 `/emby` 双注册。
- 来源中心 Provider 表展示 `csp_dex` 为 `CSP JAR`；运行时审计表展示 runtime kind、artifact 类型与信任状态。
- 来源中心 artifact 表提供“确认信任”操作，调用信任接口后刷新 artifacts/invocations；审计仍只展示 hash/错误类型/耗时，不展示敏感 URL 明文。
- 来源中心 artifact 表将 `verified` 与 `trusted` 都展示为可信状态；已通过 MD5/SHA256 校验的 jar 不再误显示为仍需人工确认。

**验证**
- `runtime/csp-sidecar/build.ps1` 通过。
- `go build ./...` 通过。
- `cd web && npm run build` 通过（保留既有 ArtPlayer CommonJS warning）。
- 未启动 FYMS 服务，未跑测试，未写测试，未写 `items`，未进入 AList/网盘范围。

---

## §C 总目标提示词（先只跑到 T21 闸门）

```
【总目标】按 docs/universal-media/Phase4-CSP运行时-开发任务与提示词.md 执行 Phase 4。
先读该文件「调研结论/范围/§A/§B」与 T21，以及总纲与技术债文档。§A 全部约束(含 Phase 4 安全补充)持续生效。
形态倾向=JVM sidecar（dex2jar+薄桩，路径 A），但本轮先做 T21 PoC 证伪，不预先铺开。

【执行】本轮只做 T21 CSP PoC：按代码/功能边界增量提交(中文、每个 commit 能 go build)，
完成后追加「实际落点」+commit 范围；go build ./... 通过。

【T21 闸门】完成且构建绿后停下，产出 PoC 报告(dex2jar 可行性/必需 android 桩清单/各方法成败/JRE 进镜像影响/
形态确认+安全方案)，明确告诉我"CSP PoC 完成，待形态确认"，不进 T22。T22~T24 待我据 PoC 结论定稿后再发。

【冲突与边界】发现与总纲冲突或会违反 §A(污染 items、动本地主链路、写测试、自行启动服务、
引入 Android 模拟器、把 AList/网盘纳入)即停下说明，不擅改、不绕约束、不扩范围。
中文汇报：改了什么、构建是否通过、已知缺口。
```

---

## 备注

- T21 是命门：dex2jar + 薄桩能否真跑通配置 jar，决定 T22~T24 全部写法（和 Phase 3 的 T16 同性质）。
- 镜像里会多一个**精简 JRE**（~50-80MB，和 Node 同量级）；FYMS 同时带 Node(JS) + JRE(CSP) 两个 sidecar runtime。
- 安全红线：dex 是不可信第三方代码，JVM 内沙箱不可靠，**靠进程/容器隔离 + jar hash 校验 + 网络回调 Go**。
- Phase 4 完成后，0821 配置的 CMS / JS / CSP 三大来源类型全覆盖（仅剩 OCR/WebView 等硬骨头少数派与 AList/网盘永久排除）。
