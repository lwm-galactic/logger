// 如果你在构建时加上了 -tags "release" 参数，这个文件不会参与编译 生成环境不打印到控制台
// 如果你不加 "release" 标签，这个文件会被正常编译
//go:build !release

package logger

// 是否将日志输出到标准输出
var log2Stdout = true
