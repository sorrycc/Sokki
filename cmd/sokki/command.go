package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"

	"sokki/internal/cli"
)

// The sokki command, on the shell's path, runs this binary as `Sokki cli`,
// which asks the running app: see control.go. Settings installs and
// uninstalls it.

// binary is the path of this binary, which the command runs.
func binary() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p
}

// transient reports whether binary runs from where it won't stay: a disk
// image, or the copy macOS translocates a quarantined app to.
func transient(binary string) bool {
	return binary == "" || strings.HasPrefix(binary, "/Volumes/") || strings.Contains(binary, "/AppTranslocation/")
}

// loadCommand learns where the command is, and points it at this copy of
// the app, as after it moved.
func (a *app) loadCommand() {
	home, _ := os.UserHomeDir()
	bin := binary()
	go func() {
		dirs := cli.AllDirs(home)
		if !transient(bin) {
			cli.Repair(dirs, bin)
		}
		p, _ := cli.Installed(dirs)
		a.post(func() { a.commandPath = p; a.settings.invalidate() })
	}()
}

// installCommand puts the sokki command on the shell's path. Main thread.
func (a *app) installCommand() {
	bin := binary()
	if transient(bin) {
		tell(mygo.MessageWarning, "Move Sokki to Applications first", "The ‘sokki’ command can’t run Sokki from a disk image.")
		return
	}
	home, _ := os.UserHomeDir()
	go func() {
		p, err := cli.Install(cli.Dirs(home, cli.ShellPath()), bin)
		switch {
		case errors.Is(err, cli.ErrCanceled):
		case err != nil:
			tell(mygo.MessageWarning, "Couldn’t install the ‘sokki’ command", err.Error())
		default:
			a.post(func() { a.commandPath = p; a.settings.invalidate() })
			tell(mygo.MessageInfo, "Installed the ‘sokki’ command in "+shortHome(home, filepath.Dir(p)),
				"Run `sokki help` in a terminal, while Sokki runs, to manage matches, settings and sync from there.")
		}
	}()
}

// uninstallCommand takes the sokki command off the shell's path. Main
// thread.
func (a *app) uninstallCommand() {
	home, _ := os.UserHomeDir()
	go func() {
		_, err := cli.Uninstall(cli.AllDirs(home))
		switch {
		case errors.Is(err, cli.ErrCanceled):
		case err != nil:
			tell(mygo.MessageWarning, "Couldn’t uninstall the ‘sokki’ command", err.Error())
		default:
			a.post(func() { a.commandPath = ""; a.settings.invalidate() })
		}
	}()
}

// tell shows a message in a dialog of its own.
func tell(kind mygo.MessageType, message, detail string) {
	mygo.Dialog.Message(mygo.MessageOptions{Type: kind, Message: message, Detail: detail})
}

// shortHome is path with ~ for the home folder.
func shortHome(home, path string) string {
	if rest, ok := strings.CutPrefix(path, home+"/"); ok {
		return "~/" + rest
	}
	return path
}
