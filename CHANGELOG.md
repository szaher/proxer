# CHANGELOG

## 2026-02-16

### Implemented
- Hardened control plane and proxy behavior:
  - configurable request/response size limits
  - configurable proxy timeout separate from generic request timeout
  - per-session and global pending-request backpressure limits
  - dispatch error mapping to deterministic `502` / `503` / `504`
  - request correlation with `X-Proxer-Request-ID`
  - stricter response ownership checks (`session_id`, `request_id`, `tunnel_id`)
  - pending request cancellation when sessions are removed/expired
- Added connector platform primitives:
  - in-memory connector store
  - hashed connector credentials
  - one-time pair tokens with TTL
  - credential rotation and connector deletion
- Added connector APIs:
  - `GET /api/connectors`
  - `POST /api/connectors`
  - `POST /api/connectors/{id}/pair`
  - `POST /api/connectors/{id}/rotate`
  - `DELETE /api/connectors/{id}`
  - `POST /api/agent/pair`
- Added connector-mode agent registration:
  - `RegisterRequest` now supports `connector_id` + `connector_secret`
  - host agent can bootstrap credentials from pair token
- Extended route model for connector-bound routing:
  - `connector_id`, `local_scheme`, `local_host`, `local_port`, `local_base_path`
  - connector routes dispatch to connector sessions, then to localhost targets
- Updated frontend UI:
  - connector management card
  - route form supports connector binding + local target fields
  - pair command surfaced in UI responses
- Updated docs and compose defaults for hardening env vars.

### Tests
- Added integration coverage for:
  - connector pairing + connector-bound route forwarding
  - oversized request rejection (`413`)
  - backpressure rejection (`503`)
- Existing integration suite remains green.

## 2026-02-15
- Initialized planning artifacts:
  - `CLONE_SPEC.md`
  - `ARCHITECTURE.md`
  - `TASKS.md`
  - `TEST_PLAN.md`
- Captured final requirements from user for ngrok-like functional clone.

### Implemented
- Built Go module with two binaries:
  - `proxer-gateway` (`/cmd/gateway`)
  - `proxer-agent` (`/cmd/agent`)
- Implemented gateway features:
  - traffic ingress at `/t/{tunnel-id}/...`
  - agent control plane APIs (`register`, `pull`, `respond`, `heartbeat`)
  - tunnel metrics/metering and dashboard
  - optional per-tunnel token enforcement
- Implemented agent features:
  - tunnel registration
  - long-poll control loop
  - request forwarding to localhost targets on multiple ports
- Added local runtime:
  - `Dockerfile`
  - `docker-compose.yml`
  - `README.md`
- Added gateway configuration UI and rule APIs:
  - create/update/delete forward rules in browser
  - direct forwarding from gateway-managed rules when no agent tunnel is connected
- Added multi-tenant frontend and APIs:
  - create/delete tenants
  - create/delete routes per tenant
  - tenant-scoped routing URL pattern: `/t/{tenant}/{route}/...`
  - backward compatibility for legacy default-tenant routes: `/t/{route}/...`
- Improved request fidelity passthrough:
  - preserves raw query params, headers, cookies/auth credentials, and body in forwarded requests
  - preserves upstream `Set-Cookie` response headers
- Added integration test:
  - `tests/integration/tunnel_integration_test.go`

### Validation
- `go test ./...` passed.
- `docker compose config` passed.

### Notes
- Control-plane transport uses HTTP long-polling (stdlib-only) to avoid external dependency fetch restrictions in this environment.
