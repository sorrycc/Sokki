package main

import (
	"slices"
	"strings"
	"unicode/utf8"

	"sokki/internal/engine"
	"sokki/internal/model"
)

// rowMarks are where the query matches a row: its title, or else its
// trigger.
type rowMarks struct{ title, trigger []int }

type scored struct {
	m     model.Match
	score int
}

// folded is a match's texts as the search compares them, folded once per
// change of the matches rather than for every key typed in the search.
type folded struct{ trigger, label, replace []rune }

// filter ranks the enabled matches by how well the query fits their
// trigger, label (a little more) or replacement (a little less), then by
// when they were last used, then by key.
func (p *palette) filter() {
	if p.foldedGen != p.a.store.Gen || len(p.folded) != len(p.a.store.Matches) {
		p.foldedGen = p.a.store.Gen
		p.folded = p.folded[:0]
		for _, m := range p.a.store.Matches {
			p.folded = append(p.folded, folded{engine.Fold(m.Trigger), engine.Fold(m.Label), engine.Fold(head(m.Replace, 200))})
		}
	}
	q := engine.Fold(strings.TrimSpace(p.query))
	p.q = q
	clear(p.rowMarks)
	recent := p.a.recency.Snapshot()
	found := p.found[:0]
	for i, m := range p.a.store.Matches {
		if !m.Enabled {
			continue
		}
		if s, ok := matchScore(q, p.folded[i]); ok {
			found = append(found, scored{m, s})
		}
	}
	slices.SortStableFunc(found, func(a, b scored) int {
		if a.score != b.score {
			return b.score - a.score
		}
		ta, tb := recent[a.m.Key()], recent[b.m.Key()]
		if !ta.Equal(tb) {
			return tb.Compare(ta)
		}
		return strings.Compare(a.m.Key(), b.m.Key())
	})
	p.found = found
	p.results = p.results[:0]
	for _, f := range found {
		p.results = append(p.results, f.m)
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
