package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"

	"sokki/internal/cli"
	"sokki/internal/cloudsync"
	"sokki/internal/config"
	"sokki/internal/engine"
	"sokki/internal/history"
	"sokki/internal/logx"
	"sokki/internal/macos"
	"sokki/internal/model"
	"sokki/internal/prefs"
	"sokki/internal/search"
	"sokki/internal/usage"
)

// The sokki command asks the running app, over a socket of its own, to do
// what its windows and menu do: see internal/cli. Each command runs in a
// goroutine of the server and reads and changes the app's state on the
// main thread.

// startControl listens for the sokki command, until the app quits.
func (a *app) startControl() {
	l, err := cli.Listen(cli.SocketPath(mygo.IsDev()))
	if err != nil {
		logx.Warnf("The sokki command can't reach this Sokki: %v", err)
		return
	}
	go http.Serve(l, cli.Handler(a.control))
}

// onMainWait runs fn on the main thread and waits for it.
func (a *app) onMainWait(fn func()) {
	done := make(chan struct{})
	a.post(func() { fn(); close(done) })
	<-done
}

// waitMain waits until done, asked on the main thread, reports true, or
// ctx ends.
func (a *app) waitMain(ctx context.Context, done func() bool) error {
	for {
		var ok bool
		a.onMainWait(func() { ok = done() })
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// args are a command's arguments.
type args map[string]any

func (g args) str(name string) string { s, _ := g[name].(string); return s }
func (g args) bool(name string) bool  { b, _ := g[name].(bool); return b }
func (g args) has(name string) bool   { _, ok := g[name]; return ok }

func (g args) int(name string, def int) int {
	if f, ok := g[name].(float64); ok && f > 0 {
		return int(f)
	}
	return def
}

// control runs a command of the sokki command.
func (a *app) control(ctx context.Context, req cli.Request) (any, error) {
	g := args(req.Args)
	var out any
	var err error
	switch req.Cmd {
	case "sync":
		return a.ctlSync(ctx)
	case "signin":
		return a.ctlSignIn(ctx, g.bool("wait"))
	case "history.import":
		// Reading every source may take a while: not on the main thread.
		added := a.history.Scan(a.historyEnv())
		a.onMainWait(func() {
			a.palette.invalidate()
			a.settings.invalidate()
		})
		return map[string]any{"new": added, "prompts": len(a.history.Entries())}, nil
	}
	a.onMainWait(func() { out, err = a.controlMain(req.Cmd, g) })
	return out, err
}

// controlMain runs a command that is done at once, on the main thread.
func (a *app) controlMain(cmd string, g args) (any, error) {
	switch cmd {
	case "status":
		return a.ctlStatus(), nil
	case "pause", "resume":
		a.setEnabled(cmd == "resume")
		return a.ctlStatus(), nil
	case "files":
		return a.ctlFiles(), nil
	case "match.list":
		return a.ctlMatches(g), nil
	case "match.show":
		m, err := a.findMatch(g.str("key"))
		if err != nil {
			return nil, err
		}
		return a.matchOut(m), nil
	case "match.add":
		return a.ctlAdd(g)
	case "match.edit":
		return a.ctlEdit(g)
	case "match.duplicate":
		return a.ctlDuplicate(g)
	case "match.enable", "match.disable":
		m, err := a.findMatch(g.str("key"))
		if err != nil {
			return nil, err
		}
		m.Enabled = cmd == "match.enable"
		a.store.Update(m)
		return a.matchOut(m), nil
	case "match.remove":
		m, err := a.findMatch(g.str("key"))
		if err != nil {
			return nil, err
		}
		a.store.Remove(m.ID)
		return map[string]any{"removed": a.matchOut(m)}, nil
	case "search":
		return a.ctlSearch(g.str("query"), g.int("limit", 20)), nil
	case "render", "copy":
		m, err := a.findMatch(g.str("key"))
		if err != nil {
			return nil, err
		}
		text := engine.Render(m.Replace, func() string { s, _ := macos.PasteboardString(); return s }, time.Now())
		if cmd == "copy" {
			copyText(text)
			a.used(m)
			return map[string]any{"copied": matchTitle(m), "text": text}, nil
		}
		return map[string]any{"title": matchTitle(m), "text": text}, nil
	case "config.get":
		return a.ctlConfigGet(g.str("key"))
	case "config.set":
		return a.ctlConfigSet(g.str("key"), g.str("value"))
	case "privacy.get":
		return a.ctlPrivacy(), nil
	case "privacy.set":
		on := g.bool("on")
		a.prefs.Update(func(p *prefs.Prefs) {
			if g.str("key") == "crash-reports" {
				p.CrashReports = &on
			} else {
				p.DailyPing = &on
			}
		})
		a.settings.invalidate()
		return a.ctlPrivacy(), nil
	case "privacy.reset-id":
		a.prefs.ResetInstallID()
		a.settings.invalidate()
		return a.ctlPrivacy(), nil
	case "history.search":
		return a.ctlHistorySearch(g.str("query"), g.int("limit", 20)), nil
	case "history.show", "history.copy", "history.hide", "history.unhide":
		return a.ctlHistoryEntry(cmd, g.str("id"))
	case "history.sources":
		return a.ctlHistorySources(), nil
	case "history.source":
		return a.ctlHistorySource(g.str("source"), g.bool("on"))
	case "history.clear":
		a.history.Clear(g.bool("reimport"))
		a.palette.invalidate()
		a.settings.invalidate()
		return map[string]any{"cleared": true, "reimport": g.bool("reimport")}, nil
	case "insights":
		return a.ctlInsights(), nil
	case "insights.clear":
		a.usage.Clear()
		a.settings.invalidate()
		return a.ctlInsights(), nil
	case "activity":
		return a.ctlActivity(g.bool("reveal")), nil
	case "account":
		return a.ctlAccount(), nil
	case "signout":
		if !a.cloud.signedIn() {
			return nil, errors.New("Sokki isn’t signed in")
		}
		a.cloud.signOut()
		a.cloud.changedState()
		return a.ctlAccount(), nil
	case "reload":
		a.reloadConfig()
		return a.ctlStatus(), nil
	case "log":
		return map[string]any{"log": logx.File(), "config_dir": string(a.dir)}, nil
	case "restart":
		// Answered first: the command hears back before Sokki goes.
		time.AfterFunc(200*time.Millisecond, func() { a.post(a.relaunch) })
		return map[string]any{"restarting": true}, nil
	case "quit":
		time.AfterFunc(200*time.Millisecond, func() { a.post(mygo.App.Quit) })
		return map[string]any{"quitting": true}, nil
	}
	return nil, fmt.Errorf("unknown command %q: this Sokki may be older than the sokki command", cmd)
}

func (a *app) ctlStatus() map[string]any {
	prompts := 0
	for _, m := range a.store.Matches {
		if m.Trigger == "" {
			prompts++
		}
	}
	out := map[string]any{
		"status":        a.status().text,
		"on":            a.status().on,
		"enabled":       a.store.Config.Enabled,
		"accessibility": a.trusted,
		"secure_input":  a.secure,
		"matches":       len(a.store.Matches) - prompts,
		"prompts":       prompts,
		"config_dir":    string(a.dir),
		"version":       mygo.App.Version(),
	}
	if a.store.LastError != "" {
		out["error"] = a.store.LastError
	}
	if name := a.cloud.account(); name != "" {
		out["account"] = name
	}
	return out
}

// matchOut is a match as the command shows it.
func (a *app) matchOut(m model.Match) map[string]any {
	kind := "trigger"
	if m.Trigger == "" {
		kind = "prompt"
	}
	out := map[string]any{
		"key": m.Key(), "kind": kind, "title": matchTitle(m), "replace": m.Replace,
		"enabled": m.Enabled, "file": fileOf(m.SourceFile),
	}
	if m.Trigger != "" {
		out["trigger"] = m.Trigger
	}
	if m.Label != "" {
		out["label"] = m.Label
	}
	if t, ok := a.recency.Last(m.Key()); ok {
		out["last_used"] = t
	}
	return out
}

// fileOf is a match's file, default.yml for none.
func fileOf(f string) string {
	if f == "" {
		return config.DefaultMatchFile
	}
	return f
}

// findMatch is the match of key: its trigger, or a prompt's title. A key
// taken by no match may be another's trigger with spaces trimmed, as the
// shell drops them.
func (a *app) findMatch(key string) (model.Match, error) {
	if key == "" {
		return model.Match{}, errors.New("missing the match's trigger or title")
	}
	if m, ok := a.store.WithKey(key); ok {
		return m, nil
	}
	for _, m := range a.store.Matches {
		if strings.TrimSpace(m.Key()) == strings.TrimSpace(key) {
			return m, nil
		}
	}
	return model.Match{}, fmt.Errorf("no match or prompt %q: see `sokki matches`", key)
}

func (a *app) ctlFiles() []map[string]any {
	counts := a.store.FileCounts()
	out := []map[string]any{}
	for _, f := range a.store.SourceFiles() {
		out = append(out, map[string]any{"file": f, "matches": counts[f]})
	}
	return out
}

func (a *app) ctlMatches(g args) []map[string]any {
	q := search.Parse(g.str("query"))
	out := []map[string]any{}
	for _, m := range a.store.Matches {
		switch {
		case g.str("file") != "" && fileOf(m.SourceFile) != existingFileOr(g.str("file"), a.store.SourceFiles()),
			g.bool("prompts") && m.Trigger != "",
			g.bool("triggers") && m.Trigger == "",
			g.bool("disabled") && m.Enabled:
			continue
		}
		if !q.Empty() {
			d := search.NewDoc(m.Trigger, m.Label, m.Replace, []string{"sokki", m.SourceFile}, nil)
			if _, ok := q.Score(&d); !ok {
				continue
			}
		}
		out = append(out, a.matchOut(m))
	}
	return out
}

// existingFileOr is the match file of files a name means, else the name
// as a file.
func existingFileOr(name string, files []string) string {
	if f := existingFile(name, files); f != "" {
		return f
	}
	return NormalizeFilename(name)
}

// matchFile is the file a name given for a match means, an error when it
// can't be one.
func (a *app) matchFile(name string) (string, error) {
	f := existingFileOr(name, a.store.SourceFiles())
	if f == "" {
		return "", fmt.Errorf("%q can't be a match file's name", name)
	}
	return f, nil
}

// checkMatch says why m can't be saved, as the editor won't save it; was
// is the match it edits, if any.
func (a *app) checkMatch(m model.Match, was *model.Match) error {
	trigger := strings.TrimSpace(m.Trigger)
	switch {
	case m.Replace == "":
		return errors.New("the text is empty")
	case m.Trigger != "" && utf8.RuneCountInString(trigger) < minTrigger && (was == nil || m.Trigger != was.Trigger):
		return fmt.Errorf("a trigger needs at least %d characters", minTrigger)
	case m.Key() == "":
		return errors.New("a prompt needs a label or text")
	}
	if other, ok := a.store.WithKey(m.Key()); ok && (was == nil || other.ID != was.ID) {
		if m.Trigger != "" {
			return fmt.Errorf("another match in %s has the trigger %q", fileOf(other.SourceFile), m.Trigger)
		}
		return fmt.Errorf("another match in %s is called %q: give this one a --label", fileOf(other.SourceFile), m.Key())
	}
	return nil
}

func (a *app) ctlAdd(g args) (any, error) {
	m := model.Match{
		Trigger: triggerOf(g.str("trigger")), Replace: g.str("replace"), Label: strings.TrimSpace(g.str("label")),
		Enabled: !g.bool("disabled"), SourceFile: config.DefaultMatchFile,
	}
	if g.bool("prompt") || m.Trigger == "" {
		m.SourceFile = config.PromptFile
	}
	if g.has("file") {
		f, err := a.matchFile(g.str("file"))
		if err != nil {
			return nil, err
		}
		m.SourceFile = f
	}
	if m.Trigger == "" && m.Label == "" {
		// As a captured prompt is labeled.
		m.Label = a.palette.uniqueLabel(promptLabel(m.Replace))
	}
	if err := a.checkMatch(m, nil); err != nil {
		return nil, err
	}
	m.ID = model.NewID()
	a.store.Add(m)
	return a.matchOut(m), nil
}

// edited is m with the fields g gives.
func (a *app) edited(m model.Match, g args) (model.Match, error) {
	if g.has("trigger") {
		m.Trigger = triggerOf(g.str("trigger"))
	}
	if g.has("label") {
		m.Label = strings.TrimSpace(g.str("label"))
	}
	if g.has("replace") {
		m.Replace = g.str("replace")
	}
	if g.has("file") {
		f, err := a.matchFile(g.str("file"))
		if err != nil {
			return m, err
		}
		m.SourceFile = f
	}
	return m, nil
}

func (a *app) ctlEdit(g args) (any, error) {
	was, err := a.findMatch(g.str("key"))
	if err != nil {
		return nil, err
	}
	m, err := a.edited(was, g)
	if err != nil {
		return nil, err
	}
	if err := a.checkMatch(m, &was); err != nil {
		return nil, err
	}
	a.store.Update(m)
	return a.matchOut(m), nil
}

func (a *app) ctlDuplicate(g args) (any, error) {
	was, err := a.findMatch(g.str("key"))
	if err != nil {
		return nil, err
	}
	m, err := a.edited(was, g)
	if err != nil {
		return nil, err
	}
	if !g.has("trigger") && !g.has("label") {
		if m.Trigger != "" {
			return nil, errors.New("a duplicate needs its own --trigger")
		}
		m.Label = a.palette.uniqueLabel(m.Key())
	}
	m.ID = 0
	if err := a.checkMatch(m, nil); err != nil {
		return nil, err
	}
	m.ID = model.NewID()
	a.store.Add(m)
	return a.matchOut(m), nil
}

// ctlSearch ranks the enabled matches for query as the palette does.
func (a *app) ctlSearch(query string, limit int) []map[string]any {
	var x index
	x.update(a)
	out := []map[string]any{}
	for _, f := range x.rank(search.Parse(query), scopeMatches, nil, nil, a.history.MatchUses(), time.Now()) {
		if len(out) == limit {
			break
		}
		out = append(out, a.matchOut(x.result(f.i).m))
	}
	return out
}

// settingKeys are the settings the command reads and sets, as
// config/default.yml names them, in the order Settings shows them.
var settingKeys = []string{
	"enable", "launch_at_login", "inject_delay", "conflict_resolve_delay_ms", "clipboard_threshold",
	"preserve_clipboard", "restore_clipboard_delay", "log_level",
	"palette_shortcut", "capture_shortcut", "toggle_shortcut", "hud_shortcut",
}

// openAtLogin and setOpenAtLogin are the app's, which tests replace: they
// wait for MyGo's main thread.
var openAtLogin, setOpenAtLogin = mygo.App.OpenAtLogin, mygo.App.SetOpenAtLogin

// intSetting is a number setting of the configuration, with the range
// Settings takes.
type intSetting struct {
	v      *int
	lo, hi int
}

func (a *app) intSettings() map[string]intSetting {
	c := &a.store.Config
	return map[string]intSetting{
		"inject_delay":              {&c.InjectDelay, 0, 1000},
		"conflict_resolve_delay_ms": {&c.ConflictResolveDelayMs, 50, 5000},
		"clipboard_threshold":       {&c.ClipboardThreshold, 0, 100000},
		"restore_clipboard_delay":   {&c.RestoreClipboardDelay, 0, 10000},
	}
}

func (a *app) choiceSettings() map[string]struct {
	v       *string
	options []option
} {
	c := &a.store.Config
	type choice = struct {
		v       *string
		options []option
	}
	return map[string]choice{
		"log_level":        {&c.LogLevel, []option{{"error", "Errors only"}, {"debug", "Debug"}}},
		"palette_shortcut": {&c.PaletteShortcut, paletteOptions},
		"capture_shortcut": {&c.CaptureShortcut, captureOptions},
		"toggle_shortcut":  {&c.ToggleShortcut, toggleOptions},
		"hud_shortcut":     {&c.HUDShortcut, toggleOptions},
	}
}

func (a *app) settingsOut() map[string]any {
	c := a.store.Config
	out := map[string]any{
		"enable":             c.Enabled,
		"launch_at_login":    openAtLogin(),
		"preserve_clipboard": c.PreserveClipboard,
	}
	for k, s := range a.intSettings() {
		out[k] = *s.v
	}
	for k, s := range a.choiceSettings() {
		out[k] = *s.v
	}
	return out
}

func (a *app) ctlConfigGet(key string) (any, error) {
	all := a.settingsOut()
	if key == "" {
		return all, nil
	}
	v, ok := all[key]
	if !ok {
		return nil, fmt.Errorf("no setting %q: it's one of %s", key, strings.Join(settingKeys, ", "))
	}
	return map[string]any{key: v}, nil
}

func (a *app) ctlConfigSet(key, value string) (any, error) {
	c := &a.store.Config
	onOff := func() (bool, error) {
		switch strings.ToLower(value) {
		case "on", "true", "yes", "1":
			return true, nil
		case "off", "false", "no", "0":
			return false, nil
		}
		return false, fmt.Errorf("%s takes on or off", key)
	}
	switch key {
	case "enable":
		on, err := onOff()
		if err != nil {
			return nil, err
		}
		a.setEnabled(on)
		return a.ctlConfigGet(key)
	case "preserve_clipboard":
		on, err := onOff()
		if err != nil {
			return nil, err
		}
		c.PreserveClipboard = on
	case "launch_at_login":
		on, err := onOff()
		if err != nil {
			return nil, err
		}
		if err := setOpenAtLogin(on); err != nil {
			return nil, fmt.Errorf("couldn't set launch at login: %w", err)
		}
		a.settings.invalidate()
		return a.ctlConfigGet(key)
	default:
		if s, ok := a.intSettings()[key]; ok {
			n, err := strconv.Atoi(value)
			if err != nil || n < s.lo || n > s.hi {
				return nil, fmt.Errorf("%s takes a number from %d to %d", key, s.lo, s.hi)
			}
			*s.v = n
		} else if s, ok := a.choiceSettings()[key]; ok {
			var values []string
			for _, o := range s.options {
				values = append(values, o.value)
			}
			if !slices.Contains(values, value) {
				return nil, fmt.Errorf("%s is one of %s", key, strings.Join(values, ", "))
			}
			*s.v = value
		} else {
			return nil, fmt.Errorf("no setting %q: it's one of %s", key, strings.Join(settingKeys, ", "))
		}
	}
	a.saveConfig()
	a.settings.invalidate()
	return a.ctlConfigGet(key)
}

func (a *app) ctlPrivacy() map[string]any {
	p := a.prefs.Get()
	return map[string]any{
		"crash_reports": p.CrashReportsOn(),
		"usage_stats":   p.DailyPingOn(),
		"anonymous_id":  a.prefs.InstallID(),
		// Only release builds send anything.
		"telemetry": a.telemetry != nil,
	}
}

func (a *app) ctlInsights() map[string]any {
	m := a.usage.Summary()
	now := time.Now()
	daily := []map[string]any{}
	for i, n := range m.Daily {
		daily = append(daily, map[string]any{"day": now.AddDate(0, 0, i-6).Format("2006-01-02"), "expansions": n})
	}
	counts := func(cs []usage.Count) []map[string]any {
		out := []map[string]any{}
		for _, c := range cs {
			out = append(out, map[string]any{"key": c.Key, "name": c.Name, "expansions": c.Count})
		}
		return out
	}
	used := map[string]bool{}
	for _, c := range m.Triggers {
		used[c.Key] = true
	}
	unused := []string{}
	for _, mt := range a.store.Matches {
		if mt.Enabled && !used[mt.Key()] {
			unused = append(unused, mt.Key())
		}
	}
	return map[string]any{
		"expansions": m.Total, "today": m.Daily[6], "active_days": m.ActiveDays(),
		"characters_saved": m.Saved, "time_saved": typingTime(m.Saved), "from_palette": m.Palette,
		"daily": daily, "matches": counts(m.Triggers), "apps": counts(m.Apps), "unused": unused,
		"agents": counts(agentCounts(m.Agents)),
	}
}

func (a *app) ctlActivity(reveal bool) []map[string]any {
	out := []map[string]any{}
	for _, ev := range a.hud.events {
		typed := ev.Buffer
		if !reveal {
			typed = masked(typed)
		}
		row := map[string]any{"time": ev.Time, "verdict": verdictLabel(ev.Verdict), "typed": typed}
		if ev.MatchedTrigger != "" {
			row["trigger"] = ev.MatchedTrigger
		}
		if t := a.hud.titles[ev.ID]; t != "" {
			row["title"] = t
		}
		if ev.Note != "" {
			row["note"] = ev.Note
		}
		out = append(out, row)
	}
	return out
}

func (a *app) ctlAccount() map[string]any {
	c := a.cloud
	out := map[string]any{"signed_in": c.signedIn(), "syncing": c.busy > 0}
	if c.signedIn() {
		st := c.engine.State()
		out["name"], out["email"] = st.Name, st.Email
	}
	if !c.lastPull.IsZero() {
		out["last_sync"] = c.lastPull
	}
	if c.failure != "" {
		out["failure"] = c.failure
	}
	if c.notice != "" {
		out["notice"] = c.notice
	}
	if si := c.signin; si != nil && si.code != "" {
		out["signin"] = map[string]any{"code": si.code, "url": si.url, "expires": si.expires}
	}
	return out
}

// ctlSync syncs now, as Sync Now does, and waits for it.
func (a *app) ctlSync(ctx context.Context) (any, error) {
	var err error
	a.onMainWait(func() {
		if !a.cloud.signedIn() {
			err = errors.New("Sokki isn’t signed in: run `sokki signin`")
			return
		}
		a.cloud.sync(cloudsync.Ask, true)
	})
	if err != nil {
		return nil, err
	}
	if err := a.waitMain(ctx, func() bool { return a.cloud.busy == 0 }); err != nil {
		return nil, err
	}
	var out map[string]any
	a.onMainWait(func() {
		out = a.ctlAccount()
		if a.cloud.failure != "" {
			err = errors.New("couldn't sync: " + a.cloud.failure)
		}
	})
	return out, err
}

// ctlSignIn starts signing in, as Settings' Sign In does, and returns its
// code once Mon gives it, or with wait the account once it's approved.
func (a *app) ctlSignIn(ctx context.Context, wait bool) (any, error) {
	var err error
	a.onMainWait(func() {
		if a.cloud.signedIn() {
			err = fmt.Errorf("Sokki is signed in as %s already", a.cloud.account())
			return
		}
		a.cloud.signIn()
	})
	if err != nil {
		return nil, err
	}
	done := func() bool { return a.cloud.signin == nil || a.cloud.signin.code != "" }
	if wait {
		done = func() bool { return a.cloud.signin == nil }
	}
	if err := a.waitMain(ctx, done); err != nil {
		return nil, err
	}
	var out map[string]any
	a.onMainWait(func() {
		out = a.ctlAccount()
		if a.cloud.signin == nil && !a.cloud.signedIn() {
			err = errors.New(noticeOr(a.cloud.notice))
		}
	})
	return out, err
}

func noticeOr(n string) string {
	if n == "" {
		return "the sign-in didn't finish"
	}
	return n
}

// entryID names a prompt of the history for the sokki command: the start
// of a hash of its text, the same across launches.
func entryID(e history.Entry) string {
	sum := sha1.Sum([]byte(history.Normalize(e.Text)))
	return hex.EncodeToString(sum[:])[:8]
}

func (a *app) entryOut(e history.Entry) map[string]any {
	agents := make([]string, len(e.Sources))
	for i, s := range e.Sources {
		agents[i] = history.SourceName(s)
	}
	out := map[string]any{
		"id": entryID(e), "title": e.Title(), "text": e.Text, "agents": agents,
		"sent": e.Count, "from_sokki": e.Recalls,
		"first": time.Unix(e.First, 0).Format(time.RFC3339), "last": time.Unix(e.Last, 0).Format(time.RFC3339),
	}
	if len(e.Projects) > 0 {
		out["projects"] = e.Projects
	}
	return out
}

// findEntry finds the prompt of the history whose ID starts with id, the
// hidden ones too.
func (a *app) findEntry(id string, hidden bool) (history.Entry, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return history.Entry{}, errors.New("missing the prompt's id: see sokki history search")
	}
	var found []history.Entry
	for _, e := range a.history.All() {
		if (hidden || !e.Hidden) && strings.HasPrefix(entryID(e), id) {
			found = append(found, e)
		}
	}
	switch len(found) {
	case 0:
		return history.Entry{}, fmt.Errorf("no prompt of the history has the id %q", id)
	case 1:
		return found[0], nil
	}
	return history.Entry{}, fmt.Errorf("%d prompts have an id starting %q: give more of it", len(found), id)
}

// ctlHistorySearch ranks the history for query as the palette does.
func (a *app) ctlHistorySearch(query string, limit int) []map[string]any {
	var x index
	x.update(a)
	out := []map[string]any{}
	for _, f := range x.rank(search.Parse(query), scopeHistory, nil, nil, nil, time.Now()) {
		if len(out) == limit {
			break
		}
		out = append(out, a.entryOut(x.result(f.i).e))
	}
	return out
}

func (a *app) ctlHistoryEntry(cmd, id string) (any, error) {
	e, err := a.findEntry(id, cmd == "history.unhide")
	if err != nil {
		return nil, err
	}
	switch cmd {
	case "history.copy":
		copyText(e.Text)
		a.history.Recall(e.ID, "")
		return map[string]any{"copied": e.Title(), "text": e.Text}, nil
	case "history.hide":
		a.history.Hide(e.ID)
	case "history.unhide":
		a.history.Unhide(e.ID)
	}
	if cmd != "history.show" {
		a.palette.invalidate()
		a.settings.invalidate()
		e, _ = a.history.Entry(e.ID)
		out := a.entryOut(e)
		out["hidden"] = e.Hidden
		return out, nil
	}
	return a.entryOut(e), nil
}

func (a *app) ctlHistorySources() map[string]any {
	st := a.history.Settings()
	counts := a.history.Counts()
	var sources []map[string]any
	for _, src := range history.Sources {
		sources = append(sources, map[string]any{
			"id": src.ID, "name": src.Name, "on": st.On(src.ID), "prompts": counts[src.ID], "reads": src.Where,
		})
	}
	out := map[string]any{"sources": sources, "prompts": len(a.history.Entries())}
	if t := a.history.Imported(); !t.IsZero() {
		out["read"] = t.Format(time.RFC3339)
	}
	return out
}

func (a *app) ctlHistorySource(id string, on bool) (any, error) {
	if !slices.ContainsFunc(history.Sources, func(s history.Source) bool { return s.ID == id }) {
		ids := make([]string, len(history.Sources))
		for i, s := range history.Sources {
			ids[i] = s.ID
		}
		return nil, fmt.Errorf("unknown agent %q: one of %s", id, strings.Join(ids, ", "))
	}
	a.setSource(id, on)
	return a.ctlHistorySources(), nil
}

// setSource turns the reading of the source of id on or off.
func (a *app) setSource(id string, on bool) {
	st := a.history.Settings()
	st.Off = slices.DeleteFunc(st.Off, func(s string) bool { return s == id })
	if !on {
		st.Off = append(st.Off, id)
	}
	a.history.SetSettings(st)
	if on {
		a.scanHistory()
	}
	a.palette.invalidate()
	a.settings.invalidate()
}
