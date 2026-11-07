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

# Saniye. ubuntu-latest, run 42258002478 (PR #31, 6 Kasım), bütün betikler.
# proxywatch 1258 ölçüldü: iki mutantı go test'in 10 dk zaman aşımını
# bekliyordu. K-164'te zaman aşımı 2 dk oldu; 300 tahmin.
SURELER='
scripts/mutate-alarm.sh 90
scripts/mutate-anchor.sh 38
scripts/mutate-appdelete.sh 11
scripts/mutate-appshow.sh 49
scripts/mutate-authz.sh 72
scripts/mutate-backup.sh 9
scripts/mutate-bootstrapssh.sh 600
scripts/mutate-caps.sh 5
scripts/mutate-client.sh 93
scripts/mutate-desktop.sh 454
scripts/mutate-domaincheck.sh 398
scripts/mutate-env.sh 34
scripts/mutate-get-kadran.sh 5
scripts/mutate-goc.sh 50
scripts/mutate-heal.sh 10
scripts/mutate-install.sh 20
scripts/mutate-kasa.sh 45
scripts/mutate-keys.sh 46
scripts/mutate-logrot.sh 16
scripts/mutate-mutation-select.sh 49
scripts/mutate-netpool.sh 6
scripts/mutate-notifycontract.sh 5
scripts/mutate-proxyadmin.sh 7
scripts/mutate-proxyguard.sh 20
scripts/mutate-proxywatch.sh 300
scripts/mutate-prune.sh 25
scripts/mutate-redirects.sh 71
scripts/mutate-release.sh 88
scripts/mutate-restore.sh 23
scripts/mutate-scaledown.sh 19
scripts/mutate-servers.sh 73
scripts/mutate-supervisor.sh 14
scripts/mutate-totp.sh 252
scripts/mutate-units.sh 27
scripts/mutate-volumes.sh 44
scripts/mutate-watchdog.sh 231
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
