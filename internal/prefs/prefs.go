// Package prefs keeps the app's own preferences, which the Swift app kept
// in UserDefaults: the telemetry switches, the anonymous install ID and
// the bookkeeping of pings and crash uploads, in a JSON file.
package prefs

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"crypto/rand"
	"fmt"
)

// Prefs are the preferences. Read and change them with Get and Update.
type Prefs struct {
	CrashReports    *bool    `json:"crashReportsEnabled,omitempty"`
	DailyPing       *bool    `json:"dauEnabled,omitempty"`
	InstallID       string   `json:"installId,omitempty"`
	LastPingDate    string   `json:"lastPingDate,omitempty"`
	UploadedCrashes []string `json:"uploadedCrashes,omitempty"`
	// Migrated is set once the UserDefaults of the Swift app were read.
	Migrated bool `json:"migratedUserDefaults,omitempty"`
}

// CrashReportsOn reports whether crash reports are sent, by default yes.
func (p Prefs) CrashReportsOn() bool { return p.CrashReports == nil || *p.CrashReports }

// DailyPingOn reports whether the daily ping is sent, by default yes.
func (p Prefs) DailyPingOn() bool { return p.DailyPing == nil || *p.DailyPing }

// Store is the preferences file.
type Store struct {
	path string
	mu   sync.Mutex
	p    Prefs
}

// Open reads the file at path, or starts empty.
func Open(path string) *Store {
	s := &Store{path: path}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &s.p)
	}
	return s
}

// Get returns a copy of the preferences.
func (s *Store) Get() Prefs {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.p
	p.UploadedCrashes = append([]string(nil), s.p.UploadedCrashes...)
	return p
}

// Update changes the preferences with fn and saves them.
func (s *Store) Update(fn func(p *Prefs)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.p)
	s.save()
}

func (s *Store) save() {
	data, err := json.MarshalIndent(s.p, "", "  ")
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(s.path), 0o755)
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, s.path)
	}
}

// InstallID returns the anonymous install ID, made the first time.
func (s *Store) InstallID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.p.InstallID == "" {
		s.p.InstallID = NewUUID()
		s.save()
	}
	return s.p.InstallID
}

// ResetInstallID replaces the install ID with a new one.
func (s *Store) ResetInstallID() string {
	s.Update(func(p *Prefs) { p.InstallID = NewUUID() })
	return s.Get().InstallID
}

// NewUUID returns a random UUID, in capitals as Foundation writes them.
func NewUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return strings.ToUpper(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]))
}

// MigrateUserDefaults copies what the Swift app kept in the UserDefaults
// of domain into the preferences, once.
func (s *Store) MigrateUserDefaults(domain string) {
	if s.Get().Migrated {
		return
	}
	read := func(key string) (string, bool) {
		out, err := exec.Command("/usr/bin/defaults", "read", domain, key).Output()
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(string(out)), true
	}
	boolOf := func(key string) *bool {
		v, ok := read(key)
		if !ok {
			return nil
		}
		b := v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
		return &b
	}
	s.Update(func(p *Prefs) {
		p.Migrated = true
		if b := boolOf("telemetry.crashReportsEnabled"); b != nil && p.CrashReports == nil {
			p.CrashReports = b
		}
		if b := boolOf("telemetry.dauEnabled"); b != nil && p.DailyPing == nil {
			p.DailyPing = b
		}
		if v, ok := read("telemetry.installId"); ok && v != "" && p.InstallID == "" {
			p.InstallID = v
		}
		if v, ok := read("telemetry.lastPingDate"); ok && p.LastPingDate == "" {
			p.LastPingDate = v
		}
		if v, ok := read("telemetry.uploadedCrashes"); ok && len(p.UploadedCrashes) == 0 {
			p.UploadedCrashes = parseArray(v)
		}
	})
}

// parseArray reads an array as `defaults read` prints it:
// (\n    "a",\n    b\n).
func parseArray(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "(")
	s = strings.TrimSuffix(s, ")")
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ","))
		line = strings.Trim(line, `"`)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
