package prefs

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestParseArray(t *testing.T) {
	got := parseArray("(\n    \"Sokki-2026-04-01.ips\",\n    plain\n)")
	if !slices.Equal(got, []string{"Sokki-2026-04-01.ips", "plain"}) {
		t.Fatalf("%q", got)
	}
	if parseArray("(\n)") != nil {
		t.Fatal("empty array")
	}
}

func TestPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")
	s := Open(path)
	off := false
	s.Update(func(p *Prefs) { p.DailyPing = &off; p.LastPingDate = "2026-10-16" })
	again := Open(path).Get()
	if again.DailyPingOn() || again.LastPingDate != "2026-10-16" || !again.CrashReportsOn() {
		t.Fatalf("%+v", again)
	}
}
