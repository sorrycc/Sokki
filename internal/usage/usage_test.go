package usage

import (
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T, now time.Time) *Store {
	t.Helper()
	return openAt(t.TempDir(), func() time.Time { return now })
}

func TestSums(t *testing.T) {
	now := time.Date(2026, 10, 29, 15, 0, 0, 0, time.Local)
	s := open(t, now)
	s.Log(Event{Trigger: ":hi", App: "com.apple.Notes", AppName: "Notes", Saved: 7})
	s.Log(Event{Trigger: ":hi", App: "com.apple.mail", AppName: "Mail", Saved: 7})
	s.Log(Event{Trigger: ":sig", App: "com.apple.mail", AppName: "Mail", Palette: true, Saved: 30})
	s.Log(Event{Trigger: ":x", Saved: -3})

	m := s.Summary()
	if m.Total != 4 || s.Total() != 4 {
		t.Fatalf("Total = %d", m.Total)
	}
	if m.Saved != 44 {
		t.Fatalf("Saved = %d", m.Saved)
	}
	if m.Palette != 1 {
		t.Fatalf("Palette = %d", m.Palette)
	}
	if tr := m.Triggers; len(tr) != 3 || tr[0].Key != ":hi" || tr[0].Count != 2 {
		t.Fatalf("Triggers = %+v", tr)
	}
	if apps := m.Apps; len(apps) != 2 || apps[0].Name != "Mail" || apps[0].Count != 2 {
		t.Fatalf("Apps = %+v", apps)
	}
	if d := m.Daily; d[6] != 4 || d[5] != 0 || m.ActiveDays() != 1 {
		t.Fatalf("Daily = %v", d)
	}
	if again := s.Summary(); again.Rev != m.Rev {
		t.Fatalf("counted again without a change: %d, %d", m.Rev, again.Rev)
	}
	s.Log(Event{Trigger: ":hi"})
	if again := s.Summary(); again.Rev == m.Rev || again.Total != 5 || m.Total != 4 {
		t.Fatalf("after a change: %+v, before %+v", again, m)
	}
}

func TestDailyAndPrune(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.Local)
	s := openAt(dir, func() time.Time { return now })
	for _, ago := range []int{0, 1, 1, 6, 8} {
		s.now = func() time.Time { return now.AddDate(0, 0, -ago) }
		s.Log(Event{Trigger: ":a"})
	}
	s.now = func() time.Time { return now }
	if d := s.Summary().Daily; d != [7]int{1, 0, 0, 0, 0, 2, 1} {
		t.Fatalf("Daily = %v", d)
	}
	s.Flush()

	// Reopened, the event of 8 days ago is gone.
	r := openAt(dir, func() time.Time { return now })
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
	if d := s.Summary().Daily; d[4] != 1 {
		t.Fatalf("Daily = %v", d)
	}
}

func TestClear(t *testing.T) {
	s := open(t, time.Now())
	s.Log(Event{Trigger: ":a"})
	s.Flush()
	s.Clear()
	if s.Total() != 0 || len(s.Summary().Triggers) != 0 {
		t.Fatal("not cleared")
	}
	if got := Open(filepath.Dir(s.path)).Total(); got != 0 {
		t.Fatalf("file kept %d events", got)
	}
}
