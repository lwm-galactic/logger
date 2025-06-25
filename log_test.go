package logger_test

import (
	"github.com/lwm-galactic/logger"
	"testing"
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
