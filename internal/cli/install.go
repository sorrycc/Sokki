package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Name is the command's name on the shell's path.
const Name = "sokki"

// scriptMark is a line of the sokki command, by which Sokki knows it as
// its own.
const scriptMark = "# Installed by Sokki: Install ‘sokki’ Command, in Settings › General."

// script is the sokki command, which runs the app's binary at binary as
// the command.
func script(binary string) string {
	return `#!/bin/sh
# sokki: do from the terminal what Sokki does; see ` + "`sokki help`" + `.
` + scriptMark + `
exec ` + shellQuote(binary) + ` cli "$@"
`
}

// Dirs are where the command goes: ~/.local/bin when the shell looks
// there, which needs no password, else /usr/local/bin, which is always on
// the path.
func Dirs(home string, path []string) []string {
	local := filepath.Join(home, ".local", "bin")
	if slices.Contains(path, local) {
		return []string{local, "/usr/local/bin"}
	}
	return []string{"/usr/local/bin"}
}

// AllDirs are every place the command may be.
func AllDirs(home string) []string {
	return []string{filepath.Join(home, ".local", "bin"), "/usr/local/bin"}
}

// ErrCanceled is the person declining to give the password.
var ErrCanceled = errors.New("canceled")

// Installed returns where the command of Sokki is in dirs, and the binary
// it runs: "" for nowhere.
func Installed(dirs []string) (path, binary string) {
	for _, dir := range dirs {
		p := filepath.Join(dir, Name)
		data, err := os.ReadFile(p)
		if err != nil || !strings.Contains(string(data), scriptMark) {
			continue
		}
		for line := range strings.Lines(string(data)) {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "exec "); ok {
				binary = unquote(strings.TrimSuffix(rest, ` cli "$@"`))
			}
		}
		return p, binary
	}
	return "", ""
}

// Install writes the command running binary into the first of dirs that
// takes it, as an administrator for the last, and returns where it went.
func Install(dirs []string, binary string) (string, error) {
	var err error
	for i, dir := range dirs {
		p := filepath.Join(dir, Name)
		if data, rerr := os.ReadFile(p); rerr == nil && !strings.Contains(string(data), scriptMark) {
			return "", fmt.Errorf("%s is another program's", p)
		}
		if err = write(dir, binary); err == nil {
			return p, nil
		}
		if i == len(dirs)-1 && errors.Is(err, os.ErrPermission) {
			if err = writeAsAdmin(dir, binary); err == nil {
				return p, nil
			}
		}
	}
	return "", err
}

func write(dir, binary string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	p := filepath.Join(dir, Name)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(script(binary)), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func writeAsAdmin(dir, binary string) error {
	f, err := os.CreateTemp("", "sokki-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString(script(binary))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	p := filepath.Join(dir, Name)
	return runAsAdmin(fmt.Sprintf("mkdir -p %s && cp %s %s && chmod 755 %s",
		shellQuote(dir), shellQuote(f.Name()), shellQuote(p), shellQuote(p)))
}

// Repair points the command, where it is Sokki's and writable without a
// password, at binary, when the binary it runs is gone, as after the app
// moved; it reports whether it did.
func Repair(dirs []string, binary string) bool {
	p, old := Installed(dirs)
	if p == "" || old == binary {
		return false
	}
	if _, err := os.Stat(old); err == nil {
		return false
	}
	return write(filepath.Dir(p), binary) == nil
}

// Uninstall removes the command from dirs, where it is Sokki's, and
// returns where it was.
func Uninstall(dirs []string) ([]string, error) {
	var removed []string
	for _, dir := range dirs {
		p := filepath.Join(dir, Name)
		data, err := os.ReadFile(p)
		if err != nil || !strings.Contains(string(data), scriptMark) {
			continue
		}
		err = os.Remove(p)
		if errors.Is(err, os.ErrPermission) {
			err = runAsAdmin("rm -f " + shellQuote(p))
		}
		if err != nil {
			return removed, err
		}
		removed = append(removed, p)
	}
	return removed, nil
}

// runAsAdmin runs a shell command as an administrator, which macOS asks
// the password for.
func runAsAdmin(cmd string) error {
	script := `do shell script "` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(cmd) + `" with administrator privileges`
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "(-128)") {
			return ErrCanceled
		}
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// unquote undoes shellQuote.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], `'\''`, "'")
	}
	return s
}

// ShellPath is where the person's shell looks for commands, which an app
// opened from the Finder does not inherit: the login shell says.
func ShellPath() []string {
	path := filepath.SplitList(os.Getenv("PATH"))
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const mark = "SOKKI_PATH="
	out, _ := exec.CommandContext(ctx, sh, "-ilc", `echo "`+mark+`$PATH"`).Output()
	for line := range strings.Lines(string(out)) {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), mark); ok {
			path = append(path, filepath.SplitList(p)...)
		}
	}
	for i, p := range path {
		path[i] = filepath.Clean(p)
	}
	return path
}
