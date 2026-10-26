package cloudsync

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"sokki/internal/model"
	"sokki/internal/mon"
)

// fakeMon is Mon's settings API for one person, as Mon answers it.
type fakeMon struct {
	mu      sync.Mutex
	version int64
	data    json.RawMessage
	revoked bool
	gets    int
	// beforePut runs before a PUT is answered, as another machine saving.
	beforePut func()
}

func (f *fakeMon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	if f.revoked {
		f.mu.Unlock()
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Method == "PUT" && f.beforePut != nil {
		hook := f.beforePut
		f.beforePut = nil
		f.mu.Unlock()
		hook()
		f.mu.Lock()
	}
	defer f.mu.Unlock()
	doc := func() map[string]any {
		return map[string]any{"app": App, "version": f.version, "data": f.data}
	}
	switch r.Method {
	case "GET":
		f.gets++
		json.NewEncoder(w).Encode(doc())
	case "PUT":
		var body struct {
			BaseVersion int64           `json:"base_version"`
			Data        json.RawMessage `json:"data"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.BaseVersion != f.version {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]any{"error": "version_conflict", "current": doc()})
			return
		}
		f.version++
		f.data = body.Data
		json.NewEncoder(w).Encode(doc())
	}
}

// save saves s as another machine would.
func (f *fakeMon) save(s Snapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data, _ = json.Marshal(s)
	f.version++
}

func (f *fakeMon) snapshot(t *testing.T) Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := decode(f.data)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// fakeLocal is a machine's configuration and matches.
type fakeLocal struct {
	s       Snapshot
	applies int
	// beforeApply runs before an Apply, as the person editing a match as
	// a sync runs.
	beforeApply func()
}

func (l *fakeLocal) Read() (Snapshot, error) { return l.s.clone(), nil }

func (l *fakeLocal) Apply(read, s Snapshot) error {
	if hook := l.beforeApply; hook != nil {
		l.beforeApply = nil
		hook()
	}
	if !l.s.Equal(read) {
		return ErrStale
	}
	l.s = s
	l.applies++
	return nil
}

func (l *fakeLocal) IsDefault(s Snapshot) bool { return len(s.Matches) == 0 }

func m(trigger, replace string) Match {
	return Match{File: "default.yml", Trigger: trigger, Replace: replace, Enabled: true}
}

func snap(ms ...Match) Snapshot {
	return Snapshot{Config: configMap(model.DefaultConfig()), Matches: ms}
}

func newEngine(t *testing.T, f *fakeMon, l *fakeLocal) *Engine {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := mon.New(srv.URL)
	c.Token = "mon_test"
	e := New(c, l, filepath.Join(t.TempDir(), "sync.json"))
	if err := e.SignedIn(&mon.User{ID: "u1", Name: "Ada"}); err != nil {
		t.Fatal(err)
	}
	return e
}

func triggers(s Snapshot) []string {
	var out []string
	for _, m := range s.Matches {
		out = append(out, m.Trigger+"="+m.Replace)
	}
	return out
}

func TestModelRoundTrips(t *testing.T) {
	c := model.DefaultConfig()
	c.ClipboardThreshold, c.PaletteShortcut = 80, "opt+space"
	ms := []model.Match{{Trigger: ":a", Replace: "A\nB", Label: "a", Enabled: false, SourceFile: "work.yml"}}
	s := FromModel(c, ms)
	data, _ := json.Marshal(s)
	back, _ := decode(data)
	gc, gm := back.Model()
	if gc != c {
		t.Errorf("config = %+v, want %+v", gc, c)
	}
	if len(gm) != 1 || gm[0].Trigger != ":a" || gm[0].Replace != "A\nB" || gm[0].Label != "a" || gm[0].Enabled || gm[0].SourceFile != "work.yml" {
		t.Errorf("matches = %+v", gm)
	}
	if !back.Equal(s) {
		t.Errorf("%+v != %+v", back, s)
	}
}

func TestMerge(t *testing.T) {
	base := snap(m(":a", "1"), m(":b", "2"), m(":c", "3"))
	local := snap(m(":a", "1"), m(":new", "here"), m(":b", "local"))
	remote := snap(m(":a", "remote"), m(":b", "2"), m(":c", "3"), m(":d", "4"))
	local.Config["clipboard_threshold"] = json.RawMessage("80")
	remote.Config["inject_delay"] = json.RawMessage("5")
	got := Merge(base, local, remote)
	want := []string{":a=remote", ":new=here", ":b=local", ":d=4"}
	if !slices.Equal(triggers(got), want) {
		t.Errorf("matches = %q, want %q", triggers(got), want)
	}
	if string(got.Config["clipboard_threshold"]) != "80" || string(got.Config["inject_delay"]) != "5" {
		t.Errorf("config = %s", got.Config)
	}

	// Both changed :b: this side's stays.
	remote.Matches[1].Replace = "remote"
	if got := Merge(base, local, remote); got.Matches[2].Replace != "local" {
		t.Errorf("matches = %q", triggers(got))
	}
}

func TestFirstSyncUploadsToAnEmptyAccount(t *testing.T) {
	f := &fakeMon{}
	l := &fakeLocal{s: snap(m(":a", "1"))}
	e := newEngine(t, f, l)
	res, err := e.Sync(context.Background(), Ask)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Pushed || res.Applied {
		t.Errorf("result = %+v", res)
	}
	if got := f.snapshot(t); !got.Equal(l.s) {
		t.Errorf("account = %+v, want %+v", got, l.s)
	}
	if st := e.State(); st.Version != 1 || st.Base == nil || st.SyncedAt.IsZero() {
		t.Errorf("state = %+v", st)
	}
}

func TestFirstSyncOfADefaultMachineTakesTheAccounts(t *testing.T) {
	f := &fakeMon{}
	f.save(snap(m(":a", "1")))
	l := &fakeLocal{s: snap()}
	e := newEngine(t, f, l)
	res, err := e.Sync(context.Background(), Ask)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied || res.Pushed || !slices.Equal(triggers(l.s), []string{":a=1"}) {
		t.Errorf("result = %+v, local %q", res, triggers(l.s))
	}
}

func TestFirstSyncAsksWhenBothHaveMatches(t *testing.T) {
	f := &fakeMon{}
	f.save(snap(m(":a", "1")))
	l := &fakeLocal{s: snap(m(":b", "2"))}
	e := newEngine(t, f, l)
	if _, err := e.Sync(context.Background(), Ask); !errors.Is(err, ErrNeedsChoice) {
		t.Fatalf("err = %v", err)
	}
	if _, err := e.Sync(context.Background(), UseLocal); err != nil {
		t.Fatal(err)
	}
	if got := f.snapshot(t); !slices.Equal(triggers(got), []string{":b=2"}) {
		t.Errorf("account = %q", triggers(got))
	}
}

func TestSyncMergesBothSides(t *testing.T) {
	f := &fakeMon{}
	l := &fakeLocal{s: snap(m(":a", "1"), m(":b", "2"))}
	e := newEngine(t, f, l)
	if _, err := e.Sync(context.Background(), Ask); err != nil {
		t.Fatal(err)
	}
	f.save(snap(m(":a", "remote"), m(":b", "2")))
	l.s.Matches = append(l.s.Matches, m(":c", "3"))
	res, err := e.Sync(context.Background(), Ask)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{":a=remote", ":b=2", ":c=3"}
	if !res.Applied || !res.Pushed || !slices.Equal(triggers(l.s), want) || !slices.Equal(triggers(f.snapshot(t)), want) {
		t.Errorf("result = %+v, local %q, account %q", res, triggers(l.s), triggers(f.snapshot(t)))
	}
}

func TestDifferentOrdersSettle(t *testing.T) {
	f := &fakeMon{}
	l := &fakeLocal{s: snap(m(":a", "1"), m(":b", "2"))}
	e := newEngine(t, f, l)
	if _, err := e.Sync(context.Background(), Ask); err != nil {
		t.Fatal(err)
	}
	// Another machine wrote the same matches the other way round: this
	// one takes its order rather than pushing its own back.
	f.save(snap(m(":b", "2"), m(":a", "1")))
	res, err := e.Sync(context.Background(), Ask)
	if err != nil || res.Pushed {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if want := []string{":b=2", ":a=1"}; !slices.Equal(triggers(l.s), want) {
		t.Errorf("local = %q", triggers(l.s))
	}
}

func TestPushWithoutChangesAsksNothing(t *testing.T) {
	f := &fakeMon{}
	l := &fakeLocal{s: snap(m(":a", "1"))}
	e := newEngine(t, f, l)
	if _, err := e.Sync(context.Background(), Ask); err != nil {
		t.Fatal(err)
	}
	gets := f.gets
	if res, err := e.Push(context.Background()); err != nil || res.Pushed || f.gets != gets {
		t.Errorf("result = %+v, %v, gets %d", res, err, f.gets-gets)
	}
	l.s.Matches[0].Replace = "changed"
	if res, err := e.Push(context.Background()); err != nil || !res.Pushed {
		t.Errorf("result = %+v, %v", res, err)
	}
}

func TestSyncRetriesAConflict(t *testing.T) {
	f := &fakeMon{}
	l := &fakeLocal{s: snap(m(":a", "1"))}
	e := newEngine(t, f, l)
	if _, err := e.Sync(context.Background(), Ask); err != nil {
		t.Fatal(err)
	}
	l.s.Matches = append(l.s.Matches, m(":b", "2"))
	f.beforePut = func() { f.save(snap(m(":a", "1"), m(":z", "26"))) }
	if _, err := e.Sync(context.Background(), Ask); err != nil {
		t.Fatal(err)
	}
	want := []string{":a=1", ":b=2", ":z=26"}
	if !slices.Equal(triggers(f.snapshot(t)), want) || !slices.Equal(triggers(l.s), want) {
		t.Errorf("account %q, local %q", triggers(f.snapshot(t)), triggers(l.s))
	}
}

func TestAnEditAsItSyncsStays(t *testing.T) {
	f := &fakeMon{}
	l := &fakeLocal{s: snap(m(":a", "1"))}
	e := newEngine(t, f, l)
	if _, err := e.Sync(context.Background(), Ask); err != nil {
		t.Fatal(err)
	}
	f.save(snap(m(":a", "1"), m(":b", "2")))
	l.beforeApply = func() { l.s.Matches[0].Replace = "edited" }
	if _, err := e.Sync(context.Background(), Ask); err != nil {
		t.Fatal(err)
	}
	want := []string{":a=edited", ":b=2"}
	if !slices.Equal(triggers(l.s), want) || !slices.Equal(triggers(f.snapshot(t)), want) {
		t.Errorf("local %q, account %q", triggers(l.s), triggers(f.snapshot(t)))
	}
}

func TestUnknownConfigStays(t *testing.T) {
	f := &fakeMon{}
	s := snap(m(":a", "1"))
	s.Config["future"] = json.RawMessage(`"yes"`)
	f.save(s)
	l := &fakeLocal{s: snap()}
	e := newEngine(t, f, l)
	if _, err := e.Sync(context.Background(), Ask); err != nil {
		t.Fatal(err)
	}
	// This Sokki can't keep it, but leaves it in the account.
	delete(l.s.Config, "future")
	l.s.Matches = append(l.s.Matches, m(":b", "2"))
	if _, err := e.Sync(context.Background(), Ask); err != nil {
		t.Fatal(err)
	}
	if got := f.snapshot(t); string(got.Config["future"]) != `"yes"` {
		t.Errorf("account config = %s", got.Config)
	}
}

func TestRevokedTokenForgetsTheAccount(t *testing.T) {
	f := &fakeMon{revoked: true}
	e := newEngine(t, f, &fakeLocal{s: snap()})
	if _, err := e.Sync(context.Background(), Ask); !errors.Is(err, mon.ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
	if e.State().UserID != "" {
		t.Error("account not forgotten")
	}
}

func TestStateLastsBetweenLaunches(t *testing.T) {
	f := &fakeMon{}
	l := &fakeLocal{s: snap(m(":a", "1"))}
	e := newEngine(t, f, l)
	if _, err := e.Sync(context.Background(), Ask); err != nil {
		t.Fatal(err)
	}
	again := New(e.Client, l, e.path)
	if st := again.State(); st.Name != "Ada" || st.Version != 1 || st.Base == nil || !st.Base.Equal(l.s) {
		t.Errorf("state = %+v", st)
	}
}
