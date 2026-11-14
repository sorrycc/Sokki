// Package search scores how well a query fits a match or a prompt of the
// history, for the palette and the sokki command: each word of the query
// must be found in the trigger, the title or the text, in any order, an
// exact trigger or title first, a whole word or the start of one scoring
// more, and words close together more still. A single word found nowhere
// may also match a trigger or a title fuzzily, its characters in order
// and close together, ranked below every exact match. "@codex" keeps what was sent in
// an agent, "#sokki" what was sent in a project.
package search

import (
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
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

// The tiers of a fit, worst first: a doc of a better tier ranks first,
// however much less it was used.
const (
	TierFuzzy         = iota // its characters in order in the trigger or title
	TierBody                 // a word found only in the text
	TierTitle                // each word in the trigger or title, some inside a word
	TierWord                 // each word at the start of one of the trigger's or title's
	TierTitleStart           // the title is, or starts with, the query's words
	TierTriggerPrefix        // the trigger starts with a word
	TierTrigger              // the trigger is a word
)

// Fit is how well a query fits a doc: its tier, then its score within
// the tier.
type Fit struct{ Tier, Score int }

func (f Fit) better(g Fit) bool { return f.Tier > g.Tier || (f.Tier == g.Tier && f.Score > g.Score) }

// Score returns how well q fits d, and whether it does at all.
func (q Query) Score(d *Doc) (Fit, bool) {
	for _, a := range q.Agents {
		if !strings.Contains(d.agents, a) {
			return Fit{}, false
		}
	}
	for _, p := range q.Projects {
		if !strings.Contains(d.projects, p) {
			return Fit{}, false
		}
	}
	if len(q.Words) == 0 {
		return Fit{}, true
	}
	if d.mask&q.mask != q.mask {
		return Fit{}, false
	}
	fit := Fit{Tier: TierTrigger}
	for _, w := range q.Words {
		f := wordFit(w, d)
		if f.Score == 0 {
			if q.fuzzy == nil {
				return Fit{}, false
			}
			s1, _, ok1 := fuzzy(q.fuzzy, d.ftrigger)
			s2, _, ok2 := fuzzy(q.fuzzy, d.ftitle)
			if !ok1 && !ok2 {
				return Fit{}, false
			}
			return Fit{TierFuzzy, max(s1, s2)}, true
		}
		fit.Tier = min(fit.Tier, f.Tier)
		fit.Score += f.Score
	}
	phrase := strings.Join(q.Words, " ")
	if len(q.Words) > 1 {
		switch {
		case strings.Contains(d.title, phrase):
			fit.Score += 10
		case strings.Contains(d.body, phrase):
			fit.Score += 5
		default:
			fit.Score += max(near(d.title, q.Words), near(d.body, q.Words))
		}
	}
	if strings.HasPrefix(d.title, q.Words[0]) {
		fit.Score += 8
	}
	if strings.HasPrefix(d.title, phrase) && atWordEnd(d.title, len(phrase)) {
		fit.Tier = max(fit.Tier, TierTitleStart)
		if d.title == phrase {
			fit.Score += 10
		}
	}
	return fit, true
}

// near scores how close together words are found in s, apart: more
// when in the order typed, 0 when far apart or not all there.
func near(s string, words []string) int {
	first, last, size := len(s), 0, 0
	ordered, prev := true, -1
	for _, w := range words {
		i, _, _ := find(s, w)
		if i < 0 {
			return 0
		}
		ordered = ordered && i > prev
		prev = i
		first, last = min(first, i), max(last, i+len(w))
		size += len(w)
	}
	// A few short words between each two.
	if last-first > size+16*(len(words)-1) {
		return 0
	}
	if ordered {
		return 6
	}
	return 4
}

// wordFit is where w is found in d, its score 0 for nowhere.
func wordFit(w string, d *Doc) Fit {
	n := min(utf8.RuneCountInString(w), 20)
	// ":sig" is found as "sig" too.
	bare := strings.TrimLeftFunc(d.trigger, func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) })
	switch {
	case d.trigger == w || (bare != "" && bare == w):
		return Fit{TierTrigger, 40 + n}
	case d.trigger != "" && (strings.HasPrefix(d.trigger, w) || (bare != "" && strings.HasPrefix(bare, w))):
		return Fit{TierTriggerPrefix, 25 + n}
	}
	var best Fit
	if at, start, whole := find(d.trigger, w); at >= 0 {
		best = Fit{TierTitle, 12 + n}
		if start {
			best = Fit{TierWord, 14 + n}
		}
		if whole {
			best.Score += 3
		}
	}
	if at, start, whole := find(d.title, w); at >= 0 {
		f := Fit{TierTitle, 7 + n}
		if start {
			f = Fit{TierWord, 12 + n}
		}
		if whole {
			f.Score += 3
		}
		if f.better(best) {
			best = f
		}
	}
	if best.Score == 0 {
		if at, start, whole := find(d.body, w); at >= 0 {
			best = Fit{TierBody, 3 + n}
			if start {
				best.Score = 6 + n
			}
			if whole {
				best.Score += 2
			}
		}
	}
	return best
}

// find returns where w is in s, by byte, or -1, and whether at the start
// of a word there and as a whole word: of its first few, the first
// whole word, else the first at a word's start, else the first.
func find(s, w string) (at int, start, whole bool) {
	at = -1
	from := 0
	for tries := 0; tries < 8; tries++ {
		i := strings.Index(s[from:], w)
		if i < 0 {
			break
		}
		i += from
		if at < 0 {
			at = i
		}
		if atWordStart(s, i, w) {
			if atWordEnd(s, i+len(w)) {
				return i, true, true
			}
			if !start {
				at, start = i, true
			}
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		from = i + size
	}
	return at, start, false
}

// atWordStart reports whether s has a word starting at byte i: the first
// rune, one after a rune that is no letter or digit, or a character of a
// script without spaces.
func atWordStart(s string, i int, w string) bool {
	if i == 0 {
		return true
	}
	first, _ := utf8.DecodeRuneInString(w)
	if spaceless(first) {
		return true
	}
	prev, _ := utf8.DecodeLastRuneInString(s[:i])
	return !unicode.IsLetter(prev) && !unicode.IsDigit(prev)
}

// atWordEnd reports whether a word of s ends at byte j: the end, or
// before a rune that is no letter or digit, or one of a script without
// spaces.
func atWordEnd(s string, j int) bool {
	if j >= len(s) {
		return true
	}
	last, _ := utf8.DecodeLastRuneInString(s[:j])
	next, _ := utf8.DecodeRuneInString(s[j:])
	return spaceless(last) || spaceless(next) || (!unicode.IsLetter(next) && !unicode.IsDigit(next))
}

// spaceless reports whether r is of a script written without spaces.
func spaceless(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

// fuzzyGap is how many runes a compact fuzzy match skips at most between
// two of its runes, and fuzzyRunes how much of a title it searches.
const (
	fuzzyGap   = 2
	fuzzyRunes = 200
	// fuzzyStarts is how many places of its first rune it tries.
	fuzzyStarts = 4
)

// fuzzy matches the folded q's runes in order in the folded t, tightly:
// each right after the one before or at the start of a word, as "gc"
// does "git commit", or each a few runes at most after the one before,
// as "rvw" does "review". Letters strewn over a long title don't match.
// It returns the score, which runes of t matched, and whether q did.
func fuzzy(q, t []rune) (int, []int, bool) {
	if len(q) == 0 {
		return 0, nil, true
	}
	t = t[:min(len(t), fuzzyRunes)]
	best, at := 0, make([]int, len(q))
	var pos []int
	for _, compact := range []bool{false, true} {
		tries := 0
		for s := 0; s < len(t) && tries < fuzzyStarts; s++ {
			if t[s] != q[0] || (!compact && !wordStart(t, s)) {
				continue
			}
			tries++
			if score, ok := fuzzyFrom(q, t, s, compact, at); ok && score > best {
				best, pos = score, slices.Clone(at)
			}
		}
	}
	return best, pos, pos != nil
}

// fuzzyFrom matches q in t from rune s on, as fuzzy does, into at.
func fuzzyFrom(q, t []rune, s int, compact bool, at []int) (int, bool) {
	at[0] = s
	for qi := 1; qi < len(q); qi++ {
		prev, next := at[qi-1], -1
		if prev+1 < len(t) && t[prev+1] == q[qi] {
			next = prev + 1
		} else {
			end := len(t)
			if compact {
				end = min(end, prev+2+fuzzyGap)
			}
			for ti := prev + 1; ti < end; ti++ {
				if t[ti] == q[qi] && (compact || wordStart(t, ti)) {
					next = ti
					break
				}
			}
		}
		if next < 0 {
			return 0, false
		}
		at[qi] = next
	}
	score, run := 0, 0
	for i, p := range at {
		if i > 0 && p == at[i-1]+1 {
			run++
		} else {
			run = 1
		}
		score += 1 + run
		if wordStart(t, p) {
			score += 2
		}
	}
	if s == 0 {
		score += 5
	}
	return score, true
}

// wordStart reports whether a word of t starts at rune i, as atWordStart.
func wordStart(t []rune, i int) bool {
	return i == 0 || spaceless(t[i]) || (!unicode.IsLetter(t[i-1]) && !unicode.IsDigit(t[i-1]))
}

// Ranges returns the byte ranges of s where the query's words are found,
// every time each is, sorted and merged, at most limit of them: no
// fuzzy match, for a long text shown whole. A text whose case changes
// its length has none.
func (q Query) Ranges(s string, limit int) [][2]int {
	if len(q.Words) == 0 {
		return nil
	}
	f := strings.ToLower(s)
	if len(f) != len(s) {
		return nil
	}
	var out [][2]int
	for _, w := range q.Words {
		for at := 0; len(out) < limit; {
			i := strings.Index(f[at:], w)
			if i < 0 {
				break
			}
			out = append(out, [2]int{at + i, at + i + len(w)})
			at += i + len(w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	merged := out[:0]
	for _, r := range out {
		if n := len(merged); n > 0 && r[0] <= merged[n-1][1] {
			merged[n-1][1] = max(merged[n-1][1], r[1])
			continue
		}
		merged = append(merged, r)
	}
	return merged
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
		i, _, _ := find(f, w)
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
			_, pos, _ := fuzzy(q.fuzzy, []rune(f))
			return pos
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
