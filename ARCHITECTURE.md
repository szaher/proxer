# ARCHITECTURE

## Overview
Proxer is implemented as a two-service system:
- **Gateway**: accepts inbound HTTP traffic and dispatches requests to connected agents.
- **Agent**: keeps a control session with gateway and forwards requests to configured localhost ports.

The current control plane uses HTTP long-polling (no external dependencies), optimized for local Docker Compose deployment.

## Stack
- Language: Go 1.24
- Runtime: Standard library HTTP servers/clients
- Packaging: Docker + Docker Compose
- Testing: Go integration tests (`tests/integration`)
- Storage: In-memory runtime state (sessions, pending requests, metrics)

## Component Diagram
```mermaid
flowchart LR
  User[External User] -->|HTTP /t/{id}/...| Gateway[Gateway Service]
  Gateway -->|Long-poll dispatch| Agent[Agent Service]
  Agent -->|HTTP forward| LocalA[localhost:3000]
  Agent -->|HTTP forward| LocalB[localhost:5173]
  Agent -->|Response payload| Gateway
  Gateway -->|HTTP response| User
  Gateway -->|/api/tunnels metrics| Ops[Operator Dashboard/API]
```

## Data Model (In-Memory)
| Entity | Fields | Purpose |
|---|---|---|
| Session | `session_id`, `agent_id`, `last_seen`, `tunnels[]`, `queue` | Tracks active agent control session |
| TunnelConfig | `id`, `target`, `token?` | Maps tunnel IDs to local target URLs |
| PendingRequest | `request_id`, `tunnel_id`, `response_channel` | Correlates dispatched proxy requests with agent responses |
| TunnelMetrics | `request_count`, `error_count`, `bytes_in/out`, `avg_latency`, `last_status` | Metering and analytics per tunnel |

## API Contracts
### Operator / Public
- `GET /api/health`
- `GET /api/tunnels`
- `GET /t/{tunnel-id}/...` (proxied traffic)

### Agent Control Plane
- `POST /api/agent/register`
  - Input: `agent_id`, `token`, `tunnels[]`
  - Output: `session_id`, public tunnel routes
- `GET /api/agent/pull?session_id=...&wait=...`
  - Long-poll request dispatch
- `POST /api/agent/respond`
  - Input: `session_id`, proxy response payload
- `POST /api/agent/heartbeat`
  - Session keepalive

## Request Lifecycle
1. Agent registers tunnels and receives `session_id`.
2. Agent continuously long-polls `/api/agent/pull`.
3. User sends request to `/t/{id}/...`.
4. Gateway enqueues a proxy request for the owning agent session.
5. Agent receives request, forwards to mapped localhost target.
6. Agent posts response to gateway.
7. Gateway returns response to user and updates metrics.

## Security Model
- Agent registration token (`PROXER_AGENT_TOKEN`) gates control-plane access.
- Optional per-tunnel token (`id@token=url`) enforced at gateway.
- Hop-by-hop headers removed on forwarding.
- Session TTL cleanup removes stale agent sessions.

## Latency Considerations
- Reuses persistent HTTP transport pools in agent.
- Bounded in-memory queue for dispatch.
- Tight request/agent timeouts to fail fast.
- Long-poll reduces connection churn while staying stdlib-only.

## Observability
- Dashboard and JSON endpoint expose active tunnel metrics.
- Health endpoint for smoke checks.
- Structured logs from gateway and agent processes.

## Deployment
- Local: `docker compose up --build`
- Services:
  - `gateway` on `localhost:8080`
  - `agent` connected to `gateway` and forwarding to `host.docker.internal`
