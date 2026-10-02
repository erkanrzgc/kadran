#!/usr/bin/env bash
# Askıda kalma tespitinin (watchdog, K-115) testlerinin GERÇEKTEN bir şey
# koruduğunu sınar.
#
# Her mutasyon korunan bir özelliği KASTEN bozar; test kırmızıya dönmezse
# o özellik korunmuyor demektir.
#
# ── Bu dilimde neyin bozulması EN PAHALI ────────────────────────────
#
# Bir döngüden Mark çağrısının silinmesi. Derleyici bir şey demez, sayaç
# yeşil kalır ve watchdog o döngünün takılmasını bir daha ASLA görmez:
# özellik yalnızca adında yaşar (K-079 sınıfı). Açılıştaki ve ticker'daki
# Mark AYRI mutantlar, çünkü kısa aralıklı bir test ikisini ayıramıyordu
# (betiğin ilk taslağında açılış mutantı yeşil kalacaktı).
#
# İkincisi: bayat bir döngüye ya da tükenmiş bir havuza rağmen ping atmak.
# O zaman watchdog K-115'in "yanlış çözüm" dediği sayaç goroutine'ine
# döner.
#
# ── Neden -timeout ─────────────────────────────────────────────────
#
# "Yoklamanın kendi sınırı yok" mutantında test, hiç dönmeyen bir
# yoklamada sonsuza dek bekler. Zaman aşımı da kırmızıdır; 10 dakikalık
# varsayılanı beklemenin anlamı yok.
set -uo pipefail

cd "$(dirname "$0")/.."
L=internal/liveness/liveness.go
H=internal/health/supervisor.go
W=cmd/panelyd/proxywatch.go
A=cmd/panelyd/alarmwatch.go
B=cmd/panelyd/backup.go
D=cmd/panelyd/watchdog.go
S=internal/store/store.go
M=cmd/panelyd/main.go
U=deploy/systemd/panelyd.service
FILES=("$L" "$H" "$W" "$A" "$B" "$D" "$S" "$M" "$U")
LIV=./internal/liveness/
HEA=./internal/health/
PAN=./cmd/panelyd/

BAK=$(mktemp -d)
for f in "${FILES[@]}"; do cp "$f" "$BAK/$(basename "$f")"; done
restore() { for f in "${FILES[@]}"; do cp "$BAK/$(basename "$f")" "$f"; done; }
trap 'restore; rm -rf "$BAK"' EXIT

fail=0

# ── Taban YEŞİL olmalı ───────────────────────────────────────────────
#
# Mutasyonsuz kodda düşen bir test her mutantı "yakalandı" gösterirdi:
# SESSİZ sahte geçiş. Her farklı test komutu, ilk mutantından önce bir kez
# mutasyonsuz kodda koşturuluyor.
declare -A TABAN=()
taban_yesil() {
    local key="$*"
    [[ -n "${TABAN[$key]:-}" ]] && return 0
    restore
    if ! go test "$@" >/dev/null 2>&1; then
        echo "!! TABAN KIRMIZI: mutasyonsuz kodda 'go test $*' düşüyor — ölçüm YAPILMADI"
        exit 1
    fi
    TABAN[$key]=1
}

# mutate <ad> <dosya> <"test-paketleri"> <python-ifadesi>
mutate() {
    local name="$1" file="$2" pkgs="$3" expr="$4"
    restore
    taban_yesil $pkgs -count=1 -timeout 120s
    if ! python -c "
import io,sys
class _S(str):
    def replace(self,a,b,*r):
        # TEK eşleşme şart: aranan metin bir yorumda da geçiyorsa
        # replace(…,1) İLKİNİ, yani yorumu değiştirir ve kod hiç mutasyona
        # uğramadan ölçülür (K-127).
        n=self.count(a)
        if n!=1:
            sys.stderr.write('REPLACE '+str(n)+' KEZ ESLESTI (1 olmali): '+repr(a[:70])+chr(10))
            sys.exit(8)
        return _S(str.replace(self,a,b,*r))
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
    # "yakalandı" diye okur (K-096). `go test` vet'in bir alt kümesini de
    # koşturuyor; vet'e takılan bir mutant da burada durur.
    local build_out
    # shellcheck disable=SC2086
    if ! build_out=$(go test $pkgs -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI,"
        echo "     mutasyon derlenebilir olacak şekilde yazılmalı. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    # shellcheck disable=SC2086
    if go test $pkgs -count=1 -timeout 120s >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Döngüler damgayı tazeliyor =="

mutate "vekil izleyicisi Mark etmiyor" "$W" "$PAN" \
    "s=s.replace('\t\t\tw.tick(ctx)\n\t\t\tbeat.Mark()\n','\t\t\tw.tick(ctx)\n',1)"

mutate "disk açılışta Mark etmiyor" "$A" "$PAN" \
    "s=s.replace('\tcheckDisk(ctx, exec, am)\n\tbeat.Mark()\n','\tcheckDisk(ctx, exec, am)\n',1)"

mutate "disk ticker'da Mark etmiyor" "$A" "$PAN" \
    "s=s.replace('\t\t\tcheckDisk(ctx, exec, am)\n\t\t\tbeat.Mark()\n','\t\t\tcheckDisk(ctx, exec, am)\n',1)"

mutate "yedek açılışta Mark etmiyor" "$B" "$PAN" \
    "s=s.replace('\ttakeBackup(ctx, db, am)\n\tbeat.Mark()\n','\ttakeBackup(ctx, db, am)\n',1)"

mutate "yedek ticker'da Mark etmiyor" "$B" "$PAN" \
    "s=s.replace('\t\t\ttakeBackup(ctx, db, am)\n\t\t\tbeat.Mark()\n','\t\t\ttakeBackup(ctx, db, am)\n',1)"

# Uygulamasız sunucu: ziyaret yok, damga yalnızca tur başında tazeleniyor.
mutate "gözetmen tur başında ilerleme bildirmiyor" "$H" "$HEA" \
    "s=s.replace('\ts.progress()\n\n\tdeps, err := s.store.ActiveDeployments(ctx)','\tdeps, err := s.store.ActiveDeployments(ctx)',1)"

mutate "gözetmen ziyaret başına ilerleme bildirmiyor" "$H" "$HEA" \
    "s=s.replace('\t\ts.visit(ctx, d)\n\t\ts.progress()\n','\t\ts.visit(ctx, d)\n',1)"

echo "== Ping ancak canlıyken =="

mutate "bayat döngüye rağmen ping" "$L" "$LIV" \
    "s=s.replace('\tif stale := w.Beats.Stale(); len(stale) > 0 {','\tif stale := w.Beats.Stale(); len(stale) > 99 {',1)"

mutate "veritabanı yoklaması yok sayılıyor" "$L" "$LIV" \
    "s=s.replace('\tif err := w.DB.PingContext(probe); err != nil {','\tif err := w.DB.PingContext(probe); err != nil && len(w.Beats.Names()) < 0 {',1)"

mutate "hata varken de ping atılıyor" "$L" "$LIV" \
    "s=s.replace('\t\t\t\"sebep\", err)\n\t\treturn\n\t}','\t\t\t\"sebep\", err)\n\t}',1)"

mutate "yoklamanın kendi sınırı yok" "$L" "$LIV" \
    "s=s.replace('\tprobe, cancel := context.WithTimeout(ctx, w.ProbeTimeout)','\tprobe, cancel := context.WithCancel(ctx)',1)"

# Yoklamanın DAYANDIĞI varsayım: havuz tek bağlantılı. İki bağlantıda
# PingContext boş olanı alır ve tükenmeyi göremez.
mutate "havuz iki bağlantılı" "$S" "$LIV" \
    "s=s.replace('\tdb.SetMaxOpenConns(1)\n\tdb.SetMaxIdleConns(1)','\tdb.SetMaxOpenConns(2)\n\tdb.SetMaxIdleConns(1)',1)"

echo "== Damga ve eşik =="

mutate "tam eşikte bayat sayılıyor" "$L" "$LIV" \
    "s=s.replace('\t\tif age > b.maxAge {','\t\tif age >= b.maxAge {',1)"

mutate "kayıt anında damga yok" "$L" "$LIV" \
    "s=s.replace('\tb.last.Store(r.now().UnixNano())\n','',1)"

mutate "Mark damgayı tazelemiyor" "$L" "$LIV" \
    "s=s.replace('\tgap := now - b.last.Swap(now)','\tgap := now - b.last.Load()',1)"

mutate "çarpan üç değil iki" "$D" "$PAN" \
    "s=s.replace('return max(3*every, minLoopMaxAge)','return max(2*every, minLoopMaxAge)',1)"

mutate "taban on dakika" "$D" "$PAN" \
    "s=s.replace('const minLoopMaxAge = 15 * time.Minute','const minLoopMaxAge = 10 * time.Minute',1)"

mutate "kapalı döngü de kaydediliyor" "$D" "$PAN" \
    "s=s.replace('\tif every <= 0 {\n\t\treturn nil\n\t}\n\treturn r.Register','\treturn r.Register',1)"

echo "== systemd sözleşmesi =="

mutate "WATCHDOG_PID yok sayılıyor" "$L" "$LIV" \
    "s=s.replace('\t\tif owner != pid {','\t\tif owner != pid && pid < 0 {',1)"

mutate "aralık yarıya bölünmüyor" "$L" "$LIV" \
    "s=s.replace('time.Duration(usec) * time.Microsecond / 2, nil','time.Duration(usec) * time.Microsecond, nil',1)"

echo "== Rapor =="

mutate "rapor sıfırlanmıyor" "$L" "$LIV" \
    "s=s.replace('b.maxGap.Swap(0)','b.maxGap.Load()',1)"

# Canlıdaki ilk rapor saatlik yedek için "0s" dedi (K-118): sürmekte olan
# aralık sayılmıyordu.
mutate "rapor sürmekte olan aralığı saymıyor" "$L" "$LIV" \
    "s=s.replace('\t\tgap := max(b.maxGap.Swap(0), now-b.last.Load())','\t\tgap := max(b.maxGap.Swap(0), now-now)',1)"

mutate "rapor en uzunu değil sonuncuyu tutuyor" "$L" "$LIV" \
    "s=s.replace('\t\tif gap <= cur || b.maxGap.CompareAndSwap(cur, gap) {','\t\tif b.maxGap.CompareAndSwap(cur, gap) {',1)"

echo "== Açılış systemd'nin sınırına sığıyor (K-117) =="

# Watchdog READY'den sonra kuruluyor; READY'ye kadarki süreyi
# TimeoutStartSec koruyor. Varsayılan 90 sn, en kötü açılış 103 sn.
mutate "TimeoutStartSec yok (varsayılan 90 sn)" "$U" "$PAN" \
    "s=s.replace('\nTimeoutStartSec=180s\n','\n',1)"

mutate "TimeoutStartSec en kötü açılıştan kısa" "$U" "$PAN" \
    "s=s.replace('\nTimeoutStartSec=180s\n','\nTimeoutStartSec=90s\n',1)"

mutate "deneme sayısı arttı, birim güncellenmedi" "$M" "$PAN" \
    "s=s.replace('const startupReconcileTries = 3','const startupReconcileTries = 5',1)"

mutate "deneme süresi arttı, birim güncellenmedi" "$M" "$PAN" \
    "s=s.replace('const startupReconcileTimeout = 30 * time.Second','const startupReconcileTimeout = 60 * time.Second',1)"

restore
if [[ $fail -ne 0 ]]; then
    echo
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi

echo
echo "Bütün mutasyonlar yakalandı."
