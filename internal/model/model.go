// Package model holds Sokki's data: matches, the configuration and the
// HUD's events, as read from and written to ~/.config/sokki.
package model

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

// Match is a trigger and what it expands to: text, or the reply of an AI
// prompt.
type Match struct {
	// ID tells matches apart within a run; it is made anew at each load.
	ID      int64
	Trigger string
	Replace string
	// Prompt makes the match an AI match, when set.
	Prompt     *string
	Label      string
	Enabled    bool
	SourceFile string
}

// IsAI reports whether the match runs an AI prompt.
func (m Match) IsAI() bool { return m.Prompt != nil }

// Title is what lists show for the match: its label, else its trigger.
func (m Match) Title() string {
	if m.Label != "" {
		return m.Label
	}
	return m.Trigger
}

var nextID atomic.Int64

// NewID returns an ID for a new match or event, unique within the run.
func NewID() int64 { return nextID.Add(1) }

// LoadIssue is a configuration problem that loading reports.
type LoadIssue struct {
	Kind    string // "legacyKey", "invalidMatch", "invalidAIArg"
	Key     string
	Hint    string
	Trigger string
	Reason  string
}

func (e *LoadIssue) Error() string {
	switch e.Kind {
	case "legacyKey":
		return fmt.Sprintf("Unsupported key `%s` — %s", e.Key, e.Hint)
	case "invalidMatch":
		return fmt.Sprintf("Invalid match `%s`: %s", e.Trigger, e.Reason)
	default:
		return fmt.Sprintf("Invalid `ai.args` entry `%s`: %s", e.Key, e.Reason)
	}
}

// mapping returns the keys and values of a YAML mapping node.
func mapping(n *yaml.Node) (map[string]*yaml.Node, error) {
	for n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		n = n.Content[0]
	}
	if n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: expected a mapping", n.Line)
	}
	m := map[string]*yaml.Node{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		m[n.Content[i].Value] = n.Content[i+1]
	}
	return m, nil
}

// lenient decodes v from the node at key, leaving v as it is when the key
// is missing or does not decode, as Sokki's Swift loader did.
func lenient[T any](m map[string]*yaml.Node, key string, v *T) {
	n, ok := m[key]
	if !ok {
		return
	}
	var t T
	if n.Decode(&t) == nil && !(n.Kind == yaml.ScalarNode && n.Tag == "!!null") {
		*v = t
	}
}

// strict decodes v from the node at key when present, failing when it
// does not decode.
func strict[T any](m map[string]*yaml.Node, key string, v **T) error {
	n, ok := m[key]
	if !ok || (n.Kind == yaml.ScalarNode && n.Tag == "!!null") {
		return nil
	}
	var t T
	if err := n.Decode(&t); err != nil {
		return fmt.Errorf("`%s`: %w", key, err)
	}
	*v = &t
	return nil
}

// UnmarshalYAML decodes a match of a match file.
func (m *Match) UnmarshalYAML(n *yaml.Node) error {
	fields, err := mapping(n)
	if err != nil {
		return err
	}
	if _, ok := fields["ai"]; ok {
		return &LoadIssue{Kind: "legacyKey", Key: "ai",
			Hint: "replace with top-level `prompt:` containing {{selection}} / {{clipboard}} placeholders"}
	}
	t, ok := fields["trigger"]
	if !ok {
		return fmt.Errorf("line %d: a match has no `trigger`", n.Line)
	}
	var trigger string
	if err := t.Decode(&trigger); err != nil {
		return fmt.Errorf("`trigger`: %w", err)
	}
	*m = Match{ID: NewID(), Trigger: trigger, Enabled: true}
	if err := strict(fields, "prompt", &m.Prompt); err != nil {
		return err
	}
	lenient(fields, "replace", &m.Replace)
	var label *string
	if err := strict(fields, "label", &label); err != nil {
		return err
	}
	if label != nil {
		m.Label = *label
	}
	lenient(fields, "enabled", &m.Enabled)
	if m.Prompt != nil {
		if *m.Prompt == "" {
			return &LoadIssue{Kind: "invalidMatch", Trigger: m.Trigger, Reason: "`prompt:` is empty"}
		}
		if m.Replace != "" {
			return &LoadIssue{Kind: "invalidMatch", Trigger: m.Trigger, Reason: "`prompt:` and `replace:` cannot both be set"}
		}
		if _, err := ParsePrompt(*m.Prompt); err != nil {
			return err
		}
	}
	return nil
}

// MarshalYAML writes the match as a match file holds it.
func (m Match) MarshalYAML() (any, error) {
	n := &yaml.Node{Kind: yaml.MappingNode}
	add := func(key, value string) {
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, stringNode(value))
	}
	add("trigger", m.Trigger)
	if m.Prompt != nil {
		add("prompt", *m.Prompt)
	} else {
		add("replace", m.Replace)
	}
	if m.Label != "" {
		add("label", m.Label)
	}
	if !m.Enabled {
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "enabled"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"})
	}
	return n, nil
}

// stringNode is a string scalar, as a literal block when it has several
// lines.
func stringNode(s string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	if strings.Contains(s, "\n") {
		n.Style = yaml.LiteralStyle
	}
	return n
}

// MatchFile is a file of match/.
type MatchFile struct {
	Matches []Match `yaml:"matches"`
}

// PlaceholderStyle is how the AI placeholder shows while a prompt runs.
type PlaceholderStyle string

const (
	PlaceholderStatic     PlaceholderStyle = "static"
	PlaceholderBraille    PlaceholderStyle = "braille"
	PlaceholderBlockSweep PlaceholderStyle = "block_sweep"
)

// AISettings are the ai: table of the configuration.
type AISettings struct {
	Binary                    *string          `yaml:"binary,omitempty"`
	Args                      []string         `yaml:"args,omitempty"`
	Model                     string           `yaml:"model"`
	TimeoutSeconds            int              `yaml:"timeout_seconds"`
	SelectionCaptureTimeoutMs int              `yaml:"selection_capture_timeout_ms"`
	PlaceholderStyle          PlaceholderStyle `yaml:"placeholder_style"`
}

// DefaultAISettings are the AI settings of a new configuration.
func DefaultAISettings() AISettings {
	return AISettings{Model: "sonnet", TimeoutSeconds: 30, SelectionCaptureTimeoutMs: 400, PlaceholderStyle: PlaceholderStatic}
}

// UnmarshalYAML decodes the ai: table.
func (a *AISettings) UnmarshalYAML(n *yaml.Node) error {
	fields, err := mapping(n)
	if err != nil {
		return err
	}
	*a = DefaultAISettings()
	if err := strict(fields, "binary", &a.Binary); err != nil {
		return err
	}
	var args *[]string
	if err := strict(fields, "args", &args); err != nil {
		return err
	}
	if args != nil {
		if err := ValidateArgs(*args); err != nil {
			return err
		}
		a.Args = *args
		if a.Args == nil {
			a.Args = []string{}
		}
	}
	lenient(fields, "model", &a.Model)
	lenient(fields, "timeout_seconds", &a.TimeoutSeconds)
	lenient(fields, "selection_capture_timeout_ms", &a.SelectionCaptureTimeoutMs)
	var style string
	lenient(fields, "placeholder_style", &style)
	switch PlaceholderStyle(style) {
	case PlaceholderStatic, PlaceholderBraille, PlaceholderBlockSweep:
		a.PlaceholderStyle = PlaceholderStyle(style)
	}
	return nil
}

// ValidateArgs rejects flags of the --flag=value form, which Sokki cannot
// canonicalize; others are left to the Claude CLI.
func ValidateArgs(args []string) error {
	for _, a := range args {
		if strings.HasPrefix(a, "--") && strings.Contains(a, "=") {
			return &LoadIssue{Kind: "invalidAIArg", Key: a,
				Reason: "`=` form not supported; use separated form, e.g. `--output-format`, `json`"}
		}
	}
	return nil
}

// Config is config/default.yml.
type Config struct {
	Enabled                bool       `yaml:"enable"`
	ClipboardThreshold     int        `yaml:"clipboard_threshold"`
	InjectDelay            int        `yaml:"inject_delay"`
	PasteShortcut          string     `yaml:"paste_shortcut"`
	PreserveClipboard      bool       `yaml:"preserve_clipboard"`
	RestoreClipboardDelay  int        `yaml:"restore_clipboard_delay"`
	LogLevel               string     `yaml:"log_level"`
	ToggleShortcut         string     `yaml:"toggle_shortcut"`
	HUDShortcut            string     `yaml:"hud_shortcut"`
	PaletteShortcut        string     `yaml:"palette_shortcut"`
	ConflictResolveDelayMs int        `yaml:"conflict_resolve_delay_ms"`
	AI                     AISettings `yaml:"ai"`
}

// DefaultConfig is the configuration without a config file.
func DefaultConfig() Config {
	return Config{
		Enabled: true, ClipboardThreshold: 50, PasteShortcut: "Cmd+V", PreserveClipboard: true,
		RestoreClipboardDelay: 100, LogLevel: "debug", ToggleShortcut: "none", HUDShortcut: "none",
		PaletteShortcut: "none", ConflictResolveDelayMs: 400, AI: DefaultAISettings(),
	}
}

// legacyAIKeys are the flat ai_* keys that moved under ai:.
var legacyAIKeys = []string{"ai_binary_path", "ai_model", "ai_timeout_seconds", "ai_selection_capture_timeout_ms"}

// UnmarshalYAML decodes the configuration, leniently: a key missing or of
// the wrong type keeps its default.
func (c *Config) UnmarshalYAML(n *yaml.Node) error {
	fields, err := mapping(n)
	if err != nil {
		return err
	}
	for _, k := range legacyAIKeys {
		if _, ok := fields[k]; ok {
			return &LoadIssue{Kind: "legacyKey", Key: k, Hint: "move under the `ai:` table (e.g. `ai.binary`, `ai.model`)"}
		}
	}
	*c = DefaultConfig()
	lenient(fields, "enable", &c.Enabled)
	lenient(fields, "clipboard_threshold", &c.ClipboardThreshold)
	lenient(fields, "inject_delay", &c.InjectDelay)
	lenient(fields, "paste_shortcut", &c.PasteShortcut)
	lenient(fields, "preserve_clipboard", &c.PreserveClipboard)
	lenient(fields, "restore_clipboard_delay", &c.RestoreClipboardDelay)
	lenient(fields, "log_level", &c.LogLevel)
	lenient(fields, "toggle_shortcut", &c.ToggleShortcut)
	lenient(fields, "hud_shortcut", &c.HUDShortcut)
	lenient(fields, "palette_shortcut", &c.PaletteShortcut)
	lenient(fields, "conflict_resolve_delay_ms", &c.ConflictResolveDelayMs)
	if ai, ok := fields["ai"]; ok && !(ai.Kind == yaml.ScalarNode && ai.Tag == "!!null") {
		if err := ai.Decode(&c.AI); err != nil {
			return err
		}
	}
	return nil
}

// Verdict is what happened to what was typed, in the HUD.
type Verdict string

const (
	VerdictMatched     Verdict = "matched"
	VerdictTooSoon     Verdict = "tooSoon"
	VerdictDisabled    Verdict = "disabled"
	VerdictAIThinking  Verdict = "aiThinking"
	VerdictAICompleted Verdict = "aiCompleted"
	VerdictAIFailed    Verdict = "aiFailed"
)

// HUDEvent is a row of the Recent Activity HUD.
type HUDEvent struct {
	ID             int64
	Time           time.Time
	Verdict        Verdict
	Buffer         string
	MatchedTrigger string
	Note           string
}
