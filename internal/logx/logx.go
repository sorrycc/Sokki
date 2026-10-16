// Package logx writes Sokki's log, ~/.config/sokki/logs/sokki.log, which
// rotates to sokki.log.1 at 1 MB.
package logx

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Level is how much the log keeps: errors and warnings always, debug
// lines at LevelDebug.
type Level int32

const (
	LevelError Level = iota
	LevelWarning
	LevelDebug
)

const maxSize = 1 << 20

var (
	mu    sync.Mutex
	dir   string
	file  string
	level atomic.Int32
)

func init() {
	level.Store(int32(LevelDebug))
	if d := os.Getenv("SOKKI_CONFIG_DIR"); d != "" {
		SetDir(filepath.Join(d, "logs"))
	} else if home, err := os.UserHomeDir(); err == nil {
		SetDir(filepath.Join(home, ".config", "sokki", "logs"))
	}
}

// SetDir moves the log to dir, for tests.
func SetDir(d string) {
	mu.Lock()
	defer mu.Unlock()
	dir = d
	file = filepath.Join(d, "sokki.log")
	os.MkdirAll(d, 0o755)
}

// File returns the log's path.
func File() string {
	mu.Lock()
	defer mu.Unlock()
	return file
}

// SetLevel sets the level from the config's log_level: "debug" keeps
// debug lines, anything else only errors and warnings.
func SetLevel(name string) {
	if name == "debug" {
		level.Store(int32(LevelDebug))
	} else {
		level.Store(int32(LevelError))
	}
}

// Errorf logs an error.
func Errorf(format string, args ...any) { write("ERROR", format, args...) }

// Warnf logs a warning.
func Warnf(format string, args ...any) { write("WARN", format, args...) }

// Debugf logs a debug line, at LevelDebug.
func Debugf(format string, args ...any) {
	if Level(level.Load()) < LevelDebug {
		return
	}
	write("DEBUG", format, args...)
}

// Tracef logs the expansion engine's decisions, at every level, so that a
// trigger that did not expand can be explained from the log alone. It must
// never carry what was typed in a secure (password) field.
func Tracef(format string, args ...any) { write("TRACE", format, args...) }

func write(label, format string, args ...any) {
	line := fmt.Sprintf("[%s] [%s] %s\n", time.Now().UTC().Format("2006-01-02T15:04:05Z"), label, fmt.Sprintf(format, args...))
	mu.Lock()
	defer mu.Unlock()
	if file == "" {
		return
	}
	if fi, err := os.Stat(file); err == nil && fi.Size() >= maxSize {
		backup := filepath.Join(dir, "sokki.log.1")
		os.Remove(backup)
		os.Rename(file, backup)
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	f.WriteString(line)
	f.Close()
}
