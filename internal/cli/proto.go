package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// The sokki command talks to the running app over a Unix socket of the
// person's cache folder, which only they can reach: an HTTP request to
// /call with a Request, answered with a Response. The app does the work,
// on its main thread, as its windows would.

// SocketPath is where the app listens, or where SOKKI_SOCKET says; dev is
// set for the development build's.
func SocketPath(dev bool) string {
	if p := os.Getenv("SOKKI_SOCKET"); p != "" {
		return p
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	name := "sokki.sock"
	if dev {
		name = "sokki-dev.sock"
	}
	return filepath.Join(dir, "Sokki", name)
}

// Request is a command for the app: its name, as "match.add", and its
// arguments.
type Request struct {
	Cmd  string         `json:"cmd"`
	Args map[string]any `json:"args,omitempty"`
}

// Response is the app's answer: its result, or what went wrong.
type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// ErrNotRunning is the app not listening.
var ErrNotRunning = errors.New("Sokki isn’t running on this Mac: open it, and try again")

// Call sends req to the app listening at socket and returns its result.
func Call(ctx context.Context, socket string, req Request) (json.RawMessage, error) {
	hc := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	hr, err := http.NewRequestWithContext(ctx, "POST", "http://sokki/call", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(hr)
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" {
			return nil, ErrNotRunning
		}
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var r Response
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("Sokki answered %s", resp.Status)
	}
	if r.Error != "" {
		return nil, errors.New(r.Error)
	}
	return r.Result, nil
}

// Handler answers the app's commands: handle runs one, and its error goes
// to the command as is.
func Handler(handle func(ctx context.Context, req Request) (any, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "POST" || r.URL.Path != "/call" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(Response{Error: "no such call"})
			return
		}
		var req Request
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(Response{Error: "bad request: " + err.Error()})
			return
		}
		out, err := handle(r.Context(), req)
		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(Response{Error: err.Error()})
			return
		}
		raw, err := json.Marshal(out)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(Response{Error: err.Error()})
			return
		}
		json.NewEncoder(w).Encode(Response{Result: raw})
	})
}

// Listen listens at socket, only for the person, in place of a socket
// left there by an app that quit.
func Listen(socket string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		return nil, err
	}
	if c, err := net.DialTimeout("unix", socket, time.Second); err == nil {
		c.Close()
		return nil, fmt.Errorf("another Sokki listens at %s", socket)
	}
	os.Remove(socket)
	l, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}
