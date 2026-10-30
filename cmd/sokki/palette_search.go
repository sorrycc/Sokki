package main

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"sokki/internal/engine"
	"sokki/internal/model"
)

// rowMarks are where the query matches a row: its title, or else its
// trigger.
type rowMarks struct{ title, trigger []int }

type scored struct {
	i     int // of the store's matches and of the palette's folded
	score int
	used  time.Time
}

// folded is a match's texts as the search compares them, folded once per
// change of the matches rather than for every key typed in the search,
// with its key and the runes of all three in a mask.
type folded struct {
	trigger, label, replace []rune
	key                     string
	runes                   uint64
}

// runeMask has a bit for each rune of the texts, by its value modulo 64:
// a query with a rune the mask lacks matches none of them, which skips
// most matches at the first key.
func runeMask(texts ...[]rune) uint64 {
	var m uint64
	for _, t := range texts {
		for _, r := range t {
			m |= 1 << (uint(r) & 63)
		}
	}
	return m
}

// filter ranks the enabled matches by how well the query fits their
// trigger, label (a little more) or replacement (a little less), then by
// when they were last used, then by key. A query that adds to the last
// one searches only what that one found: none other can match it.
func (p *palette) filter() {
	stale := p.foldedGen != p.a.store.Gen || len(p.folded) != len(p.a.store.Matches)
	if stale {
		p.foldedGen = p.a.store.Gen
		p.folded = p.folded[:0]
		for _, m := range p.a.store.Matches {
			f := folded{trigger: engine.Fold(m.Trigger), label: engine.Fold(m.Label), replace: engine.Fold(head(m.Replace, 200)), key: m.Key()}
			f.runes = runeMask(f.trigger, f.label, f.replace)
			p.folded = append(p.folded, f)
		}
	}
	q := engine.Fold(strings.TrimSpace(p.query))
	narrow := !stale && len(p.q) > 0 && len(q) > len(p.q) && slices.Equal(q[:len(p.q)], p.q)
	p.q = q
	clear(p.rowMarks)
	recent := p.a.recency.Snapshot()
	qRunes := runeMask(q)
	found := p.spare[:0]
	try := func(i int) {
		f := &p.folded[i]
		if f.runes&qRunes != qRunes {
			return
		}
		if s, ok := matchScore(q, *f); ok {
			found = append(found, scored{i, s, recent[f.key]})
		}
	}
	if narrow {
		for _, s := range p.found {
			try(s.i)
		}
	} else {
		for i, m := range p.a.store.Matches {
			if m.Enabled {
				try(i)
			}
		}
	}
	slices.SortFunc(found, func(a, b scored) int {
		if a.score != b.score {
			return b.score - a.score
		}
		if !a.used.Equal(b.used) {
			return b.used.Compare(a.used)
		}
		return strings.Compare(p.folded[a.i].key, p.folded[b.i].key)
	})
	p.found, p.spare = found, p.found
	p.results = p.results[:0]
	for _, f := range found {
		p.results = append(p.results, p.a.store.Matches[f.i])
	}
	p.choose(0)
}

func matchScore(q []rune, f folded) (int, bool) {
	if len(q) == 0 {
		return 0, true
	}
	best, any := 0, false
	if s, ok := engine.FuzzyScoreFolded(q, f.trigger); ok {
		best, any = s, true
	}
	if len(f.label) > 0 {
		if s, ok := engine.FuzzyScoreFolded(q, f.label); ok && (!any || s+2 > best) {
			best, any = s+2, true
		}
	}
	if s, ok := engine.FuzzyScoreFolded(q, f.replace); ok && (!any || max(0, s-2) > best) {
		best, any = max(0, s-2), true
	}
	return best, any
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

func (p *palette) choose(i int) {
	p.chosen = max(0, min(i, len(p.results)-1))
	if len(p.results) > 0 {
		p.list.ScrollIntoView(p.chosen)
	}
}

// chooseID chooses the result of the match with id, if it is one.
func (p *palette) chooseID(id int64) {
	if i := slices.IndexFunc(p.results, func(m model.Match) bool { return m.ID == id }); i >= 0 {
		p.choose(i)
	}
}

func (p *palette) selected() (model.Match, bool) {
	if p.chosen < 0 || p.chosen >= len(p.results) {
		return model.Match{}, false
	}
	return p.results[p.chosen], true
}

// rowMarksOf returns where the query matches m's row, whose title is
// title, worked out once per filter.
func (p *palette) rowMarksOf(m model.Match, title string) rowMarks {
	if len(p.q) == 0 {
		return rowMarks{}
	}
	if r, ok := p.rowMarks[m.ID]; ok {
		return r
	}
	r := rowMarks{title: p.marks(title)}
	if r.title == nil && m.Trigger != "" {
		r.trigger = p.marks(m.Trigger)
	}
	if p.rowMarks == nil {
		p.rowMarks = map[int64]rowMarks{}
	}
	p.rowMarks[m.ID] = r
	return r
}

// marks returns where the query matches s, for the palette to show in
// bold, or nil. A text whose case folds to more or fewer runes has none.
func (p *palette) marks(s string) []int {
	if len(p.q) == 0 {
		return nil
	}
	f := engine.Fold(s)
	if len(f) != utf8.RuneCountInString(s) {
		return nil
	}
	return engine.FuzzyPositions(p.q, f)
}
