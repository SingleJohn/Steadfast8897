//go:build !windows

package services

import (
	"os"
	"syscall"
)

// execSelf 用新二进制替换当前进程。成功时本函数不返回(进程镜像被 replace)。
func execSelf(argv []string, env []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, argv, env)
}
