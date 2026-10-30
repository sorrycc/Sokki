package main

import (
	"cmp"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sokki/internal/cloudsync"
	"sokki/internal/config"
	"sokki/internal/logx"
	"sokki/internal/mon"
)

// monURL is Mon's, the account behind settings sync, or SOKKI_MON_URL's,
// as a Mon on localhost to try it with.
func monURL() string {
	return cmp.Or(os.Getenv("SOKKI_MON_URL"), "https://mon.sorrycc.dev")
}

// tokenKey is the key of Mon's access token in the Keychain, and the name
// of the file that keeps it in development.
const tokenKey = "mon-token"

// syncFile is where settings sync keeps the account and the last sync, in
// the configuration directory.
const syncFile = "sync.json"

// pushDelay is how long settings sync waits after a change before saving
// it to the account, so that changes made together go together.
const pushDelay = 2 * time.Second

// pullEvery is how often settings sync asks the account for changes, and
// pullAfter how long it waits at least before asking again as Settings
// shows.
const (
	pullEvery = 5 * time.Minute
	pullAfter = 30 * time.Second
)

// quitPush is how long, at most, quitting waits to save a change made
// just before to the account.
const quitPush = 3 * time.Second

// syncRetryDelay is how long settings sync waits to try again after its
// n-th failure in a row, as the connection comes back.
func syncRetryDelay(n int) time.Duration {
	delays := []time.Duration{10 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute}
	if n < len(delays) {
		return delays[n]
	}
	return 5 * time.Minute
}

// errQuitting is what applying the account's matches fails with as the
// app quits: they apply at the next launch.
var errQuitting = errors.New("sokki is quitting")

// cloud syncs the configuration and the matches with the person's Mon
// account. Its fields are the main thread's, but for the engine.
type cloud struct {
	a      *app
	client *mon.Client
	engine *cloudsync.Engine
	// signin is the sign-in waiting for its approval, nil for none.
	signin *signin
	// asked is set once a first sync asked whose matches to keep, which a
	// sync of its own asks again only as the person syncs.
	asked    bool
	push     *time.Timer
	lastPull time.Time
	busy     int
	// failure says why the last sync failed, "" when it did not; retry
	// tries again at retryAt, after failures in a row.
	failure  string
	retry    *time.Timer
	retryAt  time.Time
	failures int
	// notice is what the Account page says of the last thing that
	// happened, as a sign-in that was declined.
	notice string
	// avatar is the person's picture, of avatarURL, once fetched.
	avatar    *ui.Bitmap
	avatarURL string
	quitting  atomic.Bool
	// tokenFile keeps the token in development, "" for the Keychain.
	tokenFile string
}

// signin is a sign-in waiting for the person to approve its code.
type signin struct {
	cancel    context.CancelFunc
	code, url string
	expires   time.Time
}

// newCloud makes settings sync of a's configuration directory, as it last
// was, without syncing.
func newCloud(a *app) *cloud {
	c := &cloud{a: a, client: mon.New(monURL())}
	if mygo.IsDev() {
		// Each development build signs anew, ad hoc, and the Keychain asks
		// again whether it may read what the last one kept.
		c.tokenFile = filepath.Join(string(a.dir), tokenKey)
	}
	c.engine = cloudsync.New(c.client, cloudLocal{c}, filepath.Join(string(a.dir), syncFile))
	if mygo.IsDev() {
		// Development keeps its matches out of those of the person's Macs.
		c.engine.Doc = cloudsync.App + "-dev"
	}
	return c
}

// start syncs with the account signed in, and every pullEvery after.
func (c *cloud) start() {
	go func() {
		for range time.Tick(pullEvery) {
			c.a.post(func() { c.sync(cloudsync.Ask, false) })
		}
	}()
	if c.engine.State().UserID == "" {
		return
	}
	go func() {
		token, err := c.loadToken()
		c.a.post(func() {
			if errors.Is(err, mygo.ErrSecretNotFound) {
				// The token is gone: the next sign-in starts anew.
				c.engine.Forget()
				c.changedState()
				return
			}
			if err != nil {
				logx.Errorf("Settings sync: can't read the token: %v", err)
				return
			}
			c.client.Token = string(token)
			c.sync(cloudsync.Ask, false)
			c.refreshProfile()
		})
	}()
}

// refreshProfile takes the person's name and picture as the account has
// them now, which they may have changed since signing in.
func (c *cloud) refreshProfile() {
	client := *c.client
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		u, err := client.Me(ctx)
		if err == nil && c.engine.Refreshed(u) == nil {
			c.a.post(c.changedState)
		}
	}()
}

// account is the name of the person settings sync syncs for, "" for no
// one.
func (c *cloud) account() string {
	if c == nil {
		return ""
	}
	st := c.engine.State()
	if st.UserID == "" {
		return ""
	}
	return cmp.Or(st.Name, st.Email, st.UserID)
}

// signedIn reports whether settings sync has an account and its token.
func (c *cloud) signedIn() bool {
	return c != nil && c.account() != "" && c.client.Token != ""
}

// changedState shows what changed in the tray's menu and in Settings.
func (c *cloud) changedState() {
	c.a.updateTray()
	c.a.settings.invalidate()
}

// changed saves this machine's matches to the account a moment after
// they change.
func (c *cloud) changed() {
	if !c.signedIn() {
		return
	}
	if c.push != nil {
		c.push.Stop()
	}
	c.push = time.AfterFunc(pushDelay, func() {
		c.a.post(func() { c.run(false, c.engine.Push) })
	})
}

// pull syncs as Settings shows, unless it synced a moment ago.
func (c *cloud) pull() {
	if c != nil && time.Since(c.lastPull) > pullAfter {
		c.sync(cloudsync.Ask, false)
	}
}

// sync merges the account's matches with this machine's, the first time
// as choice says. manual tells of the outcome, as Sync Now does.
func (c *cloud) sync(choice cloudsync.Choice, manual bool) {
	if !c.signedIn() {
		return
	}
	c.lastPull = time.Now()
	c.run(manual, func(ctx context.Context) (cloudsync.Result, error) { return c.engine.Sync(ctx, choice) })
}

// run runs a sync off the main thread, showing that it does.
func (c *cloud) run(manual bool, job func(context.Context) (cloudsync.Result, error)) {
	c.busy++
	if manual {
		c.notice = ""
	}
	if c.retry != nil {
		c.retry.Stop()
		c.retry = nil
	}
	c.a.settings.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		res, err := job(ctx)
		cancel()
		c.a.post(func() {
			c.busy--
			c.done(res, err, manual)
			c.changedState()
		})
	}()
}

// done tells of a sync's outcome, and asks whose matches to keep when a
// first sync needs to know.
func (c *cloud) done(res cloudsync.Result, err error, manual bool) {
	if err == nil || errors.Is(err, cloudsync.ErrNeedsChoice) || errors.Is(err, cloudsync.ErrStale) {
		c.failure, c.failures, c.retryAt = "", 0, time.Time{}
	}
	switch {
	case errors.Is(err, errQuitting):
	case errors.Is(err, cloudsync.ErrStale):
		// The matches kept changing as it synced: the push of the last
		// change syncs them.
	case errors.Is(err, cloudsync.ErrNeedsChoice):
		if c.asked && !manual {
			return
		}
		c.asked = true
		go c.askChoice()
	case errors.Is(err, mon.ErrUnauthorized):
		// Signed out elsewhere, as from the account page.
		c.forget()
		c.notice = "Your session ended. Sign in again to keep syncing."
	case err != nil:
		// Offline, most likely: nothing is lost, and the next sync merges
		// what this one did not.
		logx.Warnf("Settings sync failed: %v", err)
		c.failure = syncError(err)
		c.retryAt = time.Now().Add(syncRetryDelay(c.failures))
		c.retry = time.AfterFunc(time.Until(c.retryAt), func() {
			c.a.post(func() {
				c.retry = nil
				c.sync(cloudsync.Ask, false)
			})
		})
		c.failures++
	case manual && !res.Applied && !res.Pushed:
		c.notice = "Already up to date."
	case manual:
		c.notice = "Synced."
	}
}

// askChoice asks whose matches a first sync keeps, where this Mac and the
// account both have their own.
func (c *cloud) askChoice() {
	c.a.post(c.a.acquire)
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Type:    mygo.MessageQuestion,
		Message: "Keep the matches of your account, or this Mac's?",
		Detail: "This Mac and your account both have matches and settings of their own. " +
			"Choose which to keep: the other's are replaced.",
		Buttons: []string{"Use Account's", "Use This Mac's", "Cancel"},
	})
	c.a.post(c.a.release)
	if err != nil {
		return
	}
	switch res.Button {
	case 0:
		c.a.post(func() { c.sync(cloudsync.UseAccount, true) })
	case 1:
		c.a.post(func() { c.sync(cloudsync.UseLocal, true) })
	}
}

// syncError says why a sync failed, in words for the person rather than
// the network's.
func syncError(err error) string {
	var me *mon.Error
	var ne net.Error
	switch {
	case errors.As(err, &me) && me.Status >= 500:
		return monHost() + " is having trouble."
	case errors.As(err, &ne), errors.Is(err, context.DeadlineExceeded), errors.Is(err, io.ErrUnexpectedEOF):
		return "Can't reach " + monHost() + "."
	}
	return err.Error()
}

// monHost is the host name of Mon, as the person sees it in the browser.
func monHost() string {
	if u, err := url.Parse(monURL()); err == nil && u.Host != "" {
		return u.Host
	}
	return monURL()
}

// flush saves a change waiting to go to the account, as the app quits,
// waiting for it a moment at most.
func (c *cloud) flush() {
	if c == nil || c.push == nil || !c.push.Stop() || c.client.Token == "" {
		return
	}
	c.quitting.Store(true)
	done := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), quitPush)
		defer cancel()
		c.engine.Push(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(quitPush + 250*time.Millisecond):
	}
}

// deviceName is how Mon lists Sokki signed in on this Mac: its name, as in
// Sharing settings. A development build signs in apart from the release,
// and says so.
func deviceName() string {
	name := ""
	if out, err := exec.Command("/usr/sbin/scutil", "--get", "ComputerName").Output(); err == nil {
		name = strings.TrimSpace(string(out))
	}
	if name == "" {
		host, _ := os.Hostname()
		name = cmp.Or(strings.TrimSuffix(host, ".local"), "Mac")
	}
	if mygo.IsDev() {
		name += " (dev)"
	}
	return name
}

// signIn signs in to Mon, showing the code to approve on the Account page,
// and syncs once it is approved.
func (c *cloud) signIn() {
	c.cancelSignIn()
	ctx, cancel := context.WithCancel(context.Background())
	si := &signin{cancel: cancel}
	c.signin, c.notice = si, ""
	c.a.settings.showPage(accountPage)
	go func() {
		// A token still kept is signed out first, so Mon doesn't list it on.
		if old, err := c.loadToken(); err == nil && len(old) > 0 {
			stale := *c.client
			stale.Token = string(old)
			lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			stale.Logout(lctx)
			cancel()
		}
		dc, err := c.client.StartDeviceLogin(ctx, cloudsync.App, deviceName())
		if err != nil {
			c.a.post(func() { c.signInFailed(ctx, si, err) })
			return
		}
		c.a.post(func() {
			if ctx.Err() != nil || c.signin != si {
				return
			}
			si.code, si.url = dc.UserCode, dc.VerificationURIComplete
			if dc.ExpiresIn > 0 {
				si.expires = time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
			}
			mygo.Shell.OpenExternal(dc.VerificationURIComplete)
			c.a.settings.invalidate()
		})
		token, err := c.client.WaitForDeviceToken(ctx, dc)
		var u *mon.User
		if err == nil {
			err = c.saveToken([]byte(token))
		}
		if err == nil {
			signedIn := *c.client
			signedIn.Token = token
			u, err = signedIn.Me(ctx)
		}
		c.a.post(func() {
			if err != nil {
				c.signInFailed(ctx, si, err)
				return
			}
			c.dropSignin(si)
			c.client.Token = token
			c.asked = false
			if err := c.engine.SignedIn(u); err != nil {
				c.signInFailed(ctx, si, err)
				return
			}
			logx.Debugf("Signed in to settings sync")
			c.changedState()
			c.sync(cloudsync.Ask, false)
		})
	}()
}

// cancelSignIn stops a sign-in waiting for its approval.
func (c *cloud) cancelSignIn() {
	if c.signin != nil {
		c.signin.cancel()
		c.signin = nil
		c.a.settings.invalidate()
	}
}

func (c *cloud) dropSignin(si *signin) {
	if c.signin == si {
		c.signin = nil
		c.a.settings.invalidate()
	}
}

// signInFailed says why a sign-in did not happen, unless it was
// cancelled.
func (c *cloud) signInFailed(ctx context.Context, si *signin, err error) {
	c.dropSignin(si)
	switch {
	case ctx.Err() != nil:
	case errors.Is(err, mon.ErrAccessDenied):
		c.notice = "The sign-in was declined."
	case errors.Is(err, mon.ErrExpired):
		c.notice = "The code expired. Sign in again to get a new one."
	default:
		logx.Errorf("Sign-in failed: %v", err)
		c.notice = "Couldn't sign in: " + syncError(err)
	}
	c.a.settings.invalidate()
}

// signOut stops syncing, and revokes the token on Mon.
func (c *cloud) signOut() {
	client := *c.client
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		client.Logout(ctx)
	}()
	c.forget()
	c.notice = ""
}

// loadToken returns the token kept at sign-in, or mygo.ErrSecretNotFound.
func (c *cloud) loadToken() ([]byte, error) {
	if c.tokenFile == "" {
		return mygo.Secrets.Get(tokenKey)
	}
	b, err := os.ReadFile(c.tokenFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, mygo.ErrSecretNotFound
	}
	return b, err
}

// saveToken keeps the token for the next launches.
func (c *cloud) saveToken(token []byte) error {
	if c.tokenFile == "" {
		return mygo.Secrets.Set(tokenKey, token)
	}
	return os.WriteFile(c.tokenFile, token, 0o600)
}

// dropToken removes the token kept, if any.
func (c *cloud) dropToken() error {
	if c.tokenFile == "" {
		return mygo.Secrets.Delete(tokenKey)
	}
	if err := os.Remove(c.tokenFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// forget forgets the account and its token, as signed out.
func (c *cloud) forget() {
	if c.push != nil {
		c.push.Stop()
	}
	if c.retry != nil {
		c.retry.Stop()
		c.retry = nil
	}
	c.failure, c.failures, c.retryAt = "", 0, time.Time{}
	c.avatar, c.avatarURL = nil, ""
	c.client.Token = ""
	c.engine.Forget()
	go c.dropToken()
	c.changedState()
}

// picture returns the person's picture, nil until it is fetched, which
// it starts the first time it is asked for.
func (c *cloud) picture() *ui.Bitmap {
	url := c.engine.State().Avatar
	if url == "" || url == c.avatarURL {
		return c.avatar
	}
	c.avatarURL, c.avatar = url, nil
	go func() {
		bm := fetchAvatar(url)
		if bm == nil {
			return
		}
		c.a.post(func() {
			if c.avatarURL == url {
				c.avatar = bm
				c.a.settings.invalidate()
			}
		})
	}()
	return nil
}

// fetchAvatar fetches the picture at url, nil if it cannot.
func fetchAvatar(url string) *ui.Bitmap {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil
	}
	bm, _ := ui.DecodeBitmap(data)
	return bm
}

// cloudLocal is this Mac's configuration and matches, as settings sync
// reads and writes them, from the engine's goroutine.
type cloudLocal struct{ c *cloud }

// Read reads the files, which the app saves as soon as they change.
func (l cloudLocal) Read() (cloudsync.Snapshot, error) {
	r := l.c.a.dir.Load()
	if r.Error != "" {
		return cloudsync.Snapshot{}, errors.New(r.Error)
	}
	return cloudsync.FromModel(r.Config, r.Matches), nil
}

// Apply checks and writes on the main thread, where the person edits the
// matches, so that none of their edits falls between the two.
func (l cloudLocal) Apply(read, s cloudsync.Snapshot) error {
	if l.c.quitting.Load() {
		return errQuitting
	}
	done := make(chan error, 1)
	a := l.c.a
	a.post(func() {
		r := a.dir.Load()
		now := cloudsync.FromModel(r.Config, r.Matches)
		if r.Error != "" || !now.Equal(read) {
			done <- cloudsync.ErrStale
			return
		}
		cfg, matches := s.Model()
		var err error
		if !cloudsync.FromModel(cfg, nil).Equal(cloudsync.FromModel(r.Config, nil)) {
			err = a.dir.SaveConfig(cfg)
		}
		if err == nil && !cloudsync.FromModel(cfg, matches).Equal(cloudsync.FromModel(cfg, r.Matches)) {
			err = a.dir.SaveMatches(matches, r.Files)
		}
		a.reloadConfig()
		done <- err
	})
	return <-done
}

func (l cloudLocal) IsDefault(s cloudsync.Snapshot) bool {
	return config.IsStarter(s.Model())
}

// The tray's and Settings' side of settings sync.

func (a *app) signIn() {
	if a.cloud != nil {
		a.cloud.signIn()
	}
}

func (a *app) syncNow() {
	if a.cloud != nil {
		a.cloud.sync(cloudsync.Ask, true)
	}
}
