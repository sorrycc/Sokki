// Package history keeps the prompts sent to coding agents (Claude Code,
// Codex, Gemini and others), read from the files where each agent keeps
// them, de-duplicated across agents and counted, for the palette to find
// and insert again. It also counts the uses of matches, which the palette
// ranks by. It stays on this Mac, in history.json of the app's data
// folder, and never logs what it reads.
package history

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"sokki/internal/logx"
	"sokki/internal/model"
)

// File is the history's name in the data folder.
const File = "history.json"

const (
	// keepUses is how many uses of an entry or a match are kept, the
	// latest, for the frecency.
	keepUses = 16
	// keepProjects is how many projects an entry remembers.
	keepProjects = 5
	saveDelay    = time.Second
)

// Item is a prompt as a source read it.
type Item struct {
	Source  string
	Text    string
	Project string
	Time    time.Time
}

// Use is a sending of an entry: by an agent, or by Sokki (RecallSource).
type Use struct {
	Source  string `json:"s"`
	Project string `json:"p,omitempty"`
	Time    int64  `json:"t"` // Unix seconds
}

// RecallSource is the source of the uses that Sokki inserted.
const RecallSource = "sokki"

// Entry is a prompt, with every agent and project it was sent in.
type Entry struct {
	// ID tells entries apart within a run; it is made anew at each load.
	ID   int64  `json:"-"`
	Text string `json:"text"`
	// Count is the uses ever, Recalls those through Sokki.
	Count   int   `json:"n"`
	Recalls int   `json:"r,omitempty"`
	First   int64 `json:"first"`
	Last    int64 `json:"last"`
	// Sources are the agents it was sent in, by ID, the first first.
	Sources []string `json:"sources"`
	// Projects are where it was sent, the latest first.
	Projects []string `json:"projects,omitempty"`
	// Uses are the latest uses, oldest first.
	Uses []Use `json:"uses"`
	// Hidden is set when the person hid it: it is kept so that it is
	// not imported again, but not shown.
	Hidden bool `json:"hidden,omitempty"`
}

// Title is what lists call the entry: its first line with text.
func (e Entry) Title() string { return model.Match{Replace: e.Text}.Title() }

// Project is the folder it was last sent in, by its name.
func (e Entry) Project() string {
	if len(e.Projects) == 0 {
		return ""
	}
	return filepath.Base(e.Projects[0])
}

// UseTimes returns the times of its kept uses, for Frecency.
func (e Entry) UseTimes() []int64 {
	out := make([]int64, len(e.Uses))
	for i, u := range e.Uses {
		out[i] = u.Time
	}
	return out
}

// Settings are the history's settings, kept with it on this Mac.
type Settings struct {
	// Off are the sources not read, by ID.
	Off []string `json:"off,omitempty"`
	// Ignore are texts whose prompts are left out, case-insensitively.
	Ignore []string `json:"ignore,omitempty"`
	// MinChars is the length under which a prompt is left out; 0 for
	// DefaultMinChars.
	MinChars int `json:"minChars,omitempty"`
}

// DefaultMinChars leaves out the shortest prompts: "continue", "commit".
const DefaultMinChars = 12

// On reports whether the source of id is read.
func (s Settings) On(id string) bool { return !slices.Contains(s.Off, id) }

func (s Settings) minChars() int {
	if s.MinChars <= 0 {
		return DefaultMinChars
	}
	return s.MinChars
}

// Cursor is how far a source has read a file of its own.
type Cursor struct {
	Offset int64 `json:"o,omitempty"`
	// N counts the records read of a file read whole, as a JSON array.
	N int `json:"n,omitempty"`
	// Size and Mod are the file's when it was read whole.
	Size int64 `json:"s,omitempty"`
	Mod  int64 `json:"m,omitempty"`
}

// MatchUse is the uses of a match, typed or from the palette.
type MatchUse struct {
	Count int     `json:"n"`
	Times []int64 `json:"t"` // the latest, oldest first
}

type data struct {
	Version  int                 `json:"version"`
	Settings Settings            `json:"settings"`
	Cursors  map[string]Cursor   `json:"cursors"`
	Entries  []Entry             `json:"entries"`
	Matches  map[string]MatchUse `json:"matches"`
	// Imported is when the sources were last read, Unix seconds.
	Imported int64 `json:"imported,omitempty"`
}

// Store is the history file. Its methods are safe from any goroutine.
// The slices it returns are shared: read them only.
type Store struct {
	path string
	now  func() time.Time

	mu      sync.Mutex
	d       data
	byText  map[string]int // normalized text → index in d.Entries
	visible []Entry
	// counts are the visible entries of each source, made as asked.
	counts map[string]int
	gen    uint64
	timer  *time.Timer
	// found are the sources whose files the last scan found, by ID; nil
	// before it.
	found map[string]bool
	// scanning serializes the scans, which read cursors before and
	// write them after.
	scanning sync.Mutex
	// saving serializes the writes of the file, each of a snapshot taken
	// as it holds it, so that the last change is the last written.
	saving sync.Mutex
}

// Open reads the history of dir, or starts empty.
func Open(dir string) *Store { return openAt(dir, time.Now) }

func openAt(dir string, now func() time.Time) *Store {
	s := &Store{path: filepath.Join(dir, File), now: now}
	raw, err := os.ReadFile(s.path)
	if err == nil {
		err = json.Unmarshal(raw, &s.d)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		logx.Errorf("Failed to read %s: %v", File, err)
		s.d = data{}
	}
	s.d.Version = 1
	if s.d.Cursors == nil {
		s.d.Cursors = map[string]Cursor{}
	}
	if s.d.Matches == nil {
		s.d.Matches = map[string]MatchUse{}
	}
	for i := range s.d.Entries {
		s.d.Entries[i].ID = model.NewID()
	}
	s.reindex()
	return s
}

// reindex finds the entries by their text again after entries went, and
// then does what changed does; s.mu is held. Normalizing every text is
// what a change costs most, so only a change that removes entries does
// it: use keeps the index as it adds.
func (s *Store) reindex() {
	s.byText = make(map[string]int, len(s.d.Entries))
	for i, e := range s.d.Entries {
		s.byText[Normalize(e.Text)] = i
	}
	s.changed()
}

// changed lists the entries shown again after a change; s.mu is held.
func (s *Store) changed() {
	s.visible = make([]Entry, 0, len(s.d.Entries))
	for _, e := range s.d.Entries {
		if !e.Hidden {
			s.visible = append(s.visible, e)
		}
	}
	s.counts = nil
	s.gen++
}

// Entries returns the entries not hidden.
func (s *Store) Entries() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.visible
}

// All returns every entry, the hidden ones too.
func (s *Store) All() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.d.Entries)
}

// Gen changes with every change of the entries.
func (s *Store) Gen() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen
}

// Entry returns the entry of id.
func (s *Store) Entry(id int64) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.index(id); i >= 0 {
		return s.d.Entries[i], true
	}
	return Entry{}, false
}

func (s *Store) index(id int64) int {
	return slices.IndexFunc(s.d.Entries, func(e Entry) bool { return e.ID == id })
}

// Settings returns the settings.
func (s *Store) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.d.Settings
	st.Off = slices.Clone(st.Off)
	st.Ignore = slices.Clone(st.Ignore)
	return st
}

// SetSettings changes the settings, leaving out at once the entries they
// now leave out.
func (s *Store) SetSettings(st Settings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Settings = st
	s.d.Entries = slices.DeleteFunc(s.d.Entries, func(e Entry) bool {
		_, ok := Clean(e.Text, st)
		return !ok && !e.Hidden
	})
	s.reindex()
	s.saveLater()
}

// Found reports whether the last scan found the files of the source of
// id, and whether there was one: true until then.
func (s *Store) Found(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.found == nil || s.found[id]
}

// Imported returns when the sources were last read.
func (s *Store) Imported() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.d.Imported == 0 {
		return time.Time{}
	}
	return time.Unix(s.d.Imported, 0)
}

// Counts returns the entries not hidden of each source: shared, to read
// only.
func (s *Store) Counts() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.counts == nil {
		s.counts = map[string]int{}
		for _, e := range s.visible {
			for _, src := range e.Sources {
				s.counts[src]++
			}
		}
	}
	return s.counts
}

// Projects returns the projects the entries were sent in, which sources
// that keep a file in each project (Aider) look in.
func (s *Store) Projects() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, e := range s.d.Entries {
		for _, p := range e.Projects {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// cursors returns a copy of the cursors.
func (s *Store) cursors() map[string]Cursor {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]Cursor, len(s.d.Cursors))
	for k, v := range s.d.Cursors {
		out[k] = v
	}
	return out
}

// Import adds the items read, as entries or uses of the entries of the
// same text, and keeps the cursors of the reading. It returns how many
// entries are new.
func (s *Store) Import(items []Item, cursors map[string]Cursor) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	moved := false
	for k, v := range cursors {
		if s.d.Cursors[k] != v {
			s.d.Cursors[k] = v
			moved = true
		}
	}
	s.d.Imported = s.now().Unix()
	added, taken := 0, 0
	for _, it := range items {
		text, ok := Clean(it.Text, s.d.Settings)
		if !ok {
			continue
		}
		u := Use{Source: it.Source, Project: it.Project, Time: it.Time.Unix()}
		if it.Time.IsZero() {
			u.Time = s.d.Imported
		}
		taken++
		if s.use(text, u) {
			added++
		}
	}
	// A scan that found nothing leaves Gen, and the palette's search of
	// the entries, as they were, and the file unwritten: the time of the
	// reading alone is not worth writing every history again.
	if taken > 0 {
		s.changed()
	}
	if taken > 0 || moved {
		s.saveLater()
	}
	return added
}

// use adds a use of text, making its entry when it has none, which use
// reports; s.mu is held, and s.changed is the caller's.
func (s *Store) use(text string, u Use) bool {
	key := Normalize(text)
	i, ok := s.byText[key]
	if !ok {
		s.d.Entries = append(s.d.Entries, Entry{ID: model.NewID(), Text: text, First: u.Time, Last: u.Time})
		i = len(s.d.Entries) - 1
		s.byText[key] = i
	}
	e := &s.d.Entries[i]
	e.Count++
	if u.Source == RecallSource {
		e.Recalls++
	} else if !slices.Contains(e.Sources, u.Source) {
		e.Sources = append(slices.Clip(e.Sources), u.Source)
	}
	e.First = min(e.First, u.Time)
	// Each slice is made anew, never changed in place: Entries and a
	// save's snapshot share them, read outside s.mu.
	if u.Time >= e.Last {
		e.Last = u.Time
		if u.Project != "" {
			projects := make([]string, 0, keepProjects)
			projects = append(projects, u.Project)
			for _, p := range e.Projects {
				if p != u.Project && len(projects) < keepProjects {
					projects = append(projects, p)
				}
			}
			e.Projects = projects
		}
	}
	at, _ := slices.BinarySearchFunc(e.Uses, u.Time, func(x Use, t int64) int { return int(x.Time - t) })
	uses := make([]Use, 0, len(e.Uses)+1)
	uses = append(append(append(uses, e.Uses[:at]...), u), e.Uses[at:]...)
	e.Uses = uses[max(0, len(uses)-keepUses):]
	return !ok
}

// Recall counts an insertion of the entry of id by Sokki, in project.
func (s *Store) Recall(id int64, project string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.index(id)
	if i < 0 {
		return
	}
	s.use(s.d.Entries[i].Text, Use{Source: RecallSource, Project: project, Time: s.now().Unix()})
	s.changed()
	s.saveLater()
}

// Hide hides the entry of id for good: a new sending of it counts, but
// it does not show.
func (s *Store) Hide(id int64) (Entry, bool) {
	return s.setHidden(id, true)
}

// Unhide shows a hidden entry again, as undo.
func (s *Store) Unhide(id int64) (Entry, bool) {
	return s.setHidden(id, false)
}

func (s *Store) setHidden(id int64, hidden bool) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.index(id)
	if i < 0 {
		return Entry{}, false
	}
	s.d.Entries[i].Hidden = hidden
	s.changed()
	s.saveLater()
	return s.d.Entries[i], true
}

// Clear forgets the entries. The sources go on from where they were, so
// only prompts sent from now on come in, unless reimport, which reads
// every source again from the start.
func (s *Store) Clear(reimport bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Entries = nil
	if reimport {
		s.d.Cursors = map[string]Cursor{}
	}
	s.reindex()
	s.saveLater()
}

// BumpMatch counts a use of the match of key now.
func (s *Store) BumpMatch(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.d.Matches[key]
	m.Count++
	m.Times = append(slices.Clip(m.Times[max(0, len(m.Times)+1-keepUses):]), s.now().Unix())
	s.d.Matches[key] = m
	s.gen++
	s.saveLater()
}

// MatchUse returns the uses of the match of key.
func (s *Store) MatchUse(key string) MatchUse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.Matches[key]
}

// Find returns the ID of the entry, hidden or not, whose text is key
// once normalized: key is Normalize's.
func (s *Store) Find(key string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i, ok := s.byText[key]; ok {
		return s.d.Entries[i].ID, true
	}
	return 0, false
}

// MatchUses returns the uses of every match, by key: a copy.
func (s *Store) MatchUses() map[string]MatchUse {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]MatchUse, len(s.d.Matches))
	for k, v := range s.d.Matches {
		out[k] = v
	}
	return out
}

// saveLater saves a moment after the last change; s.mu is held.
func (s *Store) saveLater() {
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(saveDelay, s.save)
}

// Flush writes a pending change now, as the app quits, after a write
// on its way.
func (s *Store) Flush() {
	s.mu.Lock()
	pending := s.timer != nil && s.timer.Stop()
	s.mu.Unlock()
	if pending {
		s.save()
	}
	s.saving.Lock()
	s.saving.Unlock()
}

// save writes the file: a snapshot taken under s.mu, which a change
// never writes into (use), encoded and written without it, so that a
// large history holds up no one, and written whole or not at all.
func (s *Store) save() {
	s.saving.Lock()
	defer s.saving.Unlock()
	s.mu.Lock()
	s.timer = nil
	d := s.d
	d.Entries = slices.Clone(s.d.Entries)
	d.Cursors = maps.Clone(s.d.Cursors)
	d.Matches = maps.Clone(s.d.Matches)
	s.mu.Unlock()
	if err := writeJSON(s.path, d); err != nil {
		logx.Errorf("Failed to save %s: %v", File, err)
	}
}

// writeJSON writes v to path through a file beside it, synced and then
// renamed over it, so that a crash leaves the old file or the new one.
func writeJSON(path string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(f.Name(), 0o600)
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

// Normalize is what tells two prompts apart: their text with its spaces
// and line breaks collapsed.
func Normalize(text string) string { return strings.Join(strings.Fields(text), " ") }
