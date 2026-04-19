//go:build windows

package services

import "errors"

// execSelf 在 Windows 下不可用(exe 运行中被锁,无 execve 语义)。
// binary 模式不会在 Windows 上启用,这里只是占位让编译通过。
func execSelf(argv []string, env []string) error {
	_ = argv
	_ = env
	return errors.New("exec self not supported on windows")
}
