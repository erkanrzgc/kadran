#!/usr/bin/env bash
# scripts/e2e-cli-runner.sh'ı Windows iş istasyonundan WSL içinde çalıştırır.
#
# Asıl kontroller runner betiğinde; buradaki tek iş linux binary'lerini
# derleyip WSL'e geçmek. CI, Linux koşucusunda runner'ı doğrudan çağırır.
#
# Kullanım:
#   scripts/e2e-cli.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

WSL_DISTRO="${PANELY_WSL_DISTRO:-Ubuntu}"
OUT_DIR="bin/linux"

echo "==> linux/amd64 binary'leri derleniyor"
mkdir -p "$OUT_DIR"
GOOS=linux GOARCH=amd64 go build -o "$OUT_DIR/panelyd" ./cmd/panelyd
GOOS=linux GOARCH=amd64 go build -o "$OUT_DIR/panely" ./cmd/panely

# Windows yolunu WSL'in gördüğü yola çevir: C:\x\y -> /mnt/c/x/y
wsl_path() {
    local win="$1"
    local drive="${win:0:1}"
    printf '/mnt/%s%s' "$(echo "$drive" | tr '[:upper:]' '[:lower:]')" "$(echo "${win:2}" | tr '\\' '/')"
}

WSL_REPO="$(wsl_path "$(pwd -W)")"

echo "==> WSL ($WSL_DISTRO) içinde çalıştırılıyor"
MSYS_NO_PATHCONV=1 wsl.exe -d "$WSL_DISTRO" -- \
    bash "$WSL_REPO/scripts/e2e-cli-runner.sh" "$WSL_REPO/$OUT_DIR"
