package middleware

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/gin-gonic/gin"
)

// pathStrippingWriter 包裹 gin.ResponseWriter,采用懒判定:
// 首次写入时根据 Content-Type 决定是否缓冲。仅对 JSON 响应缓冲(便于整体改写),
// 其余响应(视频流、图片等二进制)直接透传到底层 Writer,不占用内存、不引入延迟。
type pathStrippingWriter struct {
	gin.ResponseWriter
	buf         *bytes.Buffer
	decided     bool
	passthrough bool
}

func (w *pathStrippingWriter) decide() {
	if w.decided {
		return
	}
	w.decided = true
	// c.JSON 会写出 "application/json; charset=utf-8"
	w.passthrough = !strings.Contains(w.Header().Get("Content-Type"), "application/json")
}

func (w *pathStrippingWriter) Write(b []byte) (int, error) {
	w.decide()
	if w.passthrough {
		return w.ResponseWriter.Write(b)
	}
	return w.buf.Write(b)
}

func (w *pathStrippingWriter) WriteString(s string) (int, error) {
	w.decide()
	if w.passthrough {
		return w.ResponseWriter.WriteString(s)
	}
	return w.buf.WriteString(s)
}

// HideMediaPaths 返回一个中间件:对非管理员用户隐藏响应 JSON 中所有的 Path 字段
// (即媒体的物理存储路径 / 媒体库根目录)。管理员的响应原样透传。
//
// 仅应挂在返回「浏览类元数据」的路由组上(媒体详情、列表、剧集、搜索等)。
// 切勿用于播放 / 视频 / 图片路由:`/Items/:id/PlaybackInfo` 返回的
// MediaSource.Path(可能是 strm 解析出的直链地址)对部分客户端的直链播放是有
// 功能意义的,剥离会影响播放。
func HideMediaPaths() gin.HandlerFunc {
	return func(c *gin.Context) {
		w := &pathStrippingWriter{ResponseWriter: c.Writer, buf: &bytes.Buffer{}}
		c.Writer = w

		c.Next()

		w.decide()
		if w.passthrough {
			// 二进制 / 非 JSON:已在 c.Next() 期间直接写出底层 Writer。
			return
		}

		body := w.buf.Bytes()
		if u := GetAuthUser(c); u == nil || !u.IsAdmin {
			var v interface{}
			if json.Unmarshal(body, &v) == nil {
				stripPathKeys(v)
				if out, err := json.Marshal(v); err == nil {
					body = out
				}
			}
		}

		// 改写后长度变化,清掉可能存在的 Content-Length,交给 net/http 重新计算。
		w.Header().Del("Content-Length")
		w.ResponseWriter.Write(body)
	}
}

// stripPathKeys 递归删除 JSON 结构中所有名为 "Path" 的键。
// Emby 协议里 "Path" 一律代表文件系统 / URL 路径,对非管理员统一隐藏。
func stripPathKeys(v interface{}) {
	switch t := v.(type) {
	case map[string]interface{}:
		delete(t, "Path")
		for _, child := range t {
			stripPathKeys(child)
		}
	case []interface{}:
		for _, child := range t {
			stripPathKeys(child)
		}
	}
}
