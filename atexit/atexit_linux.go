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
