// Package config reads and writes ~/.config/sokki: config/default.yml,
// the match files of match/, recency.json, and the starter files of a
// first launch.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"sokki/internal/logx"
	"sokki/internal/model"
)

// DefaultMatchFile is the match file new matches go to.
const DefaultMatchFile = "default.yml"

// PromptFile is the match file prompts added from the palette or captured
// go to. It has a file of its own since a Sokki before prompts cannot
// load a match without a trigger, and fails the whole file it is in.
const PromptFile = "prompts.yml"

// Dir is a configuration directory, ~/.config/sokki in the app.
type Dir string

// UserDir returns ~/.config/sokki, or $SOKKI_CONFIG_DIR when set, as
// scripts/dev.sh sets it so that a development build leaves the installed
// app's matches alone.
func UserDir() Dir {
	if d := os.Getenv("SOKKI_CONFIG_DIR"); d != "" {
		return Dir(d)
	}
	home, _ := os.UserHomeDir()
	return Dir(filepath.Join(home, ".config", "sokki"))
}

func (d Dir) path(parts ...string) string {
	return filepath.Join(append([]string{string(d)}, parts...)...)
}

// ConfigFile is config/default.yml.
func (d Dir) ConfigFile() string { return d.path("config", "default.yml") }

// MatchDir is match/.
func (d Dir) MatchDir() string { return d.path("match") }

// LogDir is logs/.
func (d Dir) LogDir() string { return d.path("logs") }

// LoadResult is what Load read.
type LoadResult struct {
	Matches []model.Match
	Config  model.Config
	// Error is the first match file that failed to parse, for the status
	// item and the match list.
	Error    string
	Warnings []string
	// Files are the match files there are, loaded or not.
	Files []string
}

// Load reads the configuration and every match file.
func (d Dir) Load() LoadResult {
	r := LoadResult{Config: d.loadConfig()}
	entries, err := os.ReadDir(d.MatchDir())
	if err != nil {
		return r
	}
	var names []string
	for _, e := range entries {
		if ext := filepath.Ext(e.Name()); !e.IsDir() && (ext == ".yml" || ext == ".yaml") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	seen := map[string]string{}
	for _, name := range names {
		r.Files = append(r.Files, name)
		data, err := os.ReadFile(filepath.Join(d.MatchDir(), name))
		if err != nil {
			continue
		}
		var f model.MatchFile
		if err := yaml.Unmarshal(data, &f); err != nil {
			msg := fmt.Sprintf("Failed to parse %s: %v", name, err)
			logx.Errorf("%s", msg)
			if r.Error == "" {
				r.Error = msg
			}
			continue
		}
		for _, m := range f.Matches {
			m.SourceFile = name
			key := m.Key()
			if prev, ok := seen[key]; ok {
				what := "trigger"
				if m.Trigger == "" {
					what = "prompt"
				}
				r.Warnings = append(r.Warnings, fmt.Sprintf("Duplicate %s %q in %s (overrides %s)", what, key, name, prev))
				r.Matches = slices.DeleteFunc(r.Matches, func(x model.Match) bool { return x.Key() == key })
			}
			seen[key] = name
			r.Matches = append(r.Matches, m)
		}
	}
	return r
}

func (d Dir) loadConfig() model.Config {
	data, err := os.ReadFile(d.ConfigFile())
	if err != nil {
		return model.DefaultConfig()
	}
	c := model.DefaultConfig()
	if err := yaml.Unmarshal(data, &c); err != nil {
		logx.Errorf("Failed to parse config/default.yml: %v", err)
		return model.DefaultConfig()
	}
	return c
}

// SaveMatches writes every match to its file, and empties the known files
// left without any.
func (d Dir) SaveMatches(matches []model.Match, known []string) error {
	if err := os.MkdirAll(d.MatchDir(), 0o755); err != nil {
		return err
	}
	grouped := map[string][]model.Match{}
	for _, m := range matches {
		f := m.SourceFile
		if f == "" {
			f = DefaultMatchFile
		}
		grouped[f] = append(grouped[f], m)
	}
	files := append(slices.Clone(known), DefaultMatchFile)
	for f := range grouped {
		files = append(files, f)
	}
	slices.Sort(files)
	files = slices.Compact(files)
	var errs []error
	for _, name := range files {
		out, err := encodeMatches(grouped[name])
		if err == nil {
			err = writeAtomic(filepath.Join(d.MatchDir(), name), out)
		}
		if err != nil {
			logx.Errorf("Failed to save %s: %v", name, err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func encodeMatches(ms []model.Match) ([]byte, error) {
	if ms == nil {
		ms = []model.Match{}
	}
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(model.MatchFile{Matches: ms}); err != nil {
		return nil, err
	}
	enc.Close()
	return []byte(b.String()), nil
}

// SaveConfig writes config/default.yml.
func (d Dir) SaveConfig(c model.Config) error {
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return err
	}
	enc.Close()
	if err := os.MkdirAll(filepath.Dir(d.ConfigFile()), 0o755); err != nil {
		return err
	}
	if err := writeAtomic(d.ConfigFile(), []byte(b.String())); err != nil {
		logx.Errorf("Failed to save config: %v", err)
		return err
	}
	return nil
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sokki-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	os.Chmod(tmp.Name(), 0o644)
	return os.Rename(tmp.Name(), path)
}

const defaultConfigYAML = `# Sokki — Global Configuration
# See docs for all available options.

enable: true
clipboard_threshold: 50          # chars before switching to clipboard mode
inject_delay: 0                  # ms between key simulations
paste_shortcut: "Cmd+V"
preserve_clipboard: true
restore_clipboard_delay: 100     # ms
log_level: "debug"               # "debug" (default) or "error"
toggle_shortcut: "none"          # "none" (default), "double_option", "double_shift", or "double_control"
hud_shortcut: "none"             # "none" (default), "double_option", "double_shift", or "double_control"
`

const defaultMatchesYAML = `# Sokki — Example matches
# Edit this file or add more .yml files in this directory.
# Changes are auto-detected and reloaded.

matches:
  # Simple text replacement
  - trigger: ":hello"
    replace: "Hi there!"

  # Insert current date
  - trigger: ":today"
    replace: "{{date:yyyy-MM-dd}}"

  # Insert clipboard content with wrapper
  - trigger: ":paste"
    replace: "Pasted: {{clipboard}}"

  # Multi-line replacement
  - trigger: ":sig"
    replace: |
      Best regards,
      Your Name
`

// Bootstrap makes the directory with a starter configuration and match
// file when it has neither config/ nor match/, and reports whether it did.
// A directory with only logs/ is new: the log makes it as the app starts.
func (d Dir) Bootstrap() (bool, error) {
	for _, sub := range []string{"config", "match"} {
		if _, err := os.Stat(d.path(sub)); err == nil {
			return false, nil
		}
	}
	for _, sub := range []string{"config", "match", "logs"} {
		if err := os.MkdirAll(d.path(sub), 0o755); err != nil {
			return false, err
		}
	}
	if err := os.WriteFile(d.ConfigFile(), []byte(defaultConfigYAML), 0o644); err != nil {
		return false, err
	}
	if err := os.WriteFile(filepath.Join(d.MatchDir(), DefaultMatchFile), []byte(defaultMatchesYAML), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// IsStarter reports whether c and matches are what a first launch has:
// the starter files of Bootstrap, or no matches at all.
func IsStarter(c model.Config, matches []model.Match) bool {
	if c != model.DefaultConfig() {
		return false
	}
	if len(matches) == 0 {
		return true
	}
	var f model.MatchFile
	if err := yaml.Unmarshal([]byte(defaultMatchesYAML), &f); err != nil || len(f.Matches) != len(matches) {
		return false
	}
	for i, m := range f.Matches {
		got := matches[i]
		if got.SourceFile != DefaultMatchFile || got.Trigger != m.Trigger || got.Replace != m.Replace || got.Label != m.Label || got.Enabled != m.Enabled {
			return false
		}
	}
	return true
}
