# CLONE_SPEC

## Objective
Build an authorized, local-first ngrok-style system inspired by [ngrok](https://ngrok.com/) that can route inbound web traffic to multiple localhost ports with low latency, security controls, and usage metering.

## Authorization Confirmation
- User confirmed explicit permission to replicate the target concept.
- Target inspiration: `https://ngrok.com/` (functional equivalence, not brand/assets copying).

## User Inputs (Final)
1. Authorized: **Yes**
2. Scope: **Core system concept** (traffic ingress and local forwarding)
3. Fidelity: **Functional equivalence** (SEO/GEO-competitive direction, not pixel clone)
4. Core MVP journey: **Redirect web traffic to localhost using different ports**
5. Constraints: **Very low latency, security-focused**
6. Integrations: **Analytics and metering now; extensibility later**
7. Delivery: **Must run locally with Docker Compose**

## Implemented MVP Scope
### In Scope
- Gateway service with public ingress route pattern: `/t/{tunnel-id}/...`
- Agent service that registers multiple local targets (different ports)
- Request forwarding from gateway to local targets with headers/body/query preserved
- Tunnel-level analytics and metering:
  - request count
  - error count
  - bytes in/out
  - average latency
- Optional per-tunnel access token protection
- Local Docker Compose deployment for full system
- Integration test covering multi-tunnel routing and metrics

### Out of Scope (Next Iterations)
- Public Internet edge POP/GEO routing layer
- Persistent metering storage (currently in-memory)
- TLS termination + custom domains
- Team/org auth UI, billing, and account management
- Full SEO/GEO optimization features for marketing/site surfaces

## Feature Inventory
| Area | Component | User Role | Behavior | Notes |
|---|---|---|---|---|
| Gateway UI | `/` | Operator | Shows active tunnels and live metrics | Lightweight dashboard |
| Gateway API | `/api/health` | Operator/Automation | Health and transport status | For smoke checks |
| Gateway API | `/api/tunnels` | Operator/Automation | Lists connected tunnels and metrics | Metering endpoint |
| Agent Control | `/api/agent/register` | Agent | Registers tunnel map and gets session | Token-protected |
| Agent Control | `/api/agent/pull` | Agent | Long-poll for inbound proxy requests | Low overhead control plane |
| Agent Control | `/api/agent/respond` | Agent | Returns proxied response payload | Correlated by request ID |
| Agent Control | `/api/agent/heartbeat` | Agent | Keeps session alive | Session TTL cleanup |
| Traffic Proxy | `/t/{id}/...` | End User | Forwards traffic to mapped localhost target | Per-tunnel optional token |

## Prioritized User Journeys
| Journey | Actor | Trigger | Main Steps | Success State | Failure Modes |
|---|---|---|---|---|---|
| J1: Start tunnel system locally | Operator | Run `docker compose up --build` | Start gateway and agent containers | Agent registers tunnels and appears in `/api/tunnels` | Invalid agent token, bad tunnel config |
| J2: Route traffic to port A | End User | Request `/t/app3000/...` | Gateway dispatches request; agent forwards to target | Response from localhost:3000 returned by gateway | Target offline, timeout, queue saturation |
| J3: Route traffic to port B | End User | Request `/t/app5173/...` | Same flow with second tunnel ID | Response from localhost:5173 returned by gateway | Unknown tunnel ID, target error |
| J4: Observe metering | Operator | Open dashboard or `/api/tunnels` | Review per-tunnel counts/latency/bytes | Metrics reflect routed traffic | Metrics reset on restart (in-memory) |
| J5: Secure sensitive tunnel | Operator | Configure `id@token=url` | Send requests with token header/query | Authorized requests succeed | Missing/invalid token returns 403 |

## Risks and Unknowns
- Current runtime state is in-memory (no persistence after restart).
- Long-poll control plane is adequate for local MVP, but a production-grade edge may prefer multiplexed streams.
- GEO/SEO competitiveness requires additional layers outside this core tunnel MVP.

## Definition of Done (Current Iteration)
- Multi-port localhost routing works end-to-end.
- Local Docker Compose stack is valid.
- Metering API exposes per-tunnel usage stats.
- Integration test validates routing + metrics behavior.
