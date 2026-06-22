package source

import (
	"fmt"
	"io"
	"net/url"
	"strings"
)

const (
	defaultDRPYBaseURL = "https://tvboxconfig.singlelovely.cn/gao/"
	defaultDRPYEngine  = "./lib/drpy2.min.js"
	defaultDRPYRule    = "./js/360影视.js"
)

func resolveDRPYURL(base *url.URL, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("artifact 路径为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("解析 artifact URL 失败: %w", err)
	}
	if !u.IsAbs() {
		u = base.ResolveReference(u)
	}
	return u.String(), nil
}

func urlPath(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Path
}

func sanitizeArtifactName(name string) string {
	name = strings.TrimSpace(name)
	replacer := strings.NewReplacer("\\", "_", "/", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	name = replacer.Replace(name)
	if len([]rune(name)) > 80 {
		rs := []rune(name)
		name = string(rs[len(rs)-80:])
	}
	return name
}

type limitWriter struct {
	w     io.Writer
	limit int
	n     int
}

func (w *limitWriter) Write(p []byte) (int, error) {
	originalLen := len(p)
	if w.limit <= 0 || w.n >= w.limit {
		return originalLen, nil
	}
	remain := w.limit - w.n
	if len(p) > remain {
		p = p[:remain]
	}
	n, err := w.w.Write(p)
	w.n += n
	if err != nil {
		return n, err
	}
	return originalLen, nil
}
