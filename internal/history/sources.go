package history

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"sokki/internal/logx"
)

// Source is an agent whose prompts the history reads, from the files it
// keeps them in. These files are no agent's promise: a source that fails
// to read logs it and reads nothing, leaving the others alone.
type Source struct {
	ID, Name string
	// Where says where it reads, for Settings.
	Where string
	// scan reads what was added since the cursors, by file, calling
	// emit for each prompt, and returns the cursors of what it read.
	scan func(env Env, cur map[string]Cursor, emit func(Item)) map[string]Cursor
	// files are the patterns (filepath.Glob's) of the files it reads,
	// which tell whether the agent is on this Mac.
	files func(env Env) []string
}

// locate returns whether a file the source reads is there, and what to
// show in the Finder for it: the file of them changed last; with none, the
// nearest folder of its first pattern that is there, short of the home
// folder; else "".
func (src Source) locate(env Env) (found bool, where string) {
	pats := src.files(env)
	var at time.Time
	for _, pat := range pats {
		m, _ := filepath.Glob(pat)
		for _, f := range m {
			if fi, err := os.Stat(f); err == nil && (where == "" || fi.ModTime().After(at)) {
				where, at = f, fi.ModTime()
			}
		}
	}
	if where != "" || len(pats) == 0 {
		return where != "", where
	}
	dir := pats[0]
	for {
		dir = filepath.Dir(dir)
		if rel, err := filepath.Rel(env.Home, dir); err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			return false, ""
		}
		if strings.ContainsAny(dir, "*?[") {
			continue
		}
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return false, dir
		}
	}
}

// under is files for a source of a single pattern, under the home
// folder.
func under(elem ...string) func(Env) []string {
	return func(env Env) []string { return []string{filepath.Join(append([]string{env.Home}, elem...)...)} }
}

// Env is where the sources look.
type Env struct {
	Home string
	// Projects are the folders prompts were sent in, where Aider keeps
	// its history.
	Projects []string
	// StateHome is $XDG_STATE_HOME, "" for ~/.local/state.
	StateHome string
}

// Sources are the agents the history reads.
var Sources = []Source{
	{ID: "claude", Name: "Claude Code", Where: "~/.claude*/history.jsonl", scan: scanClaude,
		files: claudeHistories},
	{ID: "codex", Name: "Codex", Where: "~/.codex/history.jsonl", scan: scanCodex,
		files: under(".codex", "history.jsonl")},
	{ID: "gemini", Name: "Gemini CLI", Where: "~/.gemini/tmp/*/logs.json", scan: scanGemini,
		files: under(".gemini", "tmp", "*", "logs.json")},
	{ID: "antigravity", Name: "Antigravity", Where: "~/.gemini/antigravity-cli/history.jsonl", scan: scanAntigravity,
		files: under(".gemini", "antigravity-cli", "history.jsonl")},
	{ID: "qoder", Name: "Qoder", Where: "~/.qoder/projects/*/*.jsonl", scan: scanQoder,
		files: under(".qoder", "projects", "*", "*.jsonl")},
	{ID: "grok", Name: "Grok", Where: "~/.grok/sessions/*/*/chat_history.jsonl", scan: scanGrok,
		files: under(".grok", "sessions", "*", "*", "chat_history.jsonl")},
	{ID: "opencode", Name: "opencode", Where: "~/.local/state/opencode/prompt-history.jsonl", scan: scanOpencode,
		files: func(env Env) []string { return []string{opencodeHistory(env)} }},
	{ID: "aider", Name: "Aider", Where: ".aider.input.history in each project", scan: scanAider,
		files: func(env Env) []string {
			var out []string
			for _, dir := range append([]string{env.Home}, env.Projects...) {
				out = append(out, filepath.Join(dir, ".aider.input.history"))
			}
			return out
		}},
}

// SourceName returns the name of the source of id.
func SourceName(id string) string {
	for _, s := range Sources {
		if s.ID == id {
			return s.Name
		}
	}
	if id == RecallSource {
		return "Sokki"
	}
	return id
}

// Scan reads what the sources the settings leave on added since the last
// scan into the store, and returns how many entries are new.
func (s *Store) Scan(env Env) int {
	s.scanning.Lock()
	defer s.scanning.Unlock()
	if env.Home == "" {
		env.Home, _ = os.UserHomeDir()
	}
	if env.Projects == nil {
		env.Projects = s.Projects()
	}
	st := s.Settings()
	was := s.cursors()
	var items []Item
	cursors := map[string]Cursor{}
	found, where := map[string]bool{}, map[string]string{}
	for _, src := range Sources {
		found[src.ID], where[src.ID] = src.locate(env)
		if !st.On(src.ID) || !found[src.ID] {
			continue
		}
		for k, v := range src.scan(env, was, func(it Item) {
			it.Source = src.ID
			items = append(items, it)
		}) {
			cursors[k] = v
		}
	}
	n := s.Import(items, cursors)
	s.mu.Lock()
	s.found, s.where = found, where
	s.mu.Unlock()
	if n > 0 {
		logx.Debugf("[History] %d new prompts of %d read", n, len(items))
	}
	return n
}

// tail calls line with each line added to the file at path since cur,
// that has want in it (all, for nil), and returns the cursor after the
// last whole line. A file that shrank was written anew: it is read again
// from its start.
func tail(path string, cur Cursor, want []byte, line func([]byte)) Cursor {
	fi, err := os.Stat(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			logx.Warnf("[History] %s: %v", filepath.Base(path), err)
		}
		return cur
	}
	if fi.Size() < cur.Offset {
		cur = Cursor{}
	}
	if fi.Size() == cur.Offset {
		return cur
	}
	f, err := os.Open(path)
	if err != nil {
		logx.Warnf("[History] %s: %v", filepath.Base(path), err)
		return cur
	}
	defer f.Close()
	if _, err := f.Seek(cur.Offset, io.SeekStart); err != nil {
		return cur
	}
	r := bufio.NewReaderSize(f, 64<<10)
	for {
		b, err := r.ReadBytes('\n')
		if err != nil { // a last line not yet whole waits for the next scan
			break
		}
		cur.Offset += int64(len(b))
		if want == nil || bytes.Contains(b, want) {
			line(b)
		}
	}
	return cur
}

// tailJSON is tail of a file of JSON lines, decoding each into a new T.
func tailJSON[T any](path string, cur map[string]Cursor, out map[string]Cursor, want []byte, fn func(T)) {
	out[path] = tail(path, cur[path], want, func(b []byte) {
		var v T
		if json.Unmarshal(b, &v) == nil {
			fn(v)
		}
	})
}

func unixMilli(ms int64) time.Time { return time.UnixMilli(ms) }

// claudeHistories are the patterns of Claude Code's history: in
// ~/.claude, and in the ~/.claude-* folders of its other profiles.
func claudeHistories(env Env) []string {
	return []string{
		filepath.Join(env.Home, ".claude", "history.jsonl"),
		filepath.Join(env.Home, ".claude-*", "history.jsonl"),
	}
}

// scanClaude reads Claude Code's history of each profile, putting back the
// text pasted into a prompt, which it keeps apart, in place of
// "[Pasted text #1 …]".
func scanClaude(env Env, cur map[string]Cursor, emit func(Item)) map[string]Cursor {
	out := map[string]Cursor{}
	for _, pat := range claudeHistories(env) {
		m, _ := filepath.Glob(pat)
		for _, path := range m {
			scanClaudeFile(path, cur, out, emit)
		}
	}
	return out
}

func scanClaudeFile(path string, cur, out map[string]Cursor, emit func(Item)) {
	type pasted struct {
		Type        string `json:"type"`
		Content     string `json:"content"`
		ContentHash string `json:"contentHash"`
	}
	type line struct {
		Display   string            `json:"display"`
		Pasted    map[string]pasted `json:"pastedContents"`
		Timestamp int64             `json:"timestamp"`
		Project   string            `json:"project"`
	}
	dir := filepath.Dir(path)
	tailJSON(path, cur, out, nil, func(l line) {
		text := pastedRef.ReplaceAllStringFunc(l.Display, func(ref string) string {
			p, ok := l.Pasted[pastedRef.FindStringSubmatch(ref)[1]]
			switch {
			case !ok || p.Type != "text":
				return ref
			case p.Content != "":
				return p.Content
			case p.ContentHash != "" && !strings.ContainsAny(p.ContentHash, `/\.`):
				if b, err := os.ReadFile(filepath.Join(dir, "paste-cache", p.ContentHash+".txt")); err == nil {
					return string(b)
				}
			}
			return ref
		})
		emit(Item{Text: text, Project: l.Project, Time: unixMilli(l.Timestamp)})
	})
}

var pastedRef = regexp.MustCompile(`\[Pasted text #(\d+)[^\]]*\]`)

func scanCodex(env Env, cur map[string]Cursor, emit func(Item)) map[string]Cursor {
	type line struct {
		Text string `json:"text"`
		TS   int64  `json:"ts"`
	}
	out := map[string]Cursor{}
	tailJSON(filepath.Join(env.Home, ".codex", "history.jsonl"), cur, out, nil, func(l line) {
		emit(Item{Text: l.Text, Time: time.Unix(l.TS, 0)})
	})
	return out
}

func scanAntigravity(env Env, cur map[string]Cursor, emit func(Item)) map[string]Cursor {
	type line struct {
		Display   string `json:"display"`
		Timestamp int64  `json:"timestamp"`
		Workspace string `json:"workspace"`
		Type      string `json:"type"`
	}
	out := map[string]Cursor{}
	tailJSON(filepath.Join(env.Home, ".gemini", "antigravity-cli", "history.jsonl"), cur, out, nil, func(l line) {
		if l.Type == "" || l.Type == "prompt" {
			emit(Item{Text: l.Display, Project: l.Workspace, Time: unixMilli(l.Timestamp)})
		}
	})
	return out
}

// scanGemini reads the Gemini CLI's logs of each project, a JSON array
// written anew at each message, from the record after the last read.
func scanGemini(env Env, cur map[string]Cursor, emit func(Item)) map[string]Cursor {
	type record struct {
		Type      string    `json:"type"`
		Message   string    `json:"message"`
		Timestamp time.Time `json:"timestamp"`
	}
	out := map[string]Cursor{}
	paths, _ := filepath.Glob(filepath.Join(env.Home, ".gemini", "tmp", "*", "logs.json"))
	for _, path := range paths {
		c := cur[path]
		fi, err := os.Stat(path)
		if err != nil || (fi.Size() == c.Size && fi.ModTime().Unix() == c.Mod) {
			out[path] = c
			continue
		}
		raw, err := os.ReadFile(path)
		var rs []record
		if err == nil {
			err = json.Unmarshal(raw, &rs)
		}
		if err != nil {
			logx.Warnf("[History] Gemini logs: %v", err)
			out[path] = c
			continue
		}
		if len(rs) < c.N {
			c.N = 0
		}
		for _, r := range rs[c.N:] {
			if r.Type == "user" {
				emit(Item{Text: r.Message, Time: r.Timestamp})
			}
		}
		out[path] = Cursor{N: len(rs), Size: fi.Size(), Mod: fi.ModTime().Unix()}
	}
	return out
}

// scanQoder reads the prompts that people typed out of Qoder's session
// transcripts.
func scanQoder(env Env, cur map[string]Cursor, emit func(Item)) map[string]Cursor {
	type line struct {
		Type      string    `json:"type"`
		Timestamp time.Time `json:"timestamp"`
		Cwd       string    `json:"cwd"`
		Sidechain bool      `json:"isSidechain"`
		Origin    struct {
			Kind string `json:"kind"`
		} `json:"origin"`
		Input struct {
			Text string `json:"text"`
			Mode string `json:"mode"`
		} `json:"humanInput"`
	}
	out := map[string]Cursor{}
	paths, _ := filepath.Glob(filepath.Join(env.Home, ".qoder", "projects", "*", "*.jsonl"))
	for _, path := range paths {
		tailJSON(path, cur, out, []byte(`"humanInput"`), func(l line) {
			if l.Type == "user" && !l.Sidechain && l.Origin.Kind == "human" && (l.Input.Mode == "" || l.Input.Mode == "prompt") {
				emit(Item{Text: l.Input.Text, Project: l.Cwd, Time: l.Timestamp})
			}
		})
	}
	return out
}

// userQuery is where Grok puts what was typed, among what it adds.
var userQuery = regexp.MustCompile(`(?s)<user_query>\s*(.*?)\s*</user_query>`)

// scanGrok reads the prompts of Grok's sessions: the user turns that it
// numbers, which it did not make up itself.
func scanGrok(env Env, cur map[string]Cursor, emit func(Item)) map[string]Cursor {
	type line struct {
		Type      string          `json:"type"`
		Content   json.RawMessage `json:"content"`
		Index     *int            `json:"prompt_index"`
		Synthetic string          `json:"synthetic_reason"`
	}
	out := map[string]Cursor{}
	paths, _ := filepath.Glob(filepath.Join(env.Home, ".grok", "sessions", "*", "*", "chat_history.jsonl"))
	for _, path := range paths {
		project, _ := url.PathUnescape(filepath.Base(filepath.Dir(filepath.Dir(path))))
		var at time.Time
		if fi, err := os.Stat(path); err == nil {
			at = fi.ModTime()
		}
		tailJSON(path, cur, out, []byte(`"prompt_index"`), func(l line) {
			if l.Type != "user" || l.Index == nil || l.Synthetic != "" {
				return
			}
			if m := userQuery.FindStringSubmatch(contentText(l.Content)); m != nil &&
				!strings.HasPrefix(m[1], "[") { // "[Tiller: scheduled run…]" is sent on a timer
				emit(Item{Text: m[1], Project: project, Time: at})
			}
		})
	}
	return out
}

// contentText is the text of a message's content: a string, or the text
// of its parts.
func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	json.Unmarshal(raw, &parts)
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// opencodeHistory is where opencode keeps the prompts sent.
func opencodeHistory(env Env) string {
	state := env.StateHome
	if state == "" {
		state = filepath.Join(env.Home, ".local", "state")
	}
	return filepath.Join(state, "opencode", "prompt-history.jsonl")
}

func scanOpencode(env Env, cur map[string]Cursor, emit func(Item)) map[string]Cursor {
	type line struct {
		Input string `json:"input"`
		Mode  string `json:"mode"`
	}
	path := opencodeHistory(env)
	var at time.Time
	if fi, err := os.Stat(path); err == nil {
		at = fi.ModTime()
	}
	out := map[string]Cursor{}
	tailJSON(path, cur, out, nil, func(l line) {
		if l.Mode == "" || l.Mode == "normal" {
			emit(Item{Text: l.Input, Time: at})
		}
	})
	return out
}

// scanAider reads Aider's input history, in the home folder and in each
// project prompts were sent in: blocks of "+"-prefixed lines, each under
// a "# <time>" line.
func scanAider(env Env, cur map[string]Cursor, emit func(Item)) map[string]Cursor {
	out := map[string]Cursor{}
	dirs := append([]string{env.Home}, env.Projects...)
	for _, dir := range dirs {
		path := filepath.Join(dir, ".aider.input.history")
		if _, err := os.Stat(path); err != nil {
			continue
		}
		var text strings.Builder
		var at time.Time
		flush := func() {
			if text.Len() > 0 {
				emit(Item{Text: strings.TrimSuffix(text.String(), "\n"), Project: dir, Time: at})
				text.Reset()
			}
		}
		c := tail(path, cur[path], nil, func(b []byte) {
			s := strings.TrimRight(string(b), "\r\n")
			switch {
			case strings.HasPrefix(s, "# "):
				flush()
				at, _ = time.ParseInLocation("2006-01-02 15:04:05.999999", strings.TrimPrefix(s, "# "), time.Local)
			case strings.HasPrefix(s, "+"):
				text.WriteString(s[1:])
				text.WriteString("\n")
			}
		})
		flush()
		out[path] = c
	}
	return out
}
