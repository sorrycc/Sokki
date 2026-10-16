package telemetry

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
)

// maxCrashes is how many crash logs the crashes folder keeps.
const maxCrashes = 20

// KeepCrashes has the Go runtime write the panic or fatal error that ends
// the app, with every goroutine's stack, to crash.log in dir: a crash of a
// Go program leaves no .ips report. A crash.log a crash left is first moved
// into dir/crashes, named by its time, for UploadCrashes. It returns the
// crashes folder.
func KeepCrashes(dir string) string {
	crashes := filepath.Join(dir, "crashes")
	path := filepath.Join(dir, "crash.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
		if os.MkdirAll(crashes, 0o755) == nil {
			os.Rename(path, filepath.Join(crashes, "Sokki-"+fi.ModTime().UTC().Format("2006-01-02-150405")+".log"))
			prune(crashes)
		}
	}
	os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err == nil {
		debug.SetCrashOutput(f, debug.CrashOptions{})
		f.Close() // SetCrashOutput keeps its own copy
	}
	return crashes
}

func prune(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	for len(names) > maxCrashes {
		os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}
