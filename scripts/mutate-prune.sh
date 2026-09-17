#!/usr/bin/env bash
# Budama testlerinin GERÇEKTEN bir şey koruduğunu sınar.
#
# Her mutasyon korunan bir özelliği KASTEN bozar; test kırmızıya
# dönmezse o özellik korunmuyor demektir.
#
# ── Neden bu iş mutasyon istiyor ────────────────────────────────────
#
# Budama YIKICI. Yanlış bir saklama kümesi iki ayrı felakete yol açar:
# aktif sürümü silerse siteyi düşürür, geri alma hedefini silerse
# `panely rollback` imajdan kurmak zorunda kalır ve K-061'in bütün
# gerekçesi çöker. İkisi de "budama başarılı" diyerek gerçekleşir.
#
# ── En önemli mutasyon ──────────────────────────────────────────────
#
# "önceki sürüm releases.seq'ten okunuyor": göç 0005 tam olarak bunun
# yanlış olduğunu söylüyor ve testin bunu yakalaması, saklama kümesinin
# aktivasyon GEÇMİŞİNDEN geldiğinin kanıtı.
set -uo pipefail

cd "$(dirname "$0")/.."
SRC=internal/api/appprune.go
BAK=$(mktemp)
cp "$SRC" "$BAK"
restore() { cp "$BAK" "$SRC"; }
trap restore EXIT

fail=0
WANT='TestPrune|TestKeepSet'

# mutate <ad> <python-ifadesi>
mutate() {
    local name="$1" expr="$2"
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
p='$SRC'
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
    if ! build_out=$(go test ./internal/api/ -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI,"
        echo "     mutasyon derlenebilir olacak şekilde yazılmalı. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if go test ./internal/api/ -run "$WANT" -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Budama mutasyonları =="

# ── Saklama kümesi: geri alma hedefi korunuyor mu ───────────────────

mutate "geri alma hedefi saklama kümesine HİÇ konmuyor" \
    "s=s.replace('\t\t\tkeep[prev] = \"geri alma hedefi\"','\t\t\t_ = prev',1)"

mutate "PreviousActiveRelease hatası SESSİZCE yutuluyor" \
    "s=s.replace('\tdefault:\n\t\treturn nil, err\n\t}\n\treturn keep, nil','\tdefault:\n\t\t_ = err\n\t}\n\treturn keep, nil',1)"

# ⚠ ÇIKARILDI: "saklama kümesi yalnızca aktif sürümü tutuyor".
#
# Mutasyon erken bir return ekliyordu; `prev` kullanılmadan kaldığı
# için DERLEYİCİ hata veriyordu ve `go test` sıfırdan farklı
# dönüyordu. Betik bunu "yakalandı" sayıyordu ama yakalayan test
# değil DERLEYİCİydı — yani ölçüm hiçbir şey söylemiyordu.
# Yanlış sebeple kırmızı, yanlış sebeple yeşil kadar değersizdir.
#
# Aynı değişmezi yukarıdaki "geri alma hedefi saklama kümesine HİÇ
# konmuyor" mutasyonu zaten ÖLÇÜYOR ve o derlenebilir kod üretiyor.

# ── Saklama kümesi: budamada gerçekten UYGULANIYOR mu ───────────────

mutate "korunan sürümler budama listesinden elenmiyor" \
    "s=s.replace('\t\tif _, ok := keep[r.ReleaseID]; ok {\n\t\t\tcontinue\n\t\t}\n','',1)"

# ── Fail-closed ─────────────────────────────────────────────────────

mutate "aktif sürüm okunamayınca BOŞ küme dönüyor (hepsini siler)" \
    "s=s.replace('\tactive, err := d.ActiveDeployment(ctx, appID)\n\tif err != nil {\n\t\treturn nil, err\n\t}','\tactive, err := d.ActiveDeployment(ctx, appID)\n\tif err != nil {\n\t\treturn map[string]string{}, nil\n\t}',1)"

mutate "boş app_id kabul ediliyor (hepsini budar)" \
    "s=s.replace('\tif !appIDPattern.MatchString(appID) {','\tif false {',1)"

# ── Deneme modu ─────────────────────────────────────────────────────

mutate "dry_run yok sayılıyor (deneme GERÇEKTEN siliyor)" \
    "s=s.replace('\tif !dry {','\tif true {',1)"

# ── Kapanma nezaketi ────────────────────────────────────────────────

mutate "durdurma atlanıyor (force=true ile koparma)" \
    "s=s.replace('\t\tif _, err := s.exec.StopRelease(ctx, appID, relID, pruneGrace); err != nil {\n\t\t\treturn removed, fmt.Errorf(\"sürüm %s durdurulamadı: %w\", relID, err)\n\t\t}\n','',1)"

mutate "pruneGrace sıfır (SIGTERM ile SIGKILL arasında süre yok)" \
    "s=s.replace('const pruneGrace = 10 * time.Second','const pruneGrace = 0 * time.Second',1)"

# ── NE YAPILMADIĞI söyleniyor mu (K-088) ────────────────────────────

mutate "imajlara dokunulmadığı SÖYLENMİYOR" \
    "s=s.replace('\t\tImagesUntouched:   true,','\t\tImagesUntouched:   false,',1)"

mutate "yetimlerin kapsam dışı olduğu SÖYLENMİYOR" \
    "s=s.replace('\t\tOrphansOutOfScope: true,','\t\tOrphansOutOfScope: false,',1)"

# ── Yanıtın kendisi ─────────────────────────────────────────────────

mutate "budanan sürümler yanıtta boş dönüyor" \
    "s=s.replace('\t\tPrunedReleases:    stale,','\t\tPrunedReleases:    nil,',1)"

mutate "korunan sürümler yanıtta boş dönüyor" \
    "s=s.replace('\t\tKeptReleases:      keptLabels(keep),','\t\tKeptReleases:      nil,',1)"

mutate "kaldırılan konteyner sayısı her zaman 0" \
    "s=s.replace('\t\tContainersRemoved: removed,','\t\tContainersRemoved: 0,',1)"

restore
echo
if [ "$fail" -ne 0 ]; then
    echo "BAŞARISIZ: en az bir mutasyon yakalanmadı ya da uygulanamadı."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
