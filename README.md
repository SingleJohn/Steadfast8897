# FYMS - Emby-Compatible Media Server

高性能 Emby 兼容媒体服务器，Rust 编写。支持 Infuse、Emby 等客户端连接。

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

## 快速部署（Docker）

```bash
# 下载 docker-compose.yml
curl -O https://raw.githubusercontent.com/Laiqingde/Fyms/main/docker-compose.yml

# 启动（包含 PostgreSQL + Redis）
docker-compose up -d

# 访问 http://localhost:8961
```

## 裸机部署

从 [Releases](https://github.com/Laiqingde/Fyms/releases) 下载最新版本的压缩包，解压后：

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

# 运行
./fyms-rs
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

## docker-compose.yml 配置说明

```yaml
services:
  fyms:
    image: qingde/fyms-rust:latest
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
