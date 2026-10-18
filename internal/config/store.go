package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"sokki/internal/logx"
	"sokki/internal/model"
)

// Store is the matches and the configuration the app works with, and
// saves them as they change. It is used on the main thread.
type Store struct {
	Dir     Dir
	Matches []model.Match
	Config  model.Config
	// LastError is the first match file that failed to parse.
	LastError string
	files     []string
	// OnMatchesChanged is called after the matches change, by a load or
	// an edit.
	OnMatchesChanged func()
	// Gen counts the changes of the matches, for views that keep what
	// they derive from them.
	Gen uint64

	// The files and counts of the matches as of the generation derivedGen,
	// which views ask for every frame.
	derivedGen  uint64
	sourceFiles []string
	fileCounts  map[string]int
}

// NewStore makes a store of d, empty until Load.
func NewStore(d Dir) *Store { return &Store{Dir: d, Config: model.DefaultConfig()} }

// Load reads everything again.
func (s *Store) Load() {
	r := s.Dir.Load()
	// A match keeps its ID across loads, by its file and trigger, so that
	// a list's choice and an editor open on it survive the reload that
	// follows every save.
	ids := map[[2]string]int64{}
	for _, m := range s.Matches {
		ids[[2]string{m.SourceFile, m.Trigger}] = m.ID
	}
	for i, m := range r.Matches {
		if id, ok := ids[[2]string{m.SourceFile, m.Trigger}]; ok {
			r.Matches[i].ID = id
		}
	}
	s.Matches, s.Config, s.LastError, s.files = r.Matches, r.Config, r.Error, r.Files
	if r.Error != "" {
		logx.Errorf("Config load error: %s", r.Error)
	}
	for _, w := range r.Warnings {
		logx.Warnf("%s", w)
	}
	s.changed()
}

func (s *Store) changed() {
	s.Gen++
	if s.OnMatchesChanged != nil {
		s.OnMatchesChanged()
	}
}

// SourceFiles are the match files a match can go to, sorted. The slice
// is shared: don't change it.
func (s *Store) SourceFiles() []string {
	s.derive()
	return s.sourceFiles
}

// FileCounts returns how many matches each match file has. The map is
// shared: don't change it.
func (s *Store) FileCounts() map[string]int {
	s.derive()
	return s.fileCounts
}

func (s *Store) derive() {
	if s.fileCounts != nil && s.derivedGen == s.Gen {
		return
	}
	files := append(slices.Clone(s.files), DefaultMatchFile)
	counts := make(map[string]int, len(s.files)+1)
	for _, m := range s.Matches {
		f := m.SourceFile
		if f == "" {
			f = DefaultMatchFile
		} else {
			files = append(files, f)
		}
		counts[f]++
	}
	slices.Sort(files)
	s.sourceFiles, s.fileCounts, s.derivedGen = slices.Compact(files), counts, s.Gen
}

// Match returns the match with id.
func (s *Store) Match(id int64) (model.Match, bool) {
	for _, m := range s.Matches {
		if m.ID == id {
			return m, true
		}
	}
	return model.Match{}, false
}

// Add adds a match, to default.yml unless it names a file.
func (s *Store) Add(m model.Match) {
	if m.SourceFile == "" {
		m.SourceFile = DefaultMatchFile
	}
	if m.ID == 0 {
		m.ID = model.NewID()
	}
	s.Matches = append(s.Matches, m)
	s.save()
}

// Update replaces the match with m's ID.
func (s *Store) Update(m model.Match) {
	i := slices.IndexFunc(s.Matches, func(x model.Match) bool { return x.ID == m.ID })
	if i < 0 {
		return
	}
	s.Matches[i] = m
	s.save()
}

// Remove removes the match with id.
func (s *Store) Remove(id int64) {
	s.Matches = slices.DeleteFunc(s.Matches, func(x model.Match) bool { return x.ID == id })
	s.save()
}

// SaveConfig writes the configuration.
func (s *Store) SaveConfig() { s.Dir.SaveConfig(s.Config) }

func (s *Store) save() {
	s.Dir.SaveMatches(s.Matches, s.files)
	for _, m := range s.Matches {
		if m.SourceFile != "" && !slices.Contains(s.files, m.SourceFile) {
			s.files = append(s.files, m.SourceFile)
		}
	}
	s.changed()
}

// Recency keeps when each trigger was last used from the palette, in
// recency.json, written a second after the last use. It is keyed by
// trigger since matches get new IDs at every load.
type Recency struct {
	path    string
	mu      sync.Mutex
	used    map[string]time.Time
	pending *time.Timer
}

// NewRecency reads recency.json of d.
func NewRecency(d Dir) *Recency {
	r := &Recency{path: d.path("recency.json"), used: map[string]time.Time{}}
	if data, err := os.ReadFile(r.path); err == nil {
		json.Unmarshal(data, &r.used)
	}
	return r
}

// Snapshot returns a copy of the times.
func (r *Recency) Snapshot() map[string]time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]time.Time, len(r.used))
	for k, v := range r.used {
		out[k] = v
	}
	return out
}

// Bump marks trigger used now.
func (r *Recency) Bump(trigger string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.used[trigger] = time.Now().UTC().Truncate(time.Second)
	if r.pending != nil {
		r.pending.Stop()
	}
	r.pending = time.AfterFunc(time.Second, r.save)
}

// Flush writes a pending change now.
func (r *Recency) Flush() {
	r.mu.Lock()
	p := r.pending
	r.pending = nil
	r.mu.Unlock()
	if p != nil && p.Stop() {
		r.save()
	}
}

func (r *Recency) save() {
	r.mu.Lock()
	data, err := json.MarshalIndent(r.used, "", "  ")
	r.mu.Unlock()
	if err == nil {
		os.MkdirAll(filepath.Dir(r.path), 0o755)
		err = writeAtomic(r.path, data)
	}
	if err != nil {
		logx.Errorf("Failed to save recency.json: %v", err)
	}
}
