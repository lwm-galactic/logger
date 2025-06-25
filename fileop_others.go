//go:build !linux || !amd64 || noattr

package logger

func UMask(mask int) int { //在非linux系统下这是一个 占位函数 或者说 桩函数（stub function） ，它目前没有任何实际功能。
	return 0
}
