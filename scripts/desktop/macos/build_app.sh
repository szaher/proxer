#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$ROOT_DIR"

APP_NAME="${PROXER_APP_NAME:-Proxer Agent}"
BUNDLE_ID="${PROXER_BUNDLE_ID:-io.proxer.agent}"
APP_VERSION="${PROXER_APP_VERSION:-}"
BUILD_DIR="${PROXER_BUILD_DIR:-$ROOT_DIR/output/desktop/macos}"
ICON_PATH="${PROXER_APP_ICON:-$ROOT_DIR/assets/macos/AppIcon.icns}"

if [[ -z "$APP_VERSION" ]]; then
  if command -v git >/dev/null 2>&1; then
    APP_VERSION="$(git describe --tags --always --dirty 2>/dev/null || true)"
  fi
fi
if [[ -z "$APP_VERSION" ]]; then
  APP_VERSION="0.1.0"
fi

if [[ ! -f "$ROOT_DIR/internal/nativeagent/static/index.html" ]]; then
  echo "error: native desktop UI assets are missing. Run: cd agentweb && npm install && npm run sync-native-static" >&2
  exit 1
fi

APP_PATH="$BUILD_DIR/${APP_NAME}.app"
MACOS_PATH="$APP_PATH/Contents/MacOS"
RESOURCES_PATH="$APP_PATH/Contents/Resources"

rm -rf "$APP_PATH"
mkdir -p "$MACOS_PATH" "$RESOURCES_PATH"

echo "building proxer-agent binary (wails shell enabled)"
go build -trimpath -tags wails -o "$MACOS_PATH/proxer-agent" ./cmd/agent
chmod +x "$MACOS_PATH/proxer-agent"

cat > "$APP_PATH/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key>
  <string>en</string>
  <key>CFBundleExecutable</key>
  <string>proxer-agent</string>
  <key>CFBundleIdentifier</key>
  <string>${BUNDLE_ID}</string>
  <key>CFBundleInfoDictionaryVersion</key>
  <string>6.0</string>
  <key>CFBundleName</key>
  <string>${APP_NAME}</string>
  <key>CFBundleDisplayName</key>
  <string>${APP_NAME}</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>${APP_VERSION}</string>
  <key>CFBundleVersion</key>
  <string>${APP_VERSION}</string>
  <key>LSMinimumSystemVersion</key>
  <string>11.0</string>
  <key>LSUIElement</key>
  <false/>
</dict>
</plist>
PLIST

echo "APPL????" > "$APP_PATH/Contents/PkgInfo"

if [[ -f "$ICON_PATH" ]]; then
  cp "$ICON_PATH" "$RESOURCES_PATH/AppIcon.icns"
  /usr/libexec/PlistBuddy -c "Add :CFBundleIconFile string AppIcon" "$APP_PATH/Contents/Info.plist" >/dev/null 2>&1 || true
fi

if [[ -n "${PROXER_CODESIGN_IDENTITY:-}" ]]; then
  echo "codesigning app with identity: $PROXER_CODESIGN_IDENTITY"
  codesign --force --deep --options runtime --timestamp \
    --sign "$PROXER_CODESIGN_IDENTITY" "$APP_PATH"
  codesign --verify --deep --strict --verbose=2 "$APP_PATH"
else
  echo "warning: PROXER_CODESIGN_IDENTITY not set, skipping codesign"
fi

echo "built macOS app bundle: $APP_PATH"
