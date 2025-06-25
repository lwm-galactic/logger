package logger_test

import (
	"github.com/lwm-galactic/logger"
	"testing"
	"time"
)

func TestInfo(t *testing.T) {
	logger.SetModName("log_test")
	func() {
		logger.Info("aaa")
		func() {
			logger.Info("ccc")
		}()
	}()
	logger.SetLogLevel(logger.WarnLevel)
	logger.Infof("debug")
	logger.Errorf("error")

}

func TestFile(t *testing.T) {
	err := logger.InitFileLog("log\\", "test")
	if err != nil {
		return
	}

	func() {
		logger.Info("aaa")
		func() {
			logger.Info("ccc")
		}()
	}()
	logger.SetLogLevel(logger.InfoLevel)
	logger.Debug("debug")
	logger.Info("info")
	time.Sleep(time.Second * 10)
}
