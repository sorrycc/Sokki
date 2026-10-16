package model

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func ptr(s string) *string { return &s }

func mustParse(t *testing.T, s string) PromptTemplate {
	t.Helper()
	p, err := ParsePrompt(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return p
}

func TestParsePrompt(t *testing.T) {
	if p := mustParse(t, "Tell me a joke."); len(p.Placeholders) != 0 || p.NeedsSelection() || p.NeedsClipboard() {
		t.Fatal("expected no placeholders")
	}
	if p := mustParse(t, ""); len(p.Placeholders) != 0 {
		t.Fatal("expected no placeholders")
	}
	p := mustParse(t, "Translate: {{selection}}")
	if len(p.Placeholders) != 1 || !slices.Equal(p.Placeholders[0].Chain, []PromptSource{SourceSelection}) || !p.NeedsSelection() || p.NeedsClipboard() {
		t.Fatalf("selection: %+v", p)
	}
	p = mustParse(t, "{{s}} and {{c}}")
	if p.Placeholders[0].Chain[0] != SourceSelection || p.Placeholders[1].Chain[0] != SourceClipboard {
		t.Fatal("aliases")
	}
	both := []PromptSource{SourceSelection, SourceClipboard}
	for _, s := range []string{"{{s|c}}", "{{ s | c }}", "{{selection|clipboard}}"} {
		if !slices.Equal(mustParse(t, s).Placeholders[0].Chain, both) {
			t.Fatalf("chain of %q", s)
		}
	}
	if !slices.Equal(mustParse(t, "{{ selection }}").Placeholders[0].Chain, []PromptSource{SourceSelection}) {
		t.Fatal("whitespace")
	}
	p = mustParse(t, "Before {{s}} middle {{c}} after")
	if len(p.Placeholders) != 2 || !p.NeedsSelection() || !p.NeedsClipboard() {
		t.Fatal("multiple")
	}
	if !mustParse(t, "{{s}}").RequiresSelection() || mustParse(t, "{{s|c}}").RequiresSelection() {
		t.Fatal("requiresSelection")
	}
}

func TestParsePromptErrors(t *testing.T) {
	for _, s := range []string{"{{foo}}", "{{}}", "{{|s}}", "{{s|}}", "{{s||c}}", "{{selection"} {
		var pe *PromptParseError
		if _, err := ParsePrompt(s); !errors.As(err, &pe) {
			t.Errorf("%q: expected a parse error, got %v", s, err)
		}
	}
}

func TestRender(t *testing.T) {
	cases := []struct {
		prompt    string
		sel, clip *string
		want      string
		wantOK    bool
	}{
		{"No placeholders here.", nil, nil, "No placeholders here.", true},
		{"Translate: {{selection}}", ptr("hello"), nil, "Translate: hello", true},
		{"{{s|c}}", ptr(""), ptr("fallback"), "fallback", true},
		{"{{s|c}}", nil, ptr("fallback"), "fallback", true},
		{"{{s|c}}", ptr("first"), ptr("second"), "first", true},
		{"{{s|c}}", ptr(""), ptr(""), "", false},
		{"{{s|c}}", nil, nil, "", false},
		{"A={{s}} B={{c}}", ptr("one"), ptr("two"), "A=one B=two", true},
		{"{{s}}", ptr("{{clipboard}}"), ptr("recursive"), "{{clipboard}}", true},
		{"Before {{s}} After", ptr("MID"), nil, "Before MID After", true},
		{"ü {{s}} ✓", ptr("x"), nil, "ü x ✓", true},
	}
	for _, c := range cases {
		got, ok := mustParse(t, c.prompt).Render(c.sel, c.clip)
		if got != c.want || ok != c.wantOK {
			t.Errorf("%q: got %q %v, want %q %v", c.prompt, got, ok, c.want, c.wantOK)
		}
	}
	if got := mustParse(t, "A {{s|c}} B").FirstUnresolved(ptr(""), ptr("")); got != "{{s|c}}" {
		t.Fatalf("unresolved %q", got)
	}
	if got := mustParse(t, "{{s}}").FirstUnresolved(ptr("x"), nil); got != "" {
		t.Fatalf("unresolved %q", got)
	}
	if got := Glyphs("Fix {{ s|c }} now"); got != "Fix ⟨s|c⟩ now" {
		t.Fatalf("glyphs %q", got)
	}
}

func TestMatchYAML(t *testing.T) {
	src := `matches:
  - trigger: ":hello"
    replace: "Hi there!"
  - trigger: ":n"
    replace: 42
  - trigger: ":sig"
    replace: |
      Best regards,
      Me
    label: Signature
    enabled: false
  - trigger: ":fix"
    prompt: "Fix {{s|c}}"
`
	var f MatchFile
	if err := yaml.Unmarshal([]byte(src), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Matches) != 4 {
		t.Fatalf("got %d matches", len(f.Matches))
	}
	if m := f.Matches[0]; m.Trigger != ":hello" || m.Replace != "Hi there!" || !m.Enabled || m.IsAI() {
		t.Fatalf("hello: %+v", m)
	}
	if f.Matches[1].Replace != "42" {
		t.Fatalf("number replace: %q", f.Matches[1].Replace)
	}
	if m := f.Matches[2]; m.Enabled || m.Label != "Signature" || m.Replace != "Best regards,\nMe\n" {
		t.Fatalf("sig: %+v", m)
	}
	if m := f.Matches[3]; !m.IsAI() || *m.Prompt != "Fix {{s|c}}" {
		t.Fatalf("fix: %+v", m)
	}
	out, err := yaml.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"trigger: :hello", "replace: Hi there!", "replace: |", "enabled: false", "prompt: Fix {{s|c}}", "label: Signature"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("encoded YAML lacks %q:\n%s", want, out)
		}
	}
	var back MatchFile
	if err := yaml.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	for i := range back.Matches {
		a, b := back.Matches[i], f.Matches[i]
		if a.Trigger != b.Trigger || a.Replace != b.Replace || a.Label != b.Label || a.Enabled != b.Enabled || a.IsAI() != b.IsAI() {
			t.Errorf("round trip %d: %+v != %+v", i, a, b)
		}
	}
}

func TestMatchYAMLErrors(t *testing.T) {
	for _, src := range []string{
		"matches:\n  - trigger: x\n    ai: {}\n",
		"matches:\n  - trigger: x\n    prompt: \"\"\n",
		"matches:\n  - trigger: x\n    prompt: \"{{s}}\"\n    replace: y\n",
		"matches:\n  - trigger: x\n    prompt: \"{{nope}}\"\n",
		"matches:\n  - replace: y\n",
	} {
		var f MatchFile
		if err := yaml.Unmarshal([]byte(src), &f); err == nil {
			t.Errorf("expected an error for %q", src)
		}
	}
}

func TestConfigYAML(t *testing.T) {
	var c Config
	if err := yaml.Unmarshal([]byte("enable: false\nclipboard_threshold: oops\ninject_delay: 5\nai:\n  model: haiku\n  placeholder_style: braille\n"), &c); err != nil {
		t.Fatal(err)
	}
	if c.Enabled || c.ClipboardThreshold != 50 || c.InjectDelay != 5 || c.AI.Model != "haiku" ||
		c.AI.PlaceholderStyle != PlaceholderBraille || c.AI.TimeoutSeconds != 30 || c.ConflictResolveDelayMs != 400 {
		t.Fatalf("config: %+v", c)
	}
	if err := yaml.Unmarshal([]byte("ai_model: x\n"), &c); err == nil {
		t.Fatal("legacy key accepted")
	}
	if err := yaml.Unmarshal([]byte("ai:\n  args: [\"--model=x\"]\n"), &c); err == nil {
		t.Fatal("= form accepted")
	}
	out, err := yaml.Marshal(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	var back Config
	if err := yaml.Unmarshal(out, &back); err != nil || back.Enabled != true || back.AI.Model != "sonnet" || back.AI.Binary != nil {
		t.Fatalf("round trip: %v %+v\n%s", err, back, out)
	}
}
