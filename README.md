# FYMS - Emby-Compatible Media Server

高性能 Emby 兼容媒体服务器，Go + Vue 编写。支持 Infuse、Emby 等客户端连接。

## 功能

- Emby API 兼容（Infuse、Emby 客户端可直接连接）
- STRM 虚拟文件支持（302 直链播放）
- TMDB 元数据刮削
- NFO 文件解析
- 多版本媒体源选择
- 用户管理与权限策略
- 播放进度同步
- Emby 用户迁移（支持导入 Emby users.db）
- 媒体库实时文件监听
- 备份与恢复
- Emby Gateway 兼容
- Gateway 请求与 IP 观测统计
- 观测中心播放面板支持最近播放记录自动刷新

## 项目结构

```
├── main.go             # Go 后端入口
├── internal/           # Go 后端源码
├── migrations/         # PostgreSQL 数据库迁移
├── web/                # Vue 3 前端源码
│   ├── src/            #   前端组件与页面
│   ├── package.json    #   前端依赖
│   └── dist/           #   构建产物（gitignore）
├── go.mod              # Go 依赖
├── Dockerfile          # 多阶段构建（前端 + 后端）
└── docker-compose.yml  # 一键部署
```

## 本地开发

### 前置要求

- Go 1.23+
- Node.js 20+
- PostgreSQL 16+
- Redis 7+

### 后端

```bash
# 配置环境变量（或创建 .env 文件）
export DB_HOST=localhost DB_PORT=5432 DB_NAME=media_server
export DB_USER=postgres DB_PASSWORD=postgres
export REDIS_HOST=127.0.0.1 REDIS_PORT=6379

# 启动后端
go run .
```

### 前端

```bash
cd web
npm install
npm run dev
# 访问 http://localhost:3001，API 请求代理到后端 8961 端口
```

### 构建前端

```bash
cd web
npm run build
# 产物输出到 web/dist/，后端启动时自动加载
```

## 快速部署（Docker）

```bash
# 下载 docker-compose.yml
curl -O https://raw.githubusercontent.com/ffoocn/fyms/main/docker-compose.yml

# 启动（包含 PostgreSQL + Redis）
docker-compose up -d

# 访问 http://localhost:8961
```

- 新版镜像默认不再固定以 `uid=1000` 运行，`./data` 挂载目录通常不需要再手工改成 `777`
- 建议宿主机上的 `data/` 保持常规目录权限即可，例如 `755` 或 `775`
- 如果你是从旧版本升级，重建容器后建议执行一次媒体库重扫，让历史条目的本地海报/背景图路径回填到数据库
- 平台库 logo 已直接内置到后端源码，Docker 构建和发布目录不再依赖额外的 `logo/` 资源目录

### Docker 应用内自更新

FYMS 现已支持在后台 `管理 -> 总览` 中检查更新、提示新版本并直接发起更新。该功能仅面向 Docker 部署，且必须满足以下前提：

- `fyms` 容器需要挂载 Docker Socket：`/var/run/docker.sock:/var/run/docker.sock`
- `fyms` 容器必须使用持久化的 `/app/data`，用于保存更新状态和更新前备份
- 当前实例应为单容器 FYMS 主实例，不建议在多副本或编排集群中直接使用

示例：

```yaml
services:
  fyms:
    image: eianz/fyms:latest
    volumes:
      - ./data:/app/data
      - /var/run/docker.sock:/var/run/docker.sock
    environment:
      - FYMS_UPDATE_IMAGE_REPO=eianz/fyms
      - FYMS_UPDATE_DOCKER_SOCKET=/var/run/docker.sock
```

更新流程：

1. 管理员在后台点击 `检查更新`
2. 有新版本时点击 `立即更新`
3. FYMS 会先自动创建一份更新前备份
4. 程序通过 Docker API 拉取目标镜像并重建当前容器
5. 新容器启动后自动执行数据库迁移

注意事项：

- 更新过程中页面连接短暂中断是正常现象
- 自更新本质上授予了容器 Docker 宿主机控制权限，安全风险高于外部 updater
- 首版仅支持 `stable` / `beta` 两个通道
- `FYMS_UPDATE_GITHUB_REPO` 是可选项；不配置时仍可正常按 Docker 镜像检查和更新
- 若更新失败，可先查看后台更新日志，再回退到旧镜像手工重启


## 裸机部署

从 [Releases](https://github.com/ffoocn/fyms/releases) 下载最新版本的压缩包，解压后：

```bash
# 需要自行安装 PostgreSQL 和 Redis
# 配置环境变量
export PORT=8961
export DB_HOST=localhost
export DB_PORT=5432
export DB_NAME=media_server
export DB_USER=postgres
export DB_PASSWORD=postgres
export REDIS_HOST=127.0.0.1
export REDIS_PORT=6379

# 在解压后的发布目录中运行
./fyms
```

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| PORT | 8961 | 服务端口 |
| SERVER_NAME | FYMS | 服务器名称 |
| DB_HOST | localhost | PostgreSQL 地址 |
| DB_PORT | 5432 | PostgreSQL 端口 |
| DB_NAME | media_server | 数据库名 |
| DB_USER | postgres | 数据库用户 |
| DB_PASSWORD | postgres | 数据库密码 |
| REDIS_HOST | 127.0.0.1 | Redis 地址 |
| REDIS_PORT | 6379 | Redis 端口 |
| REDIS_PASSWORD | | Redis 密码（可选） |
| DB_POOL_MAX | 400 | 数据库连接池上限 |
| FYMS_UPDATE_IMAGE_REPO | eianz/fyms | 应用内更新使用的 Docker 镜像仓库 |
| FYMS_UPDATE_GITHUB_REPO | | 可选。配置后后台可显示对应 GitHub Releases 链接 |
| FYMS_UPDATE_DOCKER_SOCKET | /var/run/docker.sock | 应用内更新访问 Docker Engine 的 Socket 路径 |

## 管理后台配置补充

- 后台 `管理 -> 媒体库` 页面已调整为顶部标签切换，`媒体库` 与 `扫描设置` 分开展示，更接近 Emby 的操作方式
- 后台 `管理 -> 总览` 已移除 `流量趋势`、`Emby 源状态`、`Top 重定向后端` 三个展示模块，页面聚焦基础运行状态与服务控制
- 后台左侧导航在 `元数据` 与 `工具` 之间增加了分割线，提升“管理”分组内的视觉层次

## 仓库清理约定

- 构建产物与本地依赖目录默认不入库：`web/dist/`、`web/node_modules/`、`web/.vite/`
- 本地工具配置与编辑器目录默认不入库：`.claude/`、`.vscode/`、`.idea/`
- 日志与覆盖率输出默认不入库：`*.log`、`coverage/`
- 平台 logo 源图可仅作为本地素材保留；实际构建使用的是 `internal/assets/platform_logos.go` 中的内置资源

## 前台首页补充

- 前台 `/#/` 首页保留轮播、继续观看、收藏和分媒体库最新内容等核心浏览区
- 首页会在检测到媒体库扫描任务时展示顶部扫描横幅，并自动轮询刷新扫描进度
- 首页已移除顶部“问候语 + 用户名”欢迎模块，减少首屏干扰
- 亮色模式下首页滚动时不再切换为暗色壳层，整体视觉跟随当前主题保持一致
- 前台媒体页已移除左侧边栏，导航聚焦为顶部搜索、返回与用户菜单
- 首页轮播、扫描横幅、列表控件等圆角统一改为跟随主题设置中的 `圆角`
- 媒体详情页已支持按展示需求隐藏“导演”模块

### 元数据代理

- 后台 `管理 -> 元数据 -> 刮削代理` 支持 `http://`、`https://`、`socks5://` 代理地址
- 代理配置会在 TMDB 客户端创建时读取
- 如果正在执行“刮削缺失元数据”批量任务，修改代理后请停止并重新启动该任务，让新配置生效
- 后端日志会输出 TMDB 是否启用代理，以及代理地址格式是否无效

### 元数据保存位置

- 后台 `管理 -> 元数据 -> 刮削保存位置` 支持：
- `数据库`：图片保存到 `data/metadata/`
- `媒体目录`：图片与 NFO 直接写入媒体目录
- `两者都写`：同时写入 `data/metadata/` 和媒体目录
- 目前媒体目录模式会写入：
- 电影：`poster.jpg`、`fanart.jpg`、`movie.nfo`
- 剧集：剧集目录 `poster.jpg`、`fanart.jpg`、`tvshow.nfo`
- 季：季目录 `poster.jpg`

## docker-compose.yml 配置说明

```yaml
services:
  fyms:
    image: eianz/fyms:latest
    ports:
      - "8961:8961"          # 左边可改
    volumes:
      - ./data:/app/data     # 应用数据
      - /mnt:/mnt:ro         # 媒体文件目录（只读）
    environment:
      - DB_HOST=db
      - REDIS_HOST=redis
      - REDIS_PASSWORD=fyms_redis_secret
```

## 客户端连接

| 客户端 | 服务器地址 |
|--------|-----------|
| Infuse | `http://IP:8961` |
| Emby 客户端 | `http://IP:8961` |
| Emby Gateway | upstream `http://IP:8961` |

## 功能更新记录

### 平台 / 虚拟库增强（library.go / platform.go / item.go / coverart / LibrariesPage.vue）

围绕平台（虚拟）库做了四项增强，相关迁移 `052`~`054`：

- **封面风格可选**：单个虚拟库「封面」与「一键生成封面」改为弹窗选择风格（复用普通库的 ninegrid/showcase 等风格与 showcase 选项），不再只能用默认风格。`POST /Library/Platforms/:id/Image/Generate` 与 `.../CoverArt/GenerateAll` 透传 `{Style, Options}`；后续在 `coverart` 注册新风格，前后端下拉自动出现
- **自定义库名称**（迁移 `052`）：新增 `platform_libraries.display_name`，可自由命名虚拟库，优先级 `display_name` > 内置本地化名 > `platform_name`；logo/渐变仍按原名匹配，改名不影响图标。入口 `POST /Library/Platforms/:id/Rename` + 列表「重命名」
- **实际库与虚拟库统一排序**（迁移 `053`）：新增 `library_display_order` 表与「整体排序」页签，把物理媒体库和虚拟库放在一个列表里混排；`getUserViews` 有记录时按统一顺序输出，无记录回退旧的「平台库排列位置」(before/after)。入口 `POST /Library/DisplayOrder`
- **多维聚合一个库**（迁移 `054`）：新增 `platform_libraries.match_values TEXT[]`，一个虚拟库可绑定多个匹配值（`= ANY`），用于把簡繁/译名等同一片商或演员的不同写法合并进同一个库；`match_value` 仍作主值保证唯一键与虚拟库 ID 稳定。列表「聚合」弹窗可查同维度值勾选合并 / 移除别名，入口 `POST·DELETE /Library/Platforms/:id/Values`

## Bug 修复记录

### 图片服务 500 错误修复（images.go）

- **WebP 支持**：注册 `golang.org/x/image/webp` 解码器，解决 WebP 格式图片在缩放时解码失败返回 500 的问题
- **缩放降级策略**：`resizeImage` 失败时不再返回 500，改为退回原图直接发送，保证图片至少可见
- **缩放缓存**：已缩放的图片会缓存到 `data/cache/`，重复请求直接命中缓存，避免 SMB/NFS 上重复 IO
- **并发提升**：图片处理信号量从 3 提升到 10，减少高并发场景下的排队延迟

### Docker 封面权限与本地海报回填修复（Dockerfile / scanner.go）

- **容器数据目录权限兼容**：运行镜像时不再固定为 `uid=1000`，避免宿主机 bind mount 的 `./data` 因属主不一致而必须设置为 `777` 才能正常生成缓存、下载图片或返回封面
- **单文件电影支持本地海报**：电影以单个视频文件存在时，扫描阶段也会从同目录识别 `poster` / `cover` / `folder` / `thumb` 以及 `fanart` / `backdrop` / `background` / `landscape`
- **重扫自动回填图片路径**：已存在的电影、剧集、季在重新扫描时，如果目录里后来补上了本地海报/背景图，会自动更新数据库中的图片路径与图片标签，不再一直沿用首次入库时的“无图”状态

### 剧集排序修复（compat.go / item.go / scanner.go）

- **`buildOrderBy` 支持 `IndexNumber`**：Emby 客户端常发 `SortBy=IndexNumber`，之前未支持会退回 `sort_name` 字母序，导致集序错乱
- **稳定排序**：`getEpisodes` SQL 增加 `i.id ASC` 作为最终排序键，保证同 `index_number` 条目的顺序稳定
- **`ApplyNfoData` 保护**：Episode 类型的 `sort_name` 不再被 NFO 标题覆盖，保留 `episode %04d` 数字格式

### 搜索空白修复（library.go / compat.go）

- **查询参数大小写兼容**：`parseItemQueryOptions` 和 `itemsSearch` 同时支持 PascalCase / camelCase / 全小写参数名，兼容更多第三方客户端
- **`GET /Items` 增加 `ParentId` + `Recursive` 支持**：compat 端点之前不处理这两个参数，导致依赖它们的客户端返回全量或空白
- **`GET /Items` 增加 `StartIndex` 分页支持**：避免客户端分页请求无效

### 元数据刮削保存修复（tmdb.go）

- **`media_dir` 模式降级**：当写入媒体目录失败（权限、网络盘不可写）时，自动降级到 `data/metadata/`，不再丢失已下载的元数据
- **Season 海报同步修复**：季海报写入逻辑同步增加降级保护
- **HTTP 路径排除**：`resolveScrapeSaveTargets` 跳过以 `http` 开头的 `file_path`（STRM 远程路径），避免拼出无效本地路径
- **增强日志**：所有媒体目录写入失败都有 `slog.Warn` 输出，方便排查权限和路径问题

### 任务总数实时统计修复（library.go / tmdb.go / probe_task.go）

- **刮削/探测总数改为实时计算**：TMDB 刮削、平台重新刮削、媒体信息探测页面在非运行态会重新按当前数据库计算“待处理数量”，不再沿用上一次任务完成后的旧 `total`
- **任务快照与实时待处理数分离**：运行中继续显示本次任务快照；任务结束后页面会展示当前仍待处理的真实数量，新增媒体库或重新扫描后无需重启页面即可看到更新
- **统一统计口径**：TMDB 刮削的启动查询与缺失数量统计共用同一条件，减少多个入口各自计算造成的数字漂移
- **统一任务汇总入口**：新增 `GET /Library/Tasks/Summary`，统一返回 TMDB 刮削、媒体信息探测、平台识别三类任务的实时状态与待处理数量，页面不再各自拼装多套统计来源

### 播放软件媒体信息获取修复（compat.go）

- **`GET /Items` 剧集/季图片回退缺失**：compat 端点的 SQL 查询未 JOIN 父级 Series 获取图片标签，导致 Episode/Season 类型条目自身无图片时，客户端（如 Infuse）无法获取到封面和背景图。修复后增加 `LEFT JOIN items sf` 获取 `series_primary_image_tag`、`series_backdrop_image_tag`、`series_fallback_id`，经 `FormatItemDto` 自动填充父级图片信息
- **`GET /Items` TotalRecordCount 返回当前页长度**：之前 `TotalRecordCount: len(items)` 返回本页条目数而非数据库总数，导致客户端分页时以为已到末页、不再请求后续页面，表现为"部分媒体信息获取不到"。修复后改为独立 COUNT 查询获取真实总数
- **`GET /Search/Hints` 剧集/季图片回退缺失**：搜索结果中 Episode/Season 缺少父剧集的图片标签，客户端展示搜索结果时无封面。修复后 JOIN 父级 Series，当条目自身无图片时回退到 `SeriesPrimaryImageTag` / `SeriesBackdropImageTag`，并补充 `PrimaryImageItemId`、`BackdropImageItemId` 让客户端能正确请求图片

### 多版本合并深度修复（item.go / images.go / videos.go / compat.go / library.go）

参考 Jellyfin 核心源码（`Video.cs`、`BaseItemRepository.cs`、`DtoService.cs`）的实现方式，对持久化合并功能进行了全面重构：

**核心设计变更（基于 Jellyfin `PresentationUniqueKey` 机制的适配）：**

- **按 studio 分组合并**：合并仅在相同平台（studio）内进行，不再跨平台合并。Netflix 的电影和 HBO 的同一电影不会被合并，各自在各自的平台库中保持可见
- **仅合并 Movie 类型**：Series 不参与合并。Series 合并会导致 secondary 的剧集/季成为孤儿（无法通过 primary Series 访问），这是 Jellyfin 也未解决的已知限制
- **普通用户媒体库优先展示合并结果**：普通媒体库浏览、最新内容与 Emby 兼容 `/Items` 列表会优先按当前库选择一个代表项，同一电影的多个物理版本只显示一条；进入详情/播放后再聚合为多个 `MediaSources`
- **避免跨库主版本导致条目消失**：如果某个合并组的 global primary 落在别的物理库，当前库不会直接把本库版本过滤没，而是会从本库 secondary 中挑一个代表项显示
- **平台虚拟库继续复用合并结果**：平台库仍按 `studio` 分组，但展示层不再是唯一的合并入口；普通用户媒体库会先得到正确的一条展示结果，再由平台库复用同一套聚合能力
- **全量重置+重算**：每次执行合并时先重置所有旧合并，然后按新规则重新计算，保证幂等性

**元数据与 MediaSources：**

- **元数据同步**：合并后 primary 从组内成员继承最佳元数据（图片、概览、评分等），使用 `NULLIF` 同时处理 NULL 和空字符串
- **图片回退**：`serveImage` 对 Movie/Series 增加 merged 回退链
- **PlaybackInfo 聚合**：`getPlaybackInfo` 聚合 primary + 所有 merged secondary 的 MediaSources
- **电影版本回填**：电影目录扫描现在会像剧集一样写入 `media_versions`；对已存在的老电影条目再次扫库时，也会自动补回缺失的版本记录，避免客户端只能看到单个默认源
- **剧集版本并入补齐**：同库同剧同季同集但来自不同目录时，扫描阶段会复用已有 `Episode`，并把新目录里的视频继续并入同一个 `Episode` 的 `media_versions`，避免只保留首个目录版本
- **secondary 透明重定向**：详情和播放请求自动重定向到 primary
- **合并诊断**：`POST /Library/MergeVersions` 返回 `merged`、`total_primaries`、`total_secondaries` 计数

## 性能优化记录

### 媒体库扫描优化（scanner.go）

| 优化项 | 优化前 | 优化后 | 影响 |
|--------|--------|--------|------|
| 文件系统遍历 | countMediaEntries 预遍历 + 扫描再遍历（两次 NFS 往返） | 一次遍历收集 → 设总数 → goroutine 直接扫描 | NFS I/O 减半 |
| 扫描并发度 | 5 个 goroutine | 10 个 goroutine | 吞吐量翻倍 |
| ApplyNfoData | 每个 item 7+ 次独立 UPDATE | 1 次合并 UPDATE + 子查询替代 SELECT | DB 往返减少 ~70% |
| IsVideoExt | slice O(n) 线性搜索 | map O(1) 查找 | 高频调用开销消除 |
| NFO 正则解析 | 每个 NFO 编译 18+ 个正则 | 包级别预编译复用 | 消除重复编译 |
