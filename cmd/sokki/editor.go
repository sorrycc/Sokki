package main

import (
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sokki/internal/config"
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
	if e.win == nil {
		e.win = mygo.NewWindow(mygo.WindowOptions{
			Title:     title,
			Width:     500,
			Height:    480,
			MinWidth:  420,
			MinHeight: 380,
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

func (e *editorWindow) view(c *ui.Context) {
	c.OnShortcut(ui.Cmd, ui.KeyEnter, e.save)
	c.OnShortcut(ui.Cmd, ui.KeyW, e.close)
	if !e.addingFile && c.Shortcut(0, ui.KeyEscape) {
		e.close()
	}
	ui.Column(c).Fill().Padding(16).Gap(12).Children(func() {
		title := "New Match"
		if e.existing != 0 {
			title = "Edit Match"
		}
		ui.Text(c, title).Bold().FontSize(15).AlignSelf(ui.Center)
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Form(c, func() {
				ui.Field(c, "Label", func() {
					ui.TextInput(c, &e.label).Placeholder("Optional").Grow(1)
				}).Description("Friendly name shown in the palette. Leave blank to fall back to the trigger.")
				ui.Field(c, "Trigger", func() {
					if ui.TextInput(c, &e.trigger).Bind(&e.triggerIn).Font("monospace").Grow(1).Submitted() {
						e.save()
					}
				}).Description("The keyword that triggers expansion (e.g., :hello)")
				ui.Field(c, "Source File", func() {
					if e.addingFile {
						e.newFileRow(c)
						return
					}
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Select(c, &e.file, e.files()).Label("Source File")
						ui.Button(c, "+").Label("Create a new source file").Tooltip("Create a new source file").OnClick(func() {
							e.newFile, e.addingFile = "", true
							e.newFileIn.Focus()
						})
					})
				}).Description("The YAML file where this match is stored.")
				ui.Field(c, "Replacement", func() {
					ui.TextArea(c, &e.replace).Font("monospace").MinHeight(90).Grow(1)
				}).Description("The text to insert. Use {{clipboard}} and {{date:format}} for variables.")
				ui.Field(c, "Enabled", func() { ui.Switch(c, &e.enabled) })
			})
		})
		ui.Row(c).Gap(8).Children(func() {
			ui.Button(c, "Cancel").OnClick(e.close)
			ui.Spacer(c)
			caption(c, "⌘↵ to save").AlignSelf(ui.Center)
			ui.PrimaryButton(c, "Save").Disabled(!e.canSave()).OnClick(e.save)
		})
	})
}

func (e *editorWindow) newFileRow(c *ui.Context) {
	ui.Column(c).Gap(4).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			in := ui.TextInput(c, &e.newFile).Bind(&e.newFileIn).Placeholder("filename.yml").Grow(1)
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
