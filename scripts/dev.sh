#!/usr/bin/env bash
# Runs Sokki for development beside the installed Sokki: builds a bare
# binary, .mygo/dev/sokki, runs it, and rebuilds and restarts it when a Go
# file, a resource, go.mod or mygo.json changes. SOKKI_CONFIG_DIR gives it
# its own profile, so that the two never expand the same trigger: its
# matches and settings, and in data/ its prefs, crashes and window state.
# Signed in, it syncs with the account's "sokki-dev" settings, not those of
# the installed Sokki.
#
# Not `go tool mygo dev`: macOS 26 keeps the menu bar icon of the dev bundle
# it makes hidden (Control Center keeps its window off screen, for reasons
# unknown), while a bare binary's shows. A bare binary gives up, for
# development only: Open at Login, which needs a bundle; its own
# Accessibility permission (the terminal's applies); and its name in System
# Settings → Menu Bar, where it shows as "sokki".
#
# Usage: scripts/dev.sh [args passed to sokki]
# SOKKI_CONFIG_DIR picks another config folder (default: ~/.config/sokki-dev),
# which starts as a copy of ~/.config/sokki the first time.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export SOKKI_CONFIG_DIR="${SOKKI_CONFIG_DIR:-$HOME/.config/sokki-dev}"
BIN="$ROOT/.mygo/dev/sokki"
STAMP="$ROOT/.mygo/dev/.built"
PID=

cd "$ROOT"
mkdir -p "$(dirname "$BIN")"

# The first time, the dev config starts as a copy of the installed Sokki's:
# its settings, matches and recency, not its log.
ORIGIN="$HOME/.config/sokki"
if [ "$SOKKI_CONFIG_DIR" != "$ORIGIN" ] && [ -d "$ORIGIN" ] \
    && [ ! -e "$SOKKI_CONFIG_DIR/config" ] && [ ! -e "$SOKKI_CONFIG_DIR/match" ]; then
    echo "==> copying config, matches and recency from $ORIGIN"
    mkdir -p "$SOKKI_CONFIG_DIR"
    for item in config match recency.json; do
        if [ -e "$ORIGIN/$item" ]; then
            cp -R "$ORIGIN/$item" "$SOKKI_CONFIG_DIR/"
        fi
    done
fi
if ps ax -o command= | grep -q "^/Applications/Sokki.app/Contents/MacOS/Sokki"; then
    echo "==> warning: the installed Sokki is running; with the same matches, both expand each trigger. Quit it first."
fi

stop() {
    if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
        kill -TERM "$PID" 2>/dev/null || true
        wait "$PID" 2>/dev/null || true
    fi
    PID=
}
trap 'stop; exit 0' INT TERM
trap stop EXIT

build() {
    echo "==> go build ./cmd/sokki"
    touch "$STAMP"
    go build -o "$BIN" ./cmd/sokki
}

start() {
    echo "==> sokki (config in $SOKKI_CONFIG_DIR)"
    "$BIN" "$@" &
    PID=$!
}

# changed reports whether a source changed since the last build.
changed() {
    [ -n "$(find cmd internal resources go.mod go.sum mygo.json -newer "$STAMP" -type f \
        \( -name '*.go' -o -path 'resources/*' -o -name 'go.mod' -o -name 'go.sum' -o -name 'mygo.json' \) \
        -print -quit 2>/dev/null)" ]
}

build && start "$@" || echo "==> build failed; waiting for a change"
while sleep 1; do
    if changed; then
        if build; then
            stop
            start "$@"
        else
            echo "==> build failed; still running the last build"
        fi
    elif [ -n "$PID" ] && ! kill -0 "$PID" 2>/dev/null; then
        echo "==> sokki exited; restarting on the next change"
        PID=
    fi
done
