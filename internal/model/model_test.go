package model

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

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
`
	var f MatchFile
	if err := yaml.Unmarshal([]byte(src), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Matches) != 3 {
		t.Fatalf("got %d matches", len(f.Matches))
	}
	if m := f.Matches[0]; m.Trigger != ":hello" || m.Replace != "Hi there!" || !m.Enabled {
		t.Fatalf("hello: %+v", m)
	}
	if f.Matches[1].Replace != "42" {
		t.Fatalf("number replace: %q", f.Matches[1].Replace)
	}
	if m := f.Matches[2]; m.Enabled || m.Label != "Signature" || m.Replace != "Best regards,\nMe\n" {
		t.Fatalf("sig: %+v", m)
	}
	out, err := yaml.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"trigger: :hello", "replace: Hi there!", "replace: |", "enabled: false", "label: Signature"} {
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
		if a.Trigger != b.Trigger || a.Replace != b.Replace || a.Label != b.Label || a.Enabled != b.Enabled {
			t.Errorf("round trip %d: %+v != %+v", i, a, b)
		}
	}
}

func TestMatchYAMLErrors(t *testing.T) {
	for _, src := range []string{
		"matches:\n  - trigger: x\n    ai: {}\n",
		"matches:\n  - trigger: x\n    prompt: \"Fix {{s}}\"\n",
		"matches:\n  - enabled: true\n",
		"matches:\n  - trigger: [x]\n    replace: y\n",
	} {
		var f MatchFile
		if err := yaml.Unmarshal([]byte(src), &f); err == nil {
			t.Errorf("expected an error for %q", src)
		}
	}
}

func TestMatchWithoutTrigger(t *testing.T) {
	src := "matches:\n  - label: Review\n    replace: |\n      Review this diff.\n  - replace: \"Explain it\\nslowly\"\n"
	var f MatchFile
	if err := yaml.Unmarshal([]byte(src), &f); err != nil {
		t.Fatal(err)
	}
	if m := f.Matches[0]; m.Trigger != "" || m.Key() != "Review" || m.Title() != "Review" || !m.Enabled {
		t.Fatalf("labeled: %+v key %q", m, m.Key())
	}
	if m := f.Matches[1]; m.Key() != "Explain it" {
		t.Fatalf("unlabeled key %q", m.Key())
	}
	if (Match{Trigger: ":x", Label: "X"}).Key() != ":x" {
		t.Fatal("a trigger is the key")
	}
	out, err := yaml.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "trigger") {
		t.Fatalf("encoded a trigger:\n%s", out)
	}
}

func TestConfigYAML(t *testing.T) {
	var c Config
	if err := yaml.Unmarshal([]byte("enable: false\nclipboard_threshold: oops\ninject_delay: 5\nai:\n  model: haiku\n"), &c); err != nil {
		t.Fatal(err)
	}
	if c.Enabled || c.ClipboardThreshold != 50 || c.InjectDelay != 5 || c.ConflictResolveDelayMs != 400 {
		t.Fatalf("config: %+v", c)
	}
	out, err := yaml.Marshal(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	var back Config
	if err := yaml.Unmarshal(out, &back); err != nil || back.Enabled != true || strings.Contains(string(out), "ai:") {
		t.Fatalf("round trip: %v %+v\n%s", err, back, out)
	}
}
