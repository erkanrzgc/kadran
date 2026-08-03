#!/usr/bin/env bash
# Kök E2E testini WSL içinde root olarak çalıştırır.
#
# # `sudo` yok, gerek de yok
#
# `wsl.exe -u root` doğrudan root veriyor — parolasız sudo aramaya gerek
# kalmıyor. Bu, testin "sunucu gelene kadar bekler" sanılmasına yol açan
# yanlış varsayımdı; aslında ilk günden çalıştırılabilirmiş.
#
# Test /mnt/c üzerinde DEĞİL /tmp altında koşuyor: /mnt/c Windows dosya
# semantiği taşır ve doğrulanan şey tam olarak unix izinleri.
#
# Kullanım:
#   scripts/e2e-executor-wsl.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

WSL_DISTRO="${PANELY_WSL_DISTRO:-Ubuntu}"

echo "==> panely-exec linux/amd64 derleniyor"
GOOS=linux GOARCH=amd64 go build -o bin/panely-exec ./cmd/panely-exec

# Windows yolunu WSL'in gördüğü yola çevir.
wsl_path() {
    local win="$1"
    local drive="${win:0:1}"
    printf '/mnt/%s%s' "$(echo "$drive" | tr '[:upper:]' '[:lower:]')" "$(echo "${win:2}" | tr '\\' '/')"
}
WSL_REPO="$(wsl_path "$(pwd -W)")"

# Uzak tarafta çalışacak adımlar ayrı bir dosyaya yazılıyor: `wsl.exe --
# bash -c '...'` çağrılarında tırnaklar ve $değişkenler düzenli olarak
# bozuluyor.
RUNNER="bin/e2e-executor-runner.sh"
cat > "$RUNNER" <<'REMOTE'
set -euo pipefail
REPO="$1"
WORK=/tmp/panely-e2e
rm -rf "$WORK"
mkdir -p "$WORK/bin" "$WORK/scripts"
cp "$REPO/bin/panely-exec"           "$WORK/bin/"
cp "$REPO/scripts/e2e-executor.sh"   "$WORK/scripts/"
chmod +x "$WORK/bin/panely-exec" "$WORK/scripts/e2e-executor.sh"
cd "$WORK"
export PANELY_EXEC_BIN="$WORK/bin/panely-exec"

echo "############ 1/2 — TESTİN KENDİSİNİ SINA ############"
echo "Executor kasten davetsiz kullanıcıyı kabul edecek."
echo "5. doğrulama BAŞARISIZ olmalı; olmazsa test hiçbir şey ölçmüyor."
echo
if PANELY_E2E_ALLOW_INTRUDER=1 bash "$WORK/scripts/e2e-executor.sh"; then
    echo
    echo "!! TESTİN KENDİSİ BOZUK: SO_PEERCRED kasten gevşetildiği hâlde test geçti." >&2
    exit 1
fi
echo
echo "  ✓ test bozuk politikayı yakaladı"

echo
echo "############ 2/2 — GERÇEK DOĞRULAMA ############"
echo
bash "$WORK/scripts/e2e-executor.sh"
REMOTE

echo "==> WSL ($WSL_DISTRO) içinde root olarak çalıştırılıyor"
MSYS_NO_PATHCONV=1 wsl.exe -d "$WSL_DISTRO" -u root -- \
    bash "$WSL_REPO/$RUNNER" "$WSL_REPO"
