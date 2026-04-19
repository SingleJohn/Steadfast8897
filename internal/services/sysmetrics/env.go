package sysmetrics

import (
	"os"
	"runtime"
	"strings"
)

// detectEnv 返回 "windows" / "linux" / "docker"。
func detectEnv() string {
	if runtime.GOOS != "linux" {
		return runtime.GOOS
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "docker"
	}
	if b, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		s := string(b)
		if strings.Contains(s, "docker") ||
			strings.Contains(s, "containerd") ||
			strings.Contains(s, "kubepods") {
			return "docker"
		}
	}
	return "linux"
}
