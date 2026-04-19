package sysmetrics

// Snapshot 是一次采样的结果；字段命名使用 JSON tag 对前端友好。
type Snapshot struct {
	Env            string  `json:"env"`            // windows / linux / docker
	CPUPercent     float64 `json:"cpuPercent"`     // 0-100
	CPUCores       int     `json:"cpuCores"`       // 容器 quota 存在时为 quota 向上取整
	MemUsed        uint64  `json:"memUsed"`
	MemTotal       uint64  `json:"memTotal"`       // 容器限额存在时使用限额
	MemPercent     float64 `json:"memPercent"`
	DirectTxBps    uint64  `json:"directTxBps"`    // 本进程 HTTP 出口字节/秒
	RedirectBpsEst uint64  `json:"redirectBpsEst"` // 活跃会话码率合计（302 转发估算）
	ActiveSessions int     `json:"activeSessions"`
	Timestamp      int64   `json:"ts"` // unix ms
}
