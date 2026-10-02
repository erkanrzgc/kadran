#!/usr/bin/env bash
# Gözetmen testlerinin gerçekten bir şey koruduğunu sınar.
#
# Uygulanamayan mutasyon SESSİZCE yeşil raporlanmaz — betik bunu ayrıca
# bildiriyor (K-071).
set -uo pipefail

cd "$(dirname "$0")/.."
SRC=internal/health/supervisor.go
BAK=$(mktemp)
cp "$SRC" "$BAK"
restore() { cp "$BAK" "$SRC"; }
trap restore EXIT

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

mutate() {
    local name="$1" want="$2" expr="$3"
    restore
    taban_yesil ./internal/health/ -run "$want"
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
p='$SRC'
s=_S(io.open(p,encoding='utf-8').read())
o=s
$expr
if s==o:
    sys.exit(9)
io.open(p,'w',encoding='utf-8').write(s)
"; then
        echo "  !! MUTASYON UYGULANAMADI: $name — ölçüm YAPILMADI"
        fail=1
        return
    fi
    # ── MUTANT DERLENMELİ ───────────────────────────────
    #
    # Derlenmeyen bir mutant `go test`'i düşürür ve betik bunu
    # "yakalandı" diye okur — yani testin iddiası hiç sınanmadan YEŞİL
    # rapor üretilir. K-092'de `mutate-alarm.sh`'ın EN ÖNEMLİ dört
    # mutasyonu tam olarak böyle sahte çıktı; K-096 kapıyı bütün
    # betiklere yaydı.
    #
    # `-run '^$'` seçildi çünkü paketi VE test dosyalarını derler ama
    # hiçbir test koşmaz. `go build` yalnızca üretim kodunu derlerdi;
    # test kodunun derlenmesini bozan bir mutasyon yine sahte
    # "yakalandı" verirdi.
    local build_out
    if ! build_out=$(go test ./internal/health/ -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI,"
        echo "     mutasyon derlenebilir olacak şekilde yazılmalı. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if go test ./internal/health/ -run "$want" >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name  (test: $want)"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Gözetmen mutasyonları =="

mutate "esik 1'e dusuruldu (tek dalgalanmada mudahale)" "TestDefaultThresholdIsNotOne" \
    "s=s.replace('FailuresBeforeHeal: 3,','FailuresBeforeHeal: 1,',1)"

mutate "basarisizlik sayaci basarida sifirlanmiyor" "TestFailureStreakResetsOnRecovery" \
    "s=s.replace('''	st.failures = 0
	st.unhealthy = false
	st.heals = 0''','''	st.unhealthy = false
	st.heals = 0''',1)"

mutate "saglikli uygulama da iyilestiriliyor" "TestHealthyAppIsNeverHealed" \
    "s=s.replace('''	if ready >= app.Replicas {
		s.markHealthy(ctx, d.AppID, st)
		return
	}''','''	if false {
		s.markHealthy(ctx, d.AppID, st)
		return
	}
	_ = ready''',1)"

mutate "geri cekilme sabit (ustel degil)" "TestBackoffGrowsBetweenAttempts" \
    "s=s.replace('''	d := s.opts.BackoffBase
	for i := 1; i < n; i++ {''','''	d := s.opts.BackoffBase
	for i := 1; i < 1 && n > 0; i++ {''',1)"

mutate "geri cekilme tavansiz" "TestBackoffIsCapped" \
    "s=s.replace('''		if d >= s.opts.BackoffMax {
			return s.opts.BackoffMax
		}''','''		if false {
			return s.opts.BackoffMax
		}''',1)"

mutate "denetim kaydi her turda (gecis degil)" "TestUnhealthyAndHealedAreRecordedOnceEach" \
    "s=s.replace('	if st.unhealthy {\n		slog.Info(\"uygulama iyileşti\"','	if true {\n		slog.Info(\"uygulama iyileşti\"',1)"

mutate "yaris korumasi silindi (eskiyen surum ayaga kaldirilir)" "TestHealIsSkippedWhenActiveReleaseChanged" \
    "s=s.replace('''	cur, err := s.store.ActiveDeployment(ctx, d.AppID)
	if err != nil || cur.ReleaseID != d.ReleaseID {''','''	cur, err := s.store.ActiveDeployment(ctx, d.AppID)
	if false && (err != nil || cur.ReleaseID != d.ReleaseID) {''',1)"

mutate "surum degisince durum sifirlanmiyor" "TestStateResetsWhenReleaseChanges" \
    "s=s.replace('if !ok || st.releaseID != d.ReleaseID {','if !ok {',1)"

mutate "olu uygulamalarin durumu birakilmiyor" "TestStateIsPrunedForRemovedApps" \
    "s=s.replace('''	for id := range s.state {
		if _, ok := live[id]; !ok {
			delete(s.state, id)
		}
	}''','''	_ = live''',1)"

restore
echo
if [ "$fail" -ne 0 ]; then
    echo "SONUÇ: en az bir mutasyon yakalanmadı."
    exit 1
fi
echo "SONUÇ: tüm mutasyonlar yakalandı."
