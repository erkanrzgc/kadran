#!/usr/bin/env bash
# Değişen dosyalara (stdin, satır satır) göre koşacak mutasyon betiklerini basar.
#
# Bütün betikler bir saatten uzun sürüyor; PR'da yalnız ilgili olanlar koşar,
# tamamı gecelik ve sürüm öncesi koşar (K-160). Bir betiğin kapsamı elle
# tutulmuyor, metninde andığı yollardan çıkıyor: liste tutmak yeni betiğin
# unutulmasına davetiye olurdu. Metinde yolu geçmeyen betik `# kapsam: <yol>`
# satırı taşır; kapsamı boş betiği check-mutation-select.sh düşürür.
#
# ⚠ Belirsizlikte HEPSİ: boş liste ya da ortak altyapı.
#
# Kullanım: git diff --name-only A B | scripts/mutation-select.sh
#           scripts/mutation-select.sh --kapsam scripts/mutate-env.sh
set -euo pipefail
cd "$(dirname "$0")/.."

# Her betiğin ortak dayandığı yerler.
ORTAK='^(\.github/|proto/|scripts/mutation-select\.sh$|(.*/)?go\.(mod|sum)$|buf\.)'

TRACKED="$(git ls-files)"

kapsam() {
    {
        sed -n 's/^# kapsam: *//p' "$1" | tr ' ' '\n' | sed 's#/*$##'
        grep -oE '[A-Za-z0-9_][A-Za-z0-9_./-]*/[A-Za-z0-9_./-]*' "$1" | sed -E 's#[./]+$##' |
            awk 'NR == FNR { dosya[$0] = 1; n = $0; while (sub(/\/[^\/]*$/, "", n)) dizin[n] = 1; next }
                 ($0 in dosya) || (($0 in dizin) && /\//) { print }' <(printf '%s\n' "$TRACKED") -
    } | grep -v '^$' | sort -u || true
}

if [ "${1:-}" = "--kapsam" ]; then
    kapsam "$2"
    exit 0
fi

betikler="$(git ls-files 'scripts/mutate-*')"
degisen="$(grep -v '^$' || true)"

if [ -z "$degisen" ] || grep -qE "$ORTAK" <<<"$degisen"; then
    printf '%s\n' "$betikler"
    exit 0
fi

while IFS= read -r b; do
    k="$(kapsam "$b")"
    # Önek bir alt yolu da kapsar.
    if grep -qxF "$b" <<<"$degisen" ||
        awk 'NR == FNR { k[$0] = 1; next }
             { p = $0; while (1) { if (p in k) { bulundu = 1; exit } if (!sub(/\/[^\/]*$/, "", p)) break } }
             END { exit !bulundu }' <(printf '%s\n' "$k") - <<<"$degisen"; then
        printf '%s\n' "$b"
    fi
done <<<"$betikler"
