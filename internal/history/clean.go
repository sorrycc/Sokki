package history

import (
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// maxChars leaves out the longest prompts, which are pasted documents
// rather than tasks.
const maxChars = 20000

// secret finds what looks like a key or a token, whose prompt is left
// out: the history would keep it in the clear.
var secret = regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}|gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,}|` +
	`AKIA[0-9A-Z]{16}|xox[abposr]-[A-Za-z0-9-]{10,}|AIza[0-9A-Za-z_-]{35}|-----BEGIN [A-Z ]*PRIVATE KEY-----`)

// wrapped finds a text wholly inside tags, as tools send rather than
// people type: <command-name>…</command-name>, <hc from="…">…</hc>.
var wrapped = regexp.MustCompile(`(?s)^<[a-zA-Z][\w-]*[^>]*>.*</[a-zA-Z][\w-]*>$`)

// placeholder is what an agent shows of text pasted into it, when the
// text itself could not be found.
var placeholder = regexp.MustCompile(`\[Pasted text #\d+[^\]]*\]`)

// Clean returns text as the history keeps it, its ends trimmed, and
// whether it keeps it at all: not a slash or shell command, not shorter
// than the settings' least, not a pasted document, not with a secret, not
// sent by a tool, and with none of the settings' texts to ignore.
func Clean(text string, st Settings) (string, bool) {
	text = strings.TrimSpace(text)
	n := utf8.RuneCountInString(text)
	switch {
	case n < st.minChars() || n > maxChars:
		return "", false
	case (text[0] == '/' || text[0] == '!') && !strings.ContainsAny(text, "\n") && !strings.Contains(text[1:], "/"):
		// "/model", "/review the diff", "!open ." — but not "/Users/…".
		return "", false
	case placeholder.MatchString(text), secret.MatchString(text):
		return "", false
	}
	if wrapped.MatchString(text) {
		return "", false
	}
	lower := strings.ToLower(text)
	for _, ig := range st.Ignore {
		if ig = strings.ToLower(strings.TrimSpace(ig)); ig != "" && strings.Contains(lower, ig) {
			return "", false
		}
	}
	return text, true
}

// Frecency weighs how often and how lately something was used: the
// count of its uses times the mean weight of the latest, a use of the
// last day weighing most, as Firefox ranks its history.
func Frecency(count int, uses []int64, now time.Time) float64 {
	if count == 0 || len(uses) == 0 {
		return 0
	}
	total := 0.0
	for _, t := range uses {
		total += weight(now.Sub(time.Unix(t, 0)))
	}
	return float64(count) * total / float64(len(uses))
}

func weight(age time.Duration) float64 {
	const day = 24 * time.Hour
	switch {
	case age <= day:
		return 120
	case age <= 4*day:
		return 100
	case age <= 14*day:
		return 70
	case age <= 31*day:
		return 50
	case age <= 90*day:
		return 30
	}
	return 10
}

// Boost turns a frecency into points to add to a text score: a log, so
// that a prompt used a hundred times does not drown a better match.
func Boost(frecency float64) int {
	return int(12 * math.Log2(1+frecency/100))
}
