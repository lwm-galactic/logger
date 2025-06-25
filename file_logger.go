package logger

import (
	"bytes"
	"errors"
	"fmt"
	"go.uber.org/atomic"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 可通过文件配置日志最大容量（路径）
const maxLogFileSizeFile = "/etc/brick/max_log_size" //配置文件
// 动态加载最大日志文件大小
var maxFileSize = atomic.NewInt64(defaultMaxFileSize)

// 上次记录时间戳（用于判断是否切换日志文件）
var lasTime time.Time

type FileLogger struct {
	f                 *os.File    // 当前日志文件句柄
	base              string      // 基础文件名
	curHour           string      // 当前小时
	curHourTs         int64       // 当前小时的时间戳
	curSecondTs       int64       // 当前秒数
	full              bool        // 是否已满
	lastCheckFileSize int64       // 上次检查文件大小时间
	bufChan           chan string // 日志缓冲通道
	flushChan         chan bool   // 刷新触发通道
	flushWorkerExit   bool        // 刷新协程退出标志
	flushWorkerExitMu sync.Mutex  // 刷新协程退出锁
}

// NewFileLogger 创建一个新的文件日志器实例
func NewFileLogger() *FileLogger {
	p := &FileLogger{}
	return p
}

// InitFileLog 初始化主日志器
func InitFileLog(dir string, name string) error {
	if logger != nil {
		return errors.New("logger has init")
	}
	l := NewFileLogger()
	err := l.Init(dir, name)
	if err != nil {
		return err
	}
	logger = l
	return nil
}

// 实现ILogger 接口

// Sync 同步磁盘
func (l *FileLogger) Sync() error {
	if l.f == nil {
		return errors.New("file not open")
	}
	return l.f.Sync()
}

// 写入日志到缓冲通道
func (l *FileLogger) Write(buf string) error {
	if log2Stdout {
		fmt.Print(buf)
	}
	select {
	case l.bufChan <- buf:
		return nil
	default:
		println("waring: buffer channel full")
		return errors.New("buffer channel full")
	}
}

// Flush 清空缓冲区并关闭文件
func (l *FileLogger) Flush() {
	select {
	case l.flushChan <- true:
	default:
	}
	for i := 0; i < 100; i++ {
		if l.getFlushWorkerExit() {
			break
		}
		time.Sleep(50 * time.Microsecond)
	}
}

// 获取刷新协程是否已退出 (用于安全地读取一个并发标志位 ，判断后台刷新日志的协程（goroutine）是否已经退出。)
func (l *FileLogger) getFlushWorkerExit() bool {
	l.flushWorkerExitMu.Lock()
	res := l.flushWorkerExit
	l.flushWorkerExitMu.Unlock()
	return res
}

// 异步刷新日志的工作协程
func (l *FileLogger) flushWorker() {
	for {
		select {
		case buf := <-l.bufChan: // 日志内容缓冲区
			b := bytes.NewBufferString(buf)
			for { // 这个内层 for 循环尝试从 bufChan 继续读取更多日志 减少 I/O 操作
				select {
				case more := <-l.bufChan:
					b.WriteString(more)
					if b.Len() >= 2*1024*1024 { // 最大到2m 就实际写到文件中
						goto OUT // 在 Go 语言中，goto 是一种跳转语句 ，用于将程序的控制流直接转移到同一函数内的某个标签(label)处。(因为要跳出两层嵌套结构（外层是 select，内层是 for），使用 goto 可以避免引入额外状态变量或复杂控制逻辑)
					}
				default: // channel 被读完了
					goto OUT
				}
			}
		OUT:
			_ = l.realWrite(b.String()) // 实际写到文件中
		case <-l.flushChan: // 清空缓冲区（bufChan），将其中剩余的所有日志内容立即写入磁盘文件中。
			con := true
			for con {
				select {
				case buf := <-l.bufChan:
					_ = l.realWrite(buf)
				default:
					con = false
					break
				}
			}
			l.flushWorkerExitMu.Lock()
			l.flushWorkerExit = true //标记刷新协程已退出
			l.flushWorkerExitMu.Unlock()
			return
		}
	}
}

// Init
// dir：日志输出目录
// name：日志文件的基础名称
func (l *FileLogger) Init(dir string, name string) error {
	if dir == "" {
		dir = "." // 如果 dir 不传 日志写在当前文件
	}
	if !strings.HasPrefix(dir, ".") { // 如果目录不是以 . 开头(即非相对路径)，则尝试创建该目录。
		old := UMask(0)                      // 临时取消权限掩码(确保创建目录时权限不受限制)
		defer UMask(old)                     // 函数退出后恢复原来的权限掩码
		err := os.MkdirAll(dir, os.ModePerm) // 递归创建目录(如果中间目录不存在也会一并创建), os.ModePerm 表示最大权限 0777，配合 UMask(0) 可以让创建的目录权限为 rwxrwxrwx
		if err != nil {
			fmt.Printf("make dir fail, dir %s, err %s\n", dir, err)
			return err
		}
	}
	name = removeSuffixIfMatched(name, ".log") // 清理日志文件名(去掉 .log 后缀)
	l.base = fmt.Sprintf("%s%s%s", dir, fileSep, name)
	l.bufChan = make(chan string, 100000)
	l.flushChan = make(chan bool, 100)
	go l.flushWorker() // 启动协程 从channel中循环读取 日志数据 并写入文件
	return nil
}

func removeSuffixIfMatched(s string, suffix string) string {
	if strings.HasSuffix(s, suffix) {
		return s[0 : len(s)-len(suffix)]
	}
	return s
}

// 实际写入文件操作
func (l *FileLogger) realWrite(buf string) error {
	if l.needOpen() { // 判断日志是否需要轮转 true 需要
		err := l.open()
		if err != nil {
			return err
		}
		l.refreshMaxSize()
	}
	if l.f == nil {
		return errors.New("file not open")
	}
	l.checkFull()
	if l.full {
		now := time.Now()
		if lasTime.Add(time.Minute).Before(now) {
			lasTime = now
		}
		return errors.New("file has full")
	}
	n, err := l.f.WriteString(buf) // 实际文件写入
	if err != nil {
		println(fmt.Sprintf("err %v,%s", err, time.Now().Format("2006-01-02 15:04:05.0000")))
		return err
	}
	for n < len(buf) {
		x, err := l.f.WriteString(buf[n:])
		if err != nil {
			println(fmt.Sprintf("err %v,%s", err, time.Now().Format("2006-01-02 15:04:05.0000")))
			return err
		}
		n += x
	}
	return nil
}

// 判断是否需要打开新文件(按小时或大小) 日志轮转的实现 按小时 ：当前时间进入下一个小时
func (l *FileLogger) needOpen() bool {
	res := false      // 判断是否要打开新文件
	now := time.Now() // 获取当前时间对象，用于判断是否跨小时或是否超时
	if l.f == nil {   // 当前文件句柄为空 (当前文件没打开) 这是第一次写入日志时的初始化情况。
		res = true
	} else if l.full { // 文件已满
		ts := now.Unix()
		if l.curSecondTs+60 < ts { // curSecondTs + 60 < 当前时间戳  需等待至少60秒后再创建新文件 限流机制 （rate limiting）
			l.curSecondTs = ts // 更新当前时间
			res = true
		}
	} else {
		ts := now.Unix() / 3600 // 把当前时间转换为“以小时为单位”的整数
		if ts != l.curHourTs {  // 如果当前小时不同于记录的小时，说明跨小时了
			l.curHourTs = ts // 更新当前小时
			res = true
		}
	}
	if res {
		l.curHour = now.Format("2006010215") // 设置当前小时格式为 YYYYMMDDHH 用于文件命名
	}
	return res
}

// 实际打开一个新的日志文件
func (l *FileLogger) open() error {
	path := fmt.Sprintf("%s%s.log", l.base, l.curHour)                            //文件命名 base (用户定义)  curHour 当前小说 YYYYMMDDHH
	old := UMask(0)                                                               // 设置当前进程的文件权限掩码（umask）为 0
	defer UMask(old)                                                              // 恢复文件权限
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, os.ModePerm) // 给文件权限
	if err != nil {
		fmt.Printf("open file fail, path %s, err %s", path, err)
		return err
	}
	oldFile := l.f
	l.f = f
	if oldFile != nil {
		err = oldFile.Close()
		if err != nil {
			fmt.Printf("close old file fail, name %s, err %s\n", oldFile.Name(), err)
		}
	}
	l.full = false
	return nil
}

// 更新最大日志文件大小 打开一个新的日志文件时执行
func (l *FileLogger) refreshMaxSize() {
	dat, err := os.ReadFile(maxLogFileSizeFile) // 读配置文件
	if err == nil && len(dat) > 0 {
		v, err := strconv.ParseInt(strings.TrimSpace(string(dat)), 10, 64)
		if err == nil && v > 0 {
			maxFileSize.Store(v)
		}
	}
}

// checkFull 检查当前日志文件是否已达到最大大小（maxFileSize），用于触发日志轮转。
func (l *FileLogger) checkFull() {
	// 获取当前时间戳（单位：秒）
	now := time.Now().Unix()

	// 获取上一次检查文件大小的时间
	last := l.lastCheckFileSize

	// 限制检查频率：至少间隔10秒才进行一次检查，防止频繁 IO 操作
	if last+10 < now {
		// 获取当前的日志文件句柄
		f := l.f

		// 如果文件句柄不为空，则继续检查
		if f != nil {
			// 更新上次检查时间
			l.lastCheckFileSize = now

			// 使用 Seek(0, io.SeekEnd) 获取当前文件的大小（字节数）
			// Seek 返回值是当前文件指针的位置，SeekEnd 表示从文件末尾偏移
			size, err := f.Seek(0, io.SeekEnd)
			if err == nil {
				// 获取配置的最大文件大小（使用原子操作保证并发安全）
				maxSize := maxFileSize.Load()

				// 判断当前文件大小是否超过最大限制
				if size >= maxSize {
					// 文件已满，标记 full 为 true
					l.full = true
				} else {
					// 文件未满，标记 full 为 false
					l.full = false
				}
			}
			// 如果 err 不为 nil（如文件被关闭），可以选择记录日志或忽略
			// 示例：log.Printf("failed to get file size: %v", err)
		}
	}
}
