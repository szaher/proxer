# Native Agent Linux Release (AppImage)

This flow packages desktop `proxer-agent` for Linux as an AppImage.

## Prerequisites

- Linux build host with Go + Node.
- Build dependencies for Wails GTK/WebKit (distribution-specific).
- `file` and `squashfs-tools` installed.
- `appimagetool` available in `PATH` or set via `PROXER_APPIMAGETOOL`.
- Desktop UI assets synced:

```bash
cd agentweb
npm install
npm run sync-native-static
cd ..
```

## Build AppImage

```bash
./scripts/desktop/linux/build_appimage.sh
```

Output:

- `output/desktop/linux/proxer-agent-<version>-<x86_64|aarch64>.AppImage`

## Notes

- The AppImage launches GUI mode by default.
- `build_appimage.sh` auto-detects host architecture unless overridden via `PROXER_TARGET_ARCH`.
- For containerized host validation, use `./scripts/desktop/linux/e2e_docker_smoke.sh`.
- To pass CLI args through AppImage:

```bash
./proxer-agent-0.1.0-x86_64.AppImage status --json
```

- Secret storage uses Secret Service (`secret-tool`). If unavailable, the agent fails closed with remediation guidance.
