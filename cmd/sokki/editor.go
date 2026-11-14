package main

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sokki/internal/config"
	"sokki/internal/engine"
	"sokki/internal/macos"
	"sokki/internal/model"
)

// editorWindow is the window that edits a match, or makes a new one,
// from Settings, the palette (⌘↵, and ⌘E from its New Prompt form) and the
// HUD. A match without a trigger is a prompt, inserted from the palette.
type editorWindow struct {
	a   *app
	win *mygo.Window
	// headless leaves the window out, for tests that run the view alone.
	headless bool

	matchDraft
	extra []string // files made here, not saved yet

	// confirmDiscard asks whether to drop the edits on closing.
	confirmDiscard bool

	addingFile bool
	newFile    string
	newFileIn  ui.Handle

	// verb is what the editor does, New, Edit or Duplicate, to a match
	// or, while the trigger is empty, a prompt.
	verb string
}

// matchDraft is a match as it is edited, in the editor window or in the
// detail of the Matches page: its fields, where a variable goes in its
// replacement, and the preview of its variables.
type matchDraft struct {
	existing int64 // the match edited, 0 for a new one
	trigger  string
	replace  string
	label    string
	enabled  bool
	file     string

	// orig is the match as editing began, or as last saved, to tell an
	// edit.
	orig model.Match

	triggerIn ui.Handle
	replaceIn ui.Handle

	// selStart and selEnd are the selection of the replacement, in runes,
	// as of when it last had the focus (selKnown), where a variable is
	// inserted; caret, when caretSet, is where to put the caret next frame.
	selStart, selEnd int
	selKnown         bool
	caret            int
	caretSet         bool

	// previewText is the preview of previewOf, the replacement it was
	// rendered for, kept while the replacement does not change.
	previewOf, previewText string
	// ranges are the variables of rangesOf, colored rangesIn, kept while
	// neither changes.
	rangesOf string
	rangesIn ui.Color
	ranges   []ui.TextRange
}

// set takes m's fields, in default.yml when it names no file.
func (d *matchDraft) set(m model.Match) {
	d.existing = m.ID
	d.trigger, d.replace, d.label, d.enabled = m.Trigger, m.Replace, m.Label, m.Enabled
	d.file = m.SourceFile
	if d.file == "" {
		d.file = config.DefaultMatchFile
	}
}

// begin starts editing the fields as they are.
func (d *matchDraft) begin() {
	d.selKnown, d.caretSet = false, false
	d.previewOf = "\x00" // the clipboard may have changed: render again
	d.orig = d.current()
}

// current is the match as edited.
func (d *matchDraft) current() model.Match {
	return model.Match{
		ID: d.existing, Trigger: triggerOf(d.trigger), Replace: d.replace, Label: strings.TrimSpace(d.label), Enabled: d.enabled,
		SourceFile: d.file,
	}
}

// dirty reports whether the match was edited since editing began.
func (d *matchDraft) dirty() bool {
	return d.current() != d.orig
}

// isPrompt reports whether the match edited has no trigger: a prompt.
func (d *matchDraft) isPrompt() bool { return triggerOf(d.trigger) == "" }

func (e *editorWindow) edit(m model.Match) {
	e.set(m)
	e.open("Edit")
}

func (e *editorWindow) create(file, trigger string) {
	e.existing = 0
	e.trigger = ":"
	if trigger != "" {
		e.trigger = trigger
	}
	e.replace, e.label, e.enabled = "", "", true
	e.file = file
	e.open("New")
}

// compose opens a new prompt with m's fields, as the palette's New Prompt
// form had them.
func (e *editorWindow) compose(m model.Match) {
	e.existing = 0
	e.trigger, e.replace, e.label, e.enabled = m.Trigger, m.Replace, m.Label, true
	e.file = m.SourceFile
	if e.file == "" {
		e.file = config.PromptFile
	}
	e.open("New")
}

// duplicate opens a new match with m's fields, to save under another
// trigger.
func (e *editorWindow) duplicate(m model.Match) {
	e.set(m)
	e.existing = 0
	e.open("Duplicate")
}

// requestClose closes the editor, asking first when that drops edits.
func (e *editorWindow) requestClose() {
	if !e.dirty() {
		e.close()
		return
	}
	e.confirmDiscard = true
	if e.win != nil {
		e.win.Focus()
		e.win.Invalidate()
	}
}

// title is what the editor does, as "Edit Prompt", following the trigger
// as it is typed or cleared.
func (e *editorWindow) title() string {
	if e.isPrompt() {
		return e.verb + " Prompt"
	}
	return e.verb + " Match"
}

func (e *editorWindow) open(verb string) {
	e.addingFile, e.newFile, e.extra = false, "", nil
	e.confirmDiscard = false
	e.verb = verb
	e.begin()
	if e.headless {
		return
	}
	if e.win == nil {
		e.win = mygo.NewWindow(mygo.WindowOptions{
			Title:         e.title(),
			Width:         560,
			Height:        540,
			MinWidth:      460,
			MinHeight:     440,
			Hidden:        true,
			TitleBarStyle: mygo.TitleBarHidden,
			Content:       ui.View(e.view),
		})
		e.a.hideOnClose(e.win, e.requestClose)
	}
	e.win.SetTitle(e.title())
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
	e.confirmDiscard = false
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

// triggerOf is the trigger typed in a field: as typed, spaces too, as
// "teh " has, but none when it is blank.
func triggerOf(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return s
}

// minTrigger is the fewest characters a trigger has: one character, as
// a lone ":", would expand wherever it is typed.
const minTrigger = 2

// triggerTooShort reports a trigger under minTrigger, but for one a
// match had already, from its file. No trigger at all is a prompt's.
func (d *matchDraft) triggerTooShort() bool {
	trigger := triggerOf(d.trigger)
	if trigger == "" || d.existing != 0 && trigger == d.orig.Trigger {
		return false
	}
	return utf8.RuneCountInString(strings.TrimSpace(trigger)) < minTrigger
}

func (d *matchDraft) canSave() bool {
	return !d.triggerTooShort() && d.replace != "" && d.current().Key() != ""
}

func (e *editorWindow) save() {
	if !e.canSave() {
		return
	}
	m := e.current()
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

// insert puts text in the replacement over its selection, or at its end
// when it has not had the focus, and the caret after it.
func (d *matchDraft) insert(text string) {
	r := []rune(d.replace)
	start, end := len(r), len(r)
	if d.selKnown {
		start, end = min(max(0, d.selStart), len(r)), min(max(0, d.selEnd), len(r))
		if start > end {
			start, end = end, start
		}
	}
	d.replace = string(r[:start]) + text + string(r[end:])
	d.caret, d.caretSet = start+utf8.RuneCountInString(text), true
	d.selStart, d.selEnd, d.selKnown = d.caret, d.caret, true
	d.replaceIn.Focus()
}

// variables are what the editor's Insert menu adds to the replacement.
var variables = []struct{ label, text string }{
	{"Clipboard", "{{clipboard}}"},
	{"Date (2026-10-18)", "{{date:yyyy-MM-dd}}"},
	{"Long Date (October 18, 2026)", "{{date:MMMM d, yyyy}}"},
	{"Time (14:30)", "{{date:HH:mm}}"},
	{"Weekday (Sunday)", "{{date:EEEE}}"},
}

// conflict returns the other match of matches with the key of the one
// being edited: its trigger, or a prompt's title.
func (d *matchDraft) conflict(matches []model.Match) (model.Match, bool) {
	key := d.current().Key()
	for _, m := range matches {
		if key != "" && m.Key() == key && m.ID != d.existing {
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
	c.OnShortcut(ui.Cmd, ui.KeyW, e.requestClose)
	if !e.addingFile && !e.confirmDiscard && c.Shortcut(0, ui.KeyEscape) {
		e.requestClose()
	}
	noun := "match"
	if e.isPrompt() {
		noun = "prompt"
	}
	if ui.AlertDialog(c, &e.confirmDiscard, "Discard Changes?",
		"Your edits to this "+noun+" haven't been saved.", "Keep Editing", "Discard") == 1 {
		e.close()
	}
	ui.Column(c).Fill().Children(func() {
		e.header(c)
		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			ui.Column(c).Grow(1).Padding(16, 20, 18, 20).Gap(16).Children(func() {
				ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
					labeled(c, "Trigger", func() {
						if ui.TextInput(c, &e.trigger).Bind(&e.triggerIn).Label("Trigger").Placeholder("Optional, as :hello").Font("monospace").Submitted() {
							e.save()
						}
					}).Grow(1).Basis(0)
					labeled(c, "Label", func() {
						placeholder := "Optional — names it in the palette"
						if e.isPrompt() {
							placeholder = "Names it in the palette — else its first line"
						}
						ui.TextInput(c, &e.label).Label("Label").Placeholder(placeholder)
					}).Grow(1.4).Basis(0)
				})
				e.note(c, e.a.store)

				ui.Column(c).Gap(6).Grow(1).Shrink(1).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "Replacement").FontSize(12).FontWeight(600)
						ui.Spacer(c)
						e.insertMenu(c)
					})
					e.replaceField(c).Grow(1).MinHeight(96)
					if e.replace != "" {
						caption(c, summary(e.replace)).FontFeatures("tnum").AlignSelf(ui.End)
					}
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
		ui.Row(c).Padding(12, 20).Gap(8).AlignItems(ui.Center).Background(t.Text.Alpha(0.02)).Children(func() {
			hint(c, "⌘↵", "to save")
			ui.Spacer(c)
			ui.Button(c, "Cancel").OnClick(e.close)
			ui.PrimaryButton(c, "Save").Disabled(!e.canSave()).Tooltip("Save (⌘↵)").OnClick(e.save)
		})
	})
}

// header is the editor's title bar: what it does, under the window's
// controls, dragging the window.
func (e *editorWindow) header(c *ui.Context) {
	t := c.Theme()
	bar := c.TitleBar()
	ui.Row(c).Height(max(bar.Height, 12)+44).Padding(max(bar.Height, 12), 20, 0, 20).Gap(10).AlignItems(ui.Center).DragWindow().Children(func() {
		icon := iconPlus
		switch {
		case e.isPrompt():
			icon = iconPrompt
		case e.existing != 0:
			icon = iconPencil
		}
		// A trigger typed or cleared turns a prompt into a match: its icon
		// fades in.
		c.Key(icon)
		iconPlate(c, icon, t.Accent, 30).Transition(fadeTransition)
		ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
			heading(c, e.title()).SingleLine()
			where := "Saved to " + e.file
			if e.isPrompt() {
				where += " · found in the palette"
			}
			caption(c, where).SingleLine().Ellipsis("…")
		})
	})
}

func replacePlaceholder(prompt bool) string {
	if prompt {
		return "The prompt, as the palette will insert it"
	}
	return "What the trigger types"
}

// insertMenu is the Insert Variable menu over the replacement.
func (d *matchDraft) insertMenu(c *ui.Context) ui.Element {
	return ui.MenuButton(c, "Insert Variable", func(m *ui.Menu) {
		for _, v := range variables {
			if m.Item(v.label).Chosen() {
				d.insert(v.text)
			}
		}
	}).Label("Insert Variable")
}

// replaceField is the field of the replacement, its variables in the
// accent's color, which keeps where its selection was.
func (d *matchDraft) replaceField(c *ui.Context) ui.Element {
	if accent := c.Theme().Accent; d.rangesOf != d.replace || d.rangesIn != accent {
		d.rangesOf, d.rangesIn, d.ranges = d.replace, accent, variableRanges(c, d.replace)
	}
	ta := ui.TextArea(c, &d.replace).Bind(&d.replaceIn).Label("Replacement").
		Placeholder(replacePlaceholder(d.isPrompt())).Font("monospace").TextRanges(d.ranges...)
	if d.caretSet {
		ta.SetTextSelection(d.caret, d.caret)
		d.caretSet = false
	}
	if ta.Focused() {
		d.selStart, d.selEnd = ta.TextSelection()
		d.selKnown = true
	}
	return ta
}

// previewed is the replacement as it will be typed now, rendered again
// only as it changes: it reads the clipboard.
func (d *matchDraft) previewed() string {
	if d.previewOf != d.replace {
		clip, _ := macos.PasteboardString()
		d.previewOf, d.previewText = d.replace, engine.RenderPreview(d.replace, clip, time.Now())
	}
	return d.previewText
}

// problem is what keeps the match from being saved as it is, if
// anything: another match with its key, or a trigger too short.
func (d *matchDraft) problem(matches []model.Match) (string, bool) {
	if m, ok := d.conflict(matches); ok {
		what := "has this trigger."
		if d.isPrompt() {
			what = "has this title. Give this one a label."
		}
		return "Another match in " + m.SourceFile + " " + what, true
	}
	if d.triggerTooShort() && triggerOf(d.trigger) != ":" {
		return "A trigger needs at least 2 characters, so that it doesn't expand by accident.", true
	}
	return "", false
}

// note is the line under the trigger and the label: what is wrong with
// them, or what the match does. It fades in as it changes from one to
// another, not as each key is typed.
func (d *matchDraft) note(c *ui.Context, store *config.Store) {
	t := c.Theme()
	icon, color, text := iconKeyboard, t.TextMuted, "Type the trigger anywhere and Sokki replaces it. Leave it empty for a prompt."
	shortcut := ""
	if p, ok := d.problem(store.Matches); ok {
		icon, color, text = iconWarning, t.Warning, p
	} else if d.isPrompt() {
		icon, text = iconPrompt, "No trigger: a prompt, inserted from the palette."
		if title := d.current().Title(); d.label == "" && title != "" {
			text = "No trigger: a prompt, in the palette as “" + truncate(title, 40) + "”"
		}
		shortcut = shortcutLabel(paletteOptions, store.Config.PaletteShortcut)
	}
	c.Key(icon)
	ui.Row(c).Gap(6).AlignItems(ui.Center).Margin(-8, 0, 0, 0).Transition(fadeTransition).Children(func() {
		ui.Icon(c, icon).FontSize(12).TextColor(color).Shrink(0)
		caption(c, text).Wrap().Shrink(1)
		if shortcut != "" {
			kbd(c, shortcut).Tooltip("Opens the palette")
		}
	})
}

// preview shows the replacement as it will be typed now.
func (e *editorWindow) preview(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, "Preview").FontSize(12).FontWeight(600).TextColor(t.TextMuted)
		textPlate(c, e.previewed()).MaxLines(6).Ellipsis("…")
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
