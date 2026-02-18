# Native Agent macOS Release

This guide packages the desktop `proxer-agent` app bundle with Wails shell + React UI.

## Prerequisites

- macOS with Xcode command line tools.
- Go installed.
- Desktop UI assets synced:

```bash
cd agentweb
npm install
npm run sync-native-static
cd ..
```

## Build `.app`

```bash
PROXER_CODESIGN_IDENTITY="Developer ID Application: Example, Inc. (TEAMID)" \
./scripts/desktop/macos/build_app.sh
```

Output:

- `output/desktop/macos/Proxer Agent.app`

If `PROXER_CODESIGN_IDENTITY` is omitted, the script builds an unsigned app.

## Notarize + Staple

Option A: keychain profile (recommended)

```bash
PROXER_NOTARY_PROFILE="proxer-notary" \
./scripts/desktop/macos/notarize_app.sh
```

Option B: direct Apple credentials

```bash
APPLE_ID="dev@example.com" \
APPLE_TEAM_ID="TEAMID" \
APPLE_APP_PASSWORD="xxxx-xxxx-xxxx-xxxx" \
./scripts/desktop/macos/notarize_app.sh
```

## Validation

```bash
codesign --verify --deep --strict --verbose=2 "output/desktop/macos/Proxer Agent.app"
spctl -a -vv "output/desktop/macos/Proxer Agent.app"
```
