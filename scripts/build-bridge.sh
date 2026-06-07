#!/usr/bin/env bash
# Builds the WhatsApp bridge binary into bridge/bin/<os>-<arch>/whatsapp-bridge.
# Idempotent via Go's own build cache — `go build` is a no-op when nothing
# changed and fast even when something did. We do NOT pre-skip based on
# mtime, because the plugin installer can copy a binary forward across
# versions without touching source mtimes, leaving a stale binary that
# looks "newer than source" and dodges recompile. (Real bug: 0.0.5→0.0.6
# debug-log code never made it into the deployed binary.)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$ROOT/bridge"
OS="$(go env GOOS)"
ARCH="$(go env GOARCH)"
OUT_DIR="$SRC/bin/${OS}-${ARCH}"
OUT="$OUT_DIR/whatsapp-bridge"
[[ "$OS" == "windows" ]] && OUT="${OUT}.exe"

if ! command -v go >/dev/null 2>&1; then
  echo "build-bridge: 'go' not found in PATH. Install Go (https://go.dev/dl/) and retry." >&2
  exit 1
fi

mkdir -p "$OUT_DIR"
echo "build-bridge: building $OUT" >&2
cd "$SRC"
CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o "$OUT" .
echo "build-bridge: done" >&2
