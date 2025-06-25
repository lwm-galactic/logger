//go:build !linux
// +build !linux

package atexit

func Register(callback func()) {
}
