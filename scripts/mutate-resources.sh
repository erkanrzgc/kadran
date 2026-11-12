#!/usr/bin/env bash
# Canlı kaynak kullanımı (K-166) testlerinin GERÇEKTEN bir şey koruduğunu
# sınar. Her mutasyon korunan bir özelliği KASTEN bozar; test kırmızıya
# dönmezse o özellik korunmuyor demektir.
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
#   - Sessiz yanlış sayı: veth ya da Docker köprüsü aynı ağ baytını, dm-
#     ya da bölüm aynı disk baytını İKİ KEZ sayar; guest sütunu CPU
#     toplamını şişirir. Ekran çalışır görünür, sayı yanlıştır.
#   - Sessiz sıfır: bellek bir kez böyle 0 raporlandı (ProcSubset=pid).
#     kadrand birimine o satır geri gelirse bellek yine 0 olur; executor
#     biriminden kalkarsa yürütücü bütün süreçleri görür.
#   - Executor düşünce sayfa düşer: "uygulamalar ölçülemedi" yerine RPC
#     hatası, host sayaçları da gider.
#
# Linux'a özgü statfs (disk alanı, inode) burada yok: Windows'ta derlenmez,
# CI'da read_test.go'nun linux dalı ölçüyor.
set -uo pipefail

cd "$(dirname "$0")/.."

FILES=(
    internal/hostmetrics/parse.go
    internal/hostmetrics/read.go
    internal/dockerdrv/stats.go
    internal/exec/stats.go
    internal/api/resources.go
    internal/api/server.go
    deploy/systemd/kadrand.service
    deploy/systemd/kadran-exec.service
)
BAKDIR=$(mktemp -d)
bak() { printf '%s/%s' "$BAKDIR" "${1//\//__}"; }
for f in "${FILES[@]}"; do cp "$f" "$(bak "$f")"; done
restore() { for f in "${FILES[@]}"; do cp "$(bak "$f")" "$f"; done; }
trap 'restore; rm -rf "$BAKDIR"' EXIT

fail=0

# ── Taban YEŞİL olmalı ───────────────────────────────────────────────
#
# Mutasyonsuz kodda düşen bir test her mutantı "yakalandı" gösterirdi.
# Her farklı test komutu, ilk mutantından önce bir kez mutasyonsuz
# kodda koşturuluyor.
declare -A TABAN=()
taban_yesil() {
    local key="$*"
    [[ -n "${TABAN[$key]:-}" ]] && return 0
    restore
    if ! go test "$1" -run "$2" -count=1 >/dev/null 2>&1; then
        echo "!! TABAN KIRMIZI: mutasyonsuz kodda 'go test $1 -run $2' düşüyor — ölçüm YAPILMADI"
        exit 1
    fi
    TABAN[$key]=1
}

# mutate <dosya> <paket> <test-deseni> <ad> <python-ifadesi>
mutate() {
    local file="$1" pkg="$2" want="$3" name="$4" expr="$5"
    restore
    taban_yesil "$pkg" "$want"
    if ! python -c "
import io,sys
class _S(str):
    def replace(self,a,b,*r):
        # TEK eşleşme şart: aranan metin bir yorumda da geçiyorsa
        # replace(…,1) İLKİNİ değiştirir ve kod mutasyonsuz ölçülür (K-127).
        n=self.count(a)
        if n!=1:
            sys.stderr.write('REPLACE '+str(n)+' KEZ ESLESTI (1 olmali): '+repr(a[:70])+chr(10))
            sys.exit(8)
        return _S(str.replace(self,a,b,*r))
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

    # ── MUTANT DERLENMELİ (K-096) ───────────────────────
    # `-run '^$'` paketi VE test dosyalarını derler, test koşmaz.
    local build_out
    if ! build_out=$(go test "$pkg" -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
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

HM=./internal/hostmetrics/
HMT='TestReadFromFixture|TestMemoryHelper|TestNetDev|TestStat|TestPressure|TestLoadavg|TestDiskstats'

echo "== Host sayaçları: çift sayım ve yanlış sütun =="
mutate internal/hostmetrics/parse.go "$HM" "$HMT" "guest sütunları CPU toplamına giriyor" \
    "s=s.replace('var v [8]uint64','var v [10]uint64',1)"
mutate internal/hostmetrics/parse.go "$HM" "$HMT" "boşta süresine iowait eklenmiyor" \
    "s=s.replace('out.idle, out.iowait, out.steal = v[3]+v[4], v[4], v[7]','out.idle, out.iowait, out.steal = v[3], v[4], v[7]',1)"
mutate internal/hostmetrics/parse.go "$HM" "$HMT" "çalınan süre yanlış sütundan" \
    "s=s.replace('out.idle, out.iowait, out.steal = v[3]+v[4], v[4], v[7]','out.idle, out.iowait, out.steal = v[3]+v[4], v[4], v[6]',1)"
mutate internal/hostmetrics/parse.go "$HM" "$HMT" "kB bayta çevrilmiyor" \
    "s=s.replace('n *= 1024','n *= 1',1)"
mutate internal/hostmetrics/parse.go "$HM" "$HMT" "konteynerin veth'i ağ toplamına giriyor" \
    "s=s.replace(' || strings.HasPrefix(name, \"veth\")','',1)"
mutate internal/hostmetrics/parse.go "$HM" "$HMT" "Docker köprüsü ağ toplamına giriyor" \
    "s=s.replace(' || strings.HasPrefix(name, \"docker\")','',1)"
mutate internal/hostmetrics/parse.go "$HM" "$HMT" "lo ağ toplamına giriyor" \
    "s=s.replace('return name == \"lo\" || ','return ',1)"
mutate internal/hostmetrics/parse.go "$HM" "$HMT" "gönderilen bayt yanlış sütundan" \
    "s=s.replace('strconv.ParseUint(fields[8], 10, 64)','strconv.ParseUint(fields[9], 10, 64)',1)"
mutate internal/hostmetrics/parse.go "$HM" "$HMT" "disk sektörü bayta çevrilmiyor" \
    "s=s.replace('read += rs * sectorBytes','read += rs',1)"
mutate internal/hostmetrics/parse.go "$HM" "$HMT" "baskı 'full' satırından okunuyor" \
    "s=s.replace('fields[0] != \"some\"','fields[0] != \"full\"',1)"
mutate internal/hostmetrics/read.go "$HM" "$HMT" "aygıt eşleyici (dm-) disk toplamına giriyor" \
    "s=s.replace('\"loop\", \"ram\", \"dm-\", ','\"loop\", \"ram\", ',1)"
mutate internal/hostmetrics/read.go "$HM" "$HMT" "loop aygıtı disk toplamına giriyor" \
    "s=s.replace('{\"loop\", \"ram\", ','{\"ram\", ',1)"
mutate internal/hostmetrics/read.go "$HM" "$HMT" "bölüm (vda1) diskiyle birlikte sayılıyor" \
    "s=s.replace('_, err := os.Stat(filepath.Join(r.Sys, \"block\", name))'+chr(10)+chr(9)+'return err == nil','return true',1)"
mutate internal/hostmetrics/read.go "$HM" "$HMT" "çekirdek sayısı /proc/stat'tan değil Go'dan" \
    "s=s.replace('if c.cores > 0 {','if false {',1)"
mutate internal/hostmetrics/read.go "$HM" "$HMT" "kullanılabilir bellek yerine boş bellek" \
    "s=s.replace('set(\"MemAvailable\", &h.MemoryAvailableBytes)','set(\"MemFree\", &h.MemoryAvailableBytes)',1)"
mutate internal/hostmetrics/read.go "$HM" "$HMT" "eksik bellek alanı tam sayılıyor" \
    "s=s.replace('return total, available, ok1 && ok2','return total, available, ok1 || ok2',1)"

echo "== Konteyner sayaçları (Docker, yürütücü) =="
mutate internal/dockerdrv/stats.go ./internal/dockerdrv/ 'TestStats' "sayfa önbelleği bellekten düşülmüyor" \
    "s=s.replace('u.MemoryBytes -= cache','u.MemoryBytes -= 0',1)"
mutate internal/dockerdrv/stats.go ./internal/dockerdrv/ 'TestStats' "önbellek kullanımdan büyükse bellek taşıyor" \
    "s=s.replace('; cache <= u.MemoryBytes {','; true {',1)"
mutate internal/dockerdrv/stats.go ./internal/dockerdrv/ 'TestStats' "cgroup v1'in 'Read' işlemi sayılmıyor" \
    "s=s.replace('switch strings.ToLower(e.Op) {','switch strings.TrimSpace(e.Op) {',1)"
mutate internal/dockerdrv/stats.go ./internal/dockerdrv/ 'TestStats' "ağ arayüzleri toplanmıyor" \
    "s=s.replace('u.RxBytes += n.Rx','u.RxBytes = n.Rx',1)"
mutate internal/dockerdrv/stats.go ./internal/dockerdrv/ 'TestStats' "akış açık kalıyor (tek seferlik değil)" \
    "s=s.replace('\"one-shot\": {\"true\"}','\"one-shot\": {\"false\"}',1)"
mutate internal/exec/stats.go ./internal/exec/ 'TestCollectStats|TestContainerStats' "durmuş konteyner ölçülüyor" \
    "s=s.replace('if c.State != \"running\" {','if false {',1)"
mutate internal/exec/stats.go ./internal/exec/ 'TestCollectStats|TestContainerStats' "tek konteynerin hatası bütün ölçümü düşürüyor" \
    "s=s.replace('if err != nil {\n\t\t\tcontinue','if err != nil {\n\t\t\treturn nil, err',1)"

echo "== kadrand: toplama ve executor düşünce =="
API=./internal/api/
APIT='TestResourceUsage|TestSystemInfoMemoryFromDaemon'
mutate internal/api/resources.go "$API" "$APIT" "executor düşünce RPC hatası (host sayaçları da gidiyor)" \
    "s=s.replace('apps_measured false kalıyor.\n\t\treturn resp, nil','apps_measured false kalıyor.\n\t\treturn nil, err',1)"
mutate internal/api/resources.go "$API" "$APIT" "executor düşünce 'ölçüldü' deniyor" \
    "s=s.replace('apps_measured false kalıyor.\n\t\treturn resp, nil','apps_measured false kalıyor.\n\t\tresp.AppsMeasured = true\n\t\treturn resp, nil',1)"
mutate internal/api/resources.go "$API" "$APIT" "uygulama istenmeden executor'a gidiliyor" \
    "s=s.replace('if !req.GetIncludeApps() {','if false {',1)"
mutate internal/api/resources.go "$API" "$APIT" "replika sayısı toplanmıyor" \
    "s=s.replace('a.Containers++','a.Containers = 1',1)"
mutate internal/api/resources.go "$API" "$APIT" "replikaların CPU'su toplanmıyor" \
    "s=s.replace('a.CpuUsageNs += c.GetCpuUsageNs()','a.CpuUsageNs = c.GetCpuUsageNs()',1)"
mutate internal/api/resources.go "$API" "$APIT" "uygulamalar ada göre sıralı değil" \
    "s=s.replace('strings.Compare(a.GetAppId(), b.GetAppId())','strings.Compare(b.GetAppId(), a.GetAppId())',1)"
mutate internal/api/server.go "$API" "$APIT" "durum ekranının belleği kadrand'den okunmuyor" \
    "s=s.replace('if total, available, ok := s.host.Memory(); ok {','if total, available, ok := s.host.Memory(); false && ok {',1)"

echo "== systemd: /proc görünürlüğü =="
mutate deploy/systemd/kadrand.service ./cmd/kadrand/ 'TestDaemonUnitSeesHostCounters|TestExecutorUnitKeepsProcSubset' "kadrand'e ProcSubset=pid geri geliyor (bellek yine 0)" \
    "s=s.replace('ProtectProc=invisible\n','ProtectProc=invisible\nProcSubset=pid\n',1)"
mutate deploy/systemd/kadran-exec.service ./cmd/kadrand/ 'TestDaemonUnitSeesHostCounters|TestExecutorUnitKeepsProcSubset' "executor'dan ProcSubset=pid kalkıyor" \
    "s=s.replace('ProcSubset=pid\n','',1)"

restore
echo
if [ "$fail" -ne 0 ]; then
    echo "BAŞARISIZ: en az bir mutasyon yakalanmadı ya da uygulanamadı."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
