#!/usr/bin/env bash
# Alan adı önkontrolünü koruyan testlerin GERÇEKTEN bir şey koruduğunu
# sınar (K-128: önkontrol ve `kadran domain check`).
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
# İki yön de pahalı:
#   - Gevşeyen denetim: yanlış DNS'le kaydedilen alan adı sertifika alamaz
#     ve Caddy yeniden denemeden önce 1 güne kadar bekleyebilir.
#   - Sıkılaşan denetim: doğru bir kurulumu (bilinmeyen IPv6, Tailscale
#     arkası sunucu, `.localhost`, dalgalı DNS) REDDEDER. Canlıdaki
#     `.localhost` rotaları güncellenemez olur.
#
# Düzenek mutate-bootstrapssh.sh'tan: tek eşleşme şartı (K-127), yeşil
# taban şartı, derleme kapısı (K-096).
set -uo pipefail

cd "$(dirname "$0")/.."
DC=internal/domaincheck/domaincheck.go
CLI=cmd/kadran/domaincheck.go
APP=cmd/kadran/app.go
UPD=cmd/kadran/appupdate.go
DOM=cmd/kadran/domain.go
JUDGE=internal/domaincheck/judge.go
PROV=internal/domaincheck/providers.go
ZONE=internal/domaincheck/zone.go
CF=internal/domaincheck/cloudflare.go
AUTH=internal/domaincheck/authoritative.go
RDAP=internal/domaincheck/rdap.go
GUIDE=cmd/kadran/domainguide.go
PKGS=(./internal/domaincheck/ ./cmd/kadran/)
TESTS='TestDomainCheck|TestCheck|TestALookupError|TestWrappedNotFound|StopsOnWrongDNS|SkipFlag|MatchingDNS|RemovingTheDomain|LocalhostDomain|DNSWarning|SSHAlias|ParseSSHHostname|Provider|AWSPattern|CleanName|CommandFor|FindZone|GuessRecordName|Cloudflare|AskAuthoritative|DefaultExchange|CheckerRules|CheckerBudget|DNSHost|OnlyTheCache|ProxiedCloudflare|StaysOffline|Registrar|WaitsUntil|WaitGivesUp|WriteDNSGuide|ShowsTheDNSHost|SplitOrLookalike|DnsWaitInterval'
FILES=("$DC" "$CLI" "$APP" "$UPD" "$DOM" "$JUDGE" "$PROV" "$ZONE" "$CF" "$AUTH" "$RDAP" "$GUIDE")

# ⚠ Yedek adı TAM YOLDAN türetiliyor: internal/domaincheck/domaincheck.go
# ile cmd/kadran/domaincheck.go'nun dosya adı aynı. Betiğin ilk sürümü
# yedeği dosya adıyla tutuyordu; ikincisi birincinin yedeğini ezdi ve
# geri yükleme paket dosyasını CLI dosyasıyla DEĞİŞTİRDİ.
BAKDIR=$(mktemp -d)
bak() { printf '%s/%s' "$BAKDIR" "${1//\//__}"; }
for f in "${FILES[@]}"; do cp "$f" "$(bak "$f")"; done
restore() { for f in "${FILES[@]}"; do cp "$(bak "$f")" "$f"; done; }
trap restore EXIT

fail=0

# mutate_in <dosya> <ad> <python-ifadesi>
mutate_in() {
    local file="$1" name="$2" expr="$3"
    restore
    if ! python -c "
import io,sys
class _S(str):
    def replace(self,a,b,*r):
        # TEK eşleşme şart: aranan metin bir yorumda da geçiyorsa
        # replace(…,1) İLKİNİ, yani yorumu değiştirir ve kod hiç mutasyona
        # uğramadan ölçülür (K-127).
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

    # ── MUTANT DERLENMELİ ───────────────────────────────
    #
    # Derlenmeyen bir mutant `go test`'i düşürür ve betik bunu
    # "yakalandı" diye okur (K-096).
    local build_out
    if ! build_out=$(go test "${PKGS[@]}" -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI,"
        echo "     mutasyon derlenebilir olacak şekilde yazılmalı. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if go test "${PKGS[@]}" -run "$TESTS" -count=1 -timeout 90s >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

# Taban YEŞİL olmalı: kırmızı bir taban her mutantı "yakalandı" gösterirdi.
if ! go test "${PKGS[@]}" -run "$TESTS" -count=1 -timeout 90s >/dev/null 2>&1; then
    echo "!! TABAN KIRMIZI: mutasyonsuz kodda testler düşüyor — ölçüm YAPILMADI"
    exit 1
fi

echo "== Karar kuralları =="

mutate_in "$JUDGE" "yanlış kayıt yalnızca uyarıyor" \
    "s=s.replace('rep.raise(Stop, \"the %s record points at','rep.raise(Warn, \"the %s record points at',1)"

# Sunucunun o ailedeki adresi bilinmeden durmak doğru bir AAAA'yı reddeder.
mutate_in "$JUDGE" "bilinmeyen aile durduruyor" \
    "s=s.replace('rep.raise(Warn, \"there is a %s record (%s) but the server','rep.raise(Stop, \"there is a %s record (%s) but the server',1)"

mutate_in "$JUDGE" "AAAA denetlenmiyor" \
    "s=s.replace('\tcheckFamily(rep, \"AAAA\", \"IPv6\", aaaa, family(rep.Server, false))\n','',1)"

# Tek ailenin yokluğu (yalnız A kaydı olan ad) "kayıt yok" değil.
mutate_in "$DC" "tek aile yoksa kayıt yok sayılıyor" \
    "s=s.replace('judge(&rep, a, aaaa, aNotFound && aaaaNotFound, serverHost)','judge(&rep, a, aaaa, aNotFound || aaaaNotFound, serverHost)',1)"

# Bozuk sorgu kaydı gizliyor olabilir; "yok" diye okunmamalı.
mutate_in "$DC" "bulunamadı ile sorgu hatası yer değiştirdi" \
    "s=s.replace('if errors.As(err, &de) && de.IsNotFound {','if errors.As(err, &de) && !de.IsNotFound {',1)"

echo "== Adres sınıfları =="

mutate_in "$DC" "CGNAT/Tailscale genel adres sayılıyor" \
    "s=s.replace(' && !cgnat.Contains(a)','',1)"

mutate_in "$DC" "IPv4 eşlemeli IPv6 çözülmüyor" \
    "s=s.replace('addrs = append(addrs, a.Unmap())','addrs = append(addrs, a)',1)"

mutate_in "$DC" ".localhost muafiyeti yok" \
    "s=s.replace('if domain == \"localhost\" || strings.HasSuffix(domain, \".localhost\") {','if false {',1)"

echo "== K-151 kuralları =="

mutate_in "$JUDGE" "karışık küme geçiyor" \
    "s=s.replace('if stray := without(records, server); len(stray) > 0 {','if stray := without(records, server); len(stray) > 99 {',1)"

mutate_in "$DC" "yetkili cevap yok sayılıyor" \
    "s=s.replace('if rep.Auth.Answered && rep.Auth.Agree {','if false {',1)"

mutate_in "$DC" "NS tutarsızlığı uyarmıyor" \
    "s=s.replace('if rep.Auth.Answered && rep.Verdict < Stop {','if false {',1)"

mutate_in "$JUDGE" "CNAME izlenmiyor" \
    "s=s.replace('if len(addrs) == 0 && rep.Auth.CNAME != \"\" {','if false {',1)"

mutate_in "$JUDGE" "Cloudflare metni NS'ye bakmıyor" \
    "s=s.replace('if rep.HasProvider && rep.Provider.Name == providerCloudflare {','if true {',1)"

mutate_in "$CF" "Cloudflare aralığı tanınmıyor" \
    "s=s.replace('\t\tif p.Contains(a) {','\t\tif false && p.Contains(a) {',1)"

mutate_in "$AUTH" "AA bayrağı aranmıyor" \
    "s=s.replace(' || h.ID != id || !h.Authoritative {',' || h.ID != id {',1)"

mutate_in "$AUTH" "sorgu kimliği aranmıyor" \
    "s=s.replace(' || h.ID != id || !h.Authoritative {',' || !h.Authoritative {',1)"

mutate_in "$AUTH" "NS'ler tutarsızken Agree kalıyor" \
    "s=s.replace('\t\t\tout.Agree = false\n','',1)"

mutate_in "$DC" "süre bütçesi uygulanmıyor" \
    "s=s.replace('ctx, cancel := context.WithTimeout(ctx, budget)','ctx, cancel := context.WithCancel(ctx); _ = budget',1)"

mutate_in "$PROV" "son ek etiket sınırını aşıyor" \
    "s=s.replace('strings.HasSuffix(ns, \".\"+suf)','strings.HasSuffix(ns, suf)',1)"

mutate_in "$PROV" "NS adı temizlenmiyor" \
    "s=s.replace(\"if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {\",'if true {',1)"

mutate_in "$ZONE" "bölge genel son eke tırmanıyor" \
    "s=s.replace('\t\tif name == top {\n\t\t\treturn Zone{}, false\n\t\t}\n','\t\t_ = top\n',1)"

# A1: LookupNS CNAME izler; takma ad olan ad bölge sayılmamalı.
mutate_in "$ZONE" "CNAME adı bölge sayılıyor" \
    "s=s.replace('!isAlias(ctx, r, name)','true',1)"

# C1: yazdırılan kabuk komutuna güvensiz girdi girmemeli.
mutate_in "$PROV" "komut girdisi doğrulanmıyor" \
    "s=s.replace('p.Command == \"\" || !commandSafe(zone, r) {','p.Command == \"\" || false {',1)"

mutate_in "$AUTH" "cevap bayrağı aranmıyor" \
    "s=s.replace('err != nil || !h.Response || h.ID != id','err != nil || h.ID != id',1)"

mutate_in "$AUTH" "soru eşleşmesi aranmıyor" \
    "s=s.replace('got.Type != q.Type || !strings.EqualFold','got.Type != q.Type && !strings.EqualFold',1)"

# N1: düşmanca bölge yüzlerce NS ve özel adres döndürebilir.
mutate_in "$ZONE" "NS sayısı sınırsız" \
    "s=s.replace('if len(out) > maxNameServers {','if len(out) > 99999 {',1)"

mutate_in "$AUTH" "özel NS adresi sorgulanıyor" \
    "s=s.replace('if isPublic(a) {','if true {',1)"

mutate_in "$RDAP" "https olmayan yönlendirme izleniyor" \
    "s=s.replace('if req.URL.Scheme != \"https\" {','if false {',1)"

mutate_in "$RDAP" "RDAP gövdesi sınırsız" \
    "s=s.replace('io.LimitReader(resp.Body, rdapMaxBody)','io.Reader(resp.Body)',1)"

mutate_in "$GUIDE" "proxy'li kayıtta kayıt ekleme öneriliyor" \
    "s=s.replace('\tif rep.Proxied {\n','\tif false {\n',1)"

mutate_in "$DOM" "-wait beklemiyor" \
    "s=s.replace('\t\tif rep.Verdict != domaincheck.Stop {\n\t\t\treturn true\n\t\t}\n','\t\treturn true\n',1)"

# T8a: kayıt otoritesi port/HTTP/TLS'ten önce sorulursa takılan sunucu süreyi yer.
mutate_in "$DOM" "kayıt firması sorgusu süreyi yiyor" \
    "s=s.replace('\tc.probePorts(ctx, r, domain)\n','\tc.printRegistrar(ctx, r, rep)\n\tc.probePorts(ctx, r, domain)\n',1)"

mutate_in "$DOM" "DNS'te olmayan ad için RDAP soruluyor" \
    "s=s.replace('if rep.Verdict == domaincheck.Skipped {','if false {',1)"

# W2
mutate_in "$DOM" "negatif -wait kabul ediliyor" \
    "s=s.replace('if *wait < 0 {','if false {',1)"

mutate_in "$DOM" "iptal sonrası denetim ölü bağlamla sürüyor" \
    "s=s.replace('!c.waitForDNS(ctx, domain, fs.Arg(1), *wait) && ctx.Err() != nil {','!c.waitForDNS(ctx, domain, fs.Arg(1), *wait) && false {',1)"

mutate_in "$DOM" "süre dolunca son rapor basılmıyor" \
    "s=s.replace('!c.waitForDNS(ctx, domain, fs.Arg(1), *wait) && ctx.Err() != nil {','!c.waitForDNS(ctx, domain, fs.Arg(1), *wait) {',1)"

mutate_in "$DOM" "bekleme satırı her turda basılıyor" \
    "s=s.replace('now != last {','now != last || true {',1)"

mutate_in "$DOM" "bekleme aralığı 15 sn değil" \
    "s=s.replace('const dnsWaitInterval = 15 * time.Second','const dnsWaitInterval = 1 * time.Second',1)"

echo "== CLI bağlantısı =="

mutate_in "$CLI" "-skip-dns-check yok sayılıyor" \
    "s=s.replace('\tif skip {','\tif false {',1)"

mutate_in "$CLI" "ssh takma adı çözülmüyor" \
    "s=s.replace('return c.resolveSSHHost(ctx, target.SSHHost), true','return target.SSHHost, true',1)"

# Rapor stdout'a giderse -json çıktısı bozulur.
mutate_in "$CLI" "uyarı stdout'a yazılıyor" \
    "s=s.replace('fmt.Fprintf(c.stderr, progName+\": warning: %s','fmt.Fprintf(c.stdout, progName+\": warning: %s',1)"

# Boş ad (vekilden çıkarma) denetlenirse kaldırma imkânsızlaşır.
mutate_in "$CLI" "boş alan adı da denetleniyor" \
    "s=s.replace('\tif domain == \"\" {\n\t\treturn nil\n\t}\n','',1)"

mutate_in "$APP" "app create denetlemiyor" \
    "s=s.replace('\tif err := c.checkDomain(ctx, *domain, fs.Arg(1), *skipDNS); err != nil {\n\t\treturn c.fail(err)\n\t}\n','\t_ = skipDNS\n',1)"

mutate_in "$UPD" "app update denetlemiyor" \
    "s=s.replace('\tif req.Domain != nil {\n\t\tif err := c.checkDomain','\tif req.Domain != nil && false {\n\t\tif err := c.checkDomain',1)"

echo "== kadran domain check =="

# Sorun bulunsa da çıkış 0 olursa betikler ve CI tanıyı göremez.
mutate_in "$DOM" "sorun çıkış koduna yansımıyor" \
    "s=s.replace('\tr.failed = true\n','',1)"

mutate_in "$DOM" "sertifika doğrulanmadan kabul ediliyor" \
    "s=s.replace('leaf, err := c.tlsLeaf(ctx, domain, false)','leaf, err := c.tlsLeaf(ctx, domain, true)',1)"

mutate_in "$DOM" "bitmek üzere olan sertifika fark edilmiyor" \
    "s=s.replace('if days < certExpiryFailDays {','if days < -1 {',1)"

# Kapalı 80 (K-121'deki GCP güvenlik duvarı) yalnızca uyarı olurdu.
mutate_in "$DOM" "kapalı port yalnızca uyarıyor" \
    "s=s.replace('r.fail(p.port+\"/tcp\"','r.warn(p.port+\"/tcp\"',1)"

# Yanlış DNS'te sonraki ✓ satırları başka sunucuyu ölçüyor; söylenmeli.
mutate_in "$DOM" "başka sunucu ölçüldüğü söylenmiyor" \
    "s=s.replace('if rep.Verdict == domaincheck.Stop {','if rep.Verdict == domaincheck.Verdict(99) {',1)"

restore
# Geri yükleme kanıtlanıyor: her dosya kendi yedeğiyle bayt bayt aynı ve
# hiçbir yedek bir başkasının yerine geçmemiş (yukarıdaki ad çakışması).
for f in "${FILES[@]}"; do
    if ! cmp -s "$f" "$(bak "$f")"; then
        echo "!! GERİ YÜKLEME BOZUK: $f yedeğiyle aynı değil"
        exit 1
    fi
done
if [[ $(for f in "${FILES[@]}"; do sha256sum < "$f"; done | sort -u | wc -l) -ne ${#FILES[@]} ]]; then
    echo "!! İki dosya aynı içeriğe sahip: yedekler birbirine karışmış olabilir"
    exit 1
fi

if [[ $fail -ne 0 ]]; then
    echo
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi

echo
echo "Bütün mutasyonlar yakalandı."
