#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

LIGHTHOUSE_IMAGE="${PROXER_LIGHTHOUSE_IMAGE:-femtopixel/google-lighthouse}"
LIGHTHOUSE_BASE_URL="${PROXER_LIGHTHOUSE_BASE_URL:-http://host.docker.internal:18080}"
ARTIFACT_DIR="${PROXER_LIGHTHOUSE_ARTIFACT_DIR:-output/lighthouse}"
MAX_ATTEMPTS="${PROXER_LIGHTHOUSE_MAX_ATTEMPTS:-4}"

mkdir -p "$ARTIFACT_DIR"

validate_report() {
  local label="$1"
  local categories="$2"
  local thresholds_json="$3"
  TARGET_PATH="$ARTIFACT_DIR/${label}.report.json" \
  TARGET_LABEL="$label" \
  TARGET_CATEGORIES="$categories" \
  TARGET_THRESHOLDS="$thresholds_json" \
  node <<'NODE'
const fs = require("node:fs");

const reportPath = process.env.TARGET_PATH;
const label = process.env.TARGET_LABEL;
const categories = String(process.env.TARGET_CATEGORIES || "")
  .split(",")
  .map((v) => v.trim())
  .filter(Boolean);
const thresholds = JSON.parse(process.env.TARGET_THRESHOLDS || "{}");
const report = JSON.parse(fs.readFileSync(reportPath, "utf8"));

if (report.runtimeError) {
  throw new Error(`${label}: runtimeError ${report.runtimeError.code}: ${report.runtimeError.message}`);
}

for (const category of categories) {
  const score = report.categories?.[category]?.score;
  if (typeof score !== "number") {
    throw new Error(`${label}: missing numeric score for category "${category}"`);
  }
  const min = Number(thresholds[category] ?? 0);
  if (score < min) {
    throw new Error(`${label}: ${category} score ${score.toFixed(2)} < threshold ${min.toFixed(2)}`);
  }
}

const summary = Object.fromEntries(categories.map((category) => [category, report.categories?.[category]?.score]));
console.log(`${label}:`, JSON.stringify(summary));
NODE
}

run_audit() {
  local route="$1"
  local label="$2"
  local categories="$3"
  local thresholds_json="$4"
  local url="${LIGHTHOUSE_BASE_URL%/}${route}"

  local attempt=1
  while [[ "$attempt" -le "$MAX_ATTEMPTS" ]]; do
    echo "Running Lighthouse (${attempt}/${MAX_ATTEMPTS}): $url"
    if docker run --rm \
      --add-host=host.docker.internal:host-gateway \
      -v "$PWD/$ARTIFACT_DIR:/tmp/reports" \
      "$LIGHTHOUSE_IMAGE" \
      "$url" \
      --output json \
      --output html \
      --output-path "/tmp/reports/${label}" \
      --quiet \
      --only-categories="$categories" && \
      validate_report "$label" "$categories" "$thresholds_json"; then
      return 0
    fi

    if [[ "$attempt" -lt "$MAX_ATTEMPTS" ]]; then
      echo "Retrying Lighthouse for $label after failed attempt $attempt"
      sleep 2
    fi
    attempt="$((attempt + 1))"
  done

  echo "Lighthouse audit failed for $label after ${MAX_ATTEMPTS} attempts" >&2
  return 1
}

run_audit "/" "home" "performance,seo,accessibility,best-practices" \
  '{"performance":0.60,"seo":0.95,"accessibility":0.95,"best-practices":0.75}'

run_audit "/signup" "signup" "seo,accessibility,best-practices" \
  '{"seo":0.95,"accessibility":0.95,"best-practices":0.75}'

echo "Lighthouse public audit passed"
