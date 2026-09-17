#!/usr/bin/env bash
# Göç öncesi yedek testlerinin GERÇEKTEN bir şey koruduğunu sınar.
#
# Her mutasyon korunan bir özelliği KASTEN bozar; test kırmızıya
# dönmezse o özellik korunmuyor demektir.
#
# ── Bu betiğin kendi geçmişi ────────────────────────────────────────
#
# İlk turda "tekrar denemede üzerine yazıyor" mutasyonu YEŞİL geçti.
# Sebep testin dosya SAYISINA ve boyutuna bakmasıydı: üzerine yazan bir
# uygulamada da tek, dolu bir dosya kalır, yani iddia ayırt edici
# değildi. Test denemeler arasına bir işaretçi satır yazacak şekilde
# güçlendirildi ve mutasyon yakalandı. K-071'in tersi yönü: bu kez zayıf
# olan mutasyon değil, TESTTİ.
#
# ── K-071'in dersi ──────────────────────────────────────────────────
#
# Yeşil kalan bir mutasyon İKİ zıt sonuç doğurabilir: test zayıftır ya da
# MUTASYON zayıftır. Bu yüzden betik, mutasyonun dosyaya uygulanıp
# uygulanmadığını da ayrıca doğruluyor.
set -uo pipefail

cd "$(dirname "$0")/.."
STORE=internal/store/store.go
BACKUP=internal/store/backup.go
BAK_STORE=$(mktemp)
BAK_BACKUP=$(mktemp)
cp "$STORE" "$BAK_STORE"
cp "$BACKUP" "$BAK_BACKUP"
restore() { cp "$BAK_STORE" "$STORE"; cp "$BAK_BACKUP" "$BACKUP"; }
trap restore EXIT

fail=0
WANT='TestFreshDatabaseTakesNoBackup|TestBrokenMigrationIsRecoverableFromBackup|TestBackupIsNotOverwrittenOnRetry|TestBackupsArePruned'

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
    if ! build_out=$(go test ./internal/store/ -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI,"
        echo "     mutasyon derlenebilir olacak şekilde yazılmalı. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if go test ./internal/store/ -run "$WANT" -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Göç öncesi yedek mutasyonları =="

mutate "yedek çağrısı tamamen kaldırıldı" "$STORE" \
    "s=s.replace('''				if err := snapshotBeforeMigrate(ctx, s.db, s.path, name); err != nil {
					return err
				}''','''				_ = name''',1)"

mutate "yedek hatası yutuluyor" "$STORE" \
    "s=s.replace('''				if err := snapshotBeforeMigrate(ctx, s.db, s.path, name); err != nil {
					return err
				}''','''				_ = snapshotBeforeMigrate(ctx, s.db, s.path, \"yanlis-ad\")''',1)"

mutate "taze veritabanı kontrolü kaldırıldı" "$STORE" \
    "s=s.replace('\t\t\tif !fresh {','\t\t\t_ = fresh\n\t\t\tif true {',1)"

mutate "snapshot no-op yapıldı" "$BACKUP" \
    "s=s.replace('''	// Bellek veritabanının dosyası yok; testler buradan geçer.
	if path == \":memory:\" || path == \"\" {
		return nil
	}''','''	return nil''',1)"

mutate "tekrar denemede üzerine yazıyor" "$BACKUP" \
    "s=s.replace('''	if _, err := os.Stat(dest); err == nil {
		return nil
	}''','''	_ = os.Remove(dest)''',1)"

mutate "budama devre dışı" "$BACKUP" \
    "s=s.replace('''	sort.Strings(matches)
	for _, old := range matches[:len(matches)-backupKeep] {
		_ = os.Remove(old)
	}''','''	sort.Strings(matches)''',1)"

mutate "budama yanlış uçtan siliyor (en yenileri atıyor)" "$BACKUP" \
    "s=s.replace('for _, old := range matches[:len(matches)-backupKeep] {','for _, old := range matches[backupKeep:] {',1)"

restore
echo
if [ "$fail" -ne 0 ]; then
    echo "BAŞARISIZ: en az bir mutasyon yakalanmadı ya da uygulanamadı."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
