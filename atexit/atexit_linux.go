//go:build linux
// +build linux

package atexit

import (
	gomonkey "github.com/agiledragon/gomonkey/v2"
	"os"
	"sync"
)

var exitCallbackList []func()
var exitCallbackListMu sync.Mutex
var exitPatches *gomonkey.Patches
var mu sync.Mutex

// 这段代码通过 gomonkey 工具“劫持”了 os.Exit 函数，在程序退出前允许执行一些自定义的回调函数（如清理资源、记录日志等），然后再真正退出。
func hookExit(code int) {
	hasReset := true
	var cbList []func()
	mu.Lock()
	if exitPatches != nil {
		exitPatches.Reset()
		hasReset = false
		exitCallbackListMu.Lock()
		cbList = exitCallbackList
		exitCallbackList = nil
		exitCallbackListMu.Unlock()
	}
	mu.Unlock()
	if hasReset {
		return
	}
	for _, cb := range cbList {
		cb()
	}
	os.Exit(code)
}
func init() {
	exitPatches = gomonkey.ApplyFunc(os.Exit, hookExit)
}
func Register(callback func()) {
	exitCallbackListMu.Lock()
	exitCallbackList = append(exitCallbackList, callback)
	exitCallbackListMu.Unlock()
}
