// Package usage logs the expansions of the last 7 days, in usage.json,
// for the Insights page. It never keeps what was typed, only the trigger.
package usage

import (
	"cmp"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"sokki/internal/logx"
)

// File is the name of the log, in the configuration directory.
const File = "usage.json"

const (
	keep      = 7 * 24 * time.Hour
	saveDelay = 300 * time.Millisecond
)

// Event is an expansion of a trigger.
type Event struct {
	Time    time.Time `json:"time"`
	Trigger string    `json:"trigger"`
	// Palette is whether it was inserted from the palette, not typed.
	Palette bool `json:"palette,omitempty"`
	// App is the bundle identifier of the app it was typed in, AppName
	// its name.
	App     string `json:"app,omitempty"`
	AppName string `json:"appName,omitempty"`
	// Saved is the characters it saved: the replacement's less the
	// trigger's.
	Saved int `json:"saved"`
}

// Count is the expansions of a key: a trigger or an app.
type Count struct {
	Key   string
	Name  string // the app's name; the trigger for a trigger
	Count int
}

// Store holds the events. It is safe to use from any goroutine.
type Store struct {
	mu     sync.Mutex
	path   string
	events []Event
	timer  *time.Timer
	now    func() time.Time

	// rev counts the changes of the events; sums keeps what the Insights
	// page asks on every frame it draws, as of sums.rev.
	rev  uint64
	sums sums
}

// sums are the counts of the events, as of a revision and a day.
type sums struct {
	rev      uint64
	today    time.Time
	daily    [7]int
	saved    int
	palette  int
	triggers []Count
	apps     []Count
}

// Open loads the events of dir, without those older than 7 days.
func Open(dir string) *Store {
	s := &Store{path: filepath.Join(dir, File), now: time.Now}
	data, err := os.ReadFile(s.path)
	if err == nil {
		err = json.Unmarshal(data, &s.events)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		logx.Errorf("Failed to read usage.json: %v", err)
		s.events = nil
	}
	s.prune()
	return s
}

// Log records an expansion now.
func (s *Store) Log(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e.Time = s.now().UTC().Truncate(time.Second)
	e.Saved = max(e.Saved, 0)
	s.events = append(s.events, e)
	s.rev++
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(saveDelay, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.save()
	})
}

// Total returns the number of expansions.
func (s *Store) Total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

// Daily returns the expansions of each of the last 7 days, by the local
// calendar, today last.
func (s *Store) Daily() [7]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.summed().daily
}

// Saved returns the characters the expansions saved.
func (s *Store) Saved() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.summed().saved
}

// Palette returns the expansions inserted from the palette; the others
// were typed.
func (s *Store) Palette() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.summed().palette
}

// Triggers returns the triggers expanded, most first.
func (s *Store) Triggers() []Count {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.summed().triggers)
}

// Apps returns the apps expanded in, most first. Insertions from the
// palette count for the app they went to.
func (s *Store) Apps() []Count {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.summed().apps)
}

// summed counts the events again after a change, or on a new day; s.mu
// is held.
func (s *Store) summed() *sums {
	now := s.now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	m := &s.sums
	if m.triggers != nil && m.rev == s.rev && m.today.Equal(today) {
		return m
	}
	m.rev, m.today = s.rev, today
	m.daily, m.saved, m.palette = [7]int{}, 0, 0
	triggers := map[string]*Count{}
	apps := map[string]*Count{}
	for _, e := range s.events {
		t := e.Time.In(now.Location())
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
		// Rounded, for the days that daylight saving time makes 23 or 25 hours.
		ago := int((today.Sub(day) + 12*time.Hour) / (24 * time.Hour))
		if ago >= 0 && ago < len(m.daily) {
			m.daily[len(m.daily)-1-ago]++
		}
		m.saved += e.Saved
		if e.Palette {
			m.palette++
		}
		add(triggers, e.Trigger, e.Trigger)
		if e.App != "" {
			add(apps, e.App, cmp.Or(e.AppName, e.App))
		}
	}
	m.triggers, m.apps = sorted(triggers), sorted(apps)
	return m
}

func add(counts map[string]*Count, key, name string) {
	c, ok := counts[key]
	if !ok {
		c = &Count{Key: key}
		counts[key] = c
	}
	c.Name = name // the latest
	c.Count++
}

func sorted(counts map[string]*Count) []Count {
	out := make([]Count, 0, len(counts))
	for _, c := range counts {
		out = append(out, *c)
	}
	slices.SortFunc(out, func(a, b Count) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Key, b.Key))
	})
	return out
}

// Clear deletes every event, and the file.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = nil
	s.rev++
	if s.timer != nil {
		s.timer.Stop()
	}
	if err := os.Remove(s.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		logx.Errorf("Failed to delete usage.json: %v", err)
	}
}

// Flush writes a pending change now, as the app quits.
func (s *Store) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil && s.timer.Stop() {
		s.save()
	}
}

// save writes the events; s.mu is held.
func (s *Store) save() {
	s.timer = nil
	s.prune()
	data, err := json.Marshal(s.events)
	if err == nil {
		os.MkdirAll(filepath.Dir(s.path), 0o755)
		tmp := s.path + ".tmp"
		if err = os.WriteFile(tmp, data, 0o644); err == nil {
			err = os.Rename(tmp, s.path)
		}
	}
	if err != nil {
		logx.Errorf("Failed to save usage.json: %v", err)
	}
}

func (s *Store) prune() {
	cutoff := s.now().Add(-keep)
	n := len(s.events)
	s.events = slices.DeleteFunc(s.events, func(e Event) bool { return e.Time.Before(cutoff) })
	if len(s.events) != n {
		s.rev++
	}
}
