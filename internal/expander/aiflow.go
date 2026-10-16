//go:build darwin

package expander

import (
	"time"

	"sokki/internal/ai"
	"sokki/internal/logx"
	"sokki/internal/macos"
	"sokki/internal/model"
)

const placeholderWord = "generating…"

var (
	brailleFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	blockFrames   = []string{"▏", "▎", "▍", "▌", "▋", "▊", "▉", "█"}
)

func placeholderFrame(style model.PlaceholderStyle, i int) string {
	switch style {
	case model.PlaceholderBraille:
		return brailleFrames[i%len(brailleFrames)]
	case model.PlaceholderBlockSweep:
		return blockFrames[i%len(blockFrames)]
	}
	return placeholderWord
}

// invocation is how an AI match was chosen: typed, with its trigger's
// length, or from the palette.
type invocation struct {
	typed  bool
	length int
}

func (v invocation) label() string {
	if v.typed {
		return "typed"
	}
	return "palette"
}

func runeLen(s string) int { return len([]rune(s)) }

// runAI runs an AI match: it captures the clipboard and the selection the
// prompt needs, shows a placeholder, runs the prompt, and replaces the
// placeholder with the reply, or puts things back as they were. It runs
// on a goroutine of its own.
func (e *Expander) runAI(m model.Match, inv invocation) {
	cfg := e.Config()
	unsuppress := func() { e.Ctx.SetSuppress(false) }
	tpl, err := model.ParsePrompt(*m.Prompt)
	if err != nil {
		logx.Errorf("[AI] template parse failed (should not happen): %v", err)
		e.hud(model.VerdictAIFailed, m.Trigger, m.Trigger, "bad prompt")
		unsuppress()
		return
	}
	// One run at a time, refused before anything is touched.
	if e.Ctx.AIActive() {
		logx.Warnf("[AI] refused — another AI run is in flight")
		e.hud(model.VerdictAIFailed, m.Trigger, m.Trigger, "AI busy")
		return
	}
	started := time.Now()
	logx.Debugf("[AI] match trigger=%s invocation=%s promptLen=%d placeholders=%d model=%s",
		m.Trigger, inv.label(), runeLen(*m.Prompt), len(tpl.Placeholders), cfg.AI.Model)

	e.Ctx.SetSuppress(true)
	if inv.typed {
		e.Injector.Erase(inv.length)
	}

	// The clipboard first, before Cmd-C writes over it, then the selection.
	var clipboard, selection *string
	if tpl.NeedsClipboard() {
		s := clipboardText()
		clipboard = &s
		logx.Debugf("[AI] clipboard snapshot len=%d", runeLen(s))
	}
	restoreTrigger := func() {
		if inv.typed {
			e.Injector.Type(m.Trigger)
		}
	}
	if tpl.NeedsSelection() {
		t0 := time.Now()
		captured, ok := e.captureSelection(cfg.AI.SelectionCaptureTimeoutMs, inv)
		ms := time.Since(t0).Milliseconds()
		switch {
		case ok:
			selection = &captured
			logx.Debugf("[AI] selection captured len=%d in %dms", runeLen(captured), ms)
		case tpl.RequiresSelection():
			logx.Warnf("[AI] no selection (timeout=%dms, elapsed=%dms)", cfg.AI.SelectionCaptureTimeoutMs, ms)
			restoreTrigger()
			e.hud(model.VerdictAIFailed, m.Trigger, m.Trigger, "no selection")
			unsuppress()
			return
		default:
			// {{s|c}}: the next source fills it, or the render fails below.
			logx.Warnf("[AI] selection missing — falling back to chain (elapsed=%dms)", ms)
		}
	}

	rendered, ok := tpl.Render(selection, clipboard)
	if !ok {
		chain := tpl.FirstUnresolved(selection, clipboard)
		if chain == "" {
			chain = "{{…}}"
		}
		logx.Warnf("[AI] render aborted: no input for %s", chain)
		restoreTrigger()
		e.hud(model.VerdictAIFailed, m.Trigger, m.Trigger, "no input for "+chain)
		unsuppress()
		return
	}

	// The placeholder's first frame, while still suppressed. From the
	// palette with a selection, it replaces the selection.
	style := cfg.AI.PlaceholderStyle
	first := placeholderFrame(style, 0)
	e.Injector.Type(first)
	e.aiMu.Lock()
	e.placeholderLen = runeLen(first)
	e.aiMu.Unlock()
	// Let our own events reach the tap while it is suppressed.
	time.Sleep(50 * time.Millisecond)

	e.Ctx.SetAIActive(true)
	e.Ctx.SetSuppress(false)
	e.hud(model.VerdictAIThinking, m.Trigger, m.Trigger, "")
	logx.Debugf("[AI] placeholder injected (style=%s), dispatching request", style)
	stopAnim := e.animate(style)

	res := e.Rewriter.Run(ai.Request{
		Prompt: rendered, Model: cfg.AI.Model, Timeout: time.Duration(cfg.AI.TimeoutSeconds) * time.Second,
		UserArgs: cfg.AI.Args, Binary: deref(cfg.AI.Binary),
	})

	// Suppress before stopping the animation, so that a frame finishing
	// cannot turn suppression off again.
	e.Ctx.SetSuppress(true)
	stopAnim()
	typed := e.Ctx.ConsumeTypedDuringAI()
	e.aiMu.Lock()
	phLen := e.placeholderLen
	e.aiMu.Unlock()
	elapsed := time.Since(started).Milliseconds()

	switch res.Outcome {
	case ai.Completed:
		logx.Debugf("[AI] completed len=%d typedDuringWait=%v elapsed=%dms", runeLen(res.Text), typed, elapsed)
		if !typed {
			e.Injector.Erase(phLen)
			e.Injector.Type(res.Text)
			e.hud(model.VerdictAICompleted, m.Trigger, m.Trigger, "")
		} else {
			e.Injector.Type(res.Text)
			e.hud(model.VerdictAICompleted, m.Trigger, m.Trigger, "result appended; loading indicator left inline — delete manually")
		}
		if inv.typed {
			e.Ctx.RecordExpansion(m.Trigger, res.Text)
		}
		e.Ctx.ClearBuffer()
	case ai.Failed:
		logx.Warnf("[AI] failed reason=%s typedDuringWait=%v elapsed=%dms", res.Reason, typed, elapsed)
		e.restoreAfterFailure(m.Trigger, selection, phLen, typed, inv)
		e.hud(model.VerdictAIFailed, m.Trigger, m.Trigger, res.Reason.String())
	case ai.Canceled:
		logx.Warnf("[AI] cancelled typedDuringWait=%v elapsed=%dms", typed, elapsed)
		e.restoreAfterFailure(m.Trigger, selection, phLen, typed, inv)
		e.hud(model.VerdictAIFailed, m.Trigger, m.Trigger, "cancelled")
	}
	unsuppress()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// restoreAfterFailure erases the placeholder and puts back the selection,
// or the trigger typed; unless the user typed meanwhile, when erasing
// could delete their text.
func (e *Expander) restoreAfterFailure(trigger string, selection *string, phLen int, typed bool, inv invocation) {
	if typed {
		return
	}
	e.Injector.Erase(phLen)
	switch {
	case selection != nil && *selection != "":
		e.Injector.Type(*selection)
	case inv.typed:
		e.Injector.Type(trigger)
	}
}

// animate swaps the placeholder's frames until the returned function is
// called (nothing for the static style), and stops by itself when the
// user types. Each frame: suppress, erase the last, type the next, let
// it through (70 ms), unsuppress, listen (50 ms).
func (e *Expander) animate(style model.PlaceholderStyle) (stop func()) {
	if style == model.PlaceholderStatic {
		return func() {}
	}
	quit := make(chan struct{})
	done := make(chan struct{})
	wait := func(d time.Duration) bool {
		select {
		case <-quit:
			return false
		case <-time.After(d):
			return true
		}
	}
	go func() {
		defer close(done)
		if !wait(50 * time.Millisecond) {
			return
		}
		for i := 1; ; i++ {
			if e.Ctx.PeekTypedDuringAI() {
				return
			}
			select {
			case <-quit:
				return
			default:
			}
			e.Ctx.SetSuppress(true)
			e.aiMu.Lock()
			n := e.placeholderLen
			e.aiMu.Unlock()
			e.Injector.Erase(n)
			frame := placeholderFrame(style, i)
			e.Injector.Type(frame)
			e.aiMu.Lock()
			e.placeholderLen = runeLen(frame)
			e.aiMu.Unlock()
			if !wait(70 * time.Millisecond) {
				return
			}
			e.Ctx.SetSuppress(false)
			if !wait(50 * time.Millisecond) {
				return
			}
		}
	}()
	return func() {
		close(quit)
		<-done
	}
}

// captureSelection copies the selection with Cmd-C and puts the clipboard
// back. ok is false when nothing was selected: the pasteboard did not
// change before the timeout. A typed trigger's selection is still there
// after the copy, and a backspace deletes it before the placeholder; from
// the palette, the placeholder replaces it.
func (e *Expander) captureSelection(timeoutMs int, inv invocation) (string, bool) {
	before := macos.PasteboardChangeCount()
	snap := macos.SavePasteboard()
	macos.TapKey(macos.KeyC, macos.FlagCommand)
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for time.Now().Before(deadline) && macos.PasteboardChangeCount() == before {
		time.Sleep(10 * time.Millisecond)
	}
	text, has := macos.PasteboardString()
	if macos.PasteboardChangeCount() == before || !has || text == "" {
		RestorePasteboardIfOurs(snap)
		return "", false
	}
	if inv.typed {
		macos.TapKey(macos.KeyBackspace, 0)
	}
	RestorePasteboardIfOurs(snap)
	return text, true
}
