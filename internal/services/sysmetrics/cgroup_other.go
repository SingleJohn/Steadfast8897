//go:build !linux

package sysmetrics

// 非 Linux 平台没有 cgroup 概念，直接返回"无限制"。
func detectMemLimit() uint64  { return 0 }
func detectCPUQuota() float64 { return 0 }
