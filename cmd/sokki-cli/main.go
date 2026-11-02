// Command sokki-cli does from the terminal what Sokki's windows and menu
// do, by asking the app running on this Mac: see `sokki-cli help`. The
// app's binary is the same command, as `Sokki cli`, which Install ‘sokki’
// Command in Settings puts on the shell's path as sokki; this one builds
// without the app, as with `go install ./cmd/sokki-cli`.
package main

import (
	"os"

	"sokki/internal/cli"
)

func main() { os.Exit(cli.Main(os.Args[1:], cli.Env{})) }
