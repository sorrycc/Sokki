package main

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"sokki/internal/config"
	"sokki/internal/engine"
	"sokki/internal/model"
)

// promptForm is the palette's New Prompt form, from ⌘N: the selection in
// the app in front, else the clipboard, saved as a prompt without leaving
// the palette.
type promptForm struct {
	label, trigger, body string
	file                 string
	// source says where body came from, "" when it came from nowhere,
	// and sourceIcon shows which.
	source     string
	sourceIcon *ui.SVG
	labelIn    ui.Handle
	bodyIn     ui.Handle
}

// match is the prompt as the form has it.
func (f *promptForm) match() model.Match {
	return model.Match{
		Trigger: triggerOf(f.trigger), Label: strings.TrimSpace(f.label), Replace: f.body,
		Enabled: true, SourceFile: f.file,
	}
}

// looksLikeTrigger reports whether what was searched is a trigger rather
// than a title: one word starting with punctuation, as :hello or ;brain.
func looksLikeTrigger(s string) bool {
	if s == "" || strings.ContainsAny(s, " \t") {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

// quickAdd turns the palette into the New Prompt form, its text the
// selection in the app in front, else the clipboard, and its label, or
// its trigger, what was searched.
func (p *palette) quickAdd() {
	f := &p.add
	q := strings.TrimSpace(p.query)
	f.label, f.trigger, f.file = q, "", config.PromptFile
	if looksLikeTrigger(q) {
		f.label, f.trigger = "", q
	}
	text, app := p.selectedText()
	switch {
	case strings.TrimSpace(text) != "":
		f.body, f.source, f.sourceIcon = text, "From the selection", iconCursor
		if app != "" {
			f.source += " in " + app
		}
	case strings.TrimSpace(p.clipboard) != "":
		f.body, f.source, f.sourceIcon = p.clipboard, "From the clipboard", iconClip
	default:
		f.body, f.source = "", ""
	}
	p.mode = modeAdd
	if f.body == "" {
		f.bodyIn.Focus()
	} else {
		f.labelIn.Focus()
	}
}

// addProblem says why the form cannot be saved, "" when it can.
func (p *palette) addProblem() string {
	m := p.add.match()
	switch {
	case strings.TrimSpace(m.Replace) == "":
		return "Type or paste the prompt."
	case m.Trigger != "" && utf8.RuneCountInString(strings.TrimSpace(m.Trigger)) < minTrigger:
		return "A trigger needs at least 2 characters."
	}
	if other, ok := p.a.store.WithKey(m.Key()); ok {
		if m.Trigger != "" {
			return "Another match in " + other.SourceFile + " has this trigger."
		}
		return "Another match in " + other.SourceFile + " is called this. Give this one a label."
	}
	return ""
}

// saveAdd saves the form's prompt and goes back to the search, the new
// prompt chosen and marked, the footer saying where it went.
func (p *palette) saveAdd(c *ui.Context) {
	if p.addProblem() != "" {
		return
	}
	m := p.add.match()
	m.ID = model.NewID()
	p.a.store.Add(m)
	p.mode, p.query = modeSearch, ""
	p.filter()
	p.chooseID(m.ID)
	p.input.Focus()
	p.flash = flash{text: "Saved to " + m.SourceFile, id: m.ID, until: c.Now().Add(flashFor)}
}

// openAddInEditor carries the form over to the editor, for more room.
func (p *palette) openAddInEditor() {
	p.hide()
	p.a.editor.compose(p.add.match())
}

func (p *palette) addKeys(c *ui.Context) {
	if c.Shortcut(0, ui.KeyEscape) {
		p.mode = modeSearch
		p.input.Focus()
	}
	if c.Shortcut(ui.Cmd, ui.KeyEnter) {
		p.saveAdd(c)
	}
	c.OnShortcut(ui.Cmd, ui.KeyE, p.openAddInEditor)
}

func (p *palette) addView(c *ui.Context) {
	t := c.Theme()
	f := &p.add
	problem := p.addProblem()

	ui.Row(c).Padding(14, 18).Gap(10).AlignItems(ui.Center).Children(func() {
		iconPlate(c, iconPrompt, t.Accent, 28)
		ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
			heading(c, "New Prompt").SingleLine()
			caption(c, "Saved to "+f.file+" · found in the palette").SingleLine().Ellipsis("…")
		})
		ui.Button(c, "Open in Editor").OnClick(p.openAddInEditor)
		ui.PrimaryButton(c, "Save").Disabled(problem != "").OnClick(func() { p.saveAdd(c) })
	})
	ui.Divider(c)
	ui.Column(c).Grow(1).Shrink(1).Padding(14, 18).Gap(12).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
			labeled(c, "Label", func() {
				ui.TextInput(c, &f.label).Bind(&f.labelIn).Label("Label").Placeholder("Optional — else its first line")
			}).Grow(1.6).Basis(0)
			labeled(c, "Trigger", func() {
				ui.TextInput(c, &f.trigger).Label("Trigger").Placeholder("Optional, as ;review").Font("monospace")
			}).Grow(1).Basis(0)
		})
		ui.Column(c).Gap(6).Grow(1).Shrink(1).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Prompt").FontSize(12).FontWeight(600)
				ui.Spacer(c)
				if f.source != "" {
					sourcePill(c, f.sourceIcon, f.source)
				}
			})
			ui.TextArea(c, &f.body).Bind(&f.bodyIn).Label("Prompt").Placeholder("What the prompt says").
				Font("monospace").Grow(1).MinHeight(120).TextRanges(variableRanges(c, f.body)...)
			ui.Row(c).Gap(6).AlignItems(ui.Center).MinHeight(16).Children(func() {
				if problem != "" && strings.TrimSpace(f.body) != "" {
					c.Key(problem)
					ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(1).Transition(fadeTransition).Children(func() {
						ui.Icon(c, iconWarning).FontSize(12).TextColor(t.Warning)
						caption(c, problem).SingleLine().Ellipsis("…").Shrink(1)
					})
				}
				ui.Spacer(c)
				caption(c, bodyStats(f.body)).FontFeatures("tnum").SingleLine().Shrink(0)
			})
		})
	})
	ui.Divider(c)
	p.footer(c, func() {
		if problem == "" {
			hint(c, "⌘↵", "Save")
		}
		hint(c, "⌘E", "Open in Editor")
		hint(c, "esc", "Back")
	})
}

// sourcePill says where the form's text came from, with icon.
func sourcePill(c *ui.Context, icon *ui.SVG, text string) {
	t := c.Theme()
	ui.Row(c).Gap(5).AlignItems(ui.Center).Padding(2, 8).Radius(10).Background(t.Text.Alpha(0.05)).Shrink(1).Children(func() {
		if icon != nil {
			ui.Icon(c, icon).FontSize(11).TextColor(t.TextMuted)
		}
		caption(c, text).SingleLine().Ellipsis("…").Shrink(1)
	})
}

// bodyStats counts a prompt's characters and variables.
func bodyStats(body string) string {
	s := plural(utf8.RuneCountInString(body), "character", "characters")
	if n := len(engine.Variables(body)); n > 0 {
		s += " · " + plural(n, "variable", "variables")
	}
	return s
}

// savedHeight is the palette's height while it says what the capture
// shortcut saved or ⌘C copied, savedFor how long it says it saved and
// copiedFor that it copied.
const (
	savedHeight = 64
	savedFor    = 3 * time.Second
	copiedFor   = 1500 * time.Millisecond
	// leftFor is how long it stays once the pointer that kept it leaves.
	leftFor = 1200 * time.Millisecond
)

// capture is what the capture shortcut did: the prompt it saved, or
// nothing, as nothing was selected; or, copied set, what ⌘C copied.
type capture struct {
	saved  model.Match
	ok     bool
	copied string
	// shown is how long it says so.
	shown time.Duration
	// gen counts the captures, so that the hiding of one leaves the
	// next alone; kept, the pointer over it, stops it hiding; timers
	// counts the countdowns started, each of which runs its line anew.
	gen    int
	kept   bool
	timers int
}

// capture saves the selection in the app in front as a prompt at once,
// for the capture shortcut, and says so in the palette, briefly and
// without taking the keyboard from the app.
func (p *palette) capture() {
	p.captureNow()
	p.notify(savedFor)
}

// notify shows what p.captured says for d, unless the pointer goes over
// it, without taking the keyboard from the app.
func (p *palette) notify(d time.Duration) {
	p.place(savedHeight)
	p.mode = modeSaved
	p.opened++
	p.win.ShowInactive()
	p.win.Invalidate()
	p.hideAfter(d)
}

// hideAfter hides what p.captured says after d, unless the pointer is
// over it then or another capture took its place.
func (p *palette) hideAfter(d time.Duration) {
	p.captured.shown = d
	p.captured.timers++
	gen, timer := p.captured.gen, p.captured.timers
	time.AfterFunc(d, func() {
		p.a.post(func() {
			if p.mode == modeSaved && p.captured.gen == gen && p.captured.timers == timer && !p.captured.kept {
				p.hide()
			}
		})
	})
}

// keep keeps what p.captured says while the pointer is over it, and
// hides it a moment after the pointer leaves.
func (p *palette) keep(hovered bool) {
	switch {
	case hovered:
		p.captured.kept = true
	case p.captured.kept:
		p.captured.kept = false
		p.hideAfter(leftFor)
	}
}

// captureNow saves the selection as a prompt of PromptFile, its label its
// first line, made unique.
func (p *palette) captureNow() {
	text, _ := p.selectedText()
	p.captured = capture{gen: p.captured.gen + 1}
	if strings.TrimSpace(text) == "" {
		return
	}
	m := model.Match{
		ID: model.NewID(), Label: p.uniqueLabel(promptLabel(text)), Replace: text, Enabled: true,
		SourceFile: config.PromptFile,
	}
	p.a.store.Add(m)
	p.captured.saved, p.captured.ok = m, true
}

// promptLabel is the label of a captured prompt: its first line, cut to
// 60 characters.
func promptLabel(text string) string {
	label := model.Match{Replace: text}.Title()
	if utf8.RuneCountInString(label) > 60 {
		label = strings.TrimSpace(string([]rune(label)[:59])) + "…"
	}
	return label
}

// uniqueLabel returns label, numbered when a match has it already.
func (p *palette) uniqueLabel(label string) string {
	try := label
	for n := 2; ; n++ {
		if _, taken := p.a.store.WithKey(try); !taken {
			return try
		}
		try = fmt.Sprintf("%s %d", label, n)
	}
}

func (p *palette) undoCapture() {
	p.hide()
	if p.captured.ok {
		p.a.store.Remove(p.captured.saved.ID)
		p.captured.ok = false
	}
}

func (p *palette) editCapture() {
	p.hide()
	if m, ok := p.a.store.Match(p.captured.saved.ID); ok {
		p.a.editor.edit(m)
	}
}

func (p *palette) savedView(c *ui.Context) {
	t := c.Theme()
	cp := &p.captured
	icon, color, title, detail := iconMatched, t.Success, "“"+cp.saved.Label+"”",
		"Saved as a prompt to "+config.PromptFile+" · find it in the palette"
	switch {
	case cp.copied != "":
		icon, color, title, detail = iconCopy, t.Accent, "Copied to the clipboard", "“"+cp.copied+"”, its variables filled in"
	case !cp.ok:
		icon, color, title, detail = iconWarning, t.Warning, "Nothing selected",
			"Select some text in an app, then press the shortcut again."
	}
	ui.Row(c).Grow(1).Padding(0, 14, 0, 14).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(32, 32).Radius(16).Background(color.Alpha(0.14)).Center().Shrink(0).Children(func() {
			ui.Icon(c, icon).FontSize(17).TextColor(color)
		})
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, title).FontWeight(600).SingleLine().Ellipsis("…")
			caption(c, detail).SingleLine().Ellipsis("…")
		})
		if cp.ok && cp.copied == "" {
			ui.Button(c, "Undo").Label("Undo saving the prompt").OnClick(p.undoCapture)
			ui.PrimaryButton(c, "Edit…").Label("Edit the prompt").OnClick(p.editCapture)
		}
	})
	// How long it stays, unless the pointer keeps it.
	if !cp.kept && cp.shown > 0 {
		c.Key(cp.timers)
		bar := ui.Box(c).Height(2).AlignSelf(ui.Start).Background(color.Alpha(0.45))
		if seen := ui.Local(bar, "seen", func() bool { return false }); !*seen {
			*seen = true
			bar.Jump("left", 1)
		}
		bar.WidthPercent(100 * bar.AnimateWith("left", 0, cp.shown, ui.Linear))
	}
}
