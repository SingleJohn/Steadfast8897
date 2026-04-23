package services

import (
	"context"
	"encoding/json"
	"regexp"
)

// ScrapeDiag 是一次 scrape 任务的最后一次 HTTP 尝试快照,
// 由 ScrapeWorker.runTask 每条任务新建一份并注入 ctx,
// tmdbGet 每次请求完成后回填,任务失败时落到 scrape_queue
// 的 request_url / response_status / response_sample 三列。
//
// per-task 单实例,worker 一个 goroutine 跑一条任务,不加锁。
type ScrapeDiag struct {
	URL      string
	Status   int
	Body     string
	Detail   string
	Attempts int
}

type scrapeDiagKey struct{}

// WithDiag 挂一个全新的 ScrapeDiag 到 ctx 并返回指针。
// 多次挂会覆盖(只有 worker 入口挂一次,下层不应再 Wrap)。
func WithDiag(ctx context.Context) (context.Context, *ScrapeDiag) {
	d := &ScrapeDiag{}
	return context.WithValue(ctx, scrapeDiagKey{}, d), d
}

// DiagFrom 从 ctx 取 buffer,没有返回 nil(所有调用者必须判空)。
func DiagFrom(ctx context.Context) *ScrapeDiag {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(scrapeDiagKey{}).(*ScrapeDiag)
	return v
}

var apiKeyRe = regexp.MustCompile(`api_key=[^&]+`)

// Record 由 HTTP 调用点在读完 body 后调用。
//
//	url    : 已替换 {API_KEY} 的真实 URL,函数内部做脱敏
//	status : 收到的 HTTP 状态码(0 表示请求未到网络层)
//	body   : resp.Body 读出的原始字节(nil 允许)
//	ok     : 业务判定是否成功(2xx 且解析正常)。成功只记 URL+status,
//	         body 丢弃避免入库膨胀;失败记完整 body 不截断。
func (d *ScrapeDiag) Record(url string, status int, body []byte, ok bool) {
	if d == nil {
		return
	}
	d.Attempts++
	d.URL = apiKeyRe.ReplaceAllString(url, "api_key=***")
	d.Status = status
	if ok {
		d.Body = ""
	} else if len(body) > 0 {
		d.Body = string(body)
	} else {
		d.Body = ""
	}
}

// SetDetail 记录结构化任务诊断,供非 HTTP 失败(如 identify no match)落到队列详情。
func (d *ScrapeDiag) SetDetail(v any) {
	if d == nil || v == nil {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return
	}
	d.Detail = string(raw)
}
