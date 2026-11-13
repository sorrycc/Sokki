// Package search scores how well a query fits a match or a prompt of the
// history, for the palette and the sokki command: each word of the query
// must be found in the trigger, the title or the text, at the start of a
// word scoring more, and a single word may also match a trigger or a
// title fuzzily, its characters in order. "@codex" keeps what was sent in
// an agent, "#sokki" what was sent in a project.
package search

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"sokki/internal/engine"
)

// Query is a parsed query.
type Query struct {
	// Words are its words, in lower case, without its filters.
	Words []string
	// Agents and Projects are its filters, without @ and #.
	Agents, Projects []string
	// fuzzy is its one word as runes, when it has one.
	fuzzy []rune
	mask  uint64
}

// Parse parses what was typed in a search.
func Parse(s string) Query {
	var q Query
	for _, w := range strings.Fields(strings.ToLower(s)) {
		switch {
		case len(w) > 1 && w[0] == '@':
			q.Agents = append(q.Agents, w[1:])
		case len(w) > 1 && w[0] == '#':
			q.Projects = append(q.Projects, w[1:])
		default:
			q.Words = append(q.Words, w)
		}
	}
	if len(q.Words) == 1 {
		q.fuzzy = []rune(q.Words[0])
	}
	for _, w := range q.Words {
		q.mask |= Mask(w)
	}
	return q
}

// Empty reports whether the query has neither words nor filters.
func (q Query) Empty() bool { return len(q.Words) == 0 && len(q.Agents) == 0 && len(q.Projects) == 0 }

// Narrows reports whether whatever fits q also fits was, so that what
// was found for was is all q needs to search: q adds to was.
func (q Query) Narrows(was Query) bool {
	return !was.Empty() && len(q.Words) >= len(was.Words) && containsEach(q.Words, was.Words, true) &&
		containsEach(q.Agents, was.Agents, false) && containsEach(q.Projects, was.Projects, false)
}

// containsEach reports whether each of was is in q at its place, as it is
// or, for the last of words, typed further.
func containsEach(q, was []string, words bool) bool {
	if words {
		for i, w := range was {
			if q[i] != w && !(i == len(was)-1 && strings.HasPrefix(q[i], w)) {
				return false
			}
		}
		return true
	}
	return len(q) == len(was) && func() bool {
		for i := range q {
			if !strings.HasPrefix(q[i], was[i]) {
				return false
			}
		}
		return true
	}()
}

// Doc is a match or a prompt as the search compares it, made once per
// change of it rather than for every key typed.
type Doc struct {
	trigger, title, body string
	agents, projects     string
	ftrigger, ftitle     []rune
	mask                 uint64
}

// bodyChars is how much of a text is searched.
const bodyChars = 4000

// NewDoc makes the Doc of a match or a prompt: its trigger, title and
// text, and the agents and projects it was sent in.
func NewDoc(trigger, title, body string, agents, projects []string) Doc {
	d := Doc{
		trigger: strings.ToLower(trigger), title: strings.ToLower(title),
		body: strings.ToLower(head(body, bodyChars)),
	}
	d.ftrigger, d.ftitle = []rune(d.trigger), []rune(d.title)
	d.mask = Mask(d.trigger) | Mask(d.title) | Mask(d.body)
	return d.With(agents, projects)
}

// With returns d sent in agents and projects instead: a prompt sent
// again changes them, not its text, which is what costs to make.
func (d Doc) With(agents, projects []string) Doc {
	d.agents, d.projects = strings.ToLower(strings.Join(agents, " ")), strings.ToLower(strings.Join(projects, " "))
	return d
}

// Mask has a bit for each rune of s, by its value modulo 64: a query with
// a rune a doc's mask lacks matches none of it, which skips most docs at
// the first key.
func Mask(s string) uint64 {
	var m uint64
	for _, r := range s {
		m |= 1 << (uint(r) & 63)
	}
	return m
}

// Score returns how well q fits d, and whether it does at all.
func (q Query) Score(d *Doc) (int, bool) {
	for _, a := range q.Agents {
		if !strings.Contains(d.agents, a) {
			return 0, false
		}
	}
	for _, p := range q.Projects {
		if !strings.Contains(d.projects, p) {
			return 0, false
		}
	}
	if len(q.Words) == 0 {
		return 0, true
	}
	if d.mask&q.mask != q.mask {
		return 0, false
	}
	total := 0
	for _, w := range q.Words {
		s := wordScore(w, d)
		if s == 0 {
			if q.fuzzy == nil {
				return 0, false
			}
			s1, ok1 := engine.FuzzyScoreFolded(q.fuzzy, d.ftrigger)
			s2, ok2 := engine.FuzzyScoreFolded(q.fuzzy, d.ftitle)
			if !ok1 && !ok2 {
				return 0, false
			}
			return max(s1, s2), true
		}
		total += s
	}
	if len(q.Words) > 1 {
		phrase := strings.Join(q.Words, " ")
		switch {
		case strings.Contains(d.title, phrase):
			total += 10
		case strings.Contains(d.body, phrase):
			total += 5
		}
	}
	if strings.HasPrefix(d.title, q.Words[0]) {
		total += 8
	}
	return total, true
}

// wordScore scores where w is found in d, 0 for nowhere.
func wordScore(w string, d *Doc) int {
	n := min(utf8.RuneCountInString(w), 20)
	// ":sig" is found as "sig" too.
	bare := strings.TrimLeftFunc(d.trigger, func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) })
	switch {
	case d.trigger == w || (bare != "" && bare == w):
		return 40 + n
	case d.trigger != "" && (strings.HasPrefix(d.trigger, w) || (bare != "" && strings.HasPrefix(bare, w))):
		return 25 + n
	}
	best := 0
	if found, start := find(d.trigger, w); found {
		best = 12 + n
		if start {
			best += 2
		}
	}
	if found, start := find(d.title, w); found {
		s := 7 + n
		if start {
			s = 12 + n
		}
		best = max(best, s)
	}
	if best == 0 {
		if found, start := find(d.body, w); found {
			best = 3 + n
			if start {
				best = 6 + n
			}
		}
	}
	return best
}

// find reports whether w is in s, and whether at the start of a word
// there.
func find(s, w string) (found, start bool) {
	from := 0
	for tries := 0; tries < 8; tries++ {
		i := strings.Index(s[from:], w)
		if i < 0 {
			return found, false
		}
		i += from
		found = true
		if atWordStart(s, i, w) {
			return true, true
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		from = i + size
	}
	return found, false
}

// atWordStart reports whether s has a word starting at byte i: the first
// rune, one after a rune that is no letter or digit, or a character of a
// script without spaces.
func atWordStart(s string, i int, w string) bool {
	if i == 0 {
		return true
	}
	first, _ := utf8.DecodeRuneInString(w)
	if unicode.In(first, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
		return true
	}
	prev, _ := utf8.DecodeLastRuneInString(s[:i])
	return !unicode.IsLetter(prev) && !unicode.IsDigit(prev)
}

// Marks returns where q matches s, as rune indexes in order, for the
// palette to show in bold, or nil. A text whose case folds to more or
// fewer runes has none.
func (q Query) Marks(s string) []int {
	if len(q.Words) == 0 {
		return nil
	}
	f := strings.ToLower(s)
	if utf8.RuneCountInString(f) != utf8.RuneCountInString(s) {
		return nil
	}
	seen := map[int]bool{}
	for _, w := range q.Words {
		i := strings.Index(f, w)
		if i < 0 {
			continue
		}
		at := utf8.RuneCountInString(f[:i])
		for k := range utf8.RuneCountInString(w) {
			seen[at+k] = true
		}
	}
	if len(seen) == 0 {
		if q.fuzzy != nil {
			return engine.FuzzyPositions(q.fuzzy, []rune(f))
		}
		return nil
	}
	out := make([]int, 0, len(seen))
	for i := range seen {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

// head returns the first n characters of s, without copying it.
func head(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}
