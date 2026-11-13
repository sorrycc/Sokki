package main

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"sokki/internal/history"
	"sokki/internal/model"
	"sokki/internal/search"
)

// result is a row of the palette: a match, or a prompt of the agents'
// history.
type result struct {
	m     model.Match
	e     history.Entry
	entry bool
}

func (r result) id() int64 {
	if r.entry {
		return r.e.ID
	}
	return r.m.ID
}

func (r result) title() string {
	if r.entry {
		return r.e.Title()
	}
	return matchTitle(r.m)
}

// text is what it inserts, its variables not yet filled in.
func (r result) text() string {
	if r.entry {
		return r.e.Text
	}
	return r.m.Replace
}

// The palette's scopes.
const (
	scopeAll = iota
	scopeMatches
	scopeHistory
)

var scopeLabels = []string{"All", "Matches", "History"}

// rowMarks are where the query matches a row: its title, or else its
// trigger.
type rowMarks struct{ title, trigger []int }

type scored struct {
	i    int // of the palette's docs
	rank int
	// frecency ranks what an empty query lists; last breaks ties.
	frecency float64
	last     int64
}

// index is the matches and the history's entries as the search compares
// them, made again only when either changes: the matches first, then the
// entries.
type index struct {
	matchGen, historyGen uint64
	docs                 []search.Doc
	matches              []model.Match
	entries              []history.Entry
	// matchDocs are the docs of the matches, and matchKeys their texts
	// normalized, made again only as the matches change.
	matchDocs []search.Doc
	matchKeys []string
	// entryDocs are the docs of the entries by ID, kept from one making
	// to the next: an entry's text never changes, and lowering every
	// long prompt again at each recall or scan would hold up the keys.
	entryDocs map[int64]search.Doc
	// frecency and last are each doc's, as of at: worked out as the docs
	// are made, and again once a minute, not at every key.
	frecency []float64
	last     []int64
	at       time.Time
}

// frecencyFor is how long the frecency the index worked out stays: its
// weights change by days.
const frecencyFor = time.Minute

func (x *index) result(i int) result {
	if i < len(x.matches) {
		return result{m: x.matches[i]}
	}
	return result{e: x.entries[i-len(x.matches)], entry: true}
}

// update makes the docs again after a change of the matches or the
// entries, and reports whether it did.
func (x *index) update(a *app) bool {
	hgen := a.history.Gen()
	if x.docs != nil && x.matchGen == a.store.Gen && x.historyGen == hgen && len(x.matches) == len(a.store.Matches) {
		return false
	}
	if x.matchDocs == nil || x.matchGen != a.store.Gen || len(x.matches) != len(a.store.Matches) {
		x.matches = a.store.Matches
		x.matchDocs, x.matchKeys = make([]search.Doc, len(x.matches)), make([]string, len(x.matches))
		for i, m := range x.matches {
			x.matchDocs[i] = search.NewDoc(m.Trigger, m.Label, m.Replace, []string{"sokki", m.SourceFile}, nil)
			x.matchKeys[i] = history.Normalize(m.Replace)
		}
	}
	x.matchGen, x.historyGen = a.store.Gen, hgen
	x.docs = append(x.docs[:0], x.matchDocs...)
	saved := make(map[int64]bool)
	for _, key := range x.matchKeys {
		if id, ok := a.history.Find(key); ok {
			saved[id] = true
		}
	}
	// A prompt saved as a match shows once, as the match.
	x.entries = slices.DeleteFunc(slices.Clone(a.history.Entries()), func(e history.Entry) bool { return saved[e.ID] })
	was := x.entryDocs
	x.entryDocs = make(map[int64]search.Doc, len(x.entries))
	for _, e := range x.entries {
		agents := slices.Clone(e.Sources)
		for _, s := range e.Sources {
			agents = append(agents, history.SourceName(s))
		}
		d, ok := was[e.ID]
		if ok {
			d = d.With(agents, e.Projects)
		} else {
			d = search.NewDoc("", e.Title(), e.Text, agents, e.Projects)
		}
		x.entryDocs[e.ID] = d
		x.docs = append(x.docs, d)
	}
	x.weigh(a.history.MatchUses(), time.Now())
	return true
}

// weigh works out the frecency of each doc as of now.
func (x *index) weigh(uses map[string]history.MatchUse, now time.Time) {
	x.at = now
	x.frecency, x.last = x.frecency[:0], x.last[:0]
	for i := range x.docs {
		var count int
		var times []int64
		if i < len(x.matches) {
			u := uses[x.matches[i].Key()]
			count, times = u.Count, u.Times
		} else {
			e := &x.entries[i-len(x.matches)]
			count, times = e.Count, e.UseTimes()
		}
		var last int64
		if len(times) > 0 {
			last = times[len(times)-1]
		}
		x.frecency = append(x.frecency, history.Frecency(count, times, now))
		x.last = append(x.last, last)
	}
}

// rank ranks the docs of x for q among those of scope, each of try's
// indexes (all, for nil), into found: by how well they fit q plus how
// often and lately they were used, or, for an empty query, by the
// latter alone.
func (x *index) rank(q search.Query, scope int, try []scored, found []scored) []scored {
	add := func(i int) {
		isEntry := i >= len(x.matches)
		if (scope == scopeMatches && isEntry) || (scope == scopeHistory && !isEntry) {
			return
		}
		if !isEntry && !x.matches[i].Enabled {
			return
		}
		s, ok := q.Score(&x.docs[i])
		if !ok {
			return
		}
		f := x.frecency[i]
		found = append(found, scored{i: i, rank: 4*s + history.Boost(f), frecency: f, last: x.last[i]})
	}
	if try != nil {
		for _, s := range try {
			add(s.i)
		}
	} else {
		for i := range x.docs {
			add(i)
		}
	}
	empty := len(q.Words) == 0
	slices.SortFunc(found, func(a, b scored) int {
		if !empty && a.rank != b.rank {
			return b.rank - a.rank
		}
		if a.frecency != b.frecency {
			return cmp.Compare(b.frecency, a.frecency)
		}
		if a.last != b.last {
			return cmp.Compare(b.last, a.last)
		}
		return cmp.Compare(a.i, b.i)
	})
	return found
}

// filter ranks the enabled matches and the history for the query. A
// query that adds to the last one searches only what that one found:
// none other can match it.
func (p *palette) filter() {
	stale := p.index.update(p.a)
	if now := time.Now(); now.Sub(p.index.at) > frecencyFor {
		p.index.weigh(p.a.history.MatchUses(), now)
		stale = true
	}
	q := search.Parse(p.query)
	narrow := !stale && p.scope == p.qScope && q.Narrows(p.q)
	p.q, p.qScope = q, p.scope
	clear(p.rowMarks)
	var try []scored
	if narrow {
		try = p.found
	}
	found := p.index.rank(q, p.scope, try, p.spare[:0])
	p.found, p.spare = found, p.found
	p.results = p.results[:0]
	for _, f := range found {
		p.results = append(p.results, p.index.result(f.i))
	}
	p.choose(0)
}

func (p *palette) choose(i int) {
	p.chosen = max(0, min(i, len(p.results)-1))
	if len(p.results) > 0 {
		p.list.ScrollIntoView(p.chosen)
	}
}

// chooseID chooses the result of the match or entry with id, if it is
// one.
func (p *palette) chooseID(id int64) {
	if i := slices.IndexFunc(p.results, func(r result) bool { return r.id() == id }); i >= 0 {
		p.choose(i)
	}
}

func (p *palette) selected() (result, bool) {
	if p.chosen < 0 || p.chosen >= len(p.results) {
		return result{}, false
	}
	return p.results[p.chosen], true
}

// rowMarksOf returns where the query matches r's row, whose title is
// title, worked out once per filter.
func (p *palette) rowMarksOf(r result, title string) rowMarks {
	if len(p.q.Words) == 0 {
		return rowMarks{}
	}
	if m, ok := p.rowMarks[r.id()]; ok {
		return m
	}
	m := rowMarks{title: p.q.Marks(title)}
	if m.title == nil && !r.entry && r.m.Trigger != "" {
		m.trigger = p.q.Marks(r.m.Trigger)
	}
	if p.rowMarks == nil {
		p.rowMarks = map[int64]rowMarks{}
	}
	p.rowMarks[r.id()] = m
	return m
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

// searchHelp is the placeholder of the search, saying what it finds.
func searchHelp(scope int) string {
	switch scope {
	case scopeMatches:
		return "Search matches and prompts"
	case scopeHistory:
		return "Search what you sent to agents — @codex, #project"
	}
	return "Search matches, prompts and agent history"
}

// agentNames names the agents an entry was sent in.
func agentNames(e history.Entry) string {
	names := make([]string, len(e.Sources))
	for i, s := range e.Sources {
		names[i] = history.SourceName(s)
	}
	return strings.Join(names, ", ")
}
