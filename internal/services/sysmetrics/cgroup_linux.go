//go:build linux

package sysmetrics

import (
	"os"
	"strconv"
	"strings"
)

// detectMemLimit 返回容器内存上限(bytes)。无限制/失败返回 0。
// 依次尝试 cgroup v2、v1。
func detectMemLimit() uint64 {
	if b, err := os.ReadFile("/sys/fs/cgroup/memory.max"); err == nil {
		s := strings.TrimSpace(string(b))
		if s != "" && s != "max" {
			if n, err := strconv.ParseUint(s, 10, 64); err == nil && n > 0 {
				return n
			}
		}
	}
	if b, err := os.ReadFile("/sys/fs/cgroup/memory/memory.limit_in_bytes"); err == nil {
		if n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64); err == nil {
			// cgroup v1 在无限制时返回 ~1<<62 或 unsigned long max
			if n > 0 && n < (1<<62) {
				return n
			}
		}
	}
	return 0
}

// detectCPUQuota 返回容器 CPU 核数配额（例如 --cpus=2 → 2.0）。
// 无限制/失败返回 0。
func detectCPUQuota() float64 {
	if b, err := os.ReadFile("/sys/fs/cgroup/cpu.max"); err == nil {
		parts := strings.Fields(strings.TrimSpace(string(b)))
		if len(parts) == 2 && parts[0] != "max" {
			q, err1 := strconv.ParseFloat(parts[0], 64)
			p, err2 := strconv.ParseFloat(parts[1], 64)
			if err1 == nil && err2 == nil && p > 0 && q > 0 {
				return q / p
			}
		}
	}
	if qb, err := os.ReadFile("/sys/fs/cgroup/cpu/cpu.cfs_quota_us"); err == nil {
		if pb, err := os.ReadFile("/sys/fs/cgroup/cpu/cpu.cfs_period_us"); err == nil {
			q, err1 := strconv.ParseFloat(strings.TrimSpace(string(qb)), 64)
			p, err2 := strconv.ParseFloat(strings.TrimSpace(string(pb)), 64)
			if err1 == nil && err2 == nil && q > 0 && p > 0 {
				return q / p
			}
		}
	}
	return 0
}
