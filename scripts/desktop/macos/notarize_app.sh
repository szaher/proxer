#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$ROOT_DIR"

APP_NAME="${PROXER_APP_NAME:-Proxer Agent}"
BUILD_DIR="${PROXER_BUILD_DIR:-$ROOT_DIR/output/desktop/macos}"
APP_PATH="${PROXER_APP_PATH:-$BUILD_DIR/${APP_NAME}.app}"
ZIP_PATH="${PROXER_ZIP_PATH:-$BUILD_DIR/${APP_NAME}.zip}"

if [[ ! -d "$APP_PATH" ]]; then
  echo "error: app bundle not found at $APP_PATH" >&2
  echo "hint: run scripts/desktop/macos/build_app.sh first" >&2
  exit 1
fi

if ! command -v xcrun >/dev/null 2>&1; then
  echo "error: xcrun not found; install Xcode command line tools" >&2
  exit 1
fi

rm -f "$ZIP_PATH"

echo "creating notarization zip: $ZIP_PATH"
ditto -c -k --keepParent --sequesterRsrc "$APP_PATH" "$ZIP_PATH"

if [[ -n "${PROXER_NOTARY_PROFILE:-}" ]]; then
  echo "submitting to Apple notary service using keychain profile: $PROXER_NOTARY_PROFILE"
  xcrun notarytool submit "$ZIP_PATH" --wait --keychain-profile "$PROXER_NOTARY_PROFILE"
else
  if [[ -z "${APPLE_ID:-}" || -z "${APPLE_TEAM_ID:-}" || -z "${APPLE_APP_PASSWORD:-}" ]]; then
    echo "error: set PROXER_NOTARY_PROFILE or APPLE_ID + APPLE_TEAM_ID + APPLE_APP_PASSWORD" >&2
    exit 1
  fi
  echo "submitting to Apple notary service using Apple ID credentials"
  xcrun notarytool submit "$ZIP_PATH" --wait \
    --apple-id "$APPLE_ID" \
    --team-id "$APPLE_TEAM_ID" \
    --password "$APPLE_APP_PASSWORD"
fi

echo "stapling notarization ticket"
xcrun stapler staple "$APP_PATH"

echo "validating notarized app"
spctl -a -vv "$APP_PATH"

echo "notarization complete for: $APP_PATH"
