#!/usr/bin/env bash
# Linux'a özgü testleri Windows iş istasyonundan çalıştırır.
#
# Panely'nin güvenlik sınırının bir kısmı Linux'a özgüdür: SO_PEERCRED,
# unix soket izinleri, systemd bütünleşmesi. Bunlar Windows'ta derlenir ama
# ÇALIŞMAZ — ve çalıştırılmayan bir güvenlik kontrolü doğrulanmış sayılmaz.
#
# Bu betik test binary'lerini linux/amd64'e çapraz derler ve WSL içinde
# çalıştırır. Go'nun WSL'de kurulu olması gerekmez.
#
# Kullanım:
#   scripts/test-linux.sh                 # tüm paketler
#   scripts/test-linux.sh peercred        # tek paket
#
# NOT: -race çapraz derlemede CGO gerektirir, bu yüzden burada kullanılmaz.
# Yarış tespiti Windows'ta `go test -race ./...` ile ve CI'da Linux
# koşucusunda yapılır.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

WSL_DISTRO="${PANELY_WSL_DISTRO:-Ubuntu}"
OUT_DIR="bin/linux-test"

# Test edilecek paketler. Argüman verilirse yalnızca o kullanılır.
if [[ $# -gt 0 ]]; then
    PACKAGES=("$@")
else
    mapfile -t PACKAGES < <(find internal -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort)
fi

mkdir -p "$OUT_DIR"

echo "==> linux/amd64 için test binary'leri derleniyor"
built=()
for pkg in "${PACKAGES[@]}"; do
    # Test dosyası olmayan paketler için `go test -c` binary üretmez.
    if ! compgen -G "internal/$pkg/*_test.go" > /dev/null; then
        echo "    $pkg — test dosyası yok, atlanıyor"
        continue
    fi
    GOOS=linux GOARCH=amd64 go test -c -o "$OUT_DIR/$pkg.test" "./internal/$pkg"
    built+=("$pkg")
done

if [[ ${#built[@]} -eq 0 ]]; then
    echo "Çalıştırılacak test bulunamadı." >&2
    exit 1
fi

# Windows yolunu WSL'in gördüğü yola çevir: C:\x\y -> /mnt/c/x/y
wsl_path() {
    local win="$1"
    local drive="${win:0:1}"
    printf '/mnt/%s%s' "$(echo "$drive" | tr '[:upper:]' '[:lower:]')" "$(echo "${win:2}" | tr '\\' '/')"
}

WSL_OUT="$(wsl_path "$(cd "$OUT_DIR" && pwd -W)")"

failed=0
for pkg in "${built[@]}"; do
    echo
    echo "==> $pkg (WSL: $WSL_DISTRO)"
    # cd /tmp: testler geçici dizin oluşturur; /mnt/c üzerinde çalışmak
    # hem yavaştır hem de dosya izinleri Windows semantiğini taşır ki bu
    # tam olarak doğrulamak istemediğimiz şeydir.
    if ! wsl.exe -d "$WSL_DISTRO" -- bash -c "cd /tmp && chmod +x '$WSL_OUT/$pkg.test' && '$WSL_OUT/$pkg.test' ${PANELY_TEST_FLAGS:-}"; then
        echo "!! $pkg BAŞARISIZ" >&2
        failed=1
    fi
done

echo
if [[ $failed -eq 0 ]]; then
    echo "Tüm Linux testleri geçti."
else
    echo "Bazı Linux testleri başarısız." >&2
fi
exit $failed
