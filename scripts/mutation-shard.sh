#!/usr/bin/env bash
# Seçilen mutasyon betiklerini (stdin; boşluk ya da satırla ayrılmış) N
# parçaya böler. Her parça bir satır, betikler boşlukla ayrılmış (K-164).
#
# Tam koşu tek işte 71 dk sürüyordu. Betikler 5 sn ile 10 dk arasında;
# ad sırasıyla eşit sayıda bölmek ağır betikleri aynı parçaya koyabilir.
# Bu yüzden en uzun süren önce, o an en hafif parçaya (LPT). Eşitlikte ad
# sırası: aynı seçim her koşuda aynı parçaları verir.
#
# Süreler CI'da ölçüldü. Tabloda olmayan betik VARSAYILAN süreyle yine bir
# parçaya girer: tablonun eskimesi yalnız dengeyi bozar, betik düşürmez.
# Betik sayısı N'den azsa parça sayısı betik sayısıdır; boş parça olmaz.
#
# Kullanım: scripts/mutation-select.sh < degisen | scripts/mutation-shard.sh 5
#           scripts/mutation-shard.sh --agirlik scripts/mutate-env.sh
set -euo pipefail
export LC_ALL=C

VARSAYILAN=60

# Saniye. ubuntu-latest, run 43205047420 (v0.12.0 öncesi tam koşu, 12 Kasım),
# bütün betikler beş parçada; süre, günlükte betiğin `▶` satırından bir
# sonrakine. Önceki tablo run 42360594478'dendi (7 Kasım); ilki (run
# 42258002478, tek iş) masaüstünü 454 sanıyordu, 618 çıkmıştı (K-164).
SURELER='
scripts/mutate-alarm.sh 70
scripts/mutate-anchor.sh 38
scripts/mutate-appdelete.sh 13
scripts/mutate-appshow.sh 44
scripts/mutate-authz.sh 87
scripts/mutate-backup.sh 8
scripts/mutate-bootstrapssh.sh 629
scripts/mutate-caps.sh 7
scripts/mutate-client.sh 95
scripts/mutate-desktop.sh 483
scripts/mutate-domaincheck.sh 392
scripts/mutate-env.sh 40
scripts/mutate-get-kadran.sh 5
scripts/mutate-goc.sh 63
scripts/mutate-heal.sh 15
scripts/mutate-install.sh 21
scripts/mutate-kasa.sh 46
scripts/mutate-keys.sh 58
scripts/mutate-logrot.sh 15
scripts/mutate-mutation-select.sh 83
scripts/mutate-mutation-shard.sh 8
scripts/mutate-netpool.sh 6
scripts/mutate-notifycontract.sh 6
scripts/mutate-proxyadmin.sh 8
scripts/mutate-proxyguard.sh 25
scripts/mutate-proxywatch.sh 310
scripts/mutate-prune.sh 33
scripts/mutate-redirects.sh 67
scripts/mutate-release.sh 81
scripts/mutate-resources.sh 47
scripts/mutate-restore.sh 19
scripts/mutate-scaledown.sh 21
scripts/mutate-servers.sh 121
scripts/mutate-supervisor.sh 16
scripts/mutate-totp.sh 278
scripts/mutate-units.sh 26
scripts/mutate-volumes.sh 50
scripts/mutate-watchdog.sh 237
'

# Her betik için "süre ad" satırı.
sureli() {
    awk -v v="$VARSAYILAN" 'NR == FNR { if (NF == 2) w[$1] = $2; next }
        { print (($0 in w) ? w[$0] : v), $0 }' <(printf '%s\n' "$SURELER") -
}

if [ "${1:-}" = "--agirlik" ]; then
    printf '%s\n' "$2" | sureli | cut -d' ' -f1
    exit 0
fi

n="${1:-}"
if ! [[ "$n" =~ ^[1-9][0-9]*$ ]]; then
    echo "kullanım: $0 <parça sayısı> < betikler" >&2
    exit 2
fi

betikler="$(tr -s ' \t' '\n' | grep -v '^$' | sort -u || true)"
if [ -z "$betikler" ]; then
    exit 0
fi
k="$(wc -l <<<"$betikler")"
if [ "$n" -gt "$k" ]; then
    n="$k"
fi

# int(n): yukarıdaki denetim atlanırsa (mutasyon) `x` dize olarak kıyaslanır,
# `p <= n` hiç yanlış olmaz ve döngü belleği bitirir. CI'da runner böyle
# kapandı (K-164); sayıya zorlanınca döngü hiç dönmez.
sureli <<<"$betikler" | sort -k1,1nr -k2,2 |
    awk -v n="$n" 'BEGIN { n = int(n) }
        {
            en = 1
            for (p = 2; p <= n; p++) if (yuk[p] < yuk[en]) en = p
            yuk[en] += $1
            parca[en] = parca[en] " " $2
        }
        END { for (p = 1; p <= n; p++) print substr(parca[p], 2) }'
