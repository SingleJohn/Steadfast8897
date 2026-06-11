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
				sanitizePathKeys(v)
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

// sanitizePathKeys 递归把 JSON 结构中所有名为 "Path" 的字符串值替换为其文件名(basename),
// 去掉存储目录结构,只保留文件名与扩展名。
//
// 关键:保留 "Path" 键本身,不删除。早先做法是整键删除,导致非管理员响应里直接缺
// "Path" 字段;部分第三方客户端(如 Infuse)在请求 Fields=...,Path 后遇到缺键会解析
// 失败报错。改为脱敏赋值既不泄露 /mnt/... 这类存储路径,又保证字段存在。
//
// 非字符串值(如 null)原样保留,仍是一个存在的键。
func sanitizePathKeys(v interface{}) {
	switch t := v.(type) {
	case map[string]interface{}:
		if p, ok := t["Path"].(string); ok {
			t["Path"] = pathBasename(p)
		}
		for _, child := range t {
			sanitizePathKeys(child)
		}
	case []interface{}:
		for _, child := range t {
			sanitizePathKeys(child)
		}
	}
}

// pathBasename 取路径最后一段,兼容 / 与 \ 分隔。空值或无分隔符时原样返回。
func pathBasename(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}
