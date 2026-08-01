#!/usr/bin/env bash
# Sunucu binary'lerini `panely bootstrap`'ın beklediği düzende derler.
#
# Çıktı:
#   bin/linux-amd64/{panelyd,panely-exec,panely-connect}
#   bin/linux-arm64/{panelyd,panely-exec,panely-connect}
#   bin/panely[.exe]                 — iş istasyonu aracı (yerel platform)
#
# Kullanım:
#   scripts/build-release.sh              # her iki mimari
#   scripts/build-release.sh arm64        # yalnızca biri

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

ARCHES=("${@:-amd64 arm64}")
# shellcheck disable=SC2206
ARCHES=(${ARCHES[*]})

VERSION="${PANELY_VERSION:-dev}"
COMMIT="$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)"

LDFLAGS="-s -w"
LDFLAGS="$LDFLAGS -X github.com/erkanrzgc/panely/internal/version.Version=$VERSION"
LDFLAGS="$LDFLAGS -X github.com/erkanrzgc/panely/internal/version.Commit=$COMMIT"

SERVER_BINARIES=(panelyd panely-exec panely-connect)

for arch in "${ARCHES[@]}"; do
    out="bin/linux-$arch"
    mkdir -p "$out"
    echo "==> linux/$arch"
    for binary in "${SERVER_BINARIES[@]}"; do
        GOOS=linux GOARCH="$arch" CGO_ENABLED=0 \
            go build -trimpath -ldflags "$LDFLAGS" -o "$out/$binary" "./cmd/$binary"
        printf '    %s\n' "$out/$binary"
    done
done

# İş istasyonu aracı yerel platforma derlenir: bootstrap'ı ve GUI'yi
# çalıştıran makine bu.
echo "==> yerel iş istasyonu aracı"
go build -trimpath -ldflags "$LDFLAGS" -o "bin/panely$( [ "$(go env GOOS)" = windows ] && echo .exe )" ./cmd/panely
printf '    bin/panely%s\n' "$( [ "$(go env GOOS)" = windows ] && echo .exe )"

echo
echo "Sürüm: $VERSION ($COMMIT)"
