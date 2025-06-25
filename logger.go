package logger

import (
	"bytes"
	"fmt"
	"github.com/lwm-galactic/logger/atexit"
	"github.com/petermattis/goid" // 获取协程的id
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Level int8

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

var colorEnd string // 日志结束颜色
var red string
var green string
var yellow string
var purple string
var blue string
var pid = 0                 // 进程id 初始化为0
var formatTimeSec uint32    // 缓存上一次的时间戳（秒级）
var formatTimeSecStr string // 缓存上一次格式化后的时间字符串
var EnableLogCtx = true
var logCtxMu sync.RWMutex       // 协程id 上下文 读写锁
var logCtx = map[int64]string{} // 这个 map 的作用是：为每个协程（goroutine）保存一个“日志上下文”字符串，通常用于日志打印时输出额外的上下文信息。
var (
	minLevel        = DebugLevel // 最低打印的日志级别
	fileSep         string
	modName         = "UNKNOWN" // go.mod 的名称 初始化为未知
	logger          ILogger
	loggerImportant ILogger
)

func SetLogLevel(l Level) {
	minLevel = l
}
func SetModName(name string) {
	modName = name
}

const (
	DebugLevel Level = iota - 1
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

func init() {
	// if runtime.GOOS != "windows" {
	red = fmt.Sprintf("\x1b[%dm", colorRed)
	green = fmt.Sprintf("\x1b[%dm", colorGreen)
	yellow = fmt.Sprintf("\x1b[%dm", colorYellow)
	blue = fmt.Sprintf("\u001B[%d;1m", colorBlue)
	purple = fmt.Sprintf("\x1b[%dm", colorPurple)
	colorEnd = "\x1b[0m"

	atexit.Register(func() {
		if logger != nil {
			logger.Flush()
		}
		if loggerImportant != nil {
			loggerImportant.Flush()
		}
	})
}

const defaultMaxFileSize int64 = 4 * 1024 * 1024 * 1024

// OptLogFileType*：控制 日志中显示的源文件路径格式
//
//	OptLogFuncType*：控制 日志中显示的函数名格式
const (
	OptLogFileTypeDefault = 0 // default is full
	OptLogFileTypeFull    = 1
	OptLogFileTypeShort   = 2
	OptLogFileTypeIgnore  = 3

	OptLogFuncTypeDefault = 0
	OptLogFuncTypeFull    = 1
	OptLogFuncTypeIgnore  = 2
)

var defLogFile = OptLogFileTypeDefault
var defLogFunc = OptLogFuncTypeDefault

type Optimization struct {
	CallerFile string
	CallerName string
	CallerLine int

	// 配置文件中读取
	LogFileType int
	LogFuncType int
	LogFileLine bool
	LogCtx      bool
}

func NewOptimization() *Optimization {
	return &Optimization{
		LogFileType: OptLogFileTypeDefault,
		LogFuncType: OptLogFuncTypeDefault,
		LogFileLine: true,
		LogCtx:      true,
	}
}

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

// args 参数 只打印参数
func logItArgs(l Level, args ...interface{}) {
	msg := fmt.Sprint(args...)
	logIt(nil, l, msg)
	afterLog(l)
}

// 携带*Optimization的打印
func logItArgsWithOpt(opt *Optimization, l Level, args ...interface{}) {
	msg := fmt.Sprint(args...)
	logIt(opt, l, msg)
	afterLog(l)
}

func logItImportant(msg string) {
	msg = formatLog(nil, ImportantLevel, msg, 4)
	loggerImportant.Write(msg)
}

// 根据日志的严重级别(Level)执行一些“善后处理”操作
func afterLog(l Level) {
	if l == FatalLevel || l == PanicLevel || l == DPanicLevel {
		PrintStack(4) //  PrintStack(4) 的作用是打印调用栈（stack trace），参数 4 表示跳过前 4 层调用栈帧
	}
	if l == FatalLevel {
		os.Exit(1) // 退出程序 无法修复
	}
	// panic 如果没有被 recover() 捕获，最终会导致程序崩溃;可以通过 defer + recover 来捕获和恢复。
	if l == PanicLevel {
		panic("")
	}
}

// PrintStack 的作用是：从指定层数开始遍历当前 goroutine 的调用栈，并将每一层的函数名、文件路径和行号打印出来，常用于调试和日志系统中的错误追踪。
func PrintStack(skip int) {
	// 当前文件报错 递归获取调用者,直到最顶层
	for ; ; skip++ {
		pc, file, line, ok := runtime.Caller(skip)
		if !ok {
			break
		}
		name := runtime.FuncForPC(pc)
		if name.Name() == "runtime.goexit" {
			break
		}
		Errorf("#STACK: %s %s:%d", name.Name(), file, line)
	}
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
	pc, callerFile, callerLine, ok = runtime.Caller(callerSkip)
	callerName = ""
	if ok {
		callerName = runtime.FuncForPC(pc).Name()
	}
	// 混合opt 一起使用
	if opt != nil {
		if opt.CallerFile != "" {
			callerFile = opt.CallerFile
		}
		if opt.CallerName != "" {
			callerName = opt.CallerName
		}
		if opt.CallerLine != 0 {
			callerLine = opt.CallerLine
		}
	}
	// 缓存
	if opt != nil && opt.CallerLine == 0 {
		opt.CallerName = callerName
		opt.CallerLine = callerLine
		opt.CallerFile = callerFile
	}
	pkg, fn := splitPkgFunc(callerName)

	// 拼接文件名
	optFile := defLogFile // 默认日志文件名格式
	if opt != nil {
		optFile = opt.LogFileType // 传参的文件名格式
	}

	switch optFile {
	/* path.Base()
	in: /home/user/file.txt  out:file.txt
	in: /home/user/ out: user
	in: C:\\Users\\test\\abc.go out: abc.go
	*/
	case OptLogFileTypeIgnore:
	case OptLogFileTypeShort:
		b.WriteString(path.Base(callerFile)) // callerFile 源文件路径（包含完整路径）
		b.WriteByte(':')
	default:
		b.WriteString(path.Join(shortPkg(pkg), path.Base(callerFile)))
		b.WriteByte(':')
	}

	// 拼接行 line
	optLine := true
	if opt != nil {
		optLine = opt.LogFileLine
	}
	if optLine {
		b.WriteString(strconv.Itoa(callerLine))
		b.WriteByte(':')
	}

	// 拼接方法名 func
	optFunc := defLogFunc
	if opt != nil {
		optFunc = opt.LogFuncType
	}
	switch optFunc {
	case OptLogFuncTypeIgnore:
	default:
		if len(fn) <= 16 {
			b.WriteString(fn)
		} else {
			b.Write([]byte(".."))
			b.WriteString(fn[len(fn)-16:])
		}
	}

	b.WriteString(colorEnd)
	b.WriteByte(' ')

	// 文本内容
	b.WriteString(buf)
	b.WriteByte('\n')

	return b.String()
}

/*
"github.com/pkg/project/service/user/impl"  "impl"
"github.com/pkg/project/service/user/impl/v2" "impl/v2"
"github.com/pkg/project/service/user" "user"(没有/impl，原样返回)
"main" "main"
""（空字符串） ""
*/
func shortPkg(pkg string) string {
	const impl = "/impl"
	pos := strings.LastIndex(pkg, impl)
	if pos >= 0 {
		n := pos + len(impl)
		if (n < len(pkg) && pkg[n] == '/') || n >= len(pkg) {
			return pkg[pos+1:]
		}
	}
	return pkg
}

// 分割 包 和 函数名 提取后：pfx = "github.com/youruser/yourpkg/" , 剩下 fullName = "mypkg.MyFunc"
func splitPkgFunc(fullName string) (pkg string, fn string) {
	slashIndex := strings.LastIndexByte(fullName, '/') //  查找最后一个 /
	var pfx string
	if slashIndex >= 0 {
		pfx = fullName[:slashIndex+1]
		fullName = fullName[slashIndex+1:]
	}

	dot := strings.IndexByte(fullName, '.')
	if dot >= 0 {
		pkg = pfx + fullName[:dot]
		fn = fullName[dot+1:]
	} else {
		pkg = pfx
		fn = fullName
	}
	return
}

// GetLogCtx 协程安全的读操作
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

func SetLogCtx(c string, i ...int64) {
	if EnableLogCtx {
		if len(c) > 64 {
			c = c[:62] + ".."
		}
		var cid int64
		if len(i) == 0 {
			cid = goid.Get()
		} else if i[0] == 0 {
			cid = goid.Get()
		} else {
			cid = i[0]
		}

		logCtxMu.Lock()
		if c == "" {
			delete(logCtx, cid)
		} else {
			logCtx[cid] = c
		}
		logCtxMu.Unlock()
	}
}

/*=========================下面是日志方法==============================*/

func Important(template string, args ...interface{}) {
	logItFmt(nil, ImportantLevel, template, args...)
	// logItFmtImportant(template, args...)
}
func Infof(template string, args ...interface{}) {
	logItFmt(nil, InfoLevel, template, args...)
}
func InfofWithOpt(opt *Optimization, template string, args ...interface{}) {
	logItFmt(opt, InfoLevel, template, args...)
}
func Printf(template string, args ...interface{}) {
	logItFmt(nil, InfoLevel, template, args...)
}
func Fatal(args ...interface{}) {
	logItArgs(FatalLevel, args...)
}
func Panic(args ...interface{}) {
	logItArgs(PanicLevel, args...)
}
func DPanic(args ...interface{}) {
	logItArgs(DPanicLevel, args...)
}
func Error(args ...interface{}) {
	logItArgs(ErrorLevel, args...)
}
func ByCode(code int, args ...interface{}) {
	prefix := fmt.Sprintf("errcode %d ", code)
	args = append([]interface{}{prefix}, args...)
	if code == 0 {
		logItArgs(InfoLevel, args...)
	} else if code > 0 {
		logItArgs(WarnLevel, args...)
	} else {
		logItArgs(ErrorLevel, args...)
	}
}
func Warn(args ...interface{}) {
	logItArgs(WarnLevel, args...)
}
func Info(args ...interface{}) {
	logItArgs(InfoLevel, args...)
}
func InfoWithOpt(opt *Optimization, args ...interface{}) {
	logItArgsWithOpt(opt, InfoLevel, args...)
}
func Debug(args ...interface{}) {
	// fast check
	if DebugLevel < minLevel {
		return
	}
	logItArgs(DebugLevel, args...)
}
func Debugf(template string, args ...interface{}) {
	// fast check
	if DebugLevel < minLevel {
		return
	}
	logItFmt(nil, DebugLevel, template, args...)
}
func Warnf(template string, args ...interface{}) {
	logItFmt(nil, WarnLevel, template, args...)
}
func WarnfWithOpt(opt *Optimization, template string, args ...interface{}) {
	logItFmt(opt, WarnLevel, template, args...)
}
func Errorf(template string, args ...interface{}) {
	logItFmt(nil, ErrorLevel, template, args...)
}
func ErrorfWithOpt(opt *Optimization, template string, args ...interface{}) {
	logItFmt(opt, ErrorLevel, template, args...)
}
func DPanicf(template string, args ...interface{}) {
	logItFmt(nil, DPanicLevel, template, args...)
}
func Panicf(template string, args ...interface{}) {
	logItFmt(nil, PanicLevel, template, args...)
}
func Fatalf(template string, args ...interface{}) {
	logItFmt(nil, FatalLevel, template, args...)
}
