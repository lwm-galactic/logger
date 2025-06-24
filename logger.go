package logger

import (
	"bytes"
	"fmt"
	"github.com/petermattis/goid" // 获取协程的id
	"path"
	"runtime"
	"time"
)

type Level uint8

func (l Level) Color() string { // 获取日志颜色
	switch l {
	case InfoLevel:
		return blue
	case DebugLevel, ImportantLevel:
		return green
	case WarnLevel:
		return yellow
	default:
		return red
	}
}
func (l Level) ShortString() string { // 日志的描述简短
	switch l {
	case DebugLevel:
		return "DBG "
	case InfoLevel:
		return "INF "
	case WarnLevel:
		return "WAR "
	case ErrorLevel:
		return "ERR "
	case DPanicLevel:
		return "PAN "
	case PanicLevel:
		return "PAN "
	case FatalLevel:
		return "FAT "
	case ImportantLevel:
		return "IMP "
	default:
		return fmt.Sprintf("L(%d) ", l)
	}
}

var red string
var green string
var yellow string
var purple string
var blue string
var pid = 0                 // 进程id 初始化为0
var modName = "UNKNOWN"     // go.mod 的名称 初始化为未知
var formatTimeSec uint32    // 缓存上一次的时间戳（秒级）
var formatTimeSecStr string // 缓存上一次格式化后的时间字符串
var EnableLogCtx = true

const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
	DPanicLevel
	PanicLevel
	FatalLevel
	ImportantLevel
)

const (
	colorRed = uint8(iota + 91)
	colorGreen
	colorYellow
	colorBlue
	colorPurple
)

const defaultMaxFileSize int64 = 4 * 1024 * 1024 * 1024

var minLevel = DebugLevel

type Optimization struct {
	ShortFile  string // 源码名字
	FuncName   string // 方法名
	CallerLine int    // 第几行

	// 配置文件中读取
	LogFileType int
	LogFuncType int
	LogFileLine bool
	LogCtx      bool
}

var logger ILogger

type ILogger interface { // 日志接口定义
	Write(buf string) error //将一段字符串日志内容 buf 写入到目标（如文件、控制台、网络等）。
	Sync() error            //将缓冲的日志数据强制刷新到底层持久化介质（例如磁盘），确保数据真正写入。
	Flush()                 //清空当前缓冲区中的日志内容（通常用于内存缓冲日志系统）。
}

// template 模板 如 "test %d" , args 参数
func logItFmt(opt *Optimization, l Level, template string, args ...interface{}) {
	msg := template
	if msg == "" && len(args) > 0 {
		msg = fmt.Sprint(args...)
	} else if msg != "" && len(args) > 0 {
		msg = fmt.Sprintf(template, args...)
	}
	logIt(opt, l, msg)
	afterLog(l)
}

// 实际日志打印
func logIt(opt *Optimization, l Level, msg string) {
	if l < minLevel {
		return
	}
	msg = formatLog(opt, l, msg, 4)
	if logger != nil {
		logger.Write(msg)
	} else {
		fmt.Print(msg)
	}
}

// 格式化 日期 降低 t.Format("01-02T15:04:05") 的执行次数
func formatTime(t time.Time) string {
	sec := uint32(t.Unix())
	pre := formatTimeSec
	preStr := formatTimeSecStr
	if pre == sec {
		// 受并行优化的影响，小概率取了旧值，因为是打LOG，就不搞这么严谨了
		return preStr
	} // 如果当前时间的“秒”没有变化，则直接返回上次缓存的结果。
	x := t.Format("01-02T15:04:05") // 去除了年份
	formatTimeSec = sec
	formatTimeSecStr = x
	return x
}

// 日志格式化函数
// buf 实际要记录的日志内容字符串。
// callerSkip 控制从调用栈中向上查找多少层来定位真正的调用位置。当前 函数为0 上层调用者为1 以此类推
func formatLog(opt *Optimization, l Level, buf string, callerSkip int) string {
	now := time.Now()

	var b bytes.Buffer

	// mod
	b.WriteString(modName)

	routineId := goid.Get()

	// 进程、协程
	b.WriteString(fmt.Sprintf("(%d,%d) ", pid, routineId))

	// 时间
	b.WriteString(formatTime(now))
	// 这段代码的作用是：
	//输出当前时间中毫秒以下的部分（粗略到 100 微秒精度） ，并格式化为 4 位数字。
	b.WriteString(fmt.Sprintf("%04d ", now.Nanosecond()/100000))

	var lc = false
	if opt != nil {
		lc = opt.LogCtx
	} else {
		lc = EnableLogCtx // 没用配置默认可以
	}
	if lc {
		reqId := GetLogCtx(routineId)
		if reqId != "" {
			b.WriteString("<")
			b.WriteString(reqId)
			b.WriteString("> ")
		}
	}

	// 日志级别
	b.WriteString(l.Color())
	b.WriteString(l.ShortString())

	var (
		callerName string  // 函数名
		callerFile string  // 源文件路径（包含完整路径）
		callerLine int     // 当前调用所在的源码行号
		ok         bool    // 是否成功获取了调用栈信息（失败可能是在某些环境或 goroutine 已退出）
		pc         uintptr // 程序计数器（Program Counter），可以用于获取函数名
	)
	//获取当前调用栈中的调用者信息（文件名、函数名、行号） ，用于日志、调试或性能分析等场景。
	if opt == nil || opt.CallerLine == 0 {
		pc, callerFile, callerLine, ok = runtime.Caller(callerSkip) //使用 runtime.Caller 获取调用栈信息
		callerName = ""
		if ok {
			callerName = runtime.FuncForPC(pc).Name()
		}

	} else {
		callerFile = opt.ShortFile
		callerName = opt.FuncName
		callerLine = opt.CallerLine
	}

	// 调用位置
	filePath, fileFunc := getPackageName(callerName)
	b.WriteString(path.Join(filePath, path.Base(callerFile)))
	b.WriteString(":")
	b.WriteString(fmt.Sprintf("%d:", callerLine))
	b.WriteString(fileFunc)
	b.WriteString(colorEnd)
	b.WriteString(" ")

	// 文本内容
	b.WriteString(buf)
	b.WriteString("\n")

	return b.String()
}

func GetLogCtx(i ...int64) string {
	if EnableLogCtx {
		var cid int64
		if len(i) == 0 {
			cid = goid.Get()
		} else if len(i) == 0 {
			cid = goid.Get()
		} else {
			cid = i[0]
		}

		var v string
		logCtxMu.RLock()
		v = logCtx[cid]
		logCtxMu.RUnlock()
		return v
	}
	return ""
}

/*=========================下面是日志方法==============================*/

func Info(args ...interface{}) {

}
func Infof(format string, args ...interface{}) {

}
func InfoWithOpt() {

}
