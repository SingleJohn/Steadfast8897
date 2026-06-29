package source

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"fyms/internal/repository"
)

// PlayPipelineDeps 聚合非 spider-runtime 播放解析所需依赖。
type PlayPipelineDeps struct {
	Repo   *repository.SourceRepository
	Client *http.Client
}

// directVideoExtPattern 判定一个地址是否已是可直接播放的视频/音频直链。
// 扩展名后必须紧跟路径/查询/锚点边界或结尾,避免把 ?url=x.mp4 之类的网页参数误判为直链。
var directVideoExtPattern = regexp.MustCompile(`(?i)\.(m3u8|mp4|mkv|flv|ts|m4v|webm|mpd|mov|avi|wmv|rmvb|mp3|m4a|flac|wav|aac)([/?#]|$)`)

// ResolvePlayPipeline 处理 CMS/通用播放地址的解析,按序尝试,命中即返回:
//
//	1. 直链         —— rawURL 本身已是视频直链
//	2. parses 解析器 —— TVBox 配置导入的全局解析器(type=1)
//	3. 网页嗅探      —— 拉取播放页正文,正则提取真实流地址
//	4. 直链兜底      —— 仍无法解析时按原始 http(s) 地址直出
//
// spider(csp/js) provider 的播放由 ProviderRuntimeManager.ResolvePlay 处理,不走此管线。
func ResolvePlayPipeline(ctx context.Context, deps PlayPipelineDeps, playSource repository.SourcePlaySource) (*PlayResult, error) {
	rawURL := strings.TrimSpace(playSource.RawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("播放原始地址为空")
	}
	mode := strings.ToLower(strings.TrimSpace(playSource.ParseMode))

	// 1. 直链:已是可直接播放的视频地址。
	if mode == "" || mode == "unknown" || mode == "direct" {
		if isDirectVideoURL(rawURL) {
			return ResolvePlay(ctx, playSource)
		}
	}

	var lastErr error

	// 2. TVBox parses[] 解析器(强制 resolver 口径让 ParserResolver 接受)。
	if deps.Repo != nil {
		ps := playSource
		ps.ParseMode = "resolver"
		if res, err := NewParserResolver(deps.Repo, deps.Client).Resolve(ctx, ps); err == nil {
			return res, nil
		} else {
			lastErr = err
		}
	}

	// 3. 网页流嗅探。
	if isHTTPURL(rawURL) {
		if res, err := SniffPlayURL(ctx, deps.Client, playSource); err == nil {
			return res, nil
		} else {
			lastErr = err
		}
	}

	// 4. 直链兜底:无法解析时按原始地址直出,交给客户端/上游决定能否播放。
	if isHTTPURL(rawURL) {
		ps := playSource
		ps.ParseMode = "direct"
		if res, err := ResolvePlay(ctx, ps); err == nil {
			return res, nil
		} else if lastErr == nil {
			lastErr = err
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("无法解析播放地址: %s", URLHash(rawURL))
	}
	return nil, lastErr
}

func isDirectVideoURL(rawURL string) bool {
	if !isHTTPURL(rawURL) {
		return false
	}
	return directVideoExtPattern.MatchString(strings.TrimSpace(rawURL))
}
