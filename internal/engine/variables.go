package engine

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var variablePattern = regexp.MustCompile(`\{\{(\w+)(?::([^}]*))?\}\}`)

// CursorGlyph stands for {{cursor}} in the palette's preview.
const CursorGlyph = "▎"

// Render fills a replacement's variables for injection: {{clipboard}}
// with what clipboard returns, {{date[:format]}} with now; unknown
// variables stay as they are.
func Render(template string, clipboard func() string, now time.Time) string {
	return substitute(template, func(name, params string, hasParams bool) string {
		switch name {
		case "clipboard":
			return clipboard()
		case "date":
			return FormatDate(now, params, hasParams)
		}
		return verbatim(name, params, hasParams)
	})
}

// RenderPreview fills a replacement's variables for the palette's preview:
// {{clipboard}} with a snapshot, which does not change as the user moves
// through the results, and {{cursor}} as CursorGlyph.
func RenderPreview(template, clipboardSnapshot string, now time.Time) string {
	return substitute(template, func(name, params string, hasParams bool) string {
		switch name {
		case "clipboard":
			return clipboardSnapshot
		case "cursor":
			return CursorGlyph
		case "date":
			return FormatDate(now, params, hasParams)
		}
		return verbatim(name, params, hasParams)
	})
}

func substitute(template string, resolve func(name, params string, hasParams bool) string) string {
	matches := variablePattern.FindAllStringSubmatchIndex(template, -1)
	if len(matches) == 0 {
		return template
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		b.WriteString(template[last:m[0]])
		name := template[m[2]:m[3]]
		params, has := "", m[4] >= 0
		if has {
			params = template[m[4]:m[5]]
		}
		b.WriteString(resolve(name, params, has))
		last = m[1]
	}
	b.WriteString(template[last:])
	return b.String()
}

func verbatim(name, params string, hasParams bool) string {
	if hasParams {
		return "{{" + name + ":" + params + "}}"
	}
	return "{{" + name + "}}"
}

// FormatDate formats t with a date format: Unicode (ICU) patterns, as
// yyyy-MM-dd, which Sokki has always taken, or strftime's, as %Y-%m-%d,
// when the format has a %. No format is yyyy-MM-dd.
func FormatDate(t time.Time, format string, hasFormat bool) string {
	if !hasFormat {
		format = "yyyy-MM-dd"
	}
	if strings.Contains(format, "%") {
		return strftime(t, format)
	}
	return icuFormat(t, format)
}

func strftime(t time.Time, f string) string {
	var b strings.Builder
	rs := []rune(f)
	for i := 0; i < len(rs); i++ {
		if rs[i] != '%' || i+1 >= len(rs) {
			b.WriteRune(rs[i])
			continue
		}
		i++
		switch rs[i] {
		case 'Y':
			fmt.Fprintf(&b, "%04d", t.Year())
		case 'y':
			fmt.Fprintf(&b, "%02d", t.Year()%100)
		case 'm':
			fmt.Fprintf(&b, "%02d", int(t.Month()))
		case 'd':
			fmt.Fprintf(&b, "%02d", t.Day())
		case 'e':
			fmt.Fprintf(&b, "%2d", t.Day())
		case 'H':
			fmt.Fprintf(&b, "%02d", t.Hour())
		case 'I':
			fmt.Fprintf(&b, "%02d", hour12(t))
		case 'M':
			fmt.Fprintf(&b, "%02d", t.Minute())
		case 'S':
			fmt.Fprintf(&b, "%02d", t.Second())
		case 'p':
			b.WriteString(ampm(t))
		case 'A':
			b.WriteString(t.Weekday().String())
		case 'a':
			b.WriteString(t.Weekday().String()[:3])
		case 'B':
			b.WriteString(t.Month().String())
		case 'b', 'h':
			b.WriteString(t.Month().String()[:3])
		case 'j':
			fmt.Fprintf(&b, "%03d", t.YearDay())
		case 'u':
			wd := int(t.Weekday())
			if wd == 0 {
				wd = 7
			}
			fmt.Fprintf(&b, "%d", wd)
		case 'w':
			fmt.Fprintf(&b, "%d", int(t.Weekday()))
		case 'Z':
			name, _ := t.Zone()
			b.WriteString(name)
		case 'z':
			b.WriteString(t.Format("-0700"))
		case 'F':
			b.WriteString(t.Format("2006-01-02"))
		case 'T':
			b.WriteString(t.Format("15:04:05"))
		case 'R':
			b.WriteString(t.Format("15:04"))
		case 'D':
			b.WriteString(t.Format("01/02/06"))
		case 's':
			fmt.Fprintf(&b, "%d", t.Unix())
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '%':
			b.WriteByte('%')
		default:
			b.WriteRune('%')
			b.WriteRune(rs[i])
		}
	}
	return b.String()
}

func hour12(t time.Time) int {
	h := t.Hour() % 12
	if h == 0 {
		h = 12
	}
	return h
}

func ampm(t time.Time) string {
	if t.Hour() < 12 {
		return "AM"
	}
	return "PM"
}

// icuFormat formats t with a Unicode date pattern (UTS #35), in English.
func icuFormat(t time.Time, f string) string {
	var b strings.Builder
	rs := []rune(f)
	for i := 0; i < len(rs); {
		r := rs[i]
		if r == '\'' {
			// '' is a quote; 'text' is literal.
			if i+1 < len(rs) && rs[i+1] == '\'' {
				b.WriteRune('\'')
				i += 2
				continue
			}
			j := i + 1
			for j < len(rs) {
				if rs[j] == '\'' {
					if j+1 < len(rs) && rs[j+1] == '\'' {
						b.WriteRune('\'')
						j += 2
						continue
					}
					break
				}
				b.WriteRune(rs[j])
				j++
			}
			i = j + 1
			continue
		}
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
			i++
			continue
		}
		n := 1
		for i+n < len(rs) && rs[i+n] == r {
			n++
		}
		b.WriteString(icuField(t, r, n))
		i += n
	}
	return b.String()
}

func pad(v, n int) string { return fmt.Sprintf("%0*d", n, v) }

func icuField(t time.Time, r rune, n int) string {
	switch r {
	case 'y', 'Y', 'u':
		if n == 2 {
			return pad(t.Year()%100, 2)
		}
		return pad(t.Year(), n)
	case 'M', 'L':
		switch {
		case n >= 5:
			return t.Month().String()[:1]
		case n == 4:
			return t.Month().String()
		case n == 3:
			return t.Month().String()[:3]
		}
		return pad(int(t.Month()), n)
	case 'd':
		return pad(t.Day(), n)
	case 'D':
		return pad(t.YearDay(), n)
	case 'E', 'e', 'c':
		if (r == 'e' || r == 'c') && n <= 2 {
			wd := int(t.Weekday())
			if wd == 0 {
				wd = 7
			}
			return pad(wd, n)
		}
		switch {
		case n >= 5:
			return t.Weekday().String()[:1]
		case n == 4:
			return t.Weekday().String()
		}
		return t.Weekday().String()[:3]
	case 'a':
		return ampm(t)
	case 'H':
		return pad(t.Hour(), n)
	case 'k':
		h := t.Hour()
		if h == 0 {
			h = 24
		}
		return pad(h, n)
	case 'h':
		return pad(hour12(t), n)
	case 'K':
		return pad(t.Hour()%12, n)
	case 'm':
		return pad(t.Minute(), n)
	case 's':
		return pad(t.Second(), n)
	case 'S':
		frac := fmt.Sprintf("%09d", t.Nanosecond())
		if n > 9 {
			return frac + strings.Repeat("0", n-9)
		}
		return frac[:n]
	case 'z':
		name, _ := t.Zone()
		return name
	case 'Z':
		if n == 4 {
			return "GMT" + t.Format("-07:00")
		}
		if n == 5 {
			return t.Format("-07:00")
		}
		return t.Format("-0700")
	case 'x', 'X':
		if n >= 3 {
			return t.Format("-07:00")
		}
		return t.Format("-0700")
	case 'Q', 'q':
		q := (int(t.Month())-1)/3 + 1
		if n >= 3 {
			return fmt.Sprintf("Q%d", q)
		}
		return pad(q, n)
	case 'w':
		_, w := t.ISOWeek()
		return pad(w, n)
	case 'G':
		return "AD"
	}
	return strings.Repeat(string(r), n)
}

// FuzzyScore scores how well query matches target, its characters in
// order, case-insensitively: runs score more, a match at the start more
// still. ok is false when the query's characters are not all there.
func FuzzyScore(query, target string) (int, bool) {
	if query == "" {
		return 0, true
	}
	return FuzzyScoreFolded(Fold(query), Fold(target))
}

// Fold returns s in lower case as runes, for FuzzyScoreFolded: a list
// searched again and again folds its texts once.
func Fold(s string) []rune { return []rune(strings.ToLower(s)) }

// FuzzyScoreFolded is FuzzyScore of a query and a target folded by Fold.
func FuzzyScoreFolded(q, t []rune) (int, bool) {
	if len(q) == 0 {
		return 0, true
	}
	qi, score, run, first := 0, 0, 0, -1
	for ti := 0; qi < len(q) && ti < len(t); ti++ {
		if q[qi] == t[ti] {
			if first < 0 {
				first = ti
			}
			run++
			score += 1 + run
			qi++
		} else {
			run = 0
		}
	}
	if qi != len(q) {
		return 0, false
	}
	if first == 0 {
		score += 5
	}
	return score, true
}
