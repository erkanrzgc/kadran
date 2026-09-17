#!/usr/bin/env bash
# Zamanlı yedek ve geri yükleme testlerinin GERÇEKTEN bir şey
# koruduğunu sınar.
#
# Her mutasyon korunan bir özelliği KASTEN bozar; test kırmızıya
# dönmezse o özellik korunmuyor demektir.
#
# ── Neden ayrı bir betik (mutate-backup.sh'a eklenmedi) ─────────────
#
# mutate-backup.sh GÖÇ ÖNCESİ yedeği sınıyor: farklı dosya, farklı
# değişmezler, farklı test kümesi. İkisini tek betiğe koymak, bir
# mutasyonun hangi özelliği ölçtüğünü okunmaz hale getirirdi.
#
# ── K-071'in dersi ──────────────────────────────────────────────────
#
# Yeşil kalan bir mutasyon İKİ zıt sonuç doğurabilir: test zayıftır ya da
# MUTASYON zayıftır. Bu yüzden betik, mutasyonun dosyaya uygulanıp
# uygulanmadığını da ayrıca doğruluyor.
set -uo pipefail

cd "$(dirname "$0")/.."
SNAP=internal/store/snapshot.go
REST=internal/store/restore.go
BAK_SNAP=$(mktemp)
BAK_REST=$(mktemp)
cp "$SNAP" "$BAK_SNAP"
cp "$REST" "$BAK_REST"
restore() { cp "$BAK_SNAP" "$SNAP"; cp "$BAK_REST" "$REST"; }
trap restore EXIT

fail=0
WANT='TestSnapshot|TestListSnapshots|TestRestore'

# mutate <ad> <dosya> <python-ifadesi>
mutate() {
    local name="$1" file="$2" expr="$3"
    restore
    if ! python -c "
import io,sys
p='$file'
s=io.open(p,encoding='utf-8').read()
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

    if go test ./internal/store/ -run "$WANT" -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Geri yükleme mutasyonları =="

# BU DİLİMİN EN ÖNEMLİ MUTASYONU.
#
# Yan dosyaları silmeyi atlayan bir geri yükleme, taze bir geçici
# dizinde koşan sıradan bir testten YEŞİL geçer. Gerçek sunucuda ise
# SQLite eski WAL'i yeni veritabanına oynatır ve geri yükleme sessizce
# etkisiz kalır.
mutate "yan dosya silme kaldırıldı" "$REST" \
    "s=s.replace('''	for _, ext := range sqliteSidecars {
		if err := os.Remove(dbPath + ext); err != nil && !os.IsNotExist(err) {''','''	for _, ext := range []string{} {
		if err := os.Remove(dbPath + ext); err != nil && !os.IsNotExist(err) {''',1)"

mutate "yalnızca -wal siliniyor, -shm bırakılıyor" "$REST" \
    "s=s.replace('var sqliteSidecars = []string{\"-wal\", \"-shm\"}','var sqliteSidecars = []string{\"-shm\"}',1)"

mutate "güvenlik kopyası alınmıyor" "$REST" \
    "s=s.replace('''		safety, err = safetyCopy(ctx, dbPath)
		if err != nil {
			return \"\", err
		}''','''		_ = safetyCopy''',1)"

mutate "şema doğrulaması kaldırıldı" "$REST" \
    "s=s.replace('''	if n == 0 {
		return fmt.Errorf(
			\"yedekte hiç göç kaydı yok (%s) — boş ya da yabancı bir dosya\",
			path)
	}''','''	_ = n''',1)"

# ⚠ Bu mutasyon bir ÖNCEKİ turda "yalnızca hatayı yut" biçimindeydi ve
# YEŞİL kaldı. Sebep ölçüldü: yabancı dosyada `n` sıfır kalıyor ve
# hemen ardından gelen `n == 0` kontrolü reddi yine üretiyordu — yani
# mutasyon davranışı hiç değiştirmiyordu, iki kontrol birbirini
# yedekliyor. Zayıf olan test değil MUTASYONDU (K-080'in ikinci sebebi).
#
# Gerçek bir kusur üretmek için şema doğrulamasının TAMAMI kaldırılıyor.
mutate "şema doğrulaması tamamen kaldırıldı" "$REST" \
    "import re
i=s.index('	// Şema kontrolü')
j=s.index('	return nil', i)
s=s[:i]+'	_ = n\n'+s[j:]"

mutate "integrity_check sonucu yok sayılıyor" "$REST" \
    "s=s.replace('''	if result != \"ok\" {''','''	if false {''',1)"

echo "== Zamanlı yedek mutasyonları =="

# Budama globu genişletiliyor: artık göç öncesi yedekleri ve SQLite'ın
# yan dosyalarını da görür. Bu tam olarak ayrı dizin kararının
# engellediği sınıf.
mutate "budama globu veritabanının yanına taşındı" "$SNAP" \
    "s=s.replace('''	matches, err := filepath.Glob(
		filepath.Join(dir, snapshotPrefix+\"*\"+snapshotExt))
	if err != nil || len(matches) <= SnapshotKeep {
		return
	}''','''	matches, err := filepath.Glob(filepath.Join(filepath.Dir(dir), \"panely.db*\"))
	if err != nil || len(matches) <= SnapshotKeep {
		return
	}''',1)"

mutate "budama EN YENİLERİ siliyor" "$SNAP" \
    "s=s.replace('for _, old := range matches[:len(matches)-SnapshotKeep] {','for _, old := range matches[SnapshotKeep:] {',1)"

# Değişken genişlikli damga: sondaki sıfırlar kırpıldığı için sözlük
# sırası kronolojik sırayı TEMSİL ETMEZ.
mutate "damga değişken genişliğe çevrildi" "$SNAP" \
    "s=s.replace('const snapshotStamp = \"20060102T150405Z\"','const snapshotStamp = \"2006-01-02T15:04:05.999999999Z\"',1)"

mutate "yedek dizini veritabanının dizinine eşitlendi" "$SNAP" \
    "s=s.replace('''	return filepath.Join(filepath.Dir(dbPath), snapshotDirName)''','''	return filepath.Dir(dbPath)''',1)"

mutate "yedek zamanı dosya adı yerine sabit" "$SNAP" \
    "s=s.replace('''	t, err := time.Parse(snapshotStamp, base)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()''','''	_ = base
	return time.Time{}''',1)"

restore
if [[ $fail -ne 0 ]]; then
    echo
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi

echo
echo "Bütün mutasyonlar yakalandı."
