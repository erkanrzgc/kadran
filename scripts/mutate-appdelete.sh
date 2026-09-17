#!/usr/bin/env bash
# Silme testlerinin gerçekten bir şey koruduğunu sınar.
set -uo pipefail
cd "$(dirname "$0")/.."
A=internal/api/appdelete.go
S=internal/store/appdelete.go
BA=$(mktemp); BS=$(mktemp)
cp "$A" "$BA"; cp "$S" "$BS"
restore() { cp "$BA" "$A"; cp "$BS" "$S"; }
trap restore EXIT
fail=0

mutate() {
    local name="$1" file="$2" pkg="$3" want="$4" expr="$5"
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
s=_S(io.open(p,encoding='utf-8').read()); o=s
$expr
if s==o: sys.exit(9)
io.open(p,'w',encoding='utf-8').write(s)
"; then
        echo "  !! MUTASYON UYGULANAMADI: $name — ölçüm YAPILMADI"; fail=1; return
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
    if ! build_out=$(go test "$pkg" -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI,"
        echo "     mutasyon derlenebilir olacak şekilde yazılmalı. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if go test "$pkg" -run "$want" >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name  (test: $want)"; fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Silme mutasyonları =="

mutate "konteyner hatasina ragmen kayitlar siliniyor" "$A" ./internal/api/ \
    "TestDeleteKeepsRecordsWhenContainerRemovalFails" \
    "s=s.replace('''	removed, err := s.removeContainers(ctx, appID)
	params[\"containers_removed\"] = strconv.FormatUint(uint64(removed), 10)
	if err != nil {
		// Kayıtlara DOKUNULMADI: komut yeniden çalıştırılabilir.
		return nil, s.completed(ctx, action, tgt, params, err)
	}''','''	removed, _ := s.removeContainers(ctx, appID)
	params[\"containers_removed\"] = strconv.FormatUint(uint64(removed), 10)''',1)"

mutate "API canlilik kontrolu silindi" "$A" ./internal/api/ \
    "TestDeleteRefusesLiveApp" \
    "s=s.replace('if live, err := s.store.ActiveDeployment(ctx, appID); err == nil {','if live, err := s.store.ActiveDeployment(ctx, appID); false and err == nil {'.replace(' and ',' && '),1)"

mutate "durdurma atlandi (dogrudan kaldir)" "$A" ./internal/api/ \
    "TestDeleteRemovesContainersThenRecords" \
    "s=s.replace('''		if _, err := s.exec.StopRelease(ctx, appID, relID, deleteGrace); err != nil {
			return removed, fmt.Errorf(\"sürüm %s durdurulamadı: %w\", relID, err)
		}
''','',1)"

mutate "DEPO canlilik kontrolu silindi" "$S" ./internal/store/ \
    "TestDeleteAppRefusesWhileLive" \
    "s=s.replace('	case err == nil:','	case false:',1)"

mutate "silme sirasi ters (apps once)" "$S" ./internal/store/ \
    "TestDeleteAppRemovesEveryTrace|TestDeleteAppLeavesOtherAppsAlone" \
    "s=s.replace('''	deployments, err := deleteRows(ctx, tx,
		\`DELETE FROM deployments WHERE app_id = ?\`, appID)''','''	if _, err := deleteRows(ctx, tx, \`DELETE FROM apps WHERE id = ?\`, appID); err != nil {
		return out, err
	}
	deployments, err := deleteRows(ctx, tx,
		\`DELETE FROM deployments WHERE app_id = ?\`, appID)''',1)"

mutate "WHERE app_id unutuldu (hepsini siler)" "$S" ./internal/store/ \
    "TestDeleteAppLeavesOtherAppsAlone" \
    "s=s.replace('\`DELETE FROM releases WHERE app_id = ?\`, appID','\`DELETE FROM releases WHERE ? IS NOT NULL\`, appID',1)"

restore
echo
[ "$fail" -ne 0 ] && { echo 'SONUÇ: en az bir mutasyon yakalanmadı.'; exit 1; }
echo "SONUÇ: tüm mutasyonlar yakalandı."
