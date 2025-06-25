//go:build linux && amd64 && !noattr

// 这个文件只有在linux环境下才会被编译
package logger

import "syscall"

// 这个 UMask 函数是对 Unix/Linux 系统调用 umask() 的封装，用来设置当前进程的 文件权限掩码（file mode creation mask） ，并返回旧的掩码值。
func UMask(mask int) int {
	oldMask := syscall.Umask(mask)
	return oldMask
}
