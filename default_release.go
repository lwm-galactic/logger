// 这段代码中的变量 log2Stdout 只有在使用 -tags release 才会不打印到控制台
//go:build release

package logger

var log2Stdout bool
