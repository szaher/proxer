# TEST_PLAN

## Testing Strategy
Primary coverage is integration and end-to-end system behavior (not unit-heavy).

## Coverage Implemented
1. Integration test (`tests/integration/tunnel_integration_test.go`)
- Boots gateway and agent in-process.
- Boots two local upstream services.
- Verifies:
  - tunnel registration
  - request forwarding to two different tunnel IDs/targets
  - response integrity and tunnel headers
  - metering updates (`request_count`, latency visibility)

2. Build/compile validation
- `go test ./... -run TestDoesNotExist`

3. Full suite
- `go test ./...`

## Local Commands
```bash
# Compile/test all packages quickly
GOCACHE=$(pwd)/.gocache GOMODCACHE=$(pwd)/.gomodcache go test ./... -run TestDoesNotExist

# Run full integration suite
GOCACHE=$(pwd)/.gocache GOMODCACHE=$(pwd)/.gomodcache go test ./...

# Validate compose wiring
docker compose config

# Run UI smoke via Playwright CLI (with gateway up)
./tests/e2e/ui_smoke.sh
```

## Docker Runtime Validation
```bash
docker compose up --build
curl -i "http://localhost:8080/t/app3000/"
curl -s "http://localhost:8080/api/tunnels"
```

## CI Coverage
- GitHub Actions workflow (`.github/workflows/ci.yml`) runs:
  - `go test ./...`
  - web build (`web`: `npm ci && npm run build`)
  - UI smoke (`tests/e2e/ui_smoke.sh`) against Docker Compose gateway

## Exit Criteria
- Integration test passes for multi-tunnel forwarding and metering.
- Full Go test suite passes.
- Docker Compose configuration validates.
