#!/usr/bin/env bash
# kadran-caddy'yi derler. scripts/build-release.sh ve CI AYNI komutu
# kullanıyor; ikisi ayrı ayrı yazıldığında biri çürüyordu.
#
# Kullanım: scripts/build-caddy.sh <arch> <çıktı yolu (mutlak)>
#
# ── -buildvcs=false ŞART (K-112) ────────────────────────────────────
#
# Go varsayılan olarak ikiliye deponun commit'ini gömüyor. build/caddy bu
# depoda durduğu için Caddy'nin kodu hiç değişmese bile her commit FARKLI
# bir ikili üretiyordu. Ölçüldü (build/caddy iki commit arasında aynı):
#
#   f3912bd  bayraksız → 331253957e3a   -buildvcs=false → eede5585b145
#   6e46d15  bayraksız → 80bbadba0284   -buildvcs=false → eede5585b145
#
# Kurulum betiği ters vekili yalnızca ikili ya da yapılandırma değişince
# yeniden başlatıyor. Bayraksız her yükseltme "değişmiş" sayılıyor ve
# trafiği kesiyordu (taze sunucuda ~1 sn, yükseltme probunda 2 hata).
#
# ── -buildid= de ŞART ───────────────────────────────────────────────
#
# Go'nun build ID'si kaynak BAYTLARININ özeti. Aynı kod, satır sonları
# CRLF olan bir Windows kopyasından derlenince farklı ikili veriyordu
# (ölçüldü: main.go 100 CR ile ve 0 CR ile iki ayrı md5; `-buildid=` ile
# ikisi de 79cb60d9382a). Yalnızca yorum değişikliği de aynı sınıf.
# Boş build ID ile ikili yalnızca derlenen koda bağlı.
#
# ⚠ Bu binary bir GÜVENLİK SINIRI taşıyor: dosya servis eden modüller
# kasten dışarıda (K-050).

set -euo pipefail

arch="$1"
out="$2"

cd "$(dirname "${BASH_SOURCE[0]}")/../build/caddy"
GOOS=linux GOARCH="$arch" CGO_ENABLED=0 \
    go build -trimpath -buildvcs=false -ldflags "-s -w -buildid=" -o "$out" .
