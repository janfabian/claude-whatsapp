#!/usr/bin/env bash
# Builds the WhatsApp bridge binary into bridge/bin/<os>-<arch>/whatsapp-bridge.
# Idempotent — skips the build if the binary is newer than every .go file.
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

# Skip if up to date — every .go file older than the binary.
if [[ -x "$OUT" ]]; then
  newest_src=$(find "$SRC" -name '*.go' -newer "$OUT" -print -quit 2>/dev/null || true)
  if [[ -z "$newest_src" ]]; then
    exit 0
  fi
fi

mkdir -p "$OUT_DIR"
echo "build-bridge: compiling $OUT" >&2
cd "$SRC"
CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o "$OUT" .
echo "build-bridge: done" >&2
