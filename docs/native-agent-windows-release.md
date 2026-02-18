# Native Agent Windows Release (MSI)

This flow packages desktop `proxer-agent` for Windows as an MSI.

## Prerequisites

- Windows build host with Go + Node + PowerShell.
- WiX Toolset v4 CLI available (`wix` command).
- Desktop UI assets synced:

```powershell
cd agentweb
npm install
npm run sync-native-static
cd ..
```

## Build MSI

```powershell
./scripts/desktop/windows/build_msi.ps1 -Version "0.1.0"
./scripts/desktop/windows/e2e_host_smoke.ps1 -OutputDir "output/desktop/windows"
```

Output:

- `output/desktop/windows/proxer-agent-<version>-x64.msi`

## Notes

- Installer currently deploys `proxer-agent.exe` to `Program Files\Proxer Agent`.
- Signing/notarization equivalent for Windows can be layered by signing the exe/msi in CI.
- Secret storage uses DPAPI-backed encrypted storage.
