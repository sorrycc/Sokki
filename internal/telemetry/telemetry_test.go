package telemetry

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"sokki/internal/logx"
	"sokki/internal/prefs"
)

func TestPingAndCrashUpload(t *testing.T) {
	logx.SetDir(t.TempDir())
	var mu sync.Mutex
	var paths []string
	key := "secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-Telemetry-Sig") != Sign(body, key) {
			w.WriteHeader(401)
			return
		}
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
	}))
	defer srv.Close()

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "telemetry.json"), []byte(`{"endpointURL":"`+srv.URL+`/","ingestKey":"`+key+`"}`), 0o644)
	c := Load(dir)
	if c == nil {
		t.Fatal("config not loaded")
	}
	p := prefs.Open(filepath.Join(dir, "prefs.json"))
	c.Ping(p)
	c.Ping(p) // once a day
	crashes := filepath.Join(dir, "crashes")
	os.MkdirAll(crashes, 0o755)
	os.WriteFile(filepath.Join(crashes, "Sokki-2026.log"), []byte("panic: x"), 0o644)
	c.UploadCrashes(p, crashes)
	c.UploadCrashes(p, crashes) // already uploaded
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(paths, ",") != "/v1/ping,/v1/crash" {
		t.Fatalf("requests %v", paths)
	}
	if Load(t.TempDir()) != nil {
		t.Fatal("config without a file")
	}
}

func TestPrefsDefaults(t *testing.T) {
	p := prefs.Open(filepath.Join(t.TempDir(), "p.json"))
	if !p.Get().CrashReportsOn() || !p.Get().DailyPingOn() {
		t.Fatal("telemetry should default on")
	}
	id := p.InstallID()
	if len(id) != 36 || p.InstallID() != id || p.ResetInstallID() == id {
		t.Fatalf("install id %q", id)
	}
}
