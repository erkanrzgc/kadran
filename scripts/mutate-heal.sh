#!/usr/bin/env bash
# Heal testlerinin GERÇEKTEN bir şey koruduğunu sınar.
#
# Her mutasyon korunan bir özelliği KASTEN bozar; test kırmızıya
# dönmezse o özellik korunmuyor demektir.
#
# ── K-071'in dersi ──────────────────────────────────────────────────
#
# Yeşil kalan bir mutasyon İKİ zıt sonuç doğurabilir: test zayıftır ya da
# MUTASYON zayıftır. Bu yüzden betik, mutasyonun dosyaya uygulanıp
# uygulanmadığını da ayrıca doğruluyor — uygulanamayan bir mutasyon
# sessizce "yeşil" raporlanırsa, hiç ölçmediğini ölçmüş gibi gösterir.
set -uo pipefail

cd "$(dirname "$0")/.."
SRC=internal/deploy/rollout.go
BAK=$(mktemp)
cp "$SRC" "$BAK"
restore() { cp "$BAK" "$SRC"; }
trap restore EXIT

fail=0

# mutate <ad> <beklenen-kirmizi-test> <python-ifadesi>
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
        echo \"  !! MUTASYON UYGULANAMADI: $name — betik bozuk, ölçüm YAPILMADI\"
        fail=1
        return
    fi

    if go test ./internal/deploy/ -run "$want" >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name  (test: $want)"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Heal mutasyonları =="

mutate "Reconcile cagrisi silindi" "TestHealRestartsStoppedContainerAndReroutes" \
    "s=s.replace('''	res, err := r.rec.Reconcile(ctx)
	if err != nil {
		return recreated, err
	}
	// POZİTİF ÖLÇÜT (K-042)''','''	res := Result{Skipped: map[string]string{}}
	_ = res
	// POZİTİF ÖLÇÜT (K-042)''',1)"

mutate "atlanan-uygulama kontrolu silindi" "TestHealFailsWhenProxySkipsApp" \
    "s=s.replace('''	if why, skipped := res.Skipped[app.ID]; skipped {
		return recreated, fmt.Errorf(\"ters vekile yazılamadı: %s\", why)
	}
	return recreated, nil
}''','''	return recreated, nil
}''',1)"

mutate "iyilestirme kapisi dagitim kapisina cevrildi" "TestHealUsesShortGateNotDeployGate" \
    "s=s.replace('r.awaitReady(ctx, app, rel.ID, r.healGate)','r.awaitReady(ctx, app, rel.ID, r.gate)',1)"

mutate "kapi tamamen silindi (uzlastirma erken)" "TestHealFailsWhenContainerNeverAnswers" \
    "s=s.replace('''	if err := r.awaitReady(ctx, app, rel.ID, r.healGate); err != nil {
		return recreated, fmt.Errorf(\"iyileştirme kapısında durdu: %w\", err)
	}
''','',1)"

mutate "Heal switchTraffic'e baglandi (bosaltma yapar)" "TestHealDoesNotTouchTrafficOwnership" \
    "s=s.replace('''	if err := r.awaitReady(ctx, app, rel.ID, r.healGate); err != nil {
		return recreated, fmt.Errorf(\"iyileştirme kapısında durdu: %w\", err)
	}
''','''	if err := r.switchTraffic(ctx, app, rel.ID); err != nil {
		return recreated, err
	}
''',1)"

mutate "dagitim kapisi ardisik sayaci sifirlamiyor" "TestReadyStreakResetsOnAnyFailedProbe|TestFailedProbeResetsTheReadyStreak" \
    "s=s.replace('''			// SIFIRLANIYOR: aralıklı bir başarı, sağlıklı sayılmaz.
			streak = 0''','''			// SIFIRLANIYOR: aralıklı bir başarı, sağlıklı sayılmaz.''',1)"

restore
echo
if [ "$fail" -ne 0 ]; then
    echo "SONUÇ: en az bir mutasyon yakalanmadı."
    exit 1
fi
echo "SONUÇ: tüm mutasyonlar yakalandı."
