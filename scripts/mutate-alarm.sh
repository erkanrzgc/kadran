#!/usr/bin/env bash
# Alarm testlerinin GERÇEKTEN bir şey koruduğunu sınar.
#
# Her mutasyon korunan bir özelliği KASTEN bozar; test kırmızıya
# dönmezse o özellik korunmuyor demektir.
#
# ── Bu dilimde neyin bozulması EN PAHALI ────────────────────────────
#
# Alarmın kendisi değil, GÜVENİLİRLİĞİ. Her turda çalan bir alarm teknik
# olarak "çalışıyor" ama pratikte kapatılır — ve kapatıldığı an gerçek
# arıza da görünmez olur. Bu yüzden mutasyonların çoğu "bir kez bildir"
# kuralını hedefliyor.
#
# ── K-071/K-080'in dersi ────────────────────────────────────────────
#
# Yeşil kalan bir mutasyon İKİ zıt sonuç doğurabilir: test zayıftır ya da
# MUTASYON zayıftır. Bu yüzden betik, mutasyonun dosyaya uygulanıp
# uygulanmadığını da ayrıca doğruluyor.
set -uo pipefail

cd "$(dirname "$0")/.."
ALARM=internal/alarm/alarm.go
STORE=internal/store/alarms.go
SUP=internal/health/supervisor.go
WATCH=cmd/panelyd/alarmwatch.go

BAK_ALARM=$(mktemp); BAK_STORE=$(mktemp)
BAK_SUP=$(mktemp); BAK_WATCH=$(mktemp)
cp "$ALARM" "$BAK_ALARM"; cp "$STORE" "$BAK_STORE"
cp "$SUP" "$BAK_SUP"; cp "$WATCH" "$BAK_WATCH"
restore() {
    cp "$BAK_ALARM" "$ALARM"; cp "$BAK_STORE" "$STORE"
    cp "$BAK_SUP" "$SUP"; cp "$BAK_WATCH" "$WATCH"
}
trap restore EXIT

fail=0

# mutate <ad> <dosya> <test-paketi> <python-ifadesi>
mutate() {
    local name="$1" file="$2" pkg="$3" expr="$4"
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
    if ! build_out=$(go test "$pkg" -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI,"
        echo "     mutasyon derlenebilir olacak şekilde yazılmalı. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if go test "$pkg" -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Kenar tetikleme mutasyonları =="

# ⚠ BU DÖRT MUTASYON DERLENEBİLİR OLMAK ZORUNDA.
#
# İlk hâlleri `if fresh {` → `if true {` diyordu ve DÖRDÜ DE
# "yakalandı" raporluyordu. Ölçünce görüldü: paket DERLENMİYORDU
# ("fresh declared and not used"), yani `go test` derleme hatasından
# düşüyordu ve testlerin iddiası hiç sınanmamıştı. Betiğin en önemli
# dört satırı hiçbir şey kanıtlamıyordu.
#
# `_ =` ile değişken tüketiliyor; artık mutasyon DERLENİYOR ve kırmızı,
# gerçekten testin iddiasından geliyor.
#
# Ders K-093'ün doğrudan uygulaması: yakalanan bir mutasyon da
# sorgulanmalı — beklenen sebeple kırmızıya dönmemiş olabilir.

# EN ÖNEMLİ MUTASYON: her turda bildir.
mutate "her yükseltme bildiriyor" "$ALARM" ./internal/alarm/ \
    "s=s.replace('	if fresh {','	_ = fresh\n	if true {',1)"

mutate "hiç alarm yokken de kapanış bildiriliyor" "$ALARM" ./internal/alarm/ \
    "s=s.replace('	if wasActive {','	_ = wasActive\n	if true {',1)"

mutate "yükseltme hiç bildirmiyor" "$ALARM" ./internal/alarm/ \
    "s=s.replace('	if fresh {','	_ = fresh\n	if false {',1)"

mutate "kapanış hiç bildirilmiyor" "$ALARM" ./internal/alarm/ \
    "s=s.replace('	if wasActive {','	_ = wasActive\n	if false {',1)"

echo "== Depo değişmezleri =="

# `since` üzerine yazılırsa "ne zamandır bozuk" her turda sıfırlanır.
mutate "since her yükseltmede üzerine yazılıyor" "$STORE" ./internal/alarm/ \
    "s=s.replace('ON CONFLICT(id) DO NOTHING','ON CONFLICT(id) DO UPDATE SET since=excluded.since',1)"

mutate "ciddiyet doğrulaması kaldırıldı" "$STORE" ./internal/alarm/ \
    "s=s.replace('''	if a.Severity != SeverityWarning && a.Severity != SeverityCritical {
		return false, fmt.Errorf(\"bilinmeyen ciddiyet: %q\", a.Severity)
	}''','''	_ = a.Severity''',1)"

mutate "tırmanma kaldırıldı" "$STORE" ./internal/alarm/ \
    "s=s.replace('''		UPDATE alarms SET severity = ?
		WHERE id = ? AND severity = ?''','''		UPDATE alarms SET severity = ?
		WHERE id = ? AND severity = ? AND 1=0''',1)"

# Tırmanmanın TERSİ de yapılabilir olsaydı, eşikte salınan bir koşul
# her turda ciddiyet değiştirirdi.
mutate "ciddiyet düşürülebiliyor" "$STORE" ./internal/alarm/ \
    "s=s.replace('''		UPDATE alarms SET severity = ?
		WHERE id = ? AND severity = ?''','''		UPDATE alarms SET severity = ?
		WHERE id = ? AND severity != ?''',1)"

mutate "kritikler öne sıralanmıyor" "$STORE" ./internal/alarm/ \
    "s=s.replace(\"ORDER BY CASE severity WHEN 'kritik' THEN 0 ELSE 1 END, since\",'ORDER BY since',1)"

echo "== Gözetmen bağlantısı =="

# Eşik yok sayılırsa ilk başarısız iyileştirme alarm açar — yani
# kendiliğinden düzelen durumlar için gürültü.
mutate "alarm eşiği yok sayılıyor" "$SUP" ./internal/health/ \
    "s=s.replace('		if st.heals >= s.opts.HealsBeforeAlarm {','		if true {',1)"

mutate "gözetmen hiç alarm açmıyor" "$SUP" ./internal/health/ \
    "s=s.replace('		if st.heals >= s.opts.HealsBeforeAlarm {','		if false {',1)"

# Kapatmayı st.unhealthy'ye bağlamak: yeniden başlatmadan sonra
# sağlıklı dönen uygulamanın alarmı sonsuza kadar açık kalır.
mutate "alarm kapatma st.unhealthy'ye bağlandı" "$SUP" ./internal/health/ \
    "s=s.replace('	s.alarms.Clear(ctx, alarm.KindHealExhausted+\":\"+appID)','	if st.unhealthy {\n\t\ts.alarms.Clear(ctx, alarm.KindHealExhausted+\":\"+appID)\n\t}',1)"

echo "== Disk histerezisi =="

mutate "histerezis kaldırıldı (tek eşik)" "$WATCH" ./cmd/panelyd/ \
    "s=s.replace('	diskClearFree    = 0.20','	diskClearFree    = 0.15',1)"

mutate "kritik eşik uyarının üstüne çıkarıldı" "$WATCH" ./cmd/panelyd/ \
    "s=s.replace('	diskCriticalFree = 0.05','	diskCriticalFree = 0.30',1)"

mutate "kapanma bandı yok sayılıyor" "$WATCH" ./cmd/panelyd/ \
    "s=s.replace('	case free >= diskClearFree:','	case true:',1)"

echo "== Kimlik eşleşmesi =="

# Açılış ve kapanış AYNI kimliği kullanmalı. Ayrışırlarsa alarm açılır
# ama bir daha asla kapanmaz — sessiz ve kalıcı bir sahte alarm, yani
# bu projede bir kontrolü öldüren şeyin ta kendisi.
#
# Bu mutasyon olmasa, TestDiskAlarmIDMatchesClearID'in gerçekten bir şey
# koruduğu bilinmezdi: iddia ettiği sapmayı hiçbir test üretmiyor.
mutate "yükseltme kimliği kapatmadan ayrıştı" "$WATCH" ./cmd/panelyd/     "s=s.replace('		ID:       diskAlarmID,','		ID:       diskAlarmID + \"-v2\",',1)"

restore
if [[ $fail -ne 0 ]]; then
    echo
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi

echo
echo "Bütün mutasyonlar yakalandı."
