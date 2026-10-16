// Package telemetry sends the opt-out daily ping and crash reports to
// Sokki's telemetry worker. Neither carries personal data, keystrokes,
// match text or clipboard contents.
package telemetry

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"sokki/internal/logx"
	"sokki/internal/prefs"
)

// Config is where telemetry goes, from telemetry.json among the app's
// resources, which only release builds have.
type Config struct {
	EndpointURL string `json:"endpointURL"`
	IngestKey   string `json:"ingestKey"`

	AppVersion string `json:"-"`
	OSVersion  string `json:"-"`
	Locale     string `json:"-"`
}

// Load reads telemetry.json from dir, nil without it.
func Load(dir string) *Config {
	data, err := os.ReadFile(filepath.Join(dir, "telemetry.json"))
	if err != nil {
		return nil
	}
	var c Config
	if json.Unmarshal(data, &c) != nil || c.EndpointURL == "" || c.IngestKey == "" {
		return nil
	}
	c.EndpointURL = strings.TrimRight(c.EndpointURL, "/")
	return &c
}

const appID = "sokki"

// Sign is the hex HMAC-SHA256 of body with key, which the worker checks.
func Sign(body []byte, key string) string {
	m := hmac.New(sha256.New, []byte(key))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

func (c *Config) post(path, contentType string, body []byte, timeout time.Duration) error {
	req, err := http.NewRequest("POST", c.EndpointURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Telemetry-Sig", Sign(body, c.IngestKey))
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("bad status %d: %s", resp.StatusCode, snippet)
	}
	return nil
}

// Ping sends the daily ping, at most once a UTC day.
func (c *Config) Ping(p *prefs.Store) {
	if !p.Get().DailyPingOn() {
		logx.Debugf("telemetry: dau disabled by user")
		return
	}
	today := time.Now().UTC().Format("2006-01-02")
	if p.Get().LastPingDate == today {
		logx.Debugf("telemetry: dau already pinged today")
		return
	}
	body, _ := json.Marshal(map[string]string{
		"app": appID, "installId": p.InstallID(), "version": c.AppVersion, "osVersion": c.OSVersion, "locale": c.Locale,
	})
	if err := c.post("/v1/ping", "application/json", body, 15*time.Second); err != nil {
		logx.Warnf("telemetry: dau ping failed: %v", err)
		return
	}
	p.Update(func(pr *prefs.Prefs) { pr.LastPingDate = today })
	logx.Debugf("telemetry: dau ping ok")
}

const uploadedCap = 500

// UploadCrashes uploads the crash logs of dir not uploaded yet: the logs
// the Go runtime leaves at a crash (see KeepCrashes), and the .ips reports
// macOS wrote for Sokki in ~/Library/Logs/DiagnosticReports.
func (c *Config) UploadCrashes(p *prefs.Store, crashDir string) {
	if !p.Get().CrashReportsOn() {
		logx.Debugf("telemetry: crash uploads disabled by user")
		return
	}
	var files []string
	if entries, err := os.ReadDir(crashDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
				files = append(files, filepath.Join(crashDir, e.Name()))
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		reports := filepath.Join(home, "Library", "Logs", "DiagnosticReports")
		if entries, err := os.ReadDir(reports); err == nil {
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "Sokki-") && strings.HasSuffix(e.Name(), ".ips") {
					files = append(files, filepath.Join(reports, e.Name()))
				}
			}
		}
	}
	slices.SortFunc(files, func(a, b string) int { return modTime(a).Compare(modTime(b)) })
	uploaded := p.Get().UploadedCrashes
	var pending []string
	for _, f := range files {
		if !slices.Contains(uploaded, filepath.Base(f)) {
			pending = append(pending, f)
		}
	}
	if len(pending) == 0 {
		return
	}
	logx.Debugf("telemetry: uploading %d crash report(s)", len(pending))
	for _, f := range pending {
		if err := c.uploadOne(p, f); err != nil {
			logx.Warnf("telemetry: upload failed for %s: %v", filepath.Base(f), err)
			continue
		}
		uploaded = append(uploaded, filepath.Base(f))
		logx.Debugf("telemetry: uploaded %s", filepath.Base(f))
	}
	if len(uploaded) > uploadedCap {
		uploaded = uploaded[len(uploaded)-uploadedCap:]
	}
	p.Update(func(pr *prefs.Prefs) { pr.UploadedCrashes = uploaded })
}

func modTime(path string) time.Time {
	if fi, err := os.Stat(path); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}

func (c *Config) uploadOne(p *prefs.Store, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	name := filepath.Base(path)
	meta, _ := json.Marshal(map[string]string{
		"app": appID, "installId": p.InstallID(), "version": c.AppVersion, "osVersion": c.OSVersion, "fileName": name,
	})
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	mh := textproto.MIMEHeader{}
	mh.Set("Content-Disposition", `form-data; name="meta"`)
	part, _ := w.CreatePart(mh)
	part.Write(meta)
	fh := textproto.MIMEHeader{}
	fh.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, name))
	fh.Set("Content-Type", "application/octet-stream")
	part, _ = w.CreatePart(fh)
	part.Write(data)
	w.Close()
	return c.post("/v1/crash", w.FormDataContentType(), body.Bytes(), 30*time.Second)
}
