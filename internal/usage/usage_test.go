package usage

import (
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T, now time.Time) *Store {
	t.Helper()
	s := Open(t.TempDir())
	s.now = func() time.Time { return now }
	return s
}

func TestSums(t *testing.T) {
	now := time.Date(2026, 10, 29, 15, 0, 0, 0, time.Local)
	s := open(t, now)
	s.Log(Event{Trigger: ":hi", App: "com.apple.Notes", AppName: "Notes", Saved: 7})
	s.Log(Event{Trigger: ":hi", App: "com.apple.mail", AppName: "Mail", Saved: 7})
	s.Log(Event{Trigger: ":sig", App: "com.apple.mail", AppName: "Mail", Palette: true, Saved: 30})
	s.Log(Event{Trigger: ":x", Saved: -3})

	if got := s.Total(); got != 4 {
		t.Fatalf("Total = %d", got)
	}
	if got := s.Saved(); got != 44 {
		t.Fatalf("Saved = %d", got)
	}
	if got := s.Palette(); got != 1 {
		t.Fatalf("Palette = %d", got)
	}
	if tr := s.Triggers(); len(tr) != 3 || tr[0].Key != ":hi" || tr[0].Count != 2 {
		t.Fatalf("Triggers = %+v", tr)
	}
	if apps := s.Apps(); len(apps) != 2 || apps[0].Name != "Mail" || apps[0].Count != 2 {
		t.Fatalf("Apps = %+v", apps)
	}
	if d := s.Daily(); d[6] != 4 || d[5] != 0 {
		t.Fatalf("Daily = %v", d)
	}
}

func TestDailyAndPrune(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.Local)
	s := Open(dir)
	for _, ago := range []int{0, 1, 1, 6, 8} {
		s.now = func() time.Time { return now.AddDate(0, 0, -ago) }
		s.Log(Event{Trigger: ":a"})
	}
	s.now = func() time.Time { return now }
	if d := s.Daily(); d != [7]int{1, 0, 0, 0, 0, 2, 1} {
		t.Fatalf("Daily = %v", d)
	}
	s.Flush()

	// Reopened, the event of 8 days ago is gone.
	r := Open(dir)
	r.now = func() time.Time { return now }
	r.prune()
	if got := r.Total(); got != 4 {
		t.Fatalf("Total after reopening = %d", got)
	}
}

func TestDailyAcrossDaylightSaving(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip(err)
	}
	// Summer time ends on October 25, 2026: that day has 25 hours.
	now := time.Date(2026, 10, 27, 0, 30, 0, 0, loc)
	s := open(t, now)
	s.now = func() time.Time { return time.Date(2026, 10, 25, 23, 30, 0, 0, loc) }
	s.Log(Event{Trigger: ":a"})
	s.now = func() time.Time { return now }
	if d := s.Daily(); d[4] != 1 {
		t.Fatalf("Daily = %v", d)
	}
}

func TestClear(t *testing.T) {
	s := open(t, time.Now())
	s.Log(Event{Trigger: ":a"})
	s.Flush()
	s.Clear()
	if s.Total() != 0 || len(s.Triggers()) != 0 {
		t.Fatal("not cleared")
	}
	if got := Open(filepath.Dir(s.path)).Total(); got != 0 {
		t.Fatalf("file kept %d events", got)
	}
}
