#!/usr/bin/env bash
# Yayın dosyalarını SIFIRDAN derler ve paketler.
#
# K-132'de depoya alındı: önceden yalnız yerel bir betikti; yayın paketleri
# hiç lisans dosyası taşımıyordu (ölçüldü: v0.2.0'ın sunucu tar'ında yalnız
# dört ikili). Artık her pakette LICENSE ve THIRD_PARTY_LICENSES.txt
# var: ikililer Caddy ve gRPC (Apache-2.0), SQLite ve protobuf (BSD) gömüyor
# ve o lisanslar metinlerinin ikiliyle birlikte verilmesini şart koşuyor.
#
# Kullanım (etiketlenmiş, temiz, LF bir klonda; derleme aracı CI'la aynı):
#   GOTOOLCHAIN=go1.26.10 scripts/package-release.sh <boş çıktı dizini> <sürüm>
#
# Çıktı:
#   kadran-<sürüm>-<os>-<arch>[.exe]           iş istasyonu aracı, 5 platform
#   kadran-server-<sürüm>-linux-<arch>.tar.gz  sunucu ikilileri + lisanslar
#   LICENSE, THIRD_PARTY_LICENSES.txt           tek başına ikililer için
#   SHA256SUMS
#
# Sıra: önce sunucu paketleri, sonra istemciler. İstemciler paketlerin
# SHA256'sını taşır ve bootstrap yalnız onlara güvenir (K-159).
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

KADRAN_VERSION="$V" bash scripts/build-release.sh > /dev/null

go run ./tools/thirdparty -o "$OUT/THIRD_PARTY_LICENSES.txt"
cp LICENSE "$OUT/"

mtime="@$(git log -1 --format=%ct)"
tarflags=(--sort=name --mtime="$mtime" --owner=0 --group=0 --numeric-owner)
for arch in amd64 arm64; do
    tarball="$OUT/kadran-server-$V-linux-$arch.tar"
    # İki adım, çünkü kip tek: ikililer 0755, lisans dosyaları 0644.
    tar "${tarflags[@]}" --mode=0755 -cf "$tarball" "bin/linux-$arch"
    # Bootstrap needs the unit/configuration assets from the same release as
    # the binaries, including the new raw-admin permission boundary.
    tar "${tarflags[@]}" --mode=u=rwX,go=rX -rf "$tarball" deploy/systemd deploy/caddy deploy/notify deploy/offsite
    tar "${tarflags[@]}" --mode=0644 -rf "$tarball" -C "$OUT" LICENSE THIRD_PARTY_LICENSES.txt
    gzip -n -9 "$tarball"
done

# İstemciler sunucu paketlerinin özetini taşıyor (K-159): bootstrap indirdiği
# paketi YALNIZ bunlarla karşılaştırır. Paketler bu yüzden istemcilerden
# ÖNCE derleniyor; deterministik oldukları için döngü yok.
amd64="$(sha256sum "$OUT/kadran-server-$V-linux-amd64.tar.gz" | cut -c1-64)"
arm64="$(sha256sum "$OUT/kadran-server-$V-linux-arm64.tar.gz" | cut -c1-64)"
CLIENT_LDFLAGS="$LDFLAGS -X $MOD/internal/version.ServerSHA256Amd64=$amd64 -X $MOD/internal/version.ServerSHA256Arm64=$arm64"

# Platform listesi tools/thirdparty'deki clientPlatforms ile aynı olmalı
# (TestReleasePlatformsMatch).
CLIENT_PLATFORMS="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64"
for t in $CLIENT_PLATFORMS; do
    os=${t%/*}; arch=${t#*/}; ext=
    [ "$os" = windows ] && ext=.exe
    f="$OUT/kadran-$V-$os-$arch$ext"
    GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "$CLIENT_LDFLAGS" -o "$f" ./cmd/kadran
    # Yanlış bir -X yolu HATASIZ yok sayılır (K-130): özetin gerçekten
    # ikiliye girdiğini derleme bilgisinden doğrula.
    info="$(go version -m "$f")"
    case "$info" in
        *"ServerSHA256Amd64=$amd64"*"ServerSHA256Arm64=$arm64"*) ;;
        *) die "client $(basename "$f") does not carry the server bundle hashes" ;;
    esac
done

(cd "$OUT" && sha256sum -- * > SHA256SUMS)
echo "commit: $COMMIT"
echo "kadran-caddy amd64 md5: $(md5sum bin/linux-amd64/kadran-caddy | cut -c1-12)"
cat "$OUT/SHA256SUMS"
