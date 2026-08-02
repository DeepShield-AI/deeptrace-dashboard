// Package logging provides leveled, colored logging with dual output:
//   - stdout with ANSI colors (Debug=white, Warn=yellow, Error=red)
//   - logs/serve.log without colors
//
// Levels are controlled by the LOG_LEVEL env var (debug|info|warn|error),
// defaulting to debug so SQL statements are visible.
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Levels.
const (
	LevelDebug = iota
	LevelInfo
	LevelWarn
	LevelError
)

// ANSI colors (only applied on stdout, never in the log file).
const (
	colorReset  = "\x1b[0m"
	colorWhite  = "\x1b[37m"
	colorYellow = "\x1b[33m"
	colorRed    = "\x1b[31m"
)

var (
	mu      sync.Mutex
	level   = LevelDebug
	fileOut io.Writer // serve.log writer (nil until Init)
	stdOut  io.Writer = os.Stdout
)

// Init opens logs/serve.log for append and sets the level from LOG_LEVEL.
// The file lives under <dir>/logs/serve.log (dir is typically the CWD).
func Init(dir string) {
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "info":
		level = LevelInfo
	case "warn":
		level = LevelWarn
	case "error":
		level = LevelError
	default:
		level = LevelDebug
	}
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err == nil {
		f, err := os.OpenFile(filepath.Join(logDir, "serve.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			fileOut = f
		}
	}
}

// Debugf logs at debug level (white). Used for SQL statements and other
// verbose diagnostics.
func Debugf(format string, args ...interface{}) {
	write(LevelDebug, colorWhite, "DEBUG", format, args...)
}

// Infof logs at info level (default color). Used for startup and routine events.
func Infof(format string, args ...interface{}) {
	write(LevelInfo, "", "INFO", format, args...)
}

// Warnf logs at warn level (yellow). Used for degraded paths such as
// serving responses from the api cache.
func Warnf(format string, args ...interface{}) {
	write(LevelWarn, colorYellow, "WARN", format, args...)
}

// Errorf logs at error level (red). Used for failures.
func Errorf(format string, args ...interface{}) {
	write(LevelError, colorRed, "ERROR", format, args...)
}

// Fatalf logs at error level and exits (replaces log.Fatal).
func Fatalf(format string, args ...interface{}) {
	write(LevelError, colorRed, "ERROR", format, args...)
	os.Exit(1)
}

func write(lvl int, color, tag, format string, args ...interface{}) {
	if lvl < level {
		return
	}
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("%s [%s] %s", timestamp(), tag, msg)

	mu.Lock()
	defer mu.Unlock()
	if fileOut != nil {
		fmt.Fprintln(fileOut, line)
	}
	if color != "" {
		fmt.Fprintf(stdOut, "%s%s%s\n", color, line, colorReset)
	} else {
		fmt.Fprintln(stdOut, line)
	}
}

// timestamp returns the standard log time prefix.
func timestamp() string {
	return time.Now().Format("2006/01/02 15:04:05")
}
