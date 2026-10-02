#!/usr/bin/env bash
# Yayın dosyalarını SIFIRDAN derler ve paketler.
#
# K-132'de depoya alındı: önceden yalnız yerel bir betikti; yayın paketleri
# hiç lisans dosyası taşımıyordu (ölçüldü: v0.2.0'ın sunucu tar'ında yalnız
# dört ikili). Artık her pakette LICENSE, NOTICE ve THIRD_PARTY_LICENSES.txt
# var: ikililer Caddy ve gRPC (Apache-2.0), SQLite ve protobuf (BSD) gömüyor
# ve o lisanslar metinlerinin ikiliyle birlikte verilmesini şart koşuyor.
#
# Kullanım (etiketlenmiş, temiz, LF bir klonda; derleme aracı CI'la aynı):
#   GOTOOLCHAIN=go1.25.13 scripts/package-release.sh <boş çıktı dizini> <sürüm>
#
# Çıktı:
#   kadran-<sürüm>-<os>-<arch>[.exe]           iş istasyonu aracı, 5 platform
#   kadran-server-<sürüm>-linux-<arch>.tar.gz  sunucu ikilileri + lisanslar
#   LICENSE, NOTICE, THIRD_PARTY_LICENSES.txt   tek başına ikililer için
#   SHA256SUMS
#
# Paket DETERMİNİSTİK: ad sırası, sabit zaman (commit'in), sabit sahip ve
# kip, gzip başlığında ad/zaman yok. Aynı commit'ten iki koşu aynı
# SHA256SUMS'u vermeli; yayından önce bu karşılaştırılıyor (K-118).
set -euo pipefail

OUT="${1:?kullanım: package-release.sh <boş çıktı dizini> <sürüm>}"
V="${2:?sürüm (ör. v0.3.0)}"
cd "$(dirname "${BASH_SOURCE[0]}")/.."

die() { printf 'package-release: %s\n' "$*" >&2; exit 1; }

# Yayın = commit: kirli ağaçtan paket yok.
[ -z "$(git status --porcelain)" ] || die "çalışma ağacı temiz değil"
# CRLF'li bir klon install.sh'i (CLI'a gömülü) bozar. grep DEĞİL: Git Bash'in
# grep'i satır sonundaki CR'yi görmüyor (ölçüldü, K-131).
f=internal/bootstrap/install.sh
[ "$(tr -d '\r' < "$f" | wc -c)" = "$(wc -c < "$f")" ] || die "$f CRLF — LF bir klon kullanın"
# Betik hiçbir dizini SİLMEZ; dolu bir dizine yazmak eski dosyaları
# SHA256SUMS'a karıştırırdı.
if [ -e "$OUT" ] && [ -n "$(ls -A "$OUT")" ]; then die "$OUT boş değil"; fi
mkdir -p "$OUT"
OUT="$(cd "$OUT" && pwd)"
[ ! -e bin ] || die "bin/ zaten var — eski ikililer pakete girmesin diye kaldırın"

COMMIT="$(git rev-parse --short=12 HEAD)"
MOD="$(go list -m)"
LDFLAGS="-s -w -X $MOD/internal/version.Version=$V -X $MOD/internal/version.Commit=$COMMIT"

PANELY_VERSION="$V" bash scripts/build-release.sh > /dev/null

# Platform listesi tools/thirdparty'deki clientPlatforms ile aynı olmalı
# (TestReleasePlatformsMatch).
CLIENT_PLATFORMS="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64"
for t in $CLIENT_PLATFORMS; do
    os=${t%/*}; arch=${t#*/}; ext=
    [ "$os" = windows ] && ext=.exe
    GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" \
        -o "$OUT/kadran-$V-$os-$arch$ext" ./cmd/kadran
done

go run ./tools/thirdparty -o "$OUT/THIRD_PARTY_LICENSES.txt"
cp LICENSE NOTICE "$OUT/"

mtime="@$(git log -1 --format=%ct)"
tarflags=(--sort=name --mtime="$mtime" --owner=0 --group=0 --numeric-owner)
for arch in amd64 arm64; do
    tarball="$OUT/kadran-server-$V-linux-$arch.tar"
    # İki adım, çünkü kip tek: ikililer 0755, lisans dosyaları 0644.
    tar "${tarflags[@]}" --mode=0755 -cf "$tarball" "bin/linux-$arch"
    tar "${tarflags[@]}" --mode=0644 -rf "$tarball" -C "$OUT" LICENSE NOTICE THIRD_PARTY_LICENSES.txt
    gzip -n -9 "$tarball"
done

(cd "$OUT" && sha256sum -- * > SHA256SUMS)
echo "commit: $COMMIT"
echo "panely-caddy amd64 md5: $(md5sum bin/linux-amd64/panely-caddy | cut -c1-12)"
cat "$OUT/SHA256SUMS"
