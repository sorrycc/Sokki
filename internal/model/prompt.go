package model

import (
	"fmt"
	"slices"
	"strings"
)

// PromptSource is where a placeholder of an AI prompt takes its text.
type PromptSource int

const (
	SourceSelection PromptSource = iota
	SourceClipboard
)

// PromptPlaceholder is a {{…}} of a prompt, with the sources it tries in
// turn, as {{s|c}}: the selection, else the clipboard.
type PromptPlaceholder struct {
	Chain []PromptSource
	// Location is the offset, in runes, of the "{{" in the prompt, and
	// Length the length of the token, braces included.
	Location, Length int
	// Inner is the text between the braces, for HUD messages.
	Inner string
}

// PromptParseError says what is wrong with a prompt.
type PromptParseError struct {
	Kind   string // "unclosed", "empty", "emptySource", "unknown"
	At     int
	Source string
}

func (e *PromptParseError) Error() string {
	switch e.Kind {
	case "unclosed":
		return fmt.Sprintf("Unclosed `{{` at position %d", e.At)
	case "empty":
		return fmt.Sprintf("Empty `{{}}` at position %d", e.At)
	case "emptySource":
		return fmt.Sprintf("Empty source in chain at position %d", e.At)
	default:
		return fmt.Sprintf("Unknown source `%s` at position %d (expected selection/clipboard/s/c)", e.Source, e.At)
	}
}

// PromptTemplate is a parsed AI prompt.
type PromptTemplate struct {
	Raw          string
	Placeholders []PromptPlaceholder
}

// NeedsSelection reports whether any placeholder may read the selection.
func (t PromptTemplate) NeedsSelection() bool { return t.uses(SourceSelection) }

// NeedsClipboard reports whether any placeholder may read the clipboard.
func (t PromptTemplate) NeedsClipboard() bool { return t.uses(SourceClipboard) }

func (t PromptTemplate) uses(s PromptSource) bool {
	for _, p := range t.Placeholders {
		if slices.Contains(p.Chain, s) {
			return true
		}
	}
	return false
}

// RequiresSelection reports whether some placeholder is the selection
// alone, {{s}}, which nothing else can fill.
func (t PromptTemplate) RequiresSelection() bool {
	for _, p := range t.Placeholders {
		if len(p.Chain) == 1 && p.Chain[0] == SourceSelection {
			return true
		}
	}
	return false
}

// ParsePrompt parses a prompt's placeholders.
func ParsePrompt(s string) (PromptTemplate, error) {
	chars := []rune(s)
	var phs []PromptPlaceholder
	for i := 0; i < len(chars); {
		if i+1 < len(chars) && chars[i] == '{' && chars[i+1] == '{' {
			open := i
			closeAt := -1
			for j := i + 2; j+1 < len(chars); j++ {
				if chars[j] == '}' && chars[j+1] == '}' {
					closeAt = j
					break
				}
			}
			if closeAt < 0 {
				return PromptTemplate{}, &PromptParseError{Kind: "unclosed", At: open}
			}
			inner := string(chars[open+2 : closeAt])
			chain, err := parseChain(inner, open)
			if err != nil {
				return PromptTemplate{}, err
			}
			phs = append(phs, PromptPlaceholder{Chain: chain, Location: open, Length: closeAt + 2 - open, Inner: inner})
			i = closeAt + 2
		} else {
			i++
		}
	}
	return PromptTemplate{Raw: s, Placeholders: phs}, nil
}

func parseChain(inner string, at int) ([]PromptSource, error) {
	if strings.TrimSpace(inner) == "" {
		return nil, &PromptParseError{Kind: "empty", At: at}
	}
	var chain []PromptSource
	for _, part := range strings.Split(inner, "|") {
		switch name := strings.TrimSpace(part); name {
		case "":
			return nil, &PromptParseError{Kind: "emptySource", At: at}
		case "selection", "s":
			chain = append(chain, SourceSelection)
		case "clipboard", "c":
			chain = append(chain, SourceClipboard)
		default:
			return nil, &PromptParseError{Kind: "unknown", At: at, Source: name}
		}
	}
	return chain, nil
}

func pick(src PromptSource, selection, clipboard *string) *string {
	if src == SourceSelection {
		return selection
	}
	return clipboard
}

// Render fills the placeholders, in one pass: what it inserts is not
// parsed again. ok is false when a placeholder's sources are all empty.
func (t PromptTemplate) Render(selection, clipboard *string) (string, bool) {
	if len(t.Placeholders) == 0 {
		return t.Raw, true
	}
	chars := []rune(t.Raw)
	var out strings.Builder
	cursor := 0
	for _, ph := range t.Placeholders {
		if cursor < ph.Location {
			out.WriteString(string(chars[cursor:ph.Location]))
		}
		resolved := ""
		found := false
		for _, src := range ph.Chain {
			if v := pick(src, selection, clipboard); v != nil && *v != "" {
				resolved, found = *v, true
				break
			}
		}
		if !found {
			return "", false
		}
		out.WriteString(resolved)
		cursor = ph.Location + ph.Length
	}
	if cursor < len(chars) {
		out.WriteString(string(chars[cursor:]))
	}
	return out.String(), true
}

// FirstUnresolved returns the first placeholder whose sources are all
// empty, as {{inner}}, or "".
func (t PromptTemplate) FirstUnresolved(selection, clipboard *string) string {
	for _, ph := range t.Placeholders {
		filled := false
		for _, src := range ph.Chain {
			if v := pick(src, selection, clipboard); v != nil && *v != "" {
				filled = true
				break
			}
		}
		if !filled {
			return "{{" + ph.Inner + "}}"
		}
	}
	return ""
}

// Glyphs shows a prompt's placeholders as ⟨selection⟩, ⟨s|c⟩, for one-line
// subtitles; a prompt that does not parse shows as it is.
func Glyphs(prompt string) string {
	t, err := ParsePrompt(prompt)
	if err != nil {
		return prompt
	}
	chars := []rune(prompt)
	var out strings.Builder
	cursor := 0
	for _, ph := range t.Placeholders {
		if cursor < ph.Location {
			out.WriteString(string(chars[cursor:ph.Location]))
		}
		out.WriteString("⟨" + strings.TrimSpace(ph.Inner) + "⟩")
		cursor = ph.Location + ph.Length
	}
	if cursor < len(chars) {
		out.WriteString(string(chars[cursor:]))
	}
	return out.String()
}
