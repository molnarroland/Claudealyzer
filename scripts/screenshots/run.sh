#!/usr/bin/env bash
# Remakes every app screenshot in docs/images/ from made-up demo data, never from real transcripts.
#
#   scripts/screenshots/run.sh [--today YYYY-MM-DD] [--keep]
#
# 1. builds the frontend and the app in Wails server mode (no window: the UI is served over HTTP),
# 2. writes fake Claude Code / Codex / Gemini history into a temporary HOME (gen_mock.py),
# 3. starts the app with that HOME and demo billing plans,
# 4. drives it with Playwright (shots.mjs) through the macOS, Windows and GNOME looks, light and dark,
# 5. scales the shots to 1800 px wide and writes them as WebP to docs/images/, plus history.png for og:image.
#
# Needs Go, Node (with npm), python3, cwebp (brew install webp) and sips (macOS) or ImageMagick.
# --keep leaves the temporary directory (fake HOME, raw PNGs, server log) for a look afterwards.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
PORT="${PORT:-18765}"
TODAY= KEEP=0
while [ $# -gt 0 ]; do
  case "$1" in
    --today) TODAY="$2"; shift 2 ;;
    --keep) KEEP=1; shift ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

# Resolved and cleaned (no symlinks, no //): the app shows paths under HOME as ~ only when the prefixes match.
WORK="$(cd "$(mktemp -d "${TMPDIR:-/tmp}/cz-shots.XXXXXX")" && pwd -P)"
DEMO_HOME="$WORK/home" RAW="$WORK/raw"
mkdir -p "$DEMO_HOME" "$RAW"
SERVER_PID=
cleanup() {
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  if [ "$KEEP" = 1 ]; then echo "kept $WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT

echo "› building"
(cd "$ROOT/frontend" && npm run build >/dev/null)
(cd "$ROOT" && go build -tags server -o "$WORK/cz-server" . 2>&1 | grep -v "ld: warning" || true)

echo "› generating demo data"
python3 -I "$HERE/gen_mock.py" "$DEMO_HOME" ${TODAY:+--today "$TODAY"}

if curl -sf "localhost:$PORT/" >/dev/null; then echo "port $PORT is in use; set PORT=..." >&2; exit 1; fi
start_server() {
  # exec: $! is the server itself, so stopping it really frees the port.
  (cd "$WORK" && HOME="$DEMO_HOME" WAILS_SERVER_PORT="$PORT" exec ./cz-server >>server.log 2>&1) &
  SERVER_PID=$!
  for _ in $(seq 1 40); do
    kill -0 "$SERVER_PID" 2>/dev/null || break
    curl -sf "localhost:$PORT/" >/dev/null && return
    sleep 0.25
  done
  echo "server did not start, see $WORK/server.log" >&2; exit 1
}
stop_server() { kill "$SERVER_PID" 2>/dev/null || true; wait "$SERVER_PID" 2>/dev/null || true; SERVER_PID=; }

# The first start writes the config with the sources it finds; give them demo plans, then restart.
start_server
CONFIG="$(find "$DEMO_HOME" -path '*claudealyzer/config.json' | head -1)"
for _ in $(seq 1 40); do [ -n "$CONFIG" ] && break; sleep 0.25; CONFIG="$(find "$DEMO_HOME" -path '*claudealyzer/config.json' | head -1)"; done
stop_server
python3 -I - "$CONFIG" <<'EOF'
import json, sys
p = sys.argv[1]
c = json.load(open(p))
plans = {
    "claude": {"kind": "subscription", "fee": 100, "renewDay": 1, "discountPct": 0, "plan": "Max"},
    "codex": {"kind": "subscription", "fee": 20, "renewDay": 5, "discountPct": 0, "plan": "Plus"},
    "gemini": {"kind": "free", "fee": 0, "renewDay": 0, "discountPct": 0, "plan": ""},
}
for s in c["sources"]:
    s["billing"] = plans[s["agent"]]
json.dump(c, open(p, "w"), indent=2)
EOF
start_server

echo "› taking screenshots"
(cd "$HERE" && { [ -d node_modules ] || npm ci --no-audit --no-fund >/dev/null; } && npx playwright install chromium >/dev/null && node shots.mjs "$RAW" "http://localhost:$PORT/")

echo "› writing docs/images"
resize() { # <in> <out> <width>
  if command -v sips >/dev/null; then sips --resampleWidth "$3" "$1" --out "$2" >/dev/null; else magick "$1" -resize "$3x" "$2"; fi
}
for f in "$RAW"/*.png; do
  n="$(basename "$f" .png)"
  resize "$f" "$WORK/$n.png" 1800
  cwebp -quiet -q 82 -m 6 "$WORK/$n.png" -o "$ROOT/docs/images/$n.webp"
done
# Link previews and GitHub's social preview don't take WebP.
cp "$WORK/history.png" "$ROOT/docs/images/history.png"
ls -la "$ROOT/docs/images"
