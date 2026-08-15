// Package logger wraps log/slog so the rest of the app logs structured
// records without depending on a logging library directly.
//
// Output is JSON in production and human-readable text in development. The
// previous implementation wrote free-form text with no request correlation,
// which meant a production incident could not be traced from a user report to
// the requests behind it.
package logger

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

var log = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

// Configure selects the output format and level. Called once at startup.
//
//	format: "json" or "text" ("" picks json in production, text otherwise)
//	level:  debug | info | warn | error
func Configure(format, level, env string) {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	if format == "" {
		if env == "production" {
			format = "json"
		} else {
			format = "text"
		}
	}

	var handler slog.Handler
	if strings.EqualFold(format, "json") {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	log = slog.New(handler)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Logger exposes the underlying slog.Logger for callers that want to attach
// structured attributes directly.
func Logger() *slog.Logger { return log }

// The printf-style helpers below keep the existing call sites working. New
// code that has structured data to record should prefer Logger().

func Debug(format string, args ...interface{}) { log.Debug(sprintf(format, args...)) }
func Info(format string, args ...interface{})  { log.Info(sprintf(format, args...)) }
func Warn(format string, args ...interface{})  { log.Warn(sprintf(format, args...)) }
func Error(format string, args ...interface{}) { log.Error(sprintf(format, args...)) }

// RequestLog is one completed HTTP request.
type RequestLog struct {
	RequestID string
	Method    string
	Route     string
	Path      string
	Status    int
	Bytes     int
	Duration  time.Duration
	RemoteIP  string
	UserAgent string
}

// Request emits an access-log record. Server errors log at error level so
// alerting can key on severity rather than parsing the status field.
func Request(r RequestLog) {
	attrs := []any{
		slog.String("request_id", r.RequestID),
		slog.String("method", r.Method),
		slog.String("route", r.Route),
		slog.String("path", r.Path),
		slog.Int("status", r.Status),
		slog.Int("bytes", r.Bytes),
		slog.Float64("duration_ms", float64(r.Duration.Microseconds())/1000),
		slog.String("remote_ip", r.RemoteIP),
	}

	switch {
	case r.Status >= 500:
		log.Error("request", attrs...)
	case r.Status >= 400:
		log.Warn("request", attrs...)
	default:
		log.Info("request", attrs...)
	}
}

func sprintf(format string, args ...interface{}) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}
