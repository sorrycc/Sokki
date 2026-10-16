// Package engine decides, keystroke by keystroke, when a trigger was typed:
// a trie of the triggers, a buffer of what was typed, undo by backspace,
// and the state machine around them that the event tap feeds.
package engine

import (
	"fmt"
	"slices"

	"sokki/internal/model"
)

type trieNode struct {
	children       map[rune]*trieNode
	match          *model.Match
	hasDescendants bool
}

type cursor struct {
	node  *trieNode
	depth int
}

// MatchResult is a trigger that fired, and how many characters it typed.
type MatchResult struct {
	Match         model.Match
	TriggerLength int
}

// Matcher follows every trigger the typing so far may be in, and fires
// the longest one once no longer trigger can follow.
type Matcher struct {
	root    *trieNode
	cursors []cursor
	pending *MatchResult
}

// NewMatcher builds the trie of the enabled matches.
func NewMatcher(matches []model.Match) *Matcher {
	var enabled []model.Match
	for _, m := range matches {
		if m.Enabled {
			enabled = append(enabled, m)
		}
	}
	return &Matcher{root: buildNode(enabled, 0)}
}

func buildNode(matches []model.Match, depth int) *trieNode {
	byRune := map[rune][]model.Match{}
	n := &trieNode{children: map[rune]*trieNode{}}
	for _, m := range matches {
		chars := []rune(m.Trigger)
		if depth == len(chars) {
			mm := m
			n.match = &mm
		} else if depth < len(chars) {
			byRune[chars[depth]] = append(byRune[chars[depth]], m)
		}
	}
	for r, ms := range byRune {
		n.children[r] = buildNode(ms, depth+1)
		n.hasDescendants = true
	}
	return n
}

// Pending returns the match held while a longer trigger may still follow.
func (m *Matcher) Pending() *MatchResult { return m.pending }

// Feed advances the matcher by a character, and returns the match that
// fires on it, if any.
func (m *Matcher) Feed(ch rune) *MatchResult {
	var next []cursor
	var longest *MatchResult
	potential := false
	step := func(n *trieNode, depth int) {
		next = append(next, cursor{n, depth})
		if n.match != nil && (longest == nil || depth > longest.TriggerLength) {
			longest = &MatchResult{Match: *n.match, TriggerLength: depth}
		}
		if n.hasDescendants {
			potential = true
		}
	}
	for _, c := range m.cursors {
		if n, ok := c.node.children[ch]; ok {
			step(n, c.depth+1)
		}
	}
	if n, ok := m.root.children[ch]; ok {
		step(n, 1)
	}
	m.cursors = next

	// A cursor past the pending trigger means the user is typing a longer
	// trigger: the shorter one is no longer meant. This runs before a new
	// match is held, so a chain (;f, then ;ff) holds the new one cleanly.
	if m.pending != nil && slices.ContainsFunc(m.cursors, func(c cursor) bool { return c.depth > m.pending.TriggerLength }) {
		m.pending = nil
	}

	if longest != nil {
		if potential {
			m.pending = longest
			return nil
		}
		m.pending = nil
		m.cursors = nil
		return longest
	}
	if !potential && m.pending != nil {
		p := m.pending
		m.pending = nil
		m.cursors = nil
		return p
	}
	return nil
}

// Reset forgets the cursors and the pending match.
func (m *Matcher) Reset() {
	m.cursors = nil
	m.pending = nil
}

// FlushPending fires the pending match, if any.
func (m *Matcher) FlushPending() *MatchResult {
	p := m.pending
	if p != nil {
		m.pending = nil
		m.cursors = nil
	}
	return p
}

// DebugSummary describes the matcher's state for the trace log: the
// depths of its cursors and the pending trigger.
func (m *Matcher) DebugSummary() string {
	depths := make([]int, len(m.cursors))
	for i, c := range m.cursors {
		depths[i] = c.depth
	}
	slices.Sort(depths)
	pending := "-"
	if m.pending != nil {
		pending = m.pending.Match.Trigger
	}
	return fmt.Sprintf("cursors=%v pending=%s", depths, pending)
}
