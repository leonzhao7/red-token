#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$ROOT_DIR/frontend"
EMBED_DIR="$ROOT_DIR/backend/internal/web/dist"
OUTPUT_DIR="$ROOT_DIR/backend/dist"
SOURCE_INDEX="$EMBED_DIR/index.html"

TARGET="${1:-all}"
case "$TARGET" in
  all|linux|windows) ;;
  *)
    echo "Usage: $0 [all|linux|windows]" >&2
    exit 2
    ;;
esac

if [[ "$TARGET" == "all" || "$TARGET" == "linux" ]]; then
  LINUX_CC="${CC_LINUX:-gcc}"
  if ! command -v "$LINUX_CC" >/dev/null 2>&1; then
    echo "Linux C compiler not found: $LINUX_CC" >&2
    exit 1
  fi
fi

if [[ "$TARGET" == "all" || "$TARGET" == "windows" ]]; then
  WINDOWS_CC="${CC_WINDOWS:-x86_64-w64-mingw32-gcc}"
  if ! command -v "$WINDOWS_CC" >/dev/null 2>&1; then
    echo "Windows cross-compiler not found: $WINDOWS_CC (install MinGW-w64 or set CC_WINDOWS)" >&2
    exit 1
  fi
fi

if [[ ! -d "$FRONTEND_DIR/node_modules" ]]; then
  echo "Installing frontend dependencies..."
  npm --prefix "$FRONTEND_DIR" ci
fi

mkdir -p "$OUTPUT_DIR"
GO_BUILD_CACHE="${GOCACHE:-$ROOT_DIR/.gocache}"
mkdir -p "$GO_BUILD_CACHE"
restore_source_index() {
  printf '%s\n' \
    '<!doctype html>' \
    '<html lang="en">' \
    '  <head>' \
    '    <meta charset="UTF-8">' \
    '    <meta name="viewport" content="width=device-width, initial-scale=1.0">' \
    '    <title>Red Token</title>' \
    '  </head>' \
    '  <body>' \
    '    <p>Frontend assets are not built. Run ./release.sh.</p>' \
    '  </body>' \
    '</html>' > "$SOURCE_INDEX"
}
trap restore_source_index EXIT

echo "Building frontend..."
npm --prefix "$FRONTEND_DIR" run build -- --outDir "$EMBED_DIR" --emptyOutDir

build_target() {
  local goos="$1"
  local cc="$2"
  local output="$3"
  echo "Building $goos executable..."
  (
    cd "$ROOT_DIR/backend"
    GOCACHE="$GO_BUILD_CACHE" CGO_ENABLED=1 CC="$cc" GOOS="$goos" GOARCH=amd64 \
      go build -trimpath -ldflags="-s -w -extldflags=-static" -o "$OUTPUT_DIR/$output" ./cmd/red-token
  )
}

if [[ "$TARGET" == "all" || "$TARGET" == "linux" ]]; then
  build_target linux "$LINUX_CC" red-token-linux-amd64
fi

if [[ "$TARGET" == "all" || "$TARGET" == "windows" ]]; then
  build_target windows "$WINDOWS_CC" red-token-windows-amd64.exe
fi

echo "Release files:"
if [[ "$TARGET" == "all" || "$TARGET" == "linux" ]]; then
  echo "  $OUTPUT_DIR/red-token-linux-amd64"
fi
if [[ "$TARGET" == "all" || "$TARGET" == "windows" ]]; then
  echo "  $OUTPUT_DIR/red-token-windows-amd64.exe"
fi
