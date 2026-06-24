#!/bin/bash
# Boot smoke-test for the refactor loop. Not behavioural (real expansion needs
# Accessibility + live typing) — this gates the failure mode refactors actually
# introduce: a crash/trap during launch. Asserts: builds, launches, survives
# ~4s, and emits no crash signature on stderr or in the log.
set -uo pipefail

LOG="$HOME/.config/sokki/logs/sokki.log"
OUT=/tmp/sokki-smoke.out
MARK="===SMOKE $(date +%s)==="
CRASH_RE='Fatal error|fatalError|precondition failure|Thread .* Crashed|EXC_BAD|Trace/BPT trap|Illegal instruction|Segmentation fault|Swift runtime failure|isolated to'

echo "[smoke] building…"
if ! swift build 2>&1 | tail -3; then
  echo "[smoke] BUILD FAILED"; exit 1
fi

mkdir -p "$(dirname "$LOG")"
echo "[smoke] $MARK" >> "$LOG"

echo "[smoke] launching app…"
swift run Sokki >"$OUT" 2>&1 &
APP_PID=$!

alive=1
for _ in $(seq 1 8); do
  sleep 0.5
  if ! kill -0 "$APP_PID" 2>/dev/null; then alive=0; break; fi
done

kill "$APP_PID" 2>/dev/null
wait "$APP_PID" 2>/dev/null

# Crash signatures from this run's stdout/stderr and the post-marker log slice.
crash=""
if grep -Eiq "$CRASH_RE" "$OUT" 2>/dev/null; then crash="stderr"; fi
if awk "/$MARK/{f=1} f" "$LOG" | grep -Eiq "$CRASH_RE"; then crash="${crash:+$crash,}log"; fi

if [ "$alive" = 0 ]; then
  echo "[smoke] FAIL — process exited early"
  echo "----- output -----"; tail -25 "$OUT"
  exit 1
fi
if [ -n "$crash" ]; then
  echo "[smoke] FAIL — crash signature in: $crash"
  echo "----- output -----"; tail -25 "$OUT"
  echo "----- log -----"; awk "/$MARK/{f=1} f" "$LOG" | tail -15
  exit 1
fi

echo "[smoke] PASS — booted and survived ~4s, no crash signature"
exit 0
