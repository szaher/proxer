# Proxer

Proxer is an ngrok-style gateway + host-agent system for routing public HTTP/HTTPS traffic to apps running on localhost.

## V2 Capabilities in This Build

- Role-aware auth model: `super_admin`, `tenant_admin`, `member`.
- Member write policy toggle (`PROXER_MEMBER_WRITE_ENABLED`) to allow/deny member route+connector mutations.
- Super-admin APIs and UI surfaces for:
  - users
  - tenants
  - plans
  - TLS certificates
  - system status and incidents
- Tenant/user APIs and UI for:
  - dashboard gauges (routes/connectors/traffic)
  - routes
  - connectors
  - tenant environment config
- Hard plan enforcement:
  - route and connector caps (`403`)
  - tenant/route rate limits (`429`)
  - per-route custom `max_rps` override (bounded by tenant plan max RPS)
  - monthly traffic cap (`429`)
- Request/response proxy fidelity:
  - method, query params, headers, cookies, body, response status, response headers
- Connector pairing model:
  - one-time pair token
  - hashed connector credentials
  - connector-bound route targets (`local_scheme`, `local_host`, `local_port`, `local_base_path`)
- Built-in frontend served by the gateway from embedded static assets.
- Pluggable state persistence with drivers:
  - `sqlite` (default, persisted)
  - `memory` (ephemeral)

## Local Run (Docker Compose)

```bash
docker compose up --build
```

Gateway UI:

- [http://localhost:18080](http://localhost:18080)

Default super admin login:

- Username: `admin`
- Password: `admin123`

State is persisted to SQLite in Docker volume `gateway-data` (`/data/proxer.db` in the container).

## Host Agent Pairing Flow

1. Login to the UI.
2. Create a connector for your tenant.
3. Click **Pair** to get a one-time pair command.
4. Run the agent on the host machine:

```bash
PROXER_GATEWAY_BASE_URL=http://localhost:18080 \
PROXER_AGENT_PAIR_TOKEN=<pair-token> \
proxer-agent
```

5. Create a route bound to that connector and local target.
6. Access the public route:

```bash
curl -i "http://localhost:18080/t/<tenant>/<route>/"
```

## Native Agent V1 (GUI + CLI)

`proxer-agent` now supports both:

- legacy env mode (fully backward compatible), and
- managed native mode with local profiles + OS secret storage.

Run GUI mode:

```bash
proxer-agent gui
```

GUI shell selection:

- `PROXER_AGENT_GUI_SHELL=auto` (default): prefer Wails host if compiled, otherwise browser shell.
- `PROXER_AGENT_GUI_SHELL=wails`: require Wails host shell.
- `PROXER_AGENT_GUI_SHELL=browser`: force browser shell.

Wails host shell (menu-bar/tray + window) is available when building with the `wails` build tag:

```bash
cd agentweb
npm install
npm run sync-native-static
cd ..
go get github.com/wailsapp/wails/v3/pkg/application
go build -tags wails -o proxer-agent ./cmd/agent
PROXER_AGENT_GUI_SHELL=wails ./proxer-agent gui
```

The Wails host shell uses the React desktop UI bundle embedded from `internal/nativeagent/static/` and invokes backend service methods exposed in `internal/nativeagent/bindings.go` for profile/runtime operations.

Run managed profile mode:

```bash
proxer-agent run --profile my-mac
```

If `PROXER_*` env vars are present and no managed profile is specified, startup behavior remains legacy-compatible.

### Managed CLI commands

- `proxer-agent status [--json]`
- `proxer-agent logs [--follow] [--tail 200]`
- `proxer-agent profile list`
- `proxer-agent profile add --name <name> [--gateway <url>] [--mode connector|legacy_tunnels]`
- `proxer-agent profile edit <name-or-id> [flags]`
- `proxer-agent profile remove <name-or-id>`
- `proxer-agent profile use <name-or-id>`
- `proxer-agent pair --token <pair_token> [--profile <name-or-id>]`
- `proxer-agent config get <key>`
- `proxer-agent config set <key> <value>`
- `proxer-agent update check`

### Native GUI local APIs

- `GET /api/status`
- `GET /api/events/runtime` (SSE stream)
- `GET /api/logs?tail=250`
- `GET /api/profiles`
- `POST /api/profiles`
- `PUT /api/profiles/{id}`
- `DELETE /api/profiles/{id}`
- `POST /api/profiles/{id}/use`
- `POST /api/profiles/{id}/pair`

### Native config locations

- macOS: `~/Library/Application Support/proxer-agent/`
- Linux: `~/.config/proxer-agent/`
- Windows: `%AppData%\\ProxerAgent\\`

Non-secret settings are stored in `settings.json`. Secrets are stored via OS-backed mechanisms:

- macOS Keychain (`security`)
- Linux Secret Service (`secret-tool`)
- Windows DPAPI-backed encrypted store

## Core API Surface

### Auth

- `POST /api/auth/login`
- `POST /api/auth/logout`
- `GET /api/auth/me`
- `POST /api/auth/register`

### Super Admin

- `GET /api/admin/users`
- `POST /api/admin/users`
- `PATCH /api/admin/users/{id}`
- `GET /api/admin/stats`
- `GET /api/admin/incidents`
- `GET /api/admin/system-status`
- `GET /api/admin/plans`
- `POST /api/admin/plans`
- `PATCH /api/admin/plans/{id}`
- `POST /api/admin/tenants/{tenantId}/assign-plan`
- `GET /api/admin/tls/certificates`
- `POST /api/admin/tls/certificates`
- `PATCH /api/admin/tls/certificates/{id}`
- `DELETE /api/admin/tls/certificates/{id}`

### Tenant/User

- `GET /api/me/dashboard`
- `GET /api/me/routes`
- `GET /api/me/connectors`
- `GET /api/me/usage`

### Tenant Configuration

- `GET /api/tenants`
- `POST /api/tenants`
- `DELETE /api/tenants/{tenantId}`
- `GET /api/tenants/{tenantId}/environment`
- `PUT /api/tenants/{tenantId}/environment`
- `GET /api/tenants/{tenantId}/routes`
- `POST /api/tenants/{tenantId}/routes`
- `DELETE /api/tenants/{tenantId}/routes/{routeId}`

Route payload supports:

- `connector_id`, `local_scheme`, `local_host`, `local_port`, `local_base_path`
- `max_rps` (optional per-route runtime cap)

### Connectors

- `GET /api/connectors`
- `POST /api/connectors`
- `POST /api/connectors/{id}/pair`
- `POST /api/connectors/{id}/rotate`
- `DELETE /api/connectors/{id}`

### Agent Control Plane

- `POST /api/agent/pair`
- `POST /api/agent/register`
- `GET /api/agent/pull`
- `POST /api/agent/respond`
- `POST /api/agent/heartbeat`

### Traffic Routing

- `GET /t/{tenantId}/{routeId}/...`
- `GET /t/{routeId}/...` (legacy default tenant compatibility)

## Storage Drivers

- Default driver: `sqlite`
- Alternative driver: `memory`
- SQLite migrations run at startup from `internal/store/sqlite_migrations/`.
- Current SQLite persistence model stores versioned JSON snapshots in SQLite (single-node friendly default).

## Frontend Workspace

A React + TypeScript + Vite source workspace for gateway UI is included in `web/`.

```bash
cd web
npm install
npm run dev
npm run build
npm run sync-static
```

The gateway serves static assets from `internal/gateway/static/`.

## Desktop UI Workspace

A dedicated React + TypeScript + Vite workspace for native desktop agent UI is included in `agentweb/`.

```bash
cd agentweb
npm install
npm run dev
npm run sync-native-static
```

The native agent embeds static desktop assets from `internal/nativeagent/static/`.

### macOS packaging helpers

For production macOS desktop release packaging/signing/notarization:

```bash
./scripts/desktop/macos/build_app.sh
./scripts/desktop/macos/notarize_app.sh
```

Detailed steps: `docs/native-agent-macos-release.md`.

CI workflow:

- `.github/workflows/desktop-macos.yml` builds macOS app artifacts on `workflow_dispatch` or `desktop-agent-v*` tags.
  It uploads zipped app package with checksums, CycloneDX SBOMs, and release notes.

### Linux packaging helper

Build AppImage:

```bash
./scripts/desktop/linux/build_appimage.sh
./scripts/desktop/linux/e2e_host_smoke.sh
./scripts/desktop/linux/e2e_docker_smoke.sh
```

Detailed steps: `docs/native-agent-linux-release.md`.

CI workflow:

- `.github/workflows/desktop-linux.yml` builds Linux AppImage artifacts on `workflow_dispatch` or `desktop-agent-v*` tags.
  Artifacts include package, `checksums.txt`, CycloneDX SBOMs, `release-notes.md`, and smoke output.

### Windows packaging helper

Build MSI:

```powershell
./scripts/desktop/windows/build_msi.ps1 -Version "0.1.0"
./scripts/desktop/windows/e2e_host_smoke.ps1 -OutputDir "output/desktop/windows"
```

Detailed steps: `docs/native-agent-windows-release.md`.

CI workflow:

- `.github/workflows/desktop-windows.yml` builds Windows MSI artifacts on `workflow_dispatch` or `desktop-agent-v*` tags.
  It validates install/uninstall smoke and publishes package + checksums + SBOM + release notes.

## Environment Variables

- `PROXER_SUPER_ADMIN_USER`
- `PROXER_SUPER_ADMIN_PASSWORD`
- `PROXER_ADMIN_USER`
- `PROXER_ADMIN_PASSWORD`
- `PROXER_SESSION_TTL`
- `PROXER_PROXY_REQUEST_TIMEOUT`
- `PROXER_MAX_REQUEST_BODY_BYTES`
- `PROXER_MAX_RESPONSE_BODY_BYTES`
- `PROXER_MAX_PENDING_PER_SESSION`
- `PROXER_MAX_PENDING_GLOBAL`
- `PROXER_PAIR_TOKEN_TTL`
- `PROXER_STORAGE_DRIVER`
- `PROXER_SQLITE_PATH`
- `PROXER_MEMBER_WRITE_ENABLED`
- `PROXER_TLS_LISTEN_ADDR`
- `PROXER_TLS_KEY_ENCRYPTION_KEY`
- `PROXER_AGENT_CONFIG_DIR`
- `PROXER_AGENT_PROXY_URL`
- `PROXER_AGENT_NO_PROXY`
- `PROXER_AGENT_TLS_SKIP_VERIFY`
- `PROXER_AGENT_CA_FILE`
- `PROXER_AGENT_LOG_LEVEL`

## Tests

```bash
go test ./...
```

UI smoke (React app) with Playwright CLI:

```bash
# Start gateway first
docker compose up -d --build gateway

# Run UI smoke (logs in and creates a route)
./tests/e2e/ui_smoke.sh
```

Optional overrides:

- `PROXER_E2E_BASE_URL`
- `PROXER_E2E_USER`
- `PROXER_E2E_PASSWORD`
- `PROXER_E2E_TARGET`
- `PROXER_E2E_ROUTE_ID`
- `PROXER_E2E_CONNECTOR_ID`

Integration tests cover:

- route/connectors flow
- connector pairing and connector-bound routing
- oversized request rejection (`413`)
- backpressure rejection (`503`)
- plan route limit enforcement (`403`)
- rate-limit rejection (`429`)
- super-admin bootstrap/admin access
- SQLite persistence across restart
- route-specific `max_rps` enforcement
- member write-policy enforcement

CI gates now include:

- `go test ./...`
- `web` build (`npm ci && npm run build`)
- Playwright UI smoke via `tests/e2e/ui_smoke.sh`
