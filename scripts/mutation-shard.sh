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

# Saniye. ubuntu-latest, run 42360594478 (PR #32, 7 Kasım), bütün betikler
# beş parçada. İlk tablo (run 42258002478, tek iş) masaüstünü 454 sanıyordu,
# 618 çıktı; o parça 907 sn ile en uzunuydu (K-164).
SURELER='
scripts/mutate-alarm.sh 63
scripts/mutate-anchor.sh 41
scripts/mutate-appdelete.sh 11
scripts/mutate-appshow.sh 52
scripts/mutate-authz.sh 79
scripts/mutate-backup.sh 9
scripts/mutate-bootstrapssh.sh 607
scripts/mutate-caps.sh 6
scripts/mutate-client.sh 94
scripts/mutate-desktop.sh 618
scripts/mutate-domaincheck.sh 438
scripts/mutate-env.sh 36
scripts/mutate-get-kadran.sh 6
scripts/mutate-goc.sh 66
scripts/mutate-heal.sh 11
scripts/mutate-install.sh 19
scripts/mutate-kasa.sh 46
scripts/mutate-keys.sh 50
scripts/mutate-logrot.sh 12
scripts/mutate-mutation-select.sh 71
scripts/mutate-mutation-shard.sh 7
scripts/mutate-netpool.sh 7
scripts/mutate-notifycontract.sh 4
scripts/mutate-proxyadmin.sh 8
scripts/mutate-proxyguard.sh 20
scripts/mutate-proxywatch.sh 331
scripts/mutate-prune.sh 27
scripts/mutate-redirects.sh 91
scripts/mutate-release.sh 111
scripts/mutate-restore.sh 26
scripts/mutate-scaledown.sh 21
scripts/mutate-servers.sh 130
scripts/mutate-supervisor.sh 17
scripts/mutate-totp.sh 322
scripts/mutate-units.sh 33
scripts/mutate-volumes.sh 53
scripts/mutate-watchdog.sh 238
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
