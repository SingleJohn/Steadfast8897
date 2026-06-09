package services

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// actorAvatarExts 是本地头像库识别的扩展名。
var actorAvatarExts = []string{".jpg", ".jpeg", ".png", ".webp"}

// resolveActorAvatarByName 按演员姓名解析头像来源,用于不依赖 TMDB 的补全
// (尤其番号/JAV)。优先本地头像库,其次外部按名源。无命中返回空串。
// 返回值可直接写入 persons.image_path:本地绝对路径或 http(s) URL。
func resolveActorAvatarByName(cfg ActorImageConfig, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if cfg.LocalLib {
		if p := lookupLocalAvatar(cfg.LocalLibDir, name); p != "" {
			return p
		}
	}
	if cfg.ExtSource && cfg.ExtURL != "" {
		// URL 模板用 {name} 占位,如 https://host/avatar/{name}.jpg。
		return strings.ReplaceAll(cfg.ExtURL, "{name}", url.QueryEscape(name))
	}
	return ""
}

// findLocalActorImage 在媒体目录的 .actors/ 子目录按 <name>.<ext> 查找演员头像
// (Emby/Kodi/MDCx 约定)。命中返回绝对路径,否则空串。
func findLocalActorImage(mediaDir, name string) string {
	if mediaDir == "" {
		return ""
	}
	return lookupLocalAvatar(filepath.Join(mediaDir, ".actors"), name)
}

// lookupLocalAvatar 在指定目录按 <name>.<ext> 查找头像文件。
func lookupLocalAvatar(dir, name string) string {
	if dir == "" {
		return ""
	}
	safe := sanitizeAvatarName(name)
	if safe == "" {
		return ""
	}
	for _, ext := range actorAvatarExts {
		p := filepath.Join(dir, safe+ext)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// sanitizeAvatarName 去掉路径分隔符 / 上跳,防目录穿越。
func sanitizeAvatarName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "..", "_")
	return name
}
