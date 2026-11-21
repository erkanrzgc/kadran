#!/usr/bin/env bash
# CLI cikisini (stdout, stderr, cikis kodu) ag istemeyen girdilerde altin
# dosyalarla karsilastirir. CLI paketini tasirken davranisin DEGISMEDIGINI
# kanitlamak icin (K-169).
#
# Kullanim:
#   scripts/cli-golden.sh record   altin dosyalari yeniden yazar
#   scripts/cli-golden.sh check    calisma agacindan derler ve karsilastirir
#
# Her kosuda APPDATA, XDG_CONFIG_HOME ve HOME bos bir gecici dizindir:
# kullanicinin gercek kayitli sunucu ve oturum dosyalari okunmaz.

set -euo pipefail

MODE="${1:-check}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"
GOLDEN="scripts/testdata/cli-golden"

# Her satir bir kosu: argumanlar bosluk ile ayrilir; bos satir = argumansiz.
CASES=(
    ""
    "help"
    "-h"
    "version"
    "bilinmeyen"
    "status -h"
    "app -h"
    "app create -h"
    "deploy -h"
    "logs -h"
    "prune -h"
    "audit verify -h"
    "totp -h"
    "key -h"
    "github -h"
    "server -h"
    "backup -h"
    "domain -h"
    "login -h"
    "deploy"
    "deploy -s yok web"
    "server list"
    "version extra"
)

case "$MODE" in
record | check) ;;
*)
    echo "kullanim: $0 record|check" >&2
    exit 2
    ;;
esac

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
BIN="$WORK/kadran"
go build -o "$BIN" ./cmd/kadran
mkdir -p "$WORK/cfg"

if [ "$MODE" = record ]; then
    rm -rf "$GOLDEN"
    mkdir -p "$GOLDEN"
fi

fail=0
n=0
for line in "${CASES[@]}"; do
    n=$((n + 1))
    id=$(printf '%02d' "$n")
    read -r -a args <<<"$line" || true
    set +e
    APPDATA="$WORK/cfg" XDG_CONFIG_HOME="$WORK/cfg" HOME="$WORK/cfg" USERPROFILE="$WORK/cfg" \
        "$BIN" ${args[@]+"${args[@]}"} >"$WORK/out" 2>"$WORK/err" </dev/null
    code=$?
    set -e
    echo "$code" >"$WORK/code"
    if [ "$MODE" = record ]; then
        for k in out err code; do cp "$WORK/$k" "$GOLDEN/$id.$k"; done
        continue
    fi
    for k in out err code; do
        if ! cmp -s "$WORK/$k" "$GOLDEN/$id.$k"; then
            echo "FARK: kosu $id (kadran $line) .$k" >&2
            diff "$GOLDEN/$id.$k" "$WORK/$k" >&2 || true
            fail=1
        fi
    done
done

if [ "$fail" -ne 0 ]; then
    exit 1
fi
echo "cli-golden $MODE: $n kosu tamam"
