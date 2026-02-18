#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <target-dir> <artifact1> [artifact2 ...]" >&2
  exit 1
fi

TARGET_DIR="$1"
shift

mkdir -p "$TARGET_DIR"
TARGET_DIR_ABS="$(cd "$TARGET_DIR" && pwd)"
find "$TARGET_DIR_ABS" -mindepth 1 -maxdepth 1 -exec rm -rf {} +

COPIED_BASENAMES=()

for artifact in "$@"; do
  if [[ ! -f "$artifact" ]]; then
    echo "error: artifact not found: $artifact" >&2
    exit 1
  fi
  base="$(basename "$artifact")"
  cp -f "$artifact" "$TARGET_DIR_ABS/$base"
  COPIED_BASENAMES+=("$base")
done

(
  cd "$TARGET_DIR_ABS"
  if command -v shasum >/dev/null 2>&1; then
    : > checksums.txt
    for base in "${COPIED_BASENAMES[@]}"; do
      shasum -a 256 "$base" >> checksums.txt
    done
  else
    : > checksums.txt
    for base in "${COPIED_BASENAMES[@]}"; do
      sha256sum "$base" >> checksums.txt
    done
  fi
)

GOBIN="$(go env GOBIN)"
if [[ -z "$GOBIN" ]]; then
  GOBIN="$(go env GOPATH)/bin"
fi
mkdir -p "$GOBIN"
CYCLONEDX_GOMOD="$GOBIN/cyclonedx-gomod"

if ! command -v cyclonedx-gomod >/dev/null 2>&1 && [[ ! -x "$CYCLONEDX_GOMOD" ]]; then
  go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.8.0
fi

if command -v cyclonedx-gomod >/dev/null 2>&1; then
  CYCLONEDX_GOMOD="$(command -v cyclonedx-gomod)"
fi
"$CYCLONEDX_GOMOD" mod -licenses -json -output "$TARGET_DIR_ABS/sbom-go.cdx.json"

if [[ -f "agentweb/package-lock.json" ]]; then
  (cd agentweb && npx --yes @cyclonedx/cyclonedx-npm --output-format JSON --output-file "$TARGET_DIR_ABS/sbom-npm.cdx.json")
fi

TIMESTAMP="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
{
  echo "# Proxer Desktop Release Notes"
  echo
  echo "- Generated at: $TIMESTAMP"
  echo "- Commit: ${GITHUB_SHA:-unknown}"
  echo "- Ref: ${GITHUB_REF_NAME:-unknown}"
  echo
  echo "## Artifacts"
  for base in "${COPIED_BASENAMES[@]}"; do
    echo "- $base"
  done
  echo
  echo "## Included Metadata"
  echo "- checksums.txt (SHA-256)"
  echo "- sbom-go.cdx.json"
  if [[ -f "$TARGET_DIR_ABS/sbom-npm.cdx.json" ]]; then
    echo "- sbom-npm.cdx.json"
  fi
} > "$TARGET_DIR_ABS/release-notes.md"
