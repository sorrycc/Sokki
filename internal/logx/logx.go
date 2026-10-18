// Package logx writes Sokki's log, ~/.config/sokki/logs/sokki.log, which
// rotates to sokki.log.1 at 1 MB.
//
// Lines are written by a goroutine of the package's own, so that logging
// costs the caller (the event tap's thread among them) no file system
// call: the file stays open between lines.
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

	// lines holds what is to be written, in order; a flush is a request
	// whose channel closes once the lines before it are written.
	lines = make(chan request, 4096)
	// dropped counts the lines lost while the queue was full.
	dropped atomic.Int64
)

type request struct {
	line  string
	flush chan struct{}
}

func init() {
	level.Store(int32(LevelDebug))
	go run()
	if d := os.Getenv("SOKKI_CONFIG_DIR"); d != "" {
		SetDir(filepath.Join(d, "logs"))
	} else if home, err := os.UserHomeDir(); err == nil {
		SetDir(filepath.Join(home, ".config", "sokki", "logs"))
	}
}

// SetDir moves the log to dir, for tests.
func SetDir(d string) {
	Flush()
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

// Flush waits until the lines logged so far are in the file, as before
// quitting. It gives up after a second.
func Flush() {
	done := make(chan struct{})
	select {
	case lines <- request{flush: done}:
	case <-time.After(time.Second):
		return
	}
	select {
	case <-done:
	case <-time.After(time.Second):
	}
}

func write(label, format string, args ...any) {
	line := fmt.Sprintf("[%s] [%s] %s\n", time.Now().UTC().Format("2006-01-02T15:04:05Z"), label, fmt.Sprintf(format, args...))
	select {
	case lines <- request{line: line}:
	default:
		// Never block the caller, which may be the event tap.
		dropped.Add(1)
	}
}

// run writes the lines as they come, keeping the file open and its size
// counted, and rotates it at maxSize.
func run() {
	var (
		f       *os.File
		path    string
		size    int64
		checked time.Time
	)
	closeFile := func() {
		if f != nil {
			f.Close()
			f = nil
		}
	}
	for req := range lines {
		if req.flush != nil {
			close(req.flush)
			continue
		}
		mu.Lock()
		want, d := file, dir
		mu.Unlock()
		if want == "" {
			continue
		}
		if want != path {
			closeFile()
			path = want
		}
		// The file may have been deleted or moved meanwhile, as by hand:
		// look at most once a second.
		if f != nil && time.Since(checked) > time.Second {
			checked = time.Now()
			on, err1 := os.Stat(path)
			open, err2 := f.Stat()
			if err1 != nil || err2 != nil || !os.SameFile(on, open) {
				closeFile()
			}
		}
		if f != nil && size >= maxSize {
			closeFile()
			backup := filepath.Join(d, "sokki.log.1")
			os.Remove(backup)
			os.Rename(path, backup)
		}
		if f == nil {
			var err error
			if f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err != nil {
				f = nil
				continue
			}
			size = 0
			if fi, err := f.Stat(); err == nil {
				size = fi.Size()
			}
		}
		line := req.line
		if n := dropped.Swap(0); n > 0 {
			line = fmt.Sprintf("[%s] [WARN] %d log lines dropped\n", time.Now().UTC().Format("2006-01-02T15:04:05Z"), n) + line
		}
		n, _ := f.WriteString(line)
		size += int64(n)
	}
}
