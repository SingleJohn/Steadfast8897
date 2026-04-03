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

## 管理后台配置补充

- 后台 `管理 -> 媒体库` 页面已调整为顶部标签切换，`媒体库` 与 `扫描设置` 分开展示，更接近 Emby 的操作方式
- 后台 `管理 -> 总览` 已移除 `流量趋势`、`Emby 源状态`、`Top 重定向后端` 三个展示模块，页面聚焦基础运行状态与服务控制
- 后台左侧导航在 `元数据` 与 `工具` 之间增加了分割线，提升“管理”分组内的视觉层次

## 仓库清理约定

- 构建产物与本地依赖目录默认不入库：`web/dist/`、`web/node_modules/`、`web/.vite/`
- 本地工具配置与编辑器目录默认不入库：`.claude/`、`.vscode/`、`.idea/`
- 日志与覆盖率输出默认不入库：`*.log`、`coverage/`

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

## Bug 修复记录

### 图片服务 500 错误修复（images.go）

- **WebP 支持**：注册 `golang.org/x/image/webp` 解码器，解决 WebP 格式图片在缩放时解码失败返回 500 的问题
- **缩放降级策略**：`resizeImage` 失败时不再返回 500，改为退回原图直接发送，保证图片至少可见
- **缩放缓存**：已缩放的图片会缓存到 `data/cache/`，重复请求直接命中缓存，避免 SMB/NFS 上重复 IO
- **并发提升**：图片处理信号量从 3 提升到 10，减少高并发场景下的排队延迟

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

## 性能优化记录

### 媒体库扫描优化（scanner.go）

| 优化项 | 优化前 | 优化后 | 影响 |
|--------|--------|--------|------|
| 文件系统遍历 | countMediaEntries 预遍历 + 扫描再遍历（两次 NFS 往返） | 一次遍历收集 → 设总数 → goroutine 直接扫描 | NFS I/O 减半 |
| 扫描并发度 | 5 个 goroutine | 10 个 goroutine | 吞吐量翻倍 |
| ApplyNfoData | 每个 item 7+ 次独立 UPDATE | 1 次合并 UPDATE + 子查询替代 SELECT | DB 往返减少 ~70% |
| IsVideoExt | slice O(n) 线性搜索 | map O(1) 查找 | 高频调用开销消除 |
| NFO 正则解析 | 每个 NFO 编译 18+ 个正则 | 包级别预编译复用 | 消除重复编译 |
