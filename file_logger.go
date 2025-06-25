package logger

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
)

type FileLogger struct {
	f                 *os.File
	base              string
	curHour           string
	curHourTs         int64
	curSecondTs       int64
	full              bool
	lastCheckFileSize int64
	bufChan           chan string
	flushChan         chan bool

	flushWorkerExit   bool
	flushWorkerExitMu sync.Mutex
}

func (l *FileLogger) flushWorker() {
	for {
		select {
		case buf := <-l.bufChan:
			b := bytes.NewBufferString(buf)
			for {
				select {
				case more := <-l.bufChan:
					b.WriteString(more)
					if b.Len() >= 2*1024*1024 {
						goto OUT
					}
				default:
					goto OUT
				}
			}
		OUT:
			_ = l.realWrite(b.String())
		case <-l.flushChan:
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
			l.flushWorkerExit = true
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
	go l.flushWorker()
	return nil
}

func removeSuffixIfMatched(s string, suffix string) string {
	if strings.HasSuffix(s, suffix) {
		return s[0 : len(s)-len(suffix)]
	}
	return s
}
