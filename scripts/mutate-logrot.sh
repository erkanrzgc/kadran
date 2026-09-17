#!/usr/bin/env bash
# Günlük rotasyonu testlerinin GERÇEKTEN bir şey koruduğunu sınar.
#
# Her mutasyon korunan bir özelliği KASTEN bozar; test kırmızıya
# dönmezse o özellik korunmuyor demektir.
#
# ── Neden bu koruma pozitif olmak zorunda ───────────────────────────
#
# CapDrop/PidsLimit ile aynı sınıf: Docker'ın varsayılan günlük tavanı
# YOKTUR, yani alanı hiç yazmamak sınırsızlığı seçmek demek. Sürücüdeki
# öteki sertleştirmeler "alanı tanımlama" ile çalışıyor; bu çalışamaz.
#
# ── İki testi BİRBİRİNE BAĞLAYAN mutasyonlar ────────────────────────
#
# Mekanizma testi gövdeyi okuyor, sınır testi sabitleri. Bağ olmadan
# arada delik kalıyordu: gövdeye elle "0" yazan bir değişiklik "alan
# dolu" iddiasını geçer, sabitlere dokunmadığı için sabit denetimini de
# geçer, ve tavan sessizce kalkar. Aşağıdaki "gövdede sabit yerine ham
# değer" mutasyonları tam olarak o deliği ölçüyor.
set -uo pipefail

cd "$(dirname "$0")/.."
SRC=internal/dockerdrv/container.go
BAK=$(mktemp)
cp "$SRC" "$BAK"
restore() { cp "$BAK" "$SRC"; }
trap restore EXIT

fail=0
WANT='TestCreatePinsLogRotation|TestLogRotationIsBounded'

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
    if ! build_out=$(go test ./internal/dockerdrv/ -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI,"
        echo "     mutasyon derlenebilir olacak şekilde yazılmalı. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if go test ./internal/dockerdrv/ -run "$WANT" -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Günlük rotasyonu mutasyonları =="

# ── Mekanizma: gövde tavanı taşıyor mu ──────────────────────────────

mutate "LogConfig tamamen gönderilmiyor" \
    "s=s.replace('\tbody.HostConfig.LogConfig.Type = \"json-file\"\n\tbody.HostConfig.LogConfig.Config = map[string]string{\n\t\t\"max-size\": defaultLogMaxSize, \"max-file\": defaultLogMaxFiles,\n\t}\n','',1)"

mutate "yalnızca Type gönderiliyor, tavan yok" \
    "s=s.replace('\tbody.HostConfig.LogConfig.Config = map[string]string{\n\t\t\"max-size\": defaultLogMaxSize, \"max-file\": defaultLogMaxFiles,\n\t}\n','',1)"

mutate "yalnızca tavan gönderiliyor, sürücü sabitlenmiyor" \
    "s=s.replace('\tbody.HostConfig.LogConfig.Type = \"json-file\"\n','',1)"

mutate "sürücü journald (ContainerLogs okuyamaz, max-size geçersiz)" \
    "s=s.replace('LogConfig.Type = \"json-file\"','LogConfig.Type = \"journald\"',1)"

mutate "max-size anahtarı düşürüldü" \
    "s=s.replace('\"max-size\": defaultLogMaxSize, ','',1)"

mutate "max-file anahtarı düşürüldü" \
    "s=s.replace(', \"max-file\": defaultLogMaxFiles',1*'',1)"

# ── Bağ: gövde sabiti mi taşıyor, yoksa ham bir değer mi ────────────

mutate "gövdede sabit yerine ham \"0\" (tavan kalkar, alan dolu görünür)" \
    "s=s.replace('\"max-size\": defaultLogMaxSize','\"max-size\": \"0\"',1)"

mutate "gövdede sabit yerine ham \"1g\"" \
    "s=s.replace('\"max-size\": defaultLogMaxSize','\"max-size\": \"1g\"',1)"

mutate "gövdede max-file yerine ham \"0\"" \
    "s=s.replace('\"max-file\": defaultLogMaxFiles','\"max-file\": \"0\"',1)"

# ── Seçim: sabitlerin kendisi bir tavan üretiyor mu ─────────────────

mutate "max-size sabiti \"0\" (Docker'da SINIRSIZ)" \
    "s=s.replace('defaultLogMaxSize  = \"10m\"','defaultLogMaxSize  = \"0\"',1)"

mutate "max-size sabiti birimsiz \"10\" (Docker BAYT sayar)" \
    "s=s.replace('defaultLogMaxSize  = \"10m\"','defaultLogMaxSize  = \"10\"',1)"

mutate "max-size sabiti \"10g\" (tavan anlamsızlaşır)" \
    "s=s.replace('defaultLogMaxSize  = \"10m\"','defaultLogMaxSize  = \"10g\"',1)"

mutate "max-size sabiti \"500m\" (tek konteyner 1.5 GiB)" \
    "s=s.replace('defaultLogMaxSize  = \"10m\"','defaultLogMaxSize  = \"500m\"',1)"

mutate "max-file sabiti \"0\"" \
    "s=s.replace('defaultLogMaxFiles = \"3\"','defaultLogMaxFiles = \"0\"',1)"

mutate "max-file sabiti sayısal değil" \
    "s=s.replace('defaultLogMaxFiles = \"3\"','defaultLogMaxFiles = \"uc\"',1)"

mutate "max-file sabiti \"1000\" (dosya × boyut tavanı yutar)" \
    "s=s.replace('defaultLogMaxFiles = \"3\"','defaultLogMaxFiles = \"1000\"',1)"

restore
echo
if [ "$fail" -ne 0 ]; then
    echo "BAŞARISIZ: en az bir mutasyon yakalanmadı ya da uygulanamadı."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
