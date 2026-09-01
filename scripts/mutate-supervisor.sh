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

mutate() {
    local name="$1" want="$2" expr="$3"
    restore
    if ! python -c "
import io,sys
p='$SRC'
s=io.open(p,encoding='utf-8').read()
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
