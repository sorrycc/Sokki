// Package cloudsync syncs Sokki's configuration and matches with the
// person's Mon account, as one settings document of Mon's:
//
//	{"config": {"clipboard_threshold": 50, …},
//	 "matches": [{"file": "default.yml", "trigger": ":hello", "replace": "Hi there!", "enabled": true}, …]}
//
// They merge against the snapshot of the last sync, the base: each key of
// config/default.yml and each match, by its file and trigger, on its own.
// One changed on one side only takes that side's, and one changed on both
// the side that syncs now.
package cloudsync

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"sokki/internal/model"
	"sokki/internal/mon"
)

// App is Sokki's client id with Mon, and its settings document's name.
const App = "sokki"

// Match is a match as it syncs.
type Match struct {
	File    string `json:"file"`
	Trigger string `json:"trigger"`
	Replace string `json:"replace"`
	Label   string `json:"label,omitempty"`
	Enabled bool   `json:"enabled"`
}

func (m Match) key() string { return m.File + "\x00" + m.Trigger }

// Snapshot is what syncs: the keys of config/default.yml, and the matches
// sorted by file, in the order of each file.
type Snapshot struct {
	Config  map[string]json.RawMessage `json:"config,omitempty"`
	Matches []Match                    `json:"matches,omitempty"`
}

// FromModel is the snapshot of a configuration and its matches.
func FromModel(c model.Config, matches []model.Match) Snapshot {
	s := Snapshot{Config: configMap(c)}
	for _, m := range matches {
		s.Matches = append(s.Matches, Match{File: m.SourceFile, Trigger: m.Trigger, Replace: m.Replace, Label: m.Label, Enabled: m.Enabled})
	}
	byFile(s.Matches)
	return s
}

// byFile sorts matches by their file, as the files hold them, keeping
// their order within each.
func byFile(ms []Match) {
	slices.SortStableFunc(ms, func(a, b Match) int { return strings.Compare(a.File, b.File) })
}

// Model is the configuration and the matches of s. Keys of the
// configuration this version of Sokki does not have are left out.
func (s Snapshot) Model() (model.Config, []model.Match) {
	c := model.DefaultConfig()
	values := map[string]any{}
	for k, raw := range s.Config {
		var v any
		if json.Unmarshal(raw, &v) == nil {
			values[k] = v
		}
	}
	if data, err := yaml.Marshal(values); err == nil {
		yaml.Unmarshal(data, &c)
	}
	var matches []model.Match
	for _, m := range s.Matches {
		matches = append(matches, model.Match{
			ID: model.NewID(), Trigger: m.Trigger, Replace: m.Replace, Label: m.Label, Enabled: m.Enabled, SourceFile: m.File,
		})
	}
	return c, matches
}

// configMap is c by its keys in config/default.yml.
func configMap(c model.Config) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	data, err := yaml.Marshal(c)
	if err != nil {
		return out
	}
	var values map[string]any
	yaml.Unmarshal(data, &values)
	for k, v := range values {
		if raw, err := json.Marshal(v); err == nil {
			out[k] = raw
		}
	}
	return out
}

// configKeys are the keys of config/default.yml this version of Sokki has.
var configKeys = slices.Collect(maps.Keys(configMap(model.DefaultConfig())))

// Equal reports whether s and o hold the same configuration and matches,
// in the same order.
func (s Snapshot) Equal(o Snapshot) bool {
	return maps.EqualFunc(s.Config, o.Config, sameJSON) && slices.Equal(s.Matches, o.Matches)
}

func (s Snapshot) clone() Snapshot {
	s.Config = maps.Clone(s.Config)
	s.Matches = slices.Clone(s.Matches)
	return s
}

func sameJSON(a, b json.RawMessage) bool {
	var va, vb any
	if json.Unmarshal(a, &va) != nil || json.Unmarshal(b, &vb) != nil {
		return string(a) == string(b)
	}
	ca, _ := json.Marshal(va)
	cb, _ := json.Marshal(vb)
	return string(ca) == string(cb)
}

// Merge merges what changed here since base, local, with what changed in
// the account, remote. Where both changed a key or a match, local's
// stays. The matches keep remote's order, with those only local has after
// the match they follow there, and sorted by file.
func Merge(base, local, remote Snapshot) Snapshot {
	merged := Snapshot{Config: map[string]json.RawMessage{}}
	for _, k := range union(base.Config, local.Config, remote.Config) {
		side := remote.Config
		if changed(base.Config, local.Config, k) {
			side = local.Config
		}
		if v, ok := side[k]; ok {
			merged.Config[k] = v
		}
	}

	b, l, r := byKey(base.Matches), byKey(local.Matches), byKey(remote.Matches)
	keep := map[string]Match{}
	for _, k := range union(b, l, r) {
		side := r
		if changedMatch(b, l, k) {
			side = l
		}
		if m, ok := side[k]; ok {
			keep[k] = m
		}
	}
	placed := map[string]bool{}
	for _, m := range remote.Matches {
		if k := m.key(); !placed[k] {
			if v, ok := keep[k]; ok {
				merged.Matches = append(merged.Matches, v)
				placed[k] = true
			}
		}
	}
	at := 0
	for _, m := range local.Matches {
		k := m.key()
		if placed[k] {
			at = slices.IndexFunc(merged.Matches, func(x Match) bool { return x.key() == k }) + 1
			continue
		}
		if v, ok := keep[k]; ok {
			merged.Matches = slices.Insert(merged.Matches, at, v)
			placed[k] = true
			at++
		}
	}
	byFile(merged.Matches)
	return merged
}

func union[V any](ms ...map[string]V) []string {
	var keys []string
	for _, m := range ms {
		for k := range m {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

func byKey(ms []Match) map[string]Match {
	out := make(map[string]Match, len(ms))
	for _, m := range ms {
		out[m.key()] = m
	}
	return out
}

// changed reports whether the key of to differs from that of from, as
// being set in one only.
func changed(from, to map[string]json.RawMessage, k string) bool {
	a, aok := from[k]
	b, bok := to[k]
	return aok != bok || aok && !sameJSON(a, b)
}

func changedMatch(from, to map[string]Match, k string) bool {
	a, aok := from[k]
	b, bok := to[k]
	return aok != bok || a != b
}

// Local is this machine's configuration and matches. The methods are
// called on the engine's goroutine.
type Local interface {
	// Read returns the configuration and matches as they are now, or why
	// they can't sync, as a match file that doesn't parse.
	Read() (Snapshot, error)
	// Apply makes s the configuration and matches. Where they are no
	// longer read, as Read returned them, it changes nothing and fails
	// with ErrStale.
	Apply(read, s Snapshot) error
	// IsDefault reports whether s is what a first launch has, which a
	// first sync replaces without asking.
	IsDefault(s Snapshot) bool
}

// State is what the engine keeps between launches, in sync.json: the
// account, and the version of the account's document and the snapshot of
// the last sync. Base is nil until the first sync.
type State struct {
	UserID string `json:"userId"`
	Name   string `json:"name,omitempty"`
	Email  string `json:"email,omitempty"`
	// Avatar is the URL of the person's picture.
	Avatar  string    `json:"avatar,omitempty"`
	Version int64     `json:"version"`
	Base    *Snapshot `json:"base,omitempty"`
	// SyncedAt is when this machine and the account last agreed.
	SyncedAt time.Time `json:"syncedAt,omitzero"`
}

// Choice is how a first sync goes, where both this machine and the
// account have matches of their own.
type Choice int

const (
	// Ask fails a first sync with ErrNeedsChoice when both sides have
	// matches of their own.
	Ask Choice = iota
	// UseAccount replaces this machine's with the account's.
	UseAccount
	// UseLocal replaces the account's with this machine's.
	UseLocal
)

// ErrNeedsChoice is what a first sync fails with when this machine and
// the account both have matches of their own: sync again with UseAccount
// or UseLocal.
var ErrNeedsChoice = errors.New("cloudsync: this Mac and the account both have matches")

// ErrStale is what Apply fails with when this machine's matches changed
// since they were read, as the person edited one as a sync ran. A sync
// reads them again and merges anew, staleTries times at most, and fails
// with it then: the change syncs as it is pushed.
var ErrStale = errors.New("cloudsync: this Mac's matches changed as they synced")

const staleTries = 3

// Result says what a sync did.
type Result struct {
	// Applied is set when the account's changes applied here, Pushed when
	// this machine's were saved to the account.
	Applied, Pushed bool
}

// Engine syncs Local with the account. Its methods may be called from any
// goroutine; one sync runs at a time.
type Engine struct {
	Client *mon.Client
	Local  Local
	path   string

	mu    sync.Mutex // held through a sync
	smu   sync.Mutex // guards state
	state State
}

// New returns an engine whose state is kept at path, as it last was.
func New(client *mon.Client, local Local, path string) *Engine {
	e := &Engine{Client: client, Local: local, path: path}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &e.state)
	}
	return e
}

// State returns the engine's state.
func (e *Engine) State() State {
	e.smu.Lock()
	defer e.smu.Unlock()
	return e.state
}

// SignedIn starts syncing for the person u, whose token the client has,
// with a first sync to come.
func (e *Engine) SignedIn(u *mon.User) error {
	return e.setState(State{UserID: u.ID, Name: u.Name, Email: u.Email, Avatar: u.AvatarURL})
}

// Refreshed takes the person's name, email and picture as the account
// has them now, keeping the rest.
func (e *Engine) Refreshed(u *mon.User) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.State()
	if st.UserID != u.ID || st.Name == u.Name && st.Email == u.Email && st.Avatar == u.AvatarURL {
		return nil
	}
	st.Name, st.Email, st.Avatar = u.Name, u.Email, u.AvatarURL
	return e.setState(st)
}

// Forget forgets the account and the last sync, as the person signs out.
func (e *Engine) Forget() {
	e.smu.Lock()
	e.state = State{}
	e.smu.Unlock()
	os.Remove(e.path)
}

func (e *Engine) setState(st State) error {
	e.smu.Lock()
	e.state = st
	e.smu.Unlock()
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(e.path), 0o755)
	tmp := e.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, e.path)
}

// Push saves this machine's changes to the account, unless there are none
// since the last sync: then it does not ask the account at all.
func (e *Engine) Push(ctx context.Context) (Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.State()
	if st.Base == nil {
		return Result{}, nil
	}
	local, err := e.Local.Read()
	if err != nil {
		return Result{}, err
	}
	if withUnknown(*st.Base, local).Equal(*st.Base) {
		return Result{}, nil
	}
	return e.sync(ctx, Ask)
}

// Sync fetches the account's settings, merges them with this machine's
// and saves the merge on both sides. A first sync goes as choice says.
// mon.ErrUnauthorized means the person signed out elsewhere, as from the
// account page: the engine has forgotten the account.
func (e *Engine) Sync(ctx context.Context, choice Choice) (Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sync(ctx, choice)
}

func (e *Engine) sync(ctx context.Context, choice Choice) (Result, error) {
	st := e.State()
	if st.UserID == "" {
		return Result{}, mon.ErrUnauthorized
	}
	remote, version, err := e.fetch(ctx)
	if err != nil {
		return Result{}, err
	}
	local, err := e.Local.Read()
	if err != nil {
		return Result{}, err
	}
	var base Snapshot
	if st.Base != nil {
		base = *st.Base
	} else {
		// The first sync: the side that has nothing of its own takes the
		// other's, else the one chosen wins.
		switch {
		case version == 0 || choice == UseLocal:
			base = remote
		case e.Local.IsDefault(local) || choice == UseAccount:
			base = local
		default:
			return Result{}, ErrNeedsChoice
		}
	}
	// This machine's changes are those of local since base, which merge
	// with each version of the account's this sync gets.
	mine := withUnknown(base, local)
	var res Result
	var applied *Snapshot
	stale := 0
	for {
		merged := Merge(base, mine, remote)
		now := mine
		if applied != nil {
			now = *applied
		}
		if !merged.Equal(now) {
			err := e.Local.Apply(local, merged.clone())
			if errors.Is(err, ErrStale) && stale < staleTries {
				// The person edited a match since it was read: merge that
				// change too, rather than undo it.
				stale++
				fresh, err := e.Local.Read()
				if err != nil {
					return res, err
				}
				mine, local = Merge(local, fresh, mine), fresh
				continue
			}
			if err != nil {
				return res, err
			}
			res.Applied = true
			applied = &merged
			// As applied, which a later Apply of this sync checks against.
			if local, err = e.Local.Read(); err != nil {
				return res, err
			}
		}
		if merged.Equal(remote) && version > 0 {
			st.Version, st.Base, st.SyncedAt = version, &merged, time.Now()
			return res, e.setState(st)
		}
		saved, err := e.Client.PutSettings(ctx, App, version, merged)
		var conflictErr *mon.ConflictError
		if errors.As(err, &conflictErr) {
			// Another machine saved since: merge this machine's changes
			// again, with what it saved.
			if remote, err = decode(conflictErr.Current.Data); err != nil {
				return res, err
			}
			version = conflictErr.Current.Version
			continue
		}
		if err != nil {
			return res, e.signedOut(err)
		}
		res.Pushed = true
		st.Version, st.Base, st.SyncedAt = saved.Version, &merged, time.Now()
		return res, e.setState(st)
	}
}

// withUnknown returns local with the keys of base's configuration this
// version of Sokki does not have, as unchanged, rather than as removed.
func withUnknown(base, local Snapshot) Snapshot {
	out := local
	out.Config = maps.Clone(local.Config)
	if out.Config == nil {
		out.Config = map[string]json.RawMessage{}
	}
	for k, v := range base.Config {
		if _, ok := out.Config[k]; !ok && !slices.Contains(configKeys, k) {
			out.Config[k] = v
		}
	}
	return out
}

// fetch returns the account's settings and their version, 0 for none.
func (e *Engine) fetch(ctx context.Context) (Snapshot, int64, error) {
	st, err := e.Client.GetSettings(ctx, App)
	if err != nil {
		return Snapshot{}, 0, e.signedOut(err)
	}
	s, err := decode(st.Data)
	return s, st.Version, err
}

func decode(data json.RawMessage) (Snapshot, error) {
	var s Snapshot
	if len(data) == 0 || string(data) == "null" {
		return s, nil
	}
	err := json.Unmarshal(data, &s)
	return s, err
}

// signedOut forgets the account when err says its token is no more.
func (e *Engine) signedOut(err error) error {
	if errors.Is(err, mon.ErrUnauthorized) {
		e.Forget()
	}
	return err
}
