#!/usr/bin/env bash
# Alan adı önkontrolünü koruyan testlerin GERÇEKTEN bir şey koruduğunu
# sınar (K-128: önkontrol ve `panely domain check`).
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
CLI=cmd/panely/domaincheck.go
APP=cmd/panely/app.go
UPD=cmd/panely/appupdate.go
DOM=cmd/panely/domain.go
PKGS=(./internal/domaincheck/ ./cmd/panely/)
TESTS='TestDomainCheck|TestCheck|TestALookupError|TestWrappedNotFound|StopsOnWrongDNS|SkipFlag|MatchingDNS|RemovingTheDomain|LocalhostDomain|DNSWarning|SSHAlias|ParseSSHHostname'
FILES=("$DC" "$CLI" "$APP" "$UPD" "$DOM")

# ⚠ Yedek adı TAM YOLDAN türetiliyor: internal/domaincheck/domaincheck.go
# ile cmd/panely/domaincheck.go'nun dosya adı aynı. Betiğin ilk sürümü
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

mutate_in "$DC" "yanlış kayıt yalnızca uyarıyor" \
    "s=s.replace('rep.raise(Stop, \"%s kaydı %s gösteriyor','rep.raise(Warn, \"%s kaydı %s gösteriyor',1)"

# Sunucunun o ailedeki adresi bilinmeden durmak doğru bir AAAA'yı reddeder.
mutate_in "$DC" "bilinmeyen aile durduruyor" \
    "s=s.replace('rep.raise(Warn, \"%s kaydı var (%s) ama sunucunun','rep.raise(Stop, \"%s kaydı var (%s) ama sunucunun',1)"

mutate_in "$DC" "AAAA denetlenmiyor" \
    "s=s.replace('\tcheckFamily(&rep, \"AAAA\", \"IPv6\", aaaa, family(rep.Server, false))\n','',1)"

# Tek ailenin yokluğu (yalnız A kaydı olan ad) "kayıt yok" değil.
mutate_in "$DC" "tek aile yoksa kayıt yok sayılıyor" \
    "s=s.replace('if aNotFound && aaaaNotFound {','if aNotFound || aaaaNotFound {',1)"

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

echo "== CLI bağlantısı =="

mutate_in "$CLI" "-skip-dns-check yok sayılıyor" \
    "s=s.replace('\tif skip {','\tif false {',1)"

mutate_in "$CLI" "ssh takma adı çözülmüyor" \
    "s=s.replace('return c.resolveSSHHost(ctx, target.SSHHost), true','return target.SSHHost, true',1)"

# Rapor stdout'a giderse -json çıktısı bozulur.
mutate_in "$CLI" "uyarı stdout'a yazılıyor" \
    "s=s.replace('fmt.Fprintf(c.stderr, \"panely: uyarı: %s','fmt.Fprintf(c.stdout, \"panely: uyarı: %s',1)"

# Boş ad (vekilden çıkarma) denetlenirse kaldırma imkânsızlaşır.
mutate_in "$CLI" "boş alan adı da denetleniyor" \
    "s=s.replace('\tif domain == \"\" {\n\t\treturn nil\n\t}\n','',1)"

mutate_in "$APP" "app create denetlemiyor" \
    "s=s.replace('\tif err := c.checkDomain(ctx, *domain, fs.Arg(1), *skipDNS); err != nil {\n\t\treturn c.fail(err)\n\t}\n','\t_ = skipDNS\n',1)"

mutate_in "$UPD" "app update denetlemiyor" \
    "s=s.replace('\tif req.Domain != nil {\n\t\tif err := c.checkDomain','\tif req.Domain != nil && false {\n\t\tif err := c.checkDomain',1)"

echo "== panely domain check =="

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
    "s=s.replace('fs.Arg(1)) == domaincheck.Stop {','fs.Arg(1)) == domaincheck.Verdict(99) {',1)"

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
