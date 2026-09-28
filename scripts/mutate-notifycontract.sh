#!/usr/bin/env bash
# LogSink ↔ Telegram göndericisi sözleşme testinin GERÇEKTEN bir şey
# koruduğunu sınar (K-108).
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
# Sessiz ayrışma. panely-notify.sh panelyd'nin `msg=ALARM` satırlarını
# ayrıştırıyor; LogSink'in biçimi değişirse betiğin kendi testleri yine
# geçer (örnekleri elle kopyalanmış) ve Telegram teslimatı SESSİZCE durur.
# Sözleşme testi iki yönü de bağlamalı: Go tarafı değişince de, betikteki
# örnek kayınca da düşmeli — son mutasyon ikincisini ölçüyor.
set -uo pipefail

cd "$(dirname "$0")/.."
SINK=internal/alarm/alarm.go
FIX=scripts/check-notify-format.sh
PKG=./internal/alarm/

BAK_SINK=$(mktemp); BAK_FIX=$(mktemp)
cp "$SINK" "$BAK_SINK"; cp "$FIX" "$BAK_FIX"
restore() { cp "$BAK_SINK" "$SINK"; cp "$BAK_FIX" "$FIX"; }
trap restore EXIT

fail=0

# mutate <ad> <dosya> <python-ifadesi>
mutate() {
    local name="$1" file="$2" expr="$3"
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
p='$file'
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
    # Derlenmeyen bir mutant `go test`'i düşürür ve betik bunu
    # "yakalandı" diye okur (K-096).
    local build_out
    if ! build_out=$(go test "$PKG" -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI,"
        echo "     mutasyon derlenebilir olacak şekilde yazılmalı. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if go test "$PKG" -run 'TestLogSink' -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Go tarafı değişiyor =="

mutate "alan adı değişti (ayrinti → detay)" "$SINK" \
    "s=s.replace('\"ayrinti\", ev.Alarm.Detail)','\"detay\", ev.Alarm.Detail)',1)"

mutate "kritik ERROR yerine WARN" "$SINK" \
    "s=s.replace('\t\tslog.Error(\"ALARM\", args...)','\t\tslog.Warn(\"ALARM\", args...)',1)"

mutate "kapanış da ciddiyet taşıyor" "$SINK" \
    "s=s.replace('\tif ev.State != Closed {\n\t\targs = append(args,','\tif true {\n\t\targs = append(args,',1)"

# Betik alanları ADLA okuyor; sıra değişse teslimat bozulmaz. Test bilerek
# BAYT düzeyinde katı: örnekler "canlıdan birebir" olmalı, sıra değişince
# örnekler de güncellenir. Bu mutant o katılığı ölçüyor, teslimatı değil.
mutate "alan sırası değişti (bilerek katı)" "$SINK" \
    "s=s.replace('\t\t\"alarm\", ev.Alarm.ID,\n\t\t\"durum\", string(ev.State),','\t\t\"durum\", string(ev.State),\n\t\t\"alarm\", ev.Alarm.ID,',1)"

echo "== Betik örneği kayıyor =="

# Kontrol grubu: sözleşme TEK yönlü olsaydı bu yeşil kalırdı.
mutate "betikteki örnek elle değişti" "$FIX" \
    "s=s.replace('durum=acildi ciddiyet=kritik hedef=panely.db','durum=acildi ciddiyet=KRITIK hedef=panely.db',1)"

restore
if [[ $fail -ne 0 ]]; then
    echo
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi

echo
echo "Bütün mutasyonlar yakalandı."
