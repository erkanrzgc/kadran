#!/usr/bin/env bash
# scripts/mutation-select.sh'ı gerçek depoya karşı sınar.
#
# Seçici sessiz bozulur: yanlış çalışırsa PR'da hiçbir mutasyon koşmaz ve iş
# yeşil kalır. En pahalı hata bu; pahalı olmayan hata fazladan betik koşmak.
set -uo pipefail
cd "$(dirname "$0")/.."

S=scripts/mutation-select.sh
fail=0
ok() { echo "  ✓ $1"; }
no() { echo "  ✗ $1"; fail=1; }

secim() { printf '%s\n' "$@" | bash "$S"; }
hepsi="$(git ls-files 'scripts/mutate-*' | sort)"

# var <ad> <beklenen betik> <değişen dosya…>
var() {
    local ad="$1" bet="$2" out
    shift 2
    # Boruya bağlama: `grep -q` erken çıkar, pipefail seçiciyi düşmüş sayar.
    out="$(secim "$@")"
    if grep -qxF "$bet" <<<"$out"; then ok "$ad"; else no "$ad"; fi
}

# tum <ad> <değişen dosya…>
tum() {
    local ad="$1"
    shift
    if [ "$(secim "$@" | sort)" = "$hepsi" ]; then ok "$ad"; else no "$ad"; fi
}

echo "== Kod değişince kendi betiği koşar =="
var "kodun kendisi" scripts/mutate-appdelete.sh internal/api/appdelete.go
var "aynı Go paketindeki başka dosya" scripts/mutate-appdelete.sh internal/api/appdelete_test.go
var "betiğin kendisi" scripts/mutate-env.sh scripts/mutate-env.sh
var "kabuk betiği" scripts/mutate-get-kadran.sh scripts/get-kadran.sh
var "betiğin denetimi" scripts/mutate-get-kadran.sh scripts/check-get-kadran.sh
var "PowerShell betiği" scripts/mutate-get-kadran.ps1 scripts/get-kadran.ps1
var "masaüstü" scripts/mutate-desktop.sh desktop/package.json

echo "== İlgisiz betik koşmaz =="
out="$(secim scripts/get-kadran.sh)"
if [ "$out" = scripts/mutate-get-kadran.sh ]; then ok "kurulum betiği yalnız kendi betiğini seçiyor"; else no "kurulum betiği yalnız kendi betiğini seçiyor"; fi
if [ -z "$(secim LICENSE)" ]; then ok "kimsenin anmadığı dosya hiçbir şey seçmiyor"; else no "kimsenin anmadığı dosya hiçbir şey seçmiyor"; fi

echo "== Belirsizlikte hepsi koşar =="
tum "boş liste"
tum "ci.yml" .github/workflows/ci.yml
tum "bileşik eylem" .github/actions/setup/action.yml
tum "go.mod" go.mod
tum "go.sum" go.sum
tum "proto" proto/kadran/v1/api.proto
tum "seçicinin kendisi" "$S"
tum "karışık liste" LICENSE go.mod

echo "== Alt modül yalnız kendini anan betiği seçer (K-164) =="
# PR #31 yalnız build/caddy'nin go.mod/go.sum'ını değiştirdi ve bütün
# betikleri 1 sa 12 dk koşturdu. Kök modülün testleri alt modülü derlemiyor.
out="$(secim build/caddy/go.mod build/caddy/go.sum)"
if [ "$out" = scripts/mutate-redirects.sh ]; then ok "build/caddy bağımlılığı yalnız mutate-redirects.sh'ı seçiyor"; else no "build/caddy bağımlılığı yalnız mutate-redirects.sh'ı seçiyor"; fi
# Bu, kök modül alt modüle bağlı olmadıkça doğru. Bağlanırsa (go.work ya da
# yerel replace) alt modülün go.mod'u yine ortak sayılmalı.
if [ ! -e go.work ] && ! grep -qE '=>[[:space:]]*\.\.?/' go.mod; then
    ok "kök modül yerel bir modüle bağlı değil"
else
    no "kök modül yerel bir modüle bağlı: alt modülün go.mod'u ORTAK'a dönmeli (K-164)"
fi

echo "== Her betiğin kapsamı var =="
# Kapsamı boş betik PR'da hiç koşmaz, ancak gecelik görülür.
bos=0
while IFS= read -r b; do
    if [ -z "$(bash "$S" --kapsam "$b")" ]; then
        echo "    kapsamı boş: $b"
        bos=1
    fi
done <<<"$hepsi"
if [ "$bos" -eq 0 ]; then ok "kapsamı boş betik yok"; else no "kapsamı boş betik yok"; fi

echo
if [ "$fail" -ne 0 ]; then
    echo "Mutasyon seçicisi denetimi düştü."
    exit 1
fi
echo "Mutasyon seçicisi denetimi yeşil."
