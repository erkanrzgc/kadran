#!/usr/bin/env bash
# scripts/mutation-shard.sh'ı gerçek depoya karşı sınar (K-164).
#
# Parçalayıcı sessiz bozulur: bir betiği hiçbir parçaya koymazsa o betik
# koşmaz ve iş yeşil kalır. En pahalı hata bu. İkincisi dengesizlik: her
# betik koşar ama tam koşu yine en ağır parça kadar sürer.
set -uo pipefail
cd "$(dirname "$0")/.."

P=scripts/mutation-shard.sh
fail=0
ok() { echo "  ✓ $1"; }
no() { echo "  ✗ $1"; fail=1; }

hepsi="$(git ls-files 'scripts/mutate-*.sh' | LC_ALL=C sort)"

# parcala <n> <girdi>: düşerse "DÜŞTÜ" basar, birleşim denetimi yakalar.
parcala() { printf '%s\n' "$2" | bash "$P" "$1" 2>/dev/null || echo "DÜŞTÜ"; }

birlesim() { printf '%s\n' $1 | LC_ALL=C sort; }

# Satırlar borudan sayılıyor: `$(…)` sondaki boş satırları siler, sondaki
# boş parçalar görünmez olurdu (mutate-mutation-shard.sh bunu buldu).
# ham_say <n> <girdi> <desen>
ham_say() { printf '%s\n' "$2" | bash "$P" "$1" 2>/dev/null | grep -c "$3"; }

# tam <ad> <n> <girdi> <beklenen parça sayısı>
tam() {
    local ad="$1" n="$2" girdi="$3" sayi="$4" out
    out="$(parcala "$n" "$girdi")"
    if [ "$(birlesim "$out")" = "$(birlesim "$girdi" | uniq)" ]; then ok "$ad: birleşim seçimin kendisi"; else no "$ad: birleşim seçimin kendisi"; fi
    if [ -z "$(birlesim "$out" | uniq -d)" ]; then ok "$ad: hiçbir betik iki parçada değil"; else no "$ad: hiçbir betik iki parçada değil"; fi
    if [ "$(ham_say "$n" "$girdi" '')" -eq "$sayi" ] && [ "$(ham_say "$n" "$girdi" '[^ ]')" -eq "$sayi" ]; then
        ok "$ad: $sayi parça, boşu yok"
    else
        no "$ad: $sayi parça, boşu yok"
    fi
}

echo "== Her betik tam bir parçada =="
tam "bütün betikler" 5 "$hepsi" 5
tam "üç betik beş parçaya" 5 "$(printf '%s\n' scripts/mutate-env.sh scripts/mutate-kasa.sh scripts/mutate-totp.sh)" 3
tam "aynı betik iki kez" 5 "$(printf '%s\n' scripts/mutate-env.sh scripts/mutate-env.sh)" 1
tam "ölçülmemiş betik" 2 "$(printf '%s\n' scripts/mutate-env.sh scripts/mutate-yeni.sh)" 2
if [ "$(parcala 5 scripts/mutate-env.sh)" = scripts/mutate-env.sh ]; then ok "tek betik tek parça"; else no "tek betik tek parça"; fi
if [ -z "$(parcala 5 "")" ]; then ok "boş seçim parça üretmiyor"; else no "boş seçim parça üretmiyor"; fi

echo "== CI biçimi ve belirlenimcilik =="
# changes işi seçimi boşlukla ayrılmış tek satır olarak veriyor.
if [ "$(parcala 5 "$(tr '\n' ' ' <<<"$hepsi")")" = "$(parcala 5 "$hepsi")" ]; then ok "boşlukla ayrılmış girdi aynı parçaları veriyor"; else no "boşlukla ayrılmış girdi aynı parçaları veriyor"; fi
if [ "$(parcala 5 "$(LC_ALL=C sort -r <<<"$hepsi")")" = "$(parcala 5 "$hepsi")" ]; then ok "girdi sırası sonucu değiştirmiyor"; else no "girdi sırası sonucu değiştirmiyor"; fi

echo "== Denge =="
out="$(parcala 5 "$hepsi")"
# CI'da ölçülen en uzun beş betik (K-164) beş ayrı parçada olmalı: her
# birinin bulunduğu satır numarası farklı.
farkli="$(for b in bootstrapssh desktop domaincheck proxywatch totp; do grep -n "scripts/mutate-$b\.sh" <<<"$out" | cut -d: -f1; done | sort -u | wc -l)"
if [ "$farkli" -eq 5 ]; then ok "en uzun beş betik ayrı parçalarda"; else no "en uzun beş betik ayrı parçalarda"; fi

# LPT sınırı: en ağır parça 4/3 · max(en ağır betik, ortalama) aşmaz.
toplam=0
enbuyuk=0
enagir=0
while IFS= read -r parca; do
    yuk=0
    for b in $parca; do
        w="$(bash "$P" --agirlik "$b" 2>/dev/null)"
        if ! [[ "$w" =~ ^[0-9]+$ ]]; then
            no "süresi okunamadı: $b"
            w=0
        fi
        yuk=$((yuk + w))
        [ "$w" -gt "$enbuyuk" ] && enbuyuk=$w
    done
    toplam=$((toplam + yuk))
    [ "$yuk" -gt "$enagir" ] && enagir=$yuk
done <<<"$out"
alt=$(((toplam + 4) / 5))
[ "$enbuyuk" -gt "$alt" ] && alt=$enbuyuk
if [ "$toplam" -gt 0 ] && [ $((enagir * 3)) -le $((alt * 4)) ]; then
    ok "en ağır parça ${enagir} sn, sınır $((alt * 4 / 3)) sn"
else
    no "en ağır parça ${enagir} sn, sınır $((alt * 4 / 3)) sn"
fi
w="$(bash "$P" --agirlik scripts/mutate-yeni.sh 2>/dev/null)"
if [[ "$w" =~ ^[0-9]+$ ]] && [ "$w" -gt 0 ]; then ok "ölçülmemiş betiğin süresi sıfır değil"; else no "ölçülmemiş betiğin süresi sıfır değil"; fi

echo "== Geçersiz parça sayısı düşer =="
for n in 0 -1 x ""; do
    if printf 'scripts/mutate-env.sh\n' | bash "$P" "$n" >/dev/null 2>&1; then no "parça sayısı '$n' kabul edildi"; else ok "parça sayısı '$n' reddedildi"; fi
done

echo
if [ "$fail" -ne 0 ]; then
    echo "Mutasyon parçalayıcısı denetimi düştü."
    exit 1
fi
echo "Mutasyon parçalayıcısı denetimi yeşil."
