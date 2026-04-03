# Emby 兼容路由清单

本文档整理自原版 Rust 程序 `Fyms-main` 的路由实现，用于识别 FYMS 为替换 Emby 而提供的兼容接口。

## 总规则

- 下列所有路由都同时支持两种访问形式：
- 根路径：`/xxx`
- Emby 前缀路径：`/emby/xxx`
- 例如：`/System/Info` 与 `/emby/System/Info` 都有效。

## 核心兼容路由

这部分是最典型的 Emby 客户端必需接口。


| 路由                                    | 方法            | 作用      | Emby 替代意义          |
| ------------------------------------- | ------------- | ------- | ------------------ |
| `/System/Info`                        | `GET`         | 服务器信息   | 客户端识别服务端           |
| `/System/Info/Public`                 | `GET`         | 公共服务器信息 | 登录前探测              |
| `/System/Ping`                        | `GET`, `POST` | 心跳      | 客户端保活              |
| `/Users/AuthenticateByName`           | `POST`        | 用户登录    | Emby 登录接口          |
| `/Users/authenticatebyname`           | `POST`        | 用户登录    | 兼容大小写变体            |
| `/Users/Me`                           | `GET`         | 当前用户信息  | 登录后拉取用户资料          |
| `/Users/Public`                       | `GET`         | 公共用户列表  | 登录页用户展示            |
| `/Users/{userId}`                     | `GET`         | 用户详情    | 用户资料读取             |
| `/Users/{userId}/Policy`              | `POST`        | 更新策略    | Emby 用户权限模型        |
| `/Sessions`                           | `GET`         | 当前会话    | Emby 设备/会话查看       |
| `/Sessions/Playing`                   | `POST`        | 开始播放    | Emby 播放上报          |
| `/Sessions/Playing/Progress`          | `POST`        | 播放进度    | Emby 播放进度同步        |
| `/Sessions/Playing/Stopped`           | `POST`        | 停止播放    | Emby 停播上报          |
| `/Items/{itemId}/PlaybackInfo`        | `GET`, `POST` | 播放信息    | 客户端获取播放源           |
| `/Videos/{itemId}/stream`             | `GET`         | 视频流     | Emby 直链播放          |
| `/Videos/{itemId}/stream.{container}` | `GET`         | 指定容器流   | Emby 流地址格式         |
| `/Items/{itemId}/Images/{imageType}`  | `GET`         | 图片      | Emby 封面接口          |
| `/Users/{userId}/Views`               | `GET`         | 媒体库视图   | 首页媒体库              |
| `/Users/{userId}/Items`               | `GET`         | 列表浏览    | Emby 目录浏览          |
| `/Users/{userId}/Items/{itemId}`      | `GET`         | 条目详情    | 影片/剧集详情            |
| `/Items`                              | `GET`         | 通用查询    | Emby Items 搜索/批量查询 |
| `/Shows/{seriesId}/Seasons`           | `GET`         | 剧集季列表   | Emby 剧集结构          |
| `/Shows/{seriesId}/Episodes`          | `GET`         | 剧集集列表   | Emby 剧集结构          |


## 用户与启动向导


| 路由                              | 方法               | 说明      |
| ------------------------------- | ---------------- | ------- |
| `/Users/New`                    | `POST`           | 新建用户    |
| `/Users/Query`                  | `GET`            | 用户查询    |
| `/Users`                        | `GET`            | 全部用户    |
| `/Users/{userId}`               | `POST`, `DELETE` | 更新或删除用户 |
| `/Users/{userId}/Password`      | `POST`           | 修改密码    |
| `/Users/{userId}/Configuration` | `POST`           | 用户配置    |
| `/Sessions/Logout`              | `POST`           | 注销      |
| `/Startup/Configuration`        | `GET`            | 初始化向导配置 |
| `/Startup/User`                 | `GET`, `POST`    | 初始化管理员  |
| `/Startup/Complete`             | `POST`           | 完成初始化   |


## 媒体库与浏览


| 路由                                  | 方法    | 说明       |
| ----------------------------------- | ----- | -------- |
| `/Users/{userId}/Items/Resume`      | `GET` | 继续观看     |
| `/Users/{userId}/Items/Latest`      | `GET` | 最新内容     |
| `/Users/{userId}/Items/LatestBatch` | `GET` | 批量最新内容   |
| `/Genres`                           | `GET` | 类型列表     |
| `/Items/Counts`                     | `GET` | 媒体总数统计   |
| `/Studios`                          | `GET` | 制片厂，占位兼容 |
| `/Persons`                          | `GET` | 人员，占位兼容  |
| `/Artists`                          | `GET` | 艺术家，占位兼容 |
| `/Shows/NextUp`                     | `GET` | 下一集，占位兼容 |


## 收藏与播放状态


| 路由                                       | 方法               | 说明      |
| ---------------------------------------- | ---------------- | ------- |
| `/Users/{userId}/PlayedItems/{itemId}`   | `POST`, `DELETE` | 标记已看或未看 |
| `/Users/{userId}/FavoriteItems/{itemId}` | `POST`, `DELETE` | 收藏或取消收藏 |


## 媒体库管理


| 路由                                     | 方法               | 说明          |
| -------------------------------------- | ---------------- | ----------- |
| `/Library/VirtualFolders`              | `GET`, `DELETE`  | 获取媒体库或删除媒体库 |
| `/Library/VirtualFolders/Add`          | `POST`           | 新建媒体库       |
| `/Library/VirtualFolders/Paths`        | `POST`, `DELETE` | 增删媒体库路径     |
| `/Library/VirtualFolders/{id}`         | `GET`            | 媒体库详情       |
| `/Library/VirtualFolders/{id}/Image`   | `POST`, `DELETE` | 媒体库封面       |
| `/Library/VirtualFolders/{id}/Refresh` | `POST`           | 刷新单个媒体库     |
| `/Library/Refresh`                     | `POST`           | 全库扫描        |
| `/Library/Scan/Progress`               | `GET`            | 扫描进度        |
| `/Library/BrowseDirectories`           | `GET`            | 浏览服务器目录     |


## 探测与刮削


| 路由                          | 方法     | 说明      |
| --------------------------- | ------ | ------- |
| `/Library/Probe/Start`      | `POST` | 启动媒体探测  |
| `/Library/Probe/Stop`       | `POST` | 停止媒体探测  |
| `/Library/Probe/Progress`   | `GET`  | 探测进度    |
| `/Items/{itemId}/Refresh`   | `POST` | 刮削单个项目  |
| `/Library/Refresh/Metadata` | `POST` | 全量刮削元数据 |
| `/Library/Scrape/Progress`  | `GET`  | 刮削进度    |
| `/Library/Scrape/Stop`      | `POST` | 停止刮削    |


## 图片、设备、显示偏好、插件兼容


| 路由                                                | 方法            | 说明          |
| ------------------------------------------------- | ------------- | ----------- |
| `/Items/{itemId}/Images/{imageType}/{imageIndex}` | `GET`         | 图片索引兼容      |
| `/DisplayPreferences/{id}`                        | `GET`, `POST` | 显示偏好        |
| `/Devices/Info`                                   | `GET`         | 设备信息        |
| `/Plugins`                                        | `GET`         | 插件列表，占位兼容   |
| `/Sessions/Capabilities/Full`                     | `POST`        | 客户端能力上报     |
| `/Sessions/{sessionId}/Playing/Stop`              | `POST`        | 指定会话停止      |
| `/Sessions/{sessionId}/Message`                   | `POST`        | 会话消息        |
| `/Notifications/{userId}/Summary`                 | `GET`         | 通知摘要，占位兼容   |
| `/LiveTv/Info`                                    | `GET`         | LiveTV，占位兼容 |
| `/Channels`                                       | `GET`         | 频道，占位兼容     |


## Emby 生态插件兼容

这部分主要用于兼容 Emby 插件，不属于最基础的播放器 API。


| 路由                                         | 方法            | 说明                       |
| ------------------------------------------ | ------------- | ------------------------ |
| `/ApiKeys`                                 | `GET`, `POST` | API Key 管理               |
| `/ApiKeys/{id}`                            | `DELETE`      | 删除 API Key               |
| `/user_usage_stats/submit_custom_query`    | `POST`        | 兼容 Playback Reporting 插件 |
| `/user_usage_stats/user_activity`          | `GET`         | 用户活跃统计                   |
| `/user_usage_stats/PlayActivity`           | `GET`         | 播放活动统计                   |
| `/user_usage_stats/HourlyReport`           | `GET`         | 小时报表                     |
| `/user_usage_stats/{type}/BreakdownReport` | `GET`         | 分类统计                     |
| `/user_usage_stats/RecentPlayback`         | `GET`         | 最近播放                     |


## 其它兼容接口


| 路由                            | 方法              | 说明                 |
| ----------------------------- | --------------- | ------------------ |
| `/web/ConfigurationPage`      | `GET`           | Web 配置页兼容          |
| `/Branding/Configuration`     | `GET`           | 品牌配置               |
| `/System/Logs`                | `GET`           | 日志查看               |
| `/System/Backup`              | `POST`          | 备份                 |
| `/System/Backups`             | `GET`           | 备份列表               |
| `/System/Backups/{filename}`  | `GET`, `DELETE` | 下载或删除备份            |
| `/System/Restore`             | `POST`          | 恢复备份               |
| `/System/EmbyMigrate`         | `POST`          | Emby 用户迁移          |
| `/Library/Webhook/CloudDrive` | `POST`          | CloudDrive Webhook |


## 最小 Emby 兼容集合

如果目标只是让 Emby、Infuse 这类客户端完成登录、浏览、播放，优先关注以下接口：

- `/System/Info`
- `/System/Info/Public`
- `/Users/AuthenticateByName`
- `/Users/Me`
- `/Users/{userId}/Views`
- `/Users/{userId}/Items`
- `/Users/{userId}/Items/{itemId}`
- `/Items/{itemId}/PlaybackInfo`
- `/Videos/{itemId}/stream`
- `/Videos/{itemId}/stream.{container}`
- `/Items/{itemId}/Images/{imageType}`
- `/Sessions/Playing`
- `/Sessions/Playing/Progress`
- `/Sessions/Playing/Stopped`
- `/Shows/{seriesId}/Seasons`
- `/Shows/{seriesId}/Episodes`

## 与当前 Go 版对比

### 结论

- 从“路由是否注册”这一层看，当前 Go 版已经覆盖了原版 Rust 清单里的绝大多数 Emby 兼容接口。
- 按我逐项对照，原版文档中列出的核心兼容路由在 Go 版里基本都能找到对应注册路径，且同样同时支持 `/xxx` 和 `/emby/xxx`。
- 当前差异主要不在“有没有这个路由”，而在“权限控制、返回内容、细节行为”上。

### 路由覆盖结果

| 对比项 | 结果 | 说明 |
|---|---|---|
| 系统类路由 | 已覆盖 | `/System/*`、`/web/ConfigurationPage`、`/Branding/Configuration` 均已注册 |
| 用户与认证 | 已覆盖 | `/Users/*`、`/Startup/*`、`/Sessions/Logout` 均已注册 |
| 媒体库浏览 | 已覆盖 | `/Users/{userId}/Views`、`/Items`、`/Shows/*`、`/Genres` 均已注册 |
| 播放链路 | 已覆盖 | `/Items/{itemId}/PlaybackInfo`、`/Videos/*`、`/Sessions/Playing*` 均已注册 |
| 图片接口 | 已覆盖 | `/Items/{itemId}/Images/*` 已注册 |
| 兼容占位接口 | 已覆盖 | `/Plugins`、`/Channels`、`/LiveTv/Info`、`/Notifications/*` 等已注册 |
| 统计插件兼容 | 已覆盖 | `/user_usage_stats/*` 已注册 |
| Webhook | 已覆盖 | `/Library/Webhook/CloudDrive` 已注册 |

### 主要行为差异

下面这些是当前 Go 版与原版 Rust 最值得注意的差异。

| 路由 | Rust 原版 | 当前 Go 版 | 影响 |
|---|---|---|---|
| `/Users/Public` | 固定返回空数组 | 返回数据库中的公开用户 | 登录页展示行为不同 |
| `/Users` | 管理员可访问 | 任何已登录用户可访问 | 权限比原版更宽 |
| `/Users/Query` | 管理员可访问 | 任何已登录用户可访问 | 权限比原版更宽 |
| `/Users/{userId}` `GET` | 任意已登录用户可读 | 仅本人或管理员可读 | 权限比原版更严 |
| `/Users/New` | `Password` 可为空 | 需要 `Name` 和 `Password` | 与原版建用户行为不完全一致 |
| `/Startup/User` `POST` | 返回 `204` | 返回新建用户 JSON | 初始化向导返回格式不同 |
| `/Plugins` | 需要认证 | 未挂认证中间件 | 权限比原版更宽 |
| `/Channels` | 需要认证 | 未挂认证中间件 | 权限比原版更宽 |
| `/LiveTv/Info` | 需要认证 | 未挂认证中间件 | 权限比原版更宽 |
| `/Notifications/{userId}/Summary` | 需要认证 | 未挂认证中间件 | 权限比原版更宽 |
| `/Sessions/Capabilities/Full` | 需要认证 | 未挂认证中间件 | 权限比原版更宽 |
| `/Library/Scan/Progress` | 需要认证 | 未挂认证中间件 | 权限比原版更宽 |
| `/Library/Probe/Progress` | 需要认证 | 未挂认证中间件 | 权限比原版更宽 |
| `/Library/Scrape/Progress` | 需要认证 | 未挂认证中间件 | 权限比原版更宽 |
| `/Genres` | 需要认证 | 未挂认证中间件 | 权限比原版更宽 |

### Go 版新增但不在原版清单中的路由

这些接口不是原版 Rust 清单里的核心兼容路由，但当前 Go 版额外提供了：

- `/Users/{userId}/Authenticate`
- `/Items/{itemId}/Similar`
- `/Library/VirtualFolders/Name`
- `/Library/VirtualFolders/Update`
- `/Library/VirtualFolders/{id}/ImageUrl`
- `/Items/{itemId}/Images/{imageType}` `POST`
- `/Items/{itemId}/Images/{imageType}` `DELETE`
- `/Items/{itemId}/Scrape`
- `/Library/Scrape/All`
- `/Library/Scrape/Missing`
- `/Library/Browse`
- `/Auth/Keys`
- `/Devices`
- `/LiveTv/Channels`
- `/LiveTv/Programs`
- `/Notifications`
- `/Notifications/Types`
- `/Stats/UserActivity`
- `/Stats/DailyActivity`
- `/Stats/HourlyReport`
- `/Stats/BreakdownReport`
- `/Stats/RecentPlayback`

### 适合优先检查的差异点

如果你的目标是“尽量复刻原版 Emby 兼容行为”，建议优先检查以下几类：

- 认证要求是否与原版一致，尤其是 `/Plugins`、`/Channels`、`/LiveTv/Info`、`/Notifications/*`
- 用户相关接口的权限边界，尤其是 `/Users`、`/Users/Query`、`/Users/{userId}`
- 初始化流程返回格式，尤其是 `/Startup/User`
- 创建用户接口对空密码或可选密码的兼容程度

## 来源文件

本清单主要整理自以下原版 Rust 文件：

- `Fyms-main/src/main.rs`
- `Fyms-main/src/routes/system.rs`
- `Fyms-main/src/routes/users.rs`
- `Fyms-main/src/routes/library.rs`
- `Fyms-main/src/routes/playback.rs`
- `Fyms-main/src/routes/videos.rs`
- `Fyms-main/src/routes/images.rs`
- `Fyms-main/src/routes/compat.rs`
- `Fyms-main/src/routes/stats.rs`
- `Fyms-main/src/routes/webhook.rs`

