#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$ROOT_DIR"

IMAGE="${PROXER_LINUX_SMOKE_IMAGE:-ubuntu:24.04}"

HOST_ARCH="$(uname -m)"
case "$HOST_ARCH" in
  x86_64|amd64)
    DEFAULT_PLATFORM="linux/amd64"
    ;;
  aarch64|arm64)
    DEFAULT_PLATFORM="linux/arm64"
    ;;
  *)
    echo "error: unsupported host arch for docker smoke: $HOST_ARCH" >&2
    exit 1
    ;;
esac

PLATFORM="${PROXER_LINUX_SMOKE_PLATFORM:-$DEFAULT_PLATFORM}"

DOCKER_ARGS=(run --rm)
if [[ -n "$PLATFORM" ]]; then
  DOCKER_ARGS+=(--platform "$PLATFORM")
fi
DOCKER_ARGS+=(
  -v "$ROOT_DIR":/workspace \
  -w /workspace \
  "$IMAGE" \
  bash -lc '
    set -euo pipefail
    export DEBIAN_FRONTEND=noninteractive

    apt-get update
    apt-get install -y \
      ca-certificates \
      curl \
      file \
      git \
      xz-utils \
      squashfs-tools \
      build-essential \
      pkg-config \
      libgtk-3-dev \
      libwebkit2gtk-4.1-dev \
      libayatana-appindicator3-dev \
      patchelf \
      desktop-file-utils \
      nodejs \
      npm

    ARCH="$(uname -m)"
    case "$ARCH" in
      x86_64|amd64)
        GO_ARCH="amd64"
        APPIMAGE_ARCH="x86_64"
        ;;
      aarch64|arm64)
        GO_ARCH="arm64"
        APPIMAGE_ARCH="aarch64"
        ;;
      *)
        echo "error: unsupported runner arch for smoke: $ARCH" >&2
        exit 1
        ;;
    esac

    curl -fsSL "https://go.dev/dl/go1.25.0.linux-${GO_ARCH}.tar.gz" -o /tmp/go.tgz
    tar -C /usr/local -xzf /tmp/go.tgz
    export PATH=/usr/local/go/bin:$PATH

    cd agentweb
    npm ci
    npm run sync-native-static
    cd ..

    curl -fL -o /tmp/appimagetool.AppImage \
      "https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-${APPIMAGE_ARCH}.AppImage"
    chmod +x /tmp/appimagetool.AppImage

    APPIMAGE_EXTRACT_AND_RUN=1 \
    PROXER_APPIMAGETOOL=/tmp/appimagetool.AppImage \
    PROXER_APP_VERSION=0.1.0 \
    ./scripts/desktop/linux/build_appimage.sh

    ./scripts/desktop/linux/e2e_host_smoke.sh
  ')

docker "${DOCKER_ARGS[@]}"
