package services

import (
	"os"
	"runtime"
	"strings"
)

type DeploymentMode string

const (
	DeployDocker DeploymentMode = "docker"
	DeployBinary DeploymentMode = "binary"
	DeployManual DeploymentMode = "manual"
)

// DetectDeploymentMode 判断当前进程的部署形态。
// docker: 容器内(有 /.dockerenv 或 cgroup 落到 docker/containerd/kubepods)
// manual: Windows,暂不支持二进制自更新
// binary: 其他(Linux/macOS 裸机)
func DetectDeploymentMode() DeploymentMode {
	if runtime.GOOS == "linux" {
		if _, err := os.Stat("/.dockerenv"); err == nil {
			return DeployDocker
		}
		if b, err := os.ReadFile("/proc/1/cgroup"); err == nil {
			s := string(b)
			if strings.Contains(s, "docker") ||
				strings.Contains(s, "containerd") ||
				strings.Contains(s, "kubepods") {
				return DeployDocker
			}
		}
	}
	if runtime.GOOS == "windows" {
		return DeployManual
	}
	return DeployBinary
}

// BuildTargetName 把 runtime.GOOS/GOARCH 映射到 workflow 产物命名里的 target 段。
// 必须与 .github/workflows/docker-publish.yml matrix 的 target 字段一致。
func BuildTargetName() string {
	switch runtime.GOOS {
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return "linux-amd64"
		case "arm64":
			return "linux-arm64"
		case "386":
			return "linux-386"
		case "arm":
			return "linux-armv7"
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64":
			return "darwin-amd64"
		case "arm64":
			return "darwin-arm64"
		}
	case "windows":
		if runtime.GOARCH == "amd64" {
			return "windows-amd64"
		}
	}
	return ""
}

// BuildArchiveExt 对应 target 的归档扩展名。Windows 走 zip,其他走 tar.gz。
func BuildArchiveExt() string {
	if runtime.GOOS == "windows" {
		return "zip"
	}
	return "tar.gz"
}

// BuildBinaryName 是包内的可执行文件名。
func BuildBinaryName() string {
	if runtime.GOOS == "windows" {
		return "fyms.exe"
	}
	return "fyms"
}
