package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sokki/internal/logx"
	"sokki/internal/model"
)

func tempDir(t *testing.T) Dir {
	t.Helper()
	d := t.TempDir()
	logx.SetDir(filepath.Join(d, "logs"))
	return Dir(filepath.Join(d, "sokki"))
}

func TestBootstrapAndLoad(t *testing.T) {
	d := tempDir(t)
	if made, err := d.Bootstrap(); err != nil || !made {
		t.Fatalf("bootstrap: %v %v", made, err)
	}
	if made, _ := d.Bootstrap(); made {
		t.Fatal("bootstrapped twice")
	}
	// The log makes the directory before the bootstrap looks.
	logsOnly := tempDir(t)
	os.MkdirAll(logsOnly.LogDir(), 0o755)
	if made, err := logsOnly.Bootstrap(); err != nil || !made {
		t.Fatalf("logs only: %v %v", made, err)
	}
	r := d.Load()
	if r.Error != "" || len(r.Matches) != 4 || !r.Config.Enabled || r.Config.ClipboardThreshold != 50 {
		t.Fatalf("load: %+v", r)
	}
	if r.Matches[3].Trigger != ":sig" || r.Matches[3].SourceFile != DefaultMatchFile {
		t.Fatalf("sig: %+v", r.Matches[3])
	}
}

func TestDuplicatesAndErrors(t *testing.T) {
	d := tempDir(t)
	os.MkdirAll(d.MatchDir(), 0o755)
	os.WriteFile(filepath.Join(d.MatchDir(), "a.yml"), []byte("matches:\n  - trigger: x\n    replace: from a\n"), 0o644)
	os.WriteFile(filepath.Join(d.MatchDir(), "b.yaml"), []byte("matches:\n  - trigger: x\n    replace: from b\n"), 0o644)
	os.WriteFile(filepath.Join(d.MatchDir(), "c.yml"), []byte("matches: [oops"), 0o644)
	os.WriteFile(filepath.Join(d.MatchDir(), "notes.txt"), []byte("ignored"), 0o644)
	r := d.Load()
	if len(r.Matches) != 1 || r.Matches[0].Replace != "from b" || len(r.Warnings) != 1 {
		t.Fatalf("duplicates: %+v", r)
	}
	if !strings.Contains(r.Error, "c.yml") || len(r.Files) != 3 {
		t.Fatalf("error %q files %v", r.Error, r.Files)
	}
}

func TestStoreSaves(t *testing.T) {
	d := tempDir(t)
	d.Bootstrap()
	s := NewStore(d)
	changes := 0
	s.OnMatchesChanged = func() { changes++ }
	s.Load()
	before := s.FileCounts()[DefaultMatchFile]
	s.Add(model.Match{Trigger: ":new", Replace: "line 1\nline 2", Enabled: true, SourceFile: "work.yml"})
	if counts := s.FileCounts(); counts["work.yml"] != 1 || counts[DefaultMatchFile] != before {
		t.Fatalf("counts not derived again after an add: %v", counts)
	}
	first := s.Matches[0]
	first.Enabled = false
	s.Update(first)
	s.Remove(s.Matches[1].ID) // :today
	s.Config.InjectDelay = 7
	s.SaveConfig()

	again := d.Load()
	if len(again.Matches) != 4 || again.Config.InjectDelay != 7 {
		t.Fatalf("reload: %+v", again)
	}
	var found bool
	for _, m := range again.Matches {
		if m.Trigger == ":new" {
			found = m.SourceFile == "work.yml" && m.Replace == "line 1\nline 2"
		}
		if m.Trigger == ":hello" && m.Enabled {
			t.Fatal("update lost")
		}
		if m.Trigger == ":today" {
			t.Fatal("remove lost")
		}
	}
	if !found || changes != 4 {
		t.Fatalf("found %v changes %d", found, changes)
	}
	if files := s.SourceFiles(); len(files) != 2 || files[1] != "work.yml" {
		t.Fatalf("files %v", files)
	}
}

func TestRecency(t *testing.T) {
	d := tempDir(t)
	r := NewRecency(d)
	r.Bump(":a")
	r.Flush()
	if _, ok := NewRecency(d).Snapshot()[":a"]; !ok {
		t.Fatal("recency not saved")
	}
}

func TestReloadKeepsIDs(t *testing.T) {
	d := tempDir(t)
	d.Bootstrap()
	s := NewStore(d)
	s.Load()
	id := s.Matches[0].ID
	s.Load()
	if s.Matches[0].ID != id {
		t.Fatal("reload changed the ID")
	}
}
