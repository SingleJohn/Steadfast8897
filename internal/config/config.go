package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

var (
	BuildVersion = ""
	BuildCommit  = ""
	BuildTime    = ""
	BuildRepo    = ""
)

type AppConfig struct {
	Port         int
	FrontendPort int
	ServerName   string
	ServerID     string
	Version      string

	databaseURLArg string // 命令行参数传入的连接字符串（私有字段）

	DBHost     string
	DBPort     int
	DBName     string
	DBUser     string
	DBPassword string
	DBPoolMax  int
	DBPoolMin  int

	RedisHost     string
	RedisPort     int
	RedisPassword string

	DataDir  string
	CacheDir string

	ImageCacheSourceDir  string
	ImageCacheResizedDir string
	ImageCacheMaxGB      int
	// CopyLocalImages 为 true 时,非 data 目录下的本地/挂载原图会复制一份到 cache/sources;
	// 默认 false:本地/挂载原图直读不复制(省 data 空间)。仅 URL 源始终下载缓存。
	CopyLocalImages bool

	UpdateImageRepo    string
	UpdateGitHubRepo   string
	UpdateDockerSocket string
}

func NewAppConfig() *AppConfig {
	return NewAppConfigWithArgs(nil)
}

func NewAppConfigWithArgs(databaseURL *string) *AppConfig {
	godotenv.Load()

	dataDir := envOr("DATA_DIR", "data")
	os.MkdirAll(dataDir, 0755)

	cacheDir := filepath.Join(dataDir, "cache")
	os.MkdirAll(cacheDir, 0755)

	imgSourceDir := filepath.Join(cacheDir, "sources")
	imgResizedDir := filepath.Join(cacheDir, "resized")
	os.MkdirAll(imgSourceDir, 0755)
	os.MkdirAll(imgResizedDir, 0755)

	serverID := loadOrCreateServerID(dataDir)

	cfg := &AppConfig{
		Port:         envInt("PORT", 8961),
		FrontendPort: envInt("FRONTEND_PORT", 3001),
		ServerName:   envOr("SERVER_NAME", "FYMS"),
		ServerID:     serverID,
		Version:      resolveVersion(),

		DBHost:     envOr("DB_HOST", "localhost"),
		DBPort:     envInt("DB_PORT", 5432),
		DBName:     envOr("DB_NAME", "media_server"),
		DBUser:     envOr("DB_USER", "postgres"),
		DBPassword: envOr("DB_PASSWORD", "postgres"),
		DBPoolMax:  envInt("DB_POOL_MAX", 400),
		DBPoolMin:  envInt("DB_POOL_MIN", 20),

		RedisHost:     envOr("REDIS_HOST", "127.0.0.1"),
		RedisPort:     envInt("REDIS_PORT", 6379),
		RedisPassword: envOr("REDIS_PASSWORD", ""),

		DataDir:  dataDir,
		CacheDir: cacheDir,

		ImageCacheSourceDir:  imgSourceDir,
		ImageCacheResizedDir: imgResizedDir,
		ImageCacheMaxGB:      envInt("IMAGE_CACHE_MAX_GB", 5),
		CopyLocalImages:      envBool("FYMS_IMAGE_CACHE_COPY_LOCAL", false),

		UpdateImageRepo:    envOr("FYMS_UPDATE_IMAGE_REPO", "eianz/fyms"),
		UpdateGitHubRepo:   envOr("FYMS_UPDATE_GITHUB_REPO", "eianz/fyms"),
		UpdateDockerSocket: envOr("FYMS_UPDATE_DOCKER_SOCKET", "/var/run/docker.sock"),
	}

	if databaseURL != nil && *databaseURL != "" {
		cfg.databaseURLArg = *databaseURL
	}

	return cfg
}

func resolveVersion() string {
	if v := envOr("VERSION", ""); v != "" {
		return v
	}
	if BuildVersion != "" {
		return BuildVersion
	}
	return "dev"
}

func (c *AppConfig) DatabaseURL() string {
	// 优先级：命令行参数 > 环境变量 DATABASE_URL > 独立环境变量拼接
	if c.databaseURLArg != "" {
		return c.databaseURLArg
	}
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		return dbURL
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName)
}

func envOr(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}

func envBool(key string, defaultVal bool) bool {
	if v := os.Getenv(key); v != "" {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return defaultVal
}

func loadOrCreateServerID(dataDir string) string {
	idPath := filepath.Join(dataDir, "server-id")
	data, err := os.ReadFile(idPath)
	if err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id
		}
	}
	id := strings.ReplaceAll(uuid.New().String(), "-", "")
	os.WriteFile(idPath, []byte(id), 0644)
	return id
}
