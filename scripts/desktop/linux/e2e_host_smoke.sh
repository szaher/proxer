#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$ROOT_DIR"

BUILD_DIR="${PROXER_BUILD_DIR:-$ROOT_DIR/output/desktop/linux}"
APPIMAGE="${PROXER_APPIMAGE_PATH:-}"

if [[ -z "$APPIMAGE" ]]; then
  APPIMAGE="$(ls -1 "$BUILD_DIR"/*.AppImage 2>/dev/null | head -n1 || true)"
fi
if [[ -z "$APPIMAGE" || ! -f "$APPIMAGE" ]]; then
  echo "error: AppImage not found under $BUILD_DIR" >&2
  exit 1
fi

mkdir -p "$BUILD_DIR"

echo "running AppImage smoke on: $APPIMAGE"
APPIMAGE_EXTRACT_AND_RUN=1 "$APPIMAGE" help > "$BUILD_DIR/smoke-help.txt"

if ! grep -Eq "Proxer Agent|Commands:" "$BUILD_DIR/smoke-help.txt"; then
  echo "error: smoke-help output did not contain expected usage text" >&2
  cat "$BUILD_DIR/smoke-help.txt" >&2
  exit 1
fi

echo "AppImage smoke passed"
