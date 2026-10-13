#!/usr/bin/env bash
# Tipli vekil aracısının (internal/proxyguard + proxydrv.FixedConfig /
# SafeRoutes) POLİTİKASINI koruyan testlerin GERÇEKTEN bir şey koruduğunu
# sınar (B2).
#
# Neden ayrı bir betik? scripts/mutate-proxyadmin.sh aracıyı DEĞİL, Caddy
# admin istemcisinin (internal/proxydrv/client.go) geri okumasını bozuyor.
# Aracının asıl korumaları — sabit yapılandırma, rota/upstream sınırları,
# upstream adres havuzu, yinelenen anahtar, derinlik sınırı — o betikte hiç
# mutasyona uğramıyordu.
#
# Her mutasyon korunan bir özelliği KASTEN bozar; test kırmızıya dönmezse o
# özellik korunmuyor demektir. Düzenek mutate-proxywatch.sh'tan: tek eşleşme
# şartı (K-127), yeşil taban şartı, derleme kapısı (K-096).
#
# Windows'ta da koşar: bu testler unix soketi istemez (soket gerektiren
# proxyguard *_linux_test.go dosyaları burada derlenmez; onlar bu betiğin
# ölçtüğü şeyin dışında).
set -uo pipefail

cd "$(dirname "$0")/.."
RTS=internal/proxydrv/routes.go
GRD=internal/proxyguard/guard.go
PKGS="./internal/proxyguard/ ./internal/proxydrv/"

BAK_RTS=$(mktemp); BAK_GRD=$(mktemp)
cp "$RTS" "$BAK_RTS"; cp "$GRD" "$BAK_GRD"
restore() { cp "$BAK_RTS" "$RTS"; cp "$BAK_GRD" "$GRD"; }
trap restore EXIT

fail=0

# Taban YEŞİL olmalı: kırmızı bir taban her mutantı "yakalandı" gösterirdi.
restore
# shellcheck disable=SC2086
if ! go test $PKGS -count=1 >/dev/null 2>&1; then
    echo "!! TABAN KIRMIZI: mutasyonsuz kodda 'go test $PKGS' düşüyor — ölçüm YAPILMADI"
    exit 1
fi

# mutate <ad> <dosya> <python-ifadesi>
mutate() {
    local name="$1" file="$2" expr="$3"
    restore
    if ! python -c "
import io,sys
class _S(str):
    def replace(self,a,b,*r):
        # TEK eşleşme şart (K-127).
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

    # MUTANT DERLENMELİ (K-096): derlenmeyen mutant sahte "yakalandı" üretir.
    local build_out
    # shellcheck disable=SC2086
    if ! build_out=$(go test $PKGS -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    # shellcheck disable=SC2086
    if go test $PKGS -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Sabit yapılandırma (FixedConfig / SafeRoutes) =="

# Canlı geri okumada sabit alanların (admin, portlar, günlük, işleyiciler)
# kayması: ele geçmiş Caddy ya da başkasının yazdığı yapılandırma.
mutate "geri okuma sabit yapılandırmayla karşılaştırılmıyor" "$RTS" \
    "s=s.replace('\tif !reflect.DeepEqual(cfg, fixed) {','\tif false && !reflect.DeepEqual(cfg, fixed) {',1)"

mutate "admin dinleyicisi sabit değil (fd/3 yerine TCP :2019)" "$RTS" \
    "s=s.replace('Admin{Listen: \"fd/3\"','Admin{Listen: \":2019\"',1)"

mutate "rota dizisi zorunlu değil (null kabul)" "$RTS" \
    "s=s.replace('\tif set.Routes == nil {','\tif false {',1)"

mutate "rota sayısı sınırı yok" "$RTS" \
    "s=s.replace('\tif len(set.Routes) > MaxRoutes {','\tif false {',1)"

mutate "upstream sayısı sınırı ve boş küme denetimi yok" "$RTS" \
    "s=s.replace('\t\tif len(r.Upstreams) == 0 || len(r.Upstreams) > MaxRouteUpstreams {','\t\tif false {',1)"

mutate "yinelenen upstream kabul ediliyor" "$RTS" \
    "s=s.replace('\t\t\tif seen[dial.Dial] {','\t\t\tif false {',1)"

mutate "alan adı doğrulanmıyor (süzme yerine ham yazım)" "$RTS" \
    "s=s.replace('\t\tif !ValidDomain(r.Domain) {','\t\tif false {',1)"

mutate "alan adı IP sabiti olabilir" "$RTS" \
    "s=s.replace('\t_, err := netip.ParseAddr(domain)\n\treturn err != nil','\treturn true',1)"

echo "== Upstream adres havuzu =="

# Ele geçmiş kadrand'ın Caddy'yi host'a / dış ağa yönlendirmesi.
mutate "havuz denetimi yok (loopback, RFC1918, link-local kabul)" "$RTS" \
    "s=s.replace(' || !netip.MustParsePrefix(\"172.16.0.0/12\").Contains(addr) {',' {',1)"

mutate "Docker ağ adresi/ağ geçidi/yayın adresi kabul (I6)" "$RTS" \
    "s=s.replace('\t\t\tif isDockerNetworkInfrastructure(addr) {','\t\t\tif false {',1)"

mutate "ağ geçidi kuralı yalnız .0.0'ı kapsıyor (host .0.1 açık)" "$RTS" \
    "s=s.replace('return low == 0 || low == 1 || low == 0xFFFF','return low == 0 || low == 0xFFFF',1)"

mutate "ağ geçidi kuralı yalnız .0.1'i kapsıyor (yayın açık)" "$RTS" \
    "s=s.replace('return low == 0 || low == 1 || low == 0xFFFF','return low == 0 || low == 1',1)"

echo "== İstek kapısı (guard.go) =="

mutate "yinelenen JSON anahtarı kabul ediliyor" "$GRD" \
    "s=s.replace('if !ok || seen[name] {','if !ok {',1)"

mutate "JSON derinlik sınırı yok" "$GRD" \
    "s=s.replace('\t\tif depth > maxJSONDepth {','\t\tif false {',1)"

mutate "JSON'dan sonraki ikinci belge kabul ediliyor" "$GRD" \
    "s=s.replace('\tif _, err := d.Token(); err != io.EOF {','\tif _, err := d.Token(); false && err != io.EOF {',1)"

mutate "bilinmeyen alanlar kabul ediliyor" "$GRD" \
    "s=s.replace('\t\t\tdec.DisallowUnknownFields()\n','',1)"

mutate "gövde boyutu sınırsız" "$GRD" \
    "s=s.replace('io.ReadAll(http.MaxBytesReader(w, r.Body, proxydrv.MaxRouteRequest))','io.ReadAll(r.Body)',1)"

mutate "GET/POST dışındaki yöntemler kabul ediliyor" "$GRD" \
    "s=s.replace('\t\tif r.Method != http.MethodPost && r.Method != http.MethodGet {','\t\tif false {',1)"

mutate "ham yollar kabul ediliyor (yol/sorgu denetimi yok)" "$GRD" \
    "s=s.replace('\t\tif r.URL.Path != \"/v1/routes\" || r.URL.RawQuery != \"\" {','\t\tif false {',1)"

mutate "iptal edilen sıradaki istek yine de yükletiyor" "$GRD" \
    "s=s.replace('\t\tcase <-r.Context().Done():\n\t\t\tfail(w, http.StatusRequestTimeout, \"proxy request canceled\")\n\t\t\treturn\n','',1)"

mutate "yükleme sonrası güvenli olmayan canlı yapılandırma kabul" "$GRD" \
    "s=s.replace('\t\tset, err := proxydrv.SafeRoutes(cfg)\n\t\tif err != nil {','\t\tset, err := proxydrv.SafeRoutes(cfg)\n\t\tif false && err != nil {',1)"

restore
if [[ $fail -ne 0 ]]; then
    echo
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi

echo
echo "Bütün mutasyonlar yakalandı."
