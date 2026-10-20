package logger

import (
	"io"
	"log"
	"os"
)

var (
	infoLog  *log.Logger
	warnLog  *log.Logger
	errLog   *log.Logger
	debugLog *log.Logger
)

// Init initializes all loggers with file and console output.
func Init(logPath string) {
	multi := io.MultiWriter(os.Stdout, mustOpen(logPath))

	infoLog = log.New(multi, "INFO  ", log.Ldate|log.Ltime|log.Lmicroseconds)
	warnLog = log.New(multi, "WARN  ", log.Ldate|log.Ltime|log.Lmicroseconds)
	errLog = log.New(multi, "ERROR ", log.Ldate|log.Ltime|log.Lmicroseconds)
	debugLog = log.New(multi, "DEBUG ", log.Ldate|log.Ltime|log.Lmicroseconds)
}

func mustOpen(path string) io.WriteCloser {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return &nopCloser{os.Stdout}
	}
	return f
}

func Info(v ...interface{})   { infoLog.Println(v...) }
func Warn(v ...interface{})  { warnLog.Println(v...) }
func Error(v ...interface{}) { errLog.Println(v...) }
func Debug(v ...interface{}) { debugLog.Println(v...) }

func Infof(format string, args ...interface{}) {
	infoLog.Printf(format, args...)
}
func Warnf(format string, args ...interface{}) {
	warnLog.Printf(format, args...)
}
func Errorf(format string, args ...interface{}) {
	errLog.Printf(format, args...)
}

type nopCloser struct {
	w io.Writer
}

func (n *nopCloser) Write(p []byte) (int, error) { return n.w.Write(p) }
func (n *nopCloser) Close() error                { return nil }
