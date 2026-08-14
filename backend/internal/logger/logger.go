// Package logger provides a tiny structured logger so the rest of the app
// doesn't depend directly on the standard "log" package.
package logger

import (
	"log"
	"os"
)

var (
	infoLog = log.New(os.Stdout, "[INFO] ", log.LstdFlags)
	warnLog = log.New(os.Stdout, "[WARN] ", log.LstdFlags)
	errLog  = log.New(os.Stderr, "[ERROR] ", log.LstdFlags)
)

func Info(format string, args ...interface{}) {
	infoLog.Printf(format, args...)
}

func Warn(format string, args ...interface{}) {
	warnLog.Printf(format, args...)
}

func Error(format string, args ...interface{}) {
	errLog.Printf(format, args...)
}
