package main

import (
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sokki/internal/config"
	"sokki/internal/engine"
	"sokki/internal/macos"
	"sokki/internal/model"
)

// editorWindow is the window that edits a match, or makes a new one,
// from Settings, the palette (⌘↵) and the HUD.
type editorWindow struct {
	a   *app
	win *mygo.Window

	existing int64 // the match edited, 0 for a new one
	trigger  string
	replace  string
	label    string
	enabled  bool
	file     string
	extra    []string // files made here, not saved yet

	addingFile bool
	newFile    string
	newFileIn  ui.Handle
	triggerIn  ui.Handle

	// previewText is the preview of previewOf, the replacement it was
	// rendered for, kept while the replacement does not change.
	previewOf, previewText string
}

func (e *editorWindow) edit(m model.Match) {
	e.existing = m.ID
	e.trigger, e.replace, e.label, e.enabled = m.Trigger, m.Replace, m.Label, m.Enabled
	e.file = m.SourceFile
	if e.file == "" {
		e.file = config.DefaultMatchFile
	}
	e.open("Edit Match")
}

func (e *editorWindow) create(file, trigger string) {
	e.existing = 0
	e.trigger = ":"
	if trigger != "" {
		e.trigger = trigger
	}
	e.replace, e.label, e.enabled = "", "", true
	e.file = file
	e.open("New Match")
}

func (e *editorWindow) open(title string) {
	e.addingFile, e.newFile, e.extra = false, "", nil
	e.previewOf = "\x00" // the clipboard may have changed: render again
	if e.win == nil {
		e.win = mygo.NewWindow(mygo.WindowOptions{
			Title:     title,
			Width:     560,
			Height:    520,
			MinWidth:  460,
			MinHeight: 420,
			Hidden:    true,
			Content:   ui.View(e.view),
		})
		e.a.hideOnClose(e.win, e.close)
	}
	e.win.SetTitle(title)
	if !e.win.IsVisible() {
		e.a.acquire()
		e.win.Center()
	}
	e.win.Show()
	e.win.Focus()
	e.triggerIn.Focus()
	e.win.Invalidate()
}

func (e *editorWindow) close() {
	if e.win == nil || !e.win.IsVisible() {
		return
	}
	e.win.Hide()
	e.a.release()
}

func (e *editorWindow) files() []string {
	files := slices.Clone(e.a.store.SourceFiles())
	for _, f := range e.extra {
		if !slices.Contains(files, f) {
			files = append(files, f)
		}
	}
	if e.file != "" && !slices.Contains(files, e.file) {
		files = append(files, e.file)
	}
	return files
}

func (e *editorWindow) canSave() bool {
	return e.trigger != "" && e.replace != ""
}

func (e *editorWindow) save() {
	if !e.canSave() {
		return
	}
	m := model.Match{
		ID: e.existing, Trigger: e.trigger, Replace: e.replace, Label: strings.TrimSpace(e.label), Enabled: e.enabled,
		SourceFile: e.file,
	}
	if e.existing != 0 {
		if _, ok := e.a.store.Match(e.existing); ok {
			e.a.store.Update(m)
		} else {
			// Reloaded meanwhile: keep the edit as a match of its own.
			m.ID = 0
			e.a.store.Add(m)
		}
	} else {
		e.a.store.Add(m)
	}
	e.close()
}

// NormalizeFilename returns the match file a name typed means, with .yml
// added, or "" when it cannot be one.
func NormalizeFilename(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || strings.HasPrefix(s, ".") || strings.ContainsAny(s, `/\`) || len([]rune(s)) > 255 {
		return ""
	}
	lower := strings.ToLower(s)
	if strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml") {
		return s
	}
	return s + ".yml"
}

// existingFile returns the file of files a name typed names, ignoring
// case and the .yml or .yaml extension.
func existingFile(raw string, files []string) string {
	name := NormalizeFilename(raw)
	if name == "" {
		return ""
	}
	stem := func(s string) string {
		l := strings.ToLower(s)
		if t, ok := strings.CutSuffix(l, ".yml"); ok {
			return t
		}
		if t, ok := strings.CutSuffix(l, ".yaml"); ok {
			return t
		}
		return l
	}
	for _, f := range files {
		if stem(f) == stem(name) {
			return f
		}
	}
	return ""
}

func (e *editorWindow) commitNewFile() {
	name := NormalizeFilename(e.newFile)
	if name == "" {
		return
	}
	if f := existingFile(e.newFile, e.files()); f != "" {
		name = f
	} else {
		e.extra = append(e.extra, name)
	}
	e.file, e.addingFile, e.newFile = name, false, ""
}

// variables are what the editor's Insert menu adds to the replacement.
var variables = []struct{ label, text string }{
	{"Clipboard", "{{clipboard}}"},
	{"Date (2026-10-18)", "{{date:yyyy-MM-dd}}"},
	{"Long Date (October 18, 2026)", "{{date:MMMM d, yyyy}}"},
	{"Time (14:30)", "{{date:HH:mm}}"},
	{"Weekday (Sunday)", "{{date:EEEE}}"},
}

// conflict returns the other match with the trigger being edited.
func (e *editorWindow) conflict() (model.Match, bool) {
	for _, m := range e.a.store.Matches {
		if m.Trigger == e.trigger && m.ID != e.existing {
			return m, true
		}
	}
	return model.Match{}, false
}

// labeled puts a label over a control, as the fields of the editor.
func labeled(c *ui.Context, label string, fn func()) ui.Element {
	return ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(600)
		fn()
	})
}

func (e *editorWindow) view(c *ui.Context) {
	t := c.Theme()
	c.OnShortcut(ui.Cmd, ui.KeyEnter, e.save)
	c.OnShortcut(ui.Cmd, ui.KeyW, e.close)
	if !e.addingFile && c.Shortcut(0, ui.KeyEscape) {
		e.close()
	}
	ui.Column(c).Fill().Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			ui.Column(c).Grow(1).Padding(18, 20).Gap(16).Children(func() {
				ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
					labeled(c, "Trigger", func() {
						if ui.TextInput(c, &e.trigger).Bind(&e.triggerIn).Label("Trigger").Placeholder(":hello").Font("monospace").Submitted() {
							e.save()
						}
					}).Grow(1).Basis(0)
					labeled(c, "Label", func() {
						ui.TextInput(c, &e.label).Label("Label").Placeholder("Optional — shown in the palette")
					}).Grow(1.4).Basis(0)
				})
				if m, ok := e.conflict(); ok && e.trigger != "" {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Margin(-8, 0, 0, 0).Children(func() {
						ui.Icon(c, iconWarning).FontSize(12).TextColor(t.Warning)
						caption(c, "Another match in "+m.SourceFile+" has this trigger.").Wrap().Shrink(1)
					})
				} else {
					caption(c, "Type the trigger anywhere and Sokki replaces it.").Margin(-8, 0, 0, 0)
				}

				ui.Column(c).Gap(6).Grow(1).Shrink(1).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "Replacement").FontSize(12).FontWeight(600)
						ui.Spacer(c)
						ui.MenuButton(c, "Insert Variable", func(m *ui.Menu) {
							for _, v := range variables {
								if m.Item(v.label).Chosen() {
									e.replace += v.text
								}
							}
						}).Label("Insert Variable")
					})
					ui.TextArea(c, &e.replace).Label("Replacement").Placeholder("What the trigger types").Font("monospace").Grow(1).MinHeight(96)
				})
				if strings.Contains(e.replace, "{{") {
					e.preview(c)
				}

				ui.Row(c).Gap(16).AlignItems(ui.End).Children(func() {
					labeled(c, "Match File", func() {
						if e.addingFile {
							e.newFileRow(c)
							return
						}
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Select(c, &e.file, e.files()).Label("Match File").MinWidth(180)
							ui.Button(c, "New File…").Label("Create a new match file").OnClick(func() {
								e.newFile, e.addingFile = "", true
								e.newFileIn.Focus()
							})
						})
					}).Grow(1)
					ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(0, 0, 6, 0).Children(func() {
						ui.Text(c, "Enabled")
						ui.Switch(c, &e.enabled).Label("Enabled")
					})
				})
			})
		})
		ui.Divider(c)
		ui.Row(c).Padding(12, 20).Gap(8).AlignItems(ui.Center).Children(func() {
			hint(c, "⌘↵", "to save")
			ui.Spacer(c)
			ui.Button(c, "Cancel").OnClick(e.close)
			ui.PrimaryButton(c, "Save").Disabled(!e.canSave()).OnClick(e.save)
		})
	})
}

// preview shows the replacement as it will be typed now.
func (e *editorWindow) preview(c *ui.Context) {
	t := c.Theme()
	if e.previewOf != e.replace {
		clip, _ := macos.PasteboardString()
		e.previewOf, e.previewText = e.replace, engine.RenderPreview(e.replace, clip, time.Now())
	}
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, "Preview").FontSize(12).FontWeight(600).TextColor(t.TextMuted)
		textPlate(c, e.previewText).MaxLines(6).Ellipsis("…")
	})
}

func (e *editorWindow) newFileRow(c *ui.Context) {
	ui.Column(c).Gap(4).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			in := ui.TextInput(c, &e.newFile).Bind(&e.newFileIn).Label("New file name").Placeholder("work.yml").Grow(1)
			if in.Submitted() {
				e.commitNewFile()
			}
			if in.Focused() && in.Shortcut(0, ui.KeyEscape) {
				e.addingFile, e.newFile = false, ""
			}
			ui.Button(c, "Create").Disabled(NormalizeFilename(e.newFile) == "").OnClick(e.commitNewFile)
			ui.Button(c, "Cancel").OnClick(func() { e.addingFile, e.newFile = false, "" })
		})
		if f := existingFile(e.newFile, e.files()); f != "" {
			caption(c, "Already exists — will use "+f)
		}
	})
}
