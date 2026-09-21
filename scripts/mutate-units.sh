#!/usr/bin/env bash
# Birim dosyası testlerinin GERÇEKTEN bir şey koruduğunu sınar.
#
# Her mutasyon korunan bir özelliği KASTEN bozar; test kırmızıya
# dönmezse o özellik korunmuyor demektir.
#
# ── Neden birim dosyaları için ayrı bir betik ───────────────────────
#
# Buradaki iki özellik Go kodunda değil, birim DOSYALARI arasındaki
# ilişkide yaşıyor (K-100, K-101, K-102):
#
#   - executor'ın denetim günlüğü daemon'un yazabildiği bir dizinde
#     durmamalı; dursa, dosyanın sahibi root olsa bile daemon onu silip
#     yerine kendi zincirini koyabilir (canlıda ölçüldü).
#   - uzak yedek yükleyicisinin rclone yapılandırması da öyle; rclone
#     yapılandırması komut çalıştırabilir ve yükleyici ağ gören tek birim.
#
# İlişkiyi okuyan testler ancak bir dosyayı bozunca kırmızıya dönüyorsa
# bir şey koruyor demektir.
set -uo pipefail

cd "$(dirname "$0")/.."
FILES=(
    deploy/systemd/panely-exec.service
    deploy/systemd/panely-tmpfiles.conf
    deploy/systemd/panely-offsite.service
)
BAK=$(mktemp -d)
for f in "${FILES[@]}"; do cp "$f" "$BAK/$(basename "$f")"; done
restore() { for f in "${FILES[@]}"; do cp "$BAK/$(basename "$f")" "$f"; done; }
trap 'restore; rm -rf "$BAK"' EXIT

fail=0
WANT='TestExecutorJournalOutsideDaemonDirs|TestOwnedPathsAreActuallyCreated|TestOffsiteRcloneConfigOutsideDaemonDirs|TestUnitsDoNotHardRequireForeignPaths'

# mutate <ad> <dosya> <python-ifadesi>
mutate() {
    local name="$1" src="$2" expr="$3"
    restore
    if ! python -c "
import io,sys
class _S(str):
    def replace(self,a,b,*r):
        out=str.replace(self,a,b,*r)
        if out==self:
            sys.stderr.write('REPLACE ESLESMEDI: '+repr(a[:70])+chr(10))
            sys.exit(8)
        return _S(out)
p='$src'
s=_S(io.open(p,encoding='utf-8').read())
o=s
$expr
if s==o:
    sys.exit(9)
io.open(p,'w',encoding='utf-8',newline='\n').write(s)
"; then
        echo "  !! MUTASYON UYGULANAMADI: $name — betik bozuk, ölçüm YAPILMADI"
        fail=1
        return
    fi

    # ── MUTANT DERLENMELİ ───────────────────────────────
    #
    # Birim dosyası değişikliği Go derlemesini bozmaz, ama kapı yine de
    # burada: test paketi derlenmiyorsa `go test` düşer ve betik bunu
    # "yakalandı" diye okurdu (K-096). CI her betikte bu kapıyı arıyor.
    local build_out
    if ! build_out=$(go test ./internal/bootstrap/ -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI,"
        echo "     mutasyon derlenebilir olacak şekilde yazılmalı. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if go test ./internal/bootstrap/ -run "$WANT" -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Executor denetim günlüğü =="

# Satır başındaki `\n` ŞART. Birimin yorum bloğunda aynı yolu taşıyan
# bir ÖRNEK satır var (`#       --journal …`); çapasız `replace` ilk
# eşleşmeyi, yani yorumu değiştiriyordu. Test yorumları okumadığı için
# yeşil kaldı ve betik "KIRMIZI OLMADI" dedi — zayıf olan test değil
# mutasyondu (K-080'in ikinci sebebi).
mutate "günlük daemon'un dizinine geri taşındı" deploy/systemd/panely-exec.service \
    "s=s.replace('\n    --journal /var/lib/panely-exec/exec-audit.log','\n    --journal /var/lib/panely/exec-audit.log',1)"

mutate "günlük dizini grup-yazılabilir" deploy/systemd/panely-tmpfiles.conf \
    "s=s.replace('d /var/lib/panely-exec        0700 root   root   -','d /var/lib/panely-exec        0770 root   panely -',1)"

mutate "günlük dizininin sahibi panely" deploy/systemd/panely-tmpfiles.conf \
    "s=s.replace('d /var/lib/panely-exec        0700 root   root   -','d /var/lib/panely-exec        0700 panely root   -',1)"

mutate "günlük dizinini kimse yaratmıyor" deploy/systemd/panely-tmpfiles.conf \
    "s=s.replace('d /var/lib/panely-exec        0700 root   root   -\n','',1)"

echo "== Uzak yedek rclone yapılandırması =="

mutate "RCLONE_CONFIG tanımlanmıyor" deploy/systemd/panely-offsite.service \
    "s=s.replace('Environment=RCLONE_CONFIG=/etc/panely/rclone.conf\n','',1)"

mutate "rclone yapılandırması daemon'un dizininde" deploy/systemd/panely-offsite.service \
    "s=s.replace('Environment=RCLONE_CONFIG=/etc/panely/rclone.conf','Environment=RCLONE_CONFIG=/var/lib/panely/.config/rclone/rclone.conf',1)"

restore
echo
if [ "$fail" -ne 0 ]; then
    echo "BAŞARISIZ: en az bir mutasyon yakalanmadı ya da uygulanamadı."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
