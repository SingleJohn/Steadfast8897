package services

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 演员头像相关的 system_config 键。全部可在前端「演员头像」卡片配置。
const (
	cfgActorImgNfoThumb    = "actor_img_nfo_thumb"      // 解析 NFO <actor><thumb>(默认开)
	cfgActorImgLocalActors = "actor_img_local_actors"   // 扫描媒体目录 .actors/(默认开)
	cfgActorImgLocalLib    = "actor_img_local_lib"       // 本地头像库按名匹配(默认关)
	cfgActorImgLocalLibDir = "actor_img_local_lib_path"  // 本地头像库目录
	cfgActorImgExtSource   = "actor_img_ext_source"      // 外部按名头像源(默认关)
	cfgActorImgExtURL      = "actor_img_ext_url"         // 外部源 URL 模板,{name} 占位
)

// defaultActorAvatarDir 是本地头像库默认目录(相对 data/)。
const defaultActorAvatarDir = "data/actor_avatars"

// ActorImageConfig 是演员头像各开关的快照。
type ActorImageConfig struct {
	NfoThumb    bool
	LocalActors bool
	LocalLib    bool
	LocalLibDir string
	ExtSource   bool
	ExtURL      string
}

var (
	actorImgCfgMu      sync.RWMutex
	actorImgCfgCache   ActorImageConfig
	actorImgCfgLoaded  time.Time
	actorImgCfgHasData bool
)

const actorImgCfgTTL = 5 * time.Second

// LoadActorImageConfig 读取(并短缓存)演员头像配置。批量扫描时避免每条目查库。
func LoadActorImageConfig(ctx context.Context, pool *pgxpool.Pool) ActorImageConfig {
	actorImgCfgMu.RLock()
	if actorImgCfgHasData && time.Since(actorImgCfgLoaded) < actorImgCfgTTL {
		cfg := actorImgCfgCache
		actorImgCfgMu.RUnlock()
		return cfg
	}
	actorImgCfgMu.RUnlock()

	cfg := ActorImageConfig{
		NfoThumb:    readBoolSystemConfig(ctx, pool, cfgActorImgNfoThumb, true),
		LocalActors: readBoolSystemConfig(ctx, pool, cfgActorImgLocalActors, true),
		LocalLib:    readBoolSystemConfig(ctx, pool, cfgActorImgLocalLib, false),
		LocalLibDir: readSystemConfigValue(ctx, pool, cfgActorImgLocalLibDir),
		ExtSource:   readBoolSystemConfig(ctx, pool, cfgActorImgExtSource, false),
		ExtURL:      readSystemConfigValue(ctx, pool, cfgActorImgExtURL),
	}
	if cfg.LocalLibDir == "" {
		cfg.LocalLibDir = defaultActorAvatarDir
	}

	actorImgCfgMu.Lock()
	actorImgCfgCache = cfg
	actorImgCfgLoaded = time.Now()
	actorImgCfgHasData = true
	actorImgCfgMu.Unlock()
	return cfg
}

// InvalidateActorImageConfig 在前端保存配置后调用,丢弃缓存让新值立即生效。
func InvalidateActorImageConfig() {
	actorImgCfgMu.Lock()
	actorImgCfgHasData = false
	actorImgCfgMu.Unlock()
}
