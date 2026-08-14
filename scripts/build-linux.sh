#!/usr/bin/env bash
# Yerel geliştirme için linux/amd64 binary'leri derler.
#
# CI'ın derleme adımıyla AYNI bayrakları kullanır (.github/workflows/ci.yml);
# tek fark sürüm bilgisinin etikete değil çalışma ağacına bakması. Aynı
# tutulmasının sebebi K-046'nın sınıfı: "yerelde derledim" ile "CI'ın
# ürettiği binary" farklı şeylerse, sunucuda ölçülen şey gönderilecek şey
# değildir.
set -euo pipefail

cd "$(dirname "$0")/.."

out="${1:-bin/linux}"
mkdir -p "$out"

version="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
commit="$(git rev-parse HEAD | cut -c1-12)"

ldflags="-s -w"
ldflags="$ldflags -X github.com/erkanrzgc/panely/internal/version.Version=$version"
ldflags="$ldflags -X github.com/erkanrzgc/panely/internal/version.Commit=$commit"

for binary in panelyd panely-exec panely-connect panely; do
    GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$ldflags" \
        -o "$out/$binary" "./cmd/$binary"
done

echo "sürüm=$version commit=$commit"
ls -la "$out"
