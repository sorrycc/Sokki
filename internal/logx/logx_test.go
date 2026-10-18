package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritesAndRotates(t *testing.T) {
	d := t.TempDir()
	SetDir(d)
	Errorf("first %d", 1)
	Flush()
	b, err := os.ReadFile(filepath.Join(d, "sokki.log"))
	if err != nil || !strings.Contains(string(b), "[ERROR] first 1") {
		t.Fatalf("log %q, %v", b, err)
	}

	big := strings.Repeat("x", 1000)
	for range maxSize/1000 + 10 {
		Tracef("%s", big)
		Flush() // keep the queue from filling
	}
	if _, err := os.Stat(filepath.Join(d, "sokki.log.1")); err != nil {
		t.Fatalf("not rotated: %v", err)
	}
}

func TestWriteIsCheap(t *testing.T) {
	SetDir(t.TempDir())
	if n := testing.AllocsPerRun(100, func() { Tracef("key %c", 'a') }); n > 20 {
		t.Errorf("%v allocations per line", n)
	}
	Flush()
}
