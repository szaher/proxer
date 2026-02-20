#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$ROOT_DIR"

APP_NAME="${PROXER_APP_NAME:-Proxer Agent}"
APP_ID="${PROXER_APP_ID:-io.proxer.agent}"
APP_VERSION="${PROXER_APP_VERSION:-0.1.0}"
HOST_ARCH="$(uname -m)"
TARGET_ARCH="${PROXER_TARGET_ARCH:-$HOST_ARCH}"
BUILD_DIR="${PROXER_BUILD_DIR:-$ROOT_DIR/output/desktop/linux}"
APPIMAGETOOL_BIN="${PROXER_APPIMAGETOOL:-appimagetool}"

if [[ ! -f "$ROOT_DIR/internal/nativeagent/static/index.html" ]]; then
  echo "error: native desktop UI assets are missing. Run: cd agentweb && npm install && npm run sync-native-static" >&2
  exit 1
fi

if ! command -v "$APPIMAGETOOL_BIN" >/dev/null 2>&1 && [[ ! -x "$APPIMAGETOOL_BIN" ]]; then
  echo "error: appimagetool not found. Set PROXER_APPIMAGETOOL or install appimagetool." >&2
  exit 1
fi

case "$TARGET_ARCH" in
  x86_64|amd64)
    GOARCH="amd64"
    APPIMAGE_ARCH="x86_64"
    ;;
  aarch64|arm64)
    GOARCH="arm64"
    APPIMAGE_ARCH="aarch64"
    ;;
  *)
    echo "error: unsupported PROXER_TARGET_ARCH=$TARGET_ARCH (expected x86_64|amd64|aarch64|arm64)" >&2
    exit 1
    ;;
esac

APPDIR="$BUILD_DIR/AppDir"
SAFE_VERSION="$(printf '%s' "$APP_VERSION" | tr -c 'A-Za-z0-9._-' '-')"
OUT_PATH="$BUILD_DIR/proxer-agent-${SAFE_VERSION}-${APPIMAGE_ARCH}.AppImage"
DESKTOP_PATH="$APPDIR/${APP_ID}.desktop"
ICON_PATH="$APPDIR/${APP_ID}.svg"
METADATA_PATH="$APPDIR/usr/share/metainfo/${APP_ID}.appdata.xml"

rm -rf "$APPDIR"
mkdir -p "$APPDIR/usr/bin"
mkdir -p "$APPDIR/usr/share/metainfo"

echo "building proxer-agent linux binary (wails shell enabled)"
GOOS=linux GOARCH="$GOARCH" CGO_ENABLED=1 go build -trimpath -tags wails -o "$APPDIR/usr/bin/proxer-agent" ./cmd/agent
chmod +x "$APPDIR/usr/bin/proxer-agent"

cat > "$APPDIR/AppRun" <<'APPRUN'
#!/usr/bin/env bash
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$HERE/usr/bin/proxer-agent"
if [[ $# -eq 0 ]]; then
  exec "$BIN" gui
fi
exec "$BIN" "$@"
APPRUN
chmod +x "$APPDIR/AppRun"

cat > "$DESKTOP_PATH" <<DESKTOP
[Desktop Entry]
Type=Application
Name=${APP_NAME}
Exec=proxer-agent gui
Icon=${APP_ID}
Categories=Development;
Keywords=localhost;tunnel;proxy;routing;
Terminal=false
Comment=Secure localhost routing and connector runtime
DESKTOP

cat > "$METADATA_PATH" <<APPDATA
<?xml version="1.0" encoding="UTF-8"?>
<component type="desktop-application">
  <id>${APP_ID}</id>
  <name>${APP_NAME}</name>
  <summary>Secure localhost routing and connector runtime</summary>
  <description>
    <p>Proxer Agent pairs with the Proxer gateway and forwards HTTP/HTTPS traffic to local services.</p>
  </description>
  <metadata_license>CC0-1.0</metadata_license>
  <project_license>LicenseRef-proprietary</project_license>
  <launchable type="desktop-id">${APP_ID}.desktop</launchable>
</component>
APPDATA

cat > "$ICON_PATH" <<'SVG'
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128" fill="none">
  <defs>
    <linearGradient id="g" x1="16" y1="12" x2="112" y2="116" gradientUnits="userSpaceOnUse">
      <stop stop-color="#3CA6FF"/>
      <stop offset="1" stop-color="#4D6EFF"/>
    </linearGradient>
  </defs>
  <rect x="8" y="8" width="112" height="112" rx="28" fill="#0E1524"/>
  <rect x="8" y="8" width="112" height="112" rx="28" stroke="#293958" stroke-width="4"/>
  <path d="M31 69.5L52 48.5L66.5 63L97 32.5" stroke="url(#g)" stroke-width="10" stroke-linecap="round" stroke-linejoin="round"/>
  <circle cx="31" cy="69" r="7" fill="#59D1FF"/>
  <circle cx="97" cy="33" r="7" fill="#9FAFFF"/>
</svg>
SVG

if [[ -x "$APPIMAGETOOL_BIN" ]]; then
  APPIMAGETOOL_EXEC="$APPIMAGETOOL_BIN"
else
  APPIMAGETOOL_EXEC="$(command -v "$APPIMAGETOOL_BIN")"
fi

echo "packaging AppImage: $OUT_PATH"
ARCH="$APPIMAGE_ARCH" "$APPIMAGETOOL_EXEC" "$APPDIR" "$OUT_PATH"

echo "built AppImage: $OUT_PATH"
