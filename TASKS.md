# TASKS

## Legend
- Status: `blocked`, `pending`, `in_progress`, `done`

## Backlog (Dependency Ordered)
| ID | Task | Est. | Acceptance Criteria | Test Expectation | Files/Modules | Status |
|---|---|---:|---|---|---|---|
| T01 | Confirm legal authorization + target scope | 0.5h | Authorization and target URLs documented in `CLONE_SPEC.md` | N/A | `CLONE_SPEC.md` | done |
| T02 | Discovery sweep of target concept/features | 1h | Functional feature inventory completed for ngrok-like core | Checklist complete | `CLONE_SPEC.md` | done |
| T03 | Map top MVP journeys | 1h | Multi-port routing journey + ops journey mapped | Journey checklist complete | `CLONE_SPEC.md` | done |
| T04 | Finalize architecture and control-plane model | 1.5h | Gateway/agent architecture, APIs, and data model documented | Architecture review checklist | `ARCHITECTURE.md` | done |
| T05 | Bootstrap Go project skeleton | 1h | Buildable module with gateway and agent commands | `go test ./... -run TestDoesNotExist` passes | `go.mod`, `cmd/*`, `internal/*` | done |
| T06 | Implement gateway ingress + tunnel dispatcher | 2h | `/t/{id}` routes to active tunnel sessions | Integration test exercises dispatch | `internal/gateway/*` | done |
| T07 | Implement agent registration/poll/respond loop | 2h | Agent registers tunnels, pulls requests, forwards to local targets | Integration test validates forwarding | `internal/agent/*` | done |
| T08 | Implement metrics/metering API | 1h | `/api/tunnels` exposes request/error/latency/bytes per tunnel | Integration test asserts request counts > 0 | `internal/gateway/hub.go`, `internal/gateway/server.go` | done |
| T09 | Add tunnel security controls | 1h | Optional per-tunnel token enforcement works | Manual + integration-compatible checks | `internal/gateway/server.go`, `internal/agent/config.go` | done |
| T10 | Add local Docker Compose runtime | 1h | `docker compose config` validates and services are defined | Compose config check passes | `Dockerfile`, `docker-compose.yml`, `README.md` | done |
| T11 | Add integration tests for core journey + metering | 1.5h | Multi-tunnel forwarding and metrics covered | `go test ./...` passes | `tests/integration/tunnel_integration_test.go` | done |
| T12 | Add CI workflow (Go test + smoke) | 1h | GitHub Actions run tests on push/PR | CI green | `.github/workflows/*` | pending |
| T13 | Add persistence for analytics/metering | 2h | Metrics survive restarts and support historical queries | Integration test with restart validation | `internal/gateway/*`, storage modules | pending |
| T14 | GEO/SEO parity hardening plan | 1h | Document production edge roadmap (latency, GEO routing, SEO surfaces) | Review checklist complete | `CLONE_SPEC.md`, `ARCHITECTURE.md` | pending |

## Immediate Next Task
- Execute `T12` (CI pipeline) or `T13` (persistent metering) based on priority.
