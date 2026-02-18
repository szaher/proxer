# UI Smoke (Playwright CLI)

This folder contains browser smoke checks for the React UI.

## Run locally

```bash
docker compose up -d --build gateway
./tests/e2e/ui_smoke.sh
```

## Environment overrides

- `PROXER_E2E_BASE_URL` (default: `http://127.0.0.1:18080`)
- `PROXER_E2E_USER` (default: `admin`)
- `PROXER_E2E_PASSWORD` (default: `admin123`)
- `PROXER_E2E_TARGET` (default: `http://127.0.0.1:3000`)
- `PROXER_E2E_ROUTE_ID` (default: generated)
- `PROXER_E2E_CONNECTOR_ID` (default: `conn-<route-id>`)

Artifacts are written to `output/playwright/`.
