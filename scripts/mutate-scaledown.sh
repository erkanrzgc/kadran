#!/usr/bin/env bash
# Ölçek küçültme testlerinin GERÇEKTEN bir şey koruduğunu sınar.
#
# Her mutasyon korunan bir özelliği KASTEN bozar; test kırmızıya
# dönmezse o özellik korunmuyor demektir.
#
# ── Bu betiğin kendi geçmişi ────────────────────────────────────────
#
# "Sürüm filtresi kaldırıldı" mutasyonu İKİ TUR yeşil geçti ve her
# turda sebep farklıydı:
#
#   1. tur — testlerin dünyasında tek sürüm vardı, dolayısıyla filtre
#      hiçbir şey yapmıyordu. TEST zayıftı; mavi-yeşil senaryosu eklendi.
#   2. tur — test eklendi ama HÂLÂ yeşil geçti. Sebep testin değil
#      KODUN kusuruydu: durdurma çağrısı gezilen replikanın
#      `rep.ReleaseID`'sini değil `rel.ID`'yi geçiyordu, yani filtre
#      kalksa bile çağrı aktif sürümü adlandırıyor ve fark gözlenemiyordu.
#
# Yani yeşil bir mutasyonun ÜÇ olası sebebi var: zayıf test, zayıf
# mutasyon, ya da gizli bir kod tutarsızlığı. K-071 ilk ikisini
# söylüyordu; üçüncüsü buradan çıktı.
set -uo pipefail

cd "$(dirname "$0")/.."
RECONCILE=internal/deploy/reconcile.go
ROLLOUT=internal/deploy/rollout.go
APPUPDATE=internal/api/appupdate.go
BAK_R=$(mktemp)
BAK_O=$(mktemp)
BAK_A=$(mktemp)
cp "$RECONCILE" "$BAK_R"
cp "$ROLLOUT" "$BAK_O"
cp "$APPUPDATE" "$BAK_A"
restore() { cp "$BAK_R" "$RECONCILE"; cp "$BAK_O" "$ROLLOUT"; cp "$BAK_A" "$APPUPDATE"; }
trap restore EXIT

fail=0
WANT='TestScaleDown|TestScaleUp|TestZeroReplicas|TestHealStopsExtraReplicas|TestHealKeepsEveryReplica|TestExtraReplicaDoesNotCountAsHealth|TestInRangeReplicaCountsAsHealth'

# mutate <ad> <dosya> <python-ifadesi> [paket] [test-deseni]
mutate() {
    local name="$1" file="$2" expr="$3"
    local pkg="${4:-./internal/deploy/}" want="${5:-$WANT}"
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

    if go test "$pkg" -run "$want" -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Ölçek küçültme mutasyonları =="

# ── Rotalama tarafı ──────────────────────────────────────────────────

mutate "rota indeks filtresi silindi" "$RECONCILE" \
    "s=s.replace('if rep.Index >= d.Replicas {','if false {',1)"

mutate "rota filtresi >= yerine > (bir fazla rotalar)" "$RECONCILE" \
    "s=s.replace('if rep.Index >= d.Replicas {','if rep.Index > d.Replicas {',1)"

mutate "rota filtresi d.Replicas yerine sabit 1" "$RECONCILE" \
    "s=s.replace('if rep.Index >= d.Replicas {','if rep.Index >= 1 {',1)"

mutate "sifir replika korumasi kaldirildi" "$RECONCILE" \
    "s=s.replace('if d.Replicas == 0 {','if false {',1)"

# ── Durdurma tarafı ──────────────────────────────────────────────────

mutate "fazlalik durdurma dongusu devre disi" "$ROLLOUT" \
    "s=s.replace('if rep.ReleaseID != rel.ID || rep.Index < app.Replicas {','if true {',1)"

mutate "durdurma < yerine <= (canli replikayi indirir)" "$ROLLOUT" \
    "s=s.replace('rep.ReleaseID != rel.ID || rep.Index < app.Replicas','rep.ReleaseID != rel.ID || rep.Index <= app.Replicas',1)"

mutate "durdurma surum filtresi kaldirildi (mavi-yesil)" "$ROLLOUT" \
    "s=s.replace('if rep.ReleaseID != rel.ID || rep.Index < app.Replicas {','if rep.Index < app.Replicas {',1)"

# ── ELENEN MUTASYON: "rep.ReleaseID yerine rel.ID + filtre kaldir" ──
#
# Bu ikili mutasyon denendi ve YESIL gecti. Once testin zayifligi sanildi,
# sonra olculdu: mutasyon korunan ozelligi KIRMIYOR.
#
# Sebep, secicinin nasil cozuldugu. Cagri (app, rel.ID, index) diyorsa
# executor AKTIF surumun o indeksli konteynerini arar. Eski surumun
# replikasi bu adla bulunamaz, dolayisiyla hicbir sey durdurulmaz -
# yalnizca bosa giden bir cagri olur. "Baska surume dokunma" ozelligi
# ihlal edilmis olmuyor.
#
# Yani bu K-071'in BIRINCI yorumuydu: yesil kalan mutasyonun KENDISI
# zayifti. Kaldirildi; yerini bir ustteki "surum filtresi kaldirildi"
# tutuyor - o gercekten eski surumu indiriyor ve YAKALANIYOR.

# ── Saglik olcusu tarafi ────────────────────────────────────────────
#
# Saglik olcusu rotalamayla AYNI kumeye bakmazsa sessiz bir kesinti
# dogar: fazlalik ayakta, rotalanan replika olu, gozetmen 'saglikli' der.

mutate "saglik olcusu fazlaliklari da sayiyor" "$ROLLOUT" \
    "s=s.replace('rep.Index >= app.Replicas','false && rep.Index >= app.Replicas',1)"

mutate "saglik olcusu >= yerine > (bir fazla sayar)" "$ROLLOUT" \
    "s=s.replace('rep.Index >= app.Replicas','rep.Index > app.Replicas',1)"

# ── Tetikleyici tarafı ───────────────────────────────────────────────
#
# Mekanizmayı düzeltip tetikleyiciyi eksik bırakmak, hatayı en sık
# kullanılan yolda açık tutmak olurdu: kullanıcı `app update -replicas 1`
# der, komut "başarılı" der, rota daralmaz.

UPD='TestUpdateAppReconcilesWhenReplicaCountChanges|TestUpdateAppSkipsReconcileWhenNothingRoutableChanges'

mutate "replika uzlastirma tetikleyicisi kaldirildi" "$APPUPDATE"     "s=s.replace('if domainMoved || replicasChanged {','if domainMoved {',1)"     "./internal/api/" "$UPD"

mutate "replika esitlik kontrolu yok (her zaman tetikler)" "$APPUPDATE"     "s=s.replace('replicasChanged := upd.Replicas != nil and *upd.Replicas != current.Replicas'.replace(' and ',' && '),'replicasChanged := upd.Replicas != nil',1)"     "./internal/api/" "$UPD"

mutate "replicasChanged hep false" "$APPUPDATE"     "s=s.replace('replicasChanged := upd.Replicas != nil and *upd.Replicas != current.Replicas'.replace(' and ',' && '),'replicasChanged := false',1)"     "./internal/api/" "$UPD"

restore
echo
if [ "$fail" -ne 0 ]; then
    echo "BAŞARISIZ: en az bir mutasyon yakalanmadı ya da uygulanamadı."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
