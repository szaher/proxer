#!/usr/bin/env bash
set -euo pipefail

if ! command -v npx >/dev/null 2>&1; then
  echo "npx is required (install Node.js/npm first)." >&2
  exit 1
fi

BASE_URL="${PROXER_E2E_BASE_URL:-http://127.0.0.1:18080}"
LOGIN_URL="${BASE_URL%/}/login"
E2E_USER="${PROXER_E2E_USER:-admin}"
E2E_PASSWORD="${PROXER_E2E_PASSWORD:-admin123}"
E2E_TARGET="${PROXER_E2E_TARGET:-http://127.0.0.1:3000}"
ROUTE_ID="${PROXER_E2E_ROUTE_ID:-e2e$(date +%s)}"
CONNECTOR_ID="${PROXER_E2E_CONNECTOR_ID:-conn-${ROUTE_ID}}"
SESSION="${PROXER_E2E_SESSION:-proxer-e2e-$$}"
ARTIFACT_DIR="${PROXER_E2E_ARTIFACT_DIR:-output/playwright}"

mkdir -p "$ARTIFACT_DIR"

export PROXER_E2E_USER="$E2E_USER"
export PROXER_E2E_PASSWORD="$E2E_PASSWORD"
export PROXER_E2E_TARGET="$E2E_TARGET"
export PROXER_E2E_ROUTE_ID="$ROUTE_ID"
export PROXER_E2E_CONNECTOR_ID="$CONNECTOR_ID"
export PROXER_E2E_ARTIFACT_DIR="$ARTIFACT_DIR"

PW_BASE=(npx --yes --package @playwright/cli playwright-cli)
PW=("${PW_BASE[@]}" "-s=${SESSION}")

js_escape() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\'/\\\'}"
  printf '%s' "$value"
}

JS_USER="$(js_escape "$E2E_USER")"
JS_PASSWORD="$(js_escape "$E2E_PASSWORD")"
JS_TARGET="$(js_escape "$E2E_TARGET")"
JS_ROUTE_ID="$(js_escape "$ROUTE_ID")"
JS_CONNECTOR_ID="$(js_escape "$CONNECTOR_ID")"
JS_SCREENSHOT_PATH="$(js_escape "${ARTIFACT_DIR}/ui-smoke.png")"

cleanup() {
  "${PW[@]}" close >/dev/null 2>&1 || true
}
trap cleanup EXIT

run_code() {
  local code="$1"
  local output
  output="$("${PW[@]}" run-code "$code" 2>&1)" || {
    echo "$output" >&2
    return 1
  }
  echo "$output"
  if grep -q "### Error" <<<"$output"; then
    echo "playwright-cli run-code returned an error" >&2
    return 1
  fi
}

"${PW[@]}" open "$LOGIN_URL" --browser chrome

run_code "$(cat <<JS
async page => {
  await page.waitForLoadState('domcontentloaded');
  await page.getByLabel('Username').waitFor({ timeout: 15000 });
  await page.getByLabel('Username').fill('${JS_USER}');
  await page.getByLabel('Password').fill('${JS_PASSWORD}');
  await page.getByRole('button', { name: /login/i }).click();
  await page.locator('.workspace-shell').waitFor({ timeout: 15000 });
}
JS
)"

run_code "$(cat <<'JS'
async page => {
  const connectorNav = page.locator('.nav button', { hasText: /^Connectors$/ }).first();
  await connectorNav.waitFor({ timeout: 10000 });
  await connectorNav.click();
  await page.getByRole('heading', { name: 'Create Connector' }).first().waitFor({ timeout: 10000 });
}
JS
)"

run_code "$(cat <<JS
async page => {
  const connectorID = '${JS_CONNECTOR_ID}';
  const createSection = page.locator('section', { hasText: 'Create Connector' }).first();
  const form = createSection.locator('form').first();
  const tenantSelect = form.locator("select[name='tenant_id']");
  if ((await tenantSelect.count()) > 0) {
    const firstTenant = await tenantSelect.locator('option').first().getAttribute('value');
    if (firstTenant) {
      await tenantSelect.selectOption(firstTenant);
    }
  }

  await form.locator("input[name='id']").fill(connectorID);
  await form.locator("input[name='name']").fill('E2E Connector');
  await form.locator("button[type='submit']").click();

  const connectorRow = page.locator('table tbody tr', { hasText: connectorID }).first();
  await connectorRow.waitFor({ timeout: 15000 });
  await connectorRow.locator('button', { hasText: /^Pair$/ }).click();

  const output = page.locator('p.output').first();
  await output.waitFor({ timeout: 15000 });
  const pairCommand = await output.textContent();
  if (!pairCommand || !pairCommand.includes('PROXER_AGENT_PAIR_TOKEN=')) {
    throw new Error('Pair command was not generated for connector');
  }
}
JS
)"

run_code "$(cat <<'JS'
async page => {
  const routeNav = page.locator('.nav button', { hasText: /^Routes$/ }).first();
  await routeNav.waitFor({ timeout: 10000 });
  await routeNav.click();
  await page.getByRole('heading', { name: 'Create Route' }).first().waitFor({ timeout: 10000 });
}
JS
)"

run_code "$(cat <<JS
async page => {
  const routeId = '${JS_ROUTE_ID}';
  const target = '${JS_TARGET}';
  const createSection = page.locator('section', { hasText: 'Create Route' }).first();
  const form = createSection.locator('form').first();

  const tenantSelect = form.locator("select[name='tenant_id']");
  if ((await tenantSelect.count()) > 0) {
    const firstTenant = await tenantSelect.locator('option').first().getAttribute('value');
    if (firstTenant) {
      await tenantSelect.selectOption(firstTenant);
    }
  }

  await form.locator("input[name='id']").fill(routeId);
  await form.locator("input[name='target']").fill(target);
  await form.locator("select[name='connector_id']").selectOption('');
  await form.locator("input[name='max_rps']").fill('5');
  await form.locator("button[type='submit']").click();

  const routeRow = page.locator('table tbody tr', { hasText: routeId }).first();
  await routeRow.waitFor({ timeout: 15000 });
}
JS
)"

run_code "$(cat <<JS
async page => {
  await page.screenshot({
    path: '${JS_SCREENSHOT_PATH}',
    fullPage: true,
  });
}
JS
)"

"${PW[@]}" state-save "$ARTIFACT_DIR/storage-state.json"

echo "UI smoke passed. Created route: $ROUTE_ID"
