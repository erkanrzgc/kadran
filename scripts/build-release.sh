#!/usr/bin/env bash
# Sunucu binary'lerini `kadran bootstrap`'ın beklediği düzende derler.
#
# Çıktı:
#   bin/linux-amd64/{kadrand,kadran-exec,kadran-connect}
#   bin/linux-arm64/{kadrand,kadran-exec,kadran-connect}
#   bin/kadran[.exe]                 — iş istasyonu aracı (yerel platform)
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

VERSION="${KADRAN_VERSION:-dev}"
COMMIT="$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)"

LDFLAGS="-s -w"
# Modül yolu go.mod'dan: yanlış bir -X yolu HATASIZ yok sayılır (K-130).
MOD="$(go list -m)"
LDFLAGS="$LDFLAGS -X $MOD/internal/version.Version=$VERSION"
LDFLAGS="$LDFLAGS -X $MOD/internal/version.Commit=$COMMIT"

SERVER_BINARIES=(kadrand kadran-exec kadran-connect)

for arch in "${ARCHES[@]}"; do
    out="bin/linux-$arch"
    mkdir -p "$out"
    echo "==> linux/$arch"
    for binary in "${SERVER_BINARIES[@]}"; do
        GOOS=linux GOARCH="$arch" CGO_ENABLED=0 \
            go build -trimpath -ldflags "$LDFLAGS" -o "$out/$binary" "./cmd/$binary"
        printf '    %s\n' "$out/$binary"
    done

    # ── Ters vekil: AYRI Go modülü ──────────────────────────────────
    #
    # build/caddy'nin kendi go.mod'u var, yani `./cmd/...` onu KAPSAMAZ
    # ve ayrı derlenmesi gerekiyor. LDFLAGS de geçirilmiyor: sürüm
    # değişkenleri kök modülün `internal/version` paketinde ve o paket
    # bu modülde yok — geçirilseydi bağlayıcı sessizce yok sayardı.
    #
    # ⚠ Bu binary bir GÜVENLİK SINIRI taşıyor: dosya servis eden modüller
    # kasten dışarıda (K-050). Kurulum betiği onu hostta ARAR ve
    # bulamazsa durur; burada üretilmezse bootstrap yarıda kalır.
    #
    # Commit'ten BAĞIMSIZ derleniyor (-buildvcs=false, K-112): aksi hâlde
    # her yükseltme ters vekili "değişmiş" sayıp yeniden başlatırdı.
    bash scripts/build-caddy.sh "$arch" "$REPO_ROOT/$out/kadran-caddy"
    printf '    %s\n' "$out/kadran-caddy"
done

# İş istasyonu aracı yerel platforma derlenir: bootstrap'ı ve GUI'yi
# çalıştıran makine bu.
echo "==> yerel iş istasyonu aracı"
go build -trimpath -ldflags "$LDFLAGS" -o "bin/kadran$( [ "$(go env GOOS)" = windows ] && echo .exe )" ./cmd/kadran
printf '    bin/kadran%s\n' "$( [ "$(go env GOOS)" = windows ] && echo .exe )"

echo
echo "Sürüm: $VERSION ($COMMIT)"
