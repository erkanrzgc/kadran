#!/usr/bin/env bash
# Ortak sunucu listesini (K-162, internal/servers ve `-s <ad>`) koruyan
# testlerin GERÇEKTEN bir şey koruduğunu sınar.
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
# Yanlış sunucuya gitmek: ad büyük/küçük harf ya da önekle eşleşirse,
# `-s` ile birlikte yazılan hedef ya da eksik bir uygulama adı yüzünden
# kayıtlı hedef kayarsa komut başka bir sunucuya (ya da yerel sokete)
# gider. Kök yolu: bootstrap kayıttaki yönetici yerine istemci hedefini
# alırsa ya da -sudo düşerse. Dosya: elle düzenlenmiş `-oProxyCommand=…`
# hedefi ssh'a ulaşırsa yerel komut çalışır; daha yeni bir sürümün dosyası
# ezilirse kullanıcının listesi bozulur.
#
# Düzenek mutate-release.sh'tan: tek eşleşme şartı (K-127), yeşil taban
# şartı, derleme kapısı (K-096), tam yoldan yedek.
set -uo pipefail

cd "$(dirname "$0")/.."
SRV=internal/servers/servers.go
FIL=internal/servers/file.go
FLG=cmd/kadran/serverflag.go
STA=cmd/kadran/status.go
MAIN=cmd/kadran/main.go
KEY=cmd/kadran/key.go
TOT=cmd/kadran/totp.go
DOM=cmd/kadran/domain.go
SCS=cmd/kadran/sidecar_servers.go
FILES=("$SRV" "$FIL" "$FLG" "$STA" "$MAIN" "$KEY" "$TOT" "$DOM" "$SCS")

BAKDIR=$(mktemp -d)
bak() { printf '%s/%s' "$BAKDIR" "${1//\//__}"; }
for f in "${FILES[@]}"; do cp "$f" "$(bak "$f")"; done
restore() { for f in "${FILES[@]}"; do cp "$(bak "$f")" "$f"; done; }
trap restore EXIT

fail=0

olc() {
    go test -count=1 -timeout 120s ./internal/servers ./cmd/kadran >/dev/null 2>&1
}

# mutate_in <dosya> <ad> <python-ifadesi>
mutate_in() {
    local file="$1" name="$2" expr="$3"
    restore
    if ! python -c "
import io,sys
class _S(str):
    def replace(self,a,b,*r):
        # TEK eşleşme şart: aranan metin bir yorumda da geçiyorsa
        # replace(…,1) İLKİNİ, yani yorumu değiştirir (K-127).
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
    # Derlenmeyen bir mutant her testi düşürür ve sahte "yakalandı" üretirdi.
    local build_out
    if ! build_out=$(go vet ./internal/servers ./cmd/kadran 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI. Çıktı:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if olc; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

# Taban YEŞİL olmalı: kırmızı bir taban her mutantı "yakalandı" gösterirdi.
if ! olc; then
    echo "!! TABAN KIRMIZI: mutasyonsuz kodda testler düşüyor — ölçüm YAPILMADI"
    exit 1
fi

echo "== Ad araması =="

mutate_in "$SRV" "ad büyük/küçük harfe bakmadan eşleşiyor" \
    "s=s.replace('if s.Name == name {','if strings.EqualFold(s.Name, name) {',1)"

mutate_in "$SRV" "ad önekle eşleşiyor" \
    "s=s.replace('if s.Name == name {','if strings.HasPrefix(s.Name, name) {',1)"

mutate_in "$SRV" "benzersizlik büyük/küçük harfe bakıyor" \
    "s=s.replace('if strings.EqualFold(other.Name, s.Name) {','if other.Name == s.Name {',1)"

mutate_in "$SRV" "ad '-' ile başlayabiliyor" \
    "s=s.replace('case strings.HasPrefix(n, \"-\"):','case false && strings.HasPrefix(n, \"-\"):',1)"

mutate_in "$SRV" "görünmez karakterli ad kabul ediliyor (ekranda aynı iki ad)" \
    "s=s.replace('return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)','return unicode.IsControl(r)',1)"

mutate_in "$SRV" "sağlayıcı eklemede kırpılmıyor" \
    "s=s.replace('s.Provider = strings.TrimSpace(s.Provider)','',1)"

echo "== -s çözümü =="

mutate_in "$STA" "status ikinci konumsal hedefi kabul ediyor" \
    "s=s.replace('if fs.NArg() > 1 {','if fs.NArg() > 99 {',1)"

mutate_in "$FLG" "ikinci -s kabul ediliyor, sonuncusu geçiyor" \
    "s=s.replace('if found {','if false && found {',1)"

mutate_in "$MAIN" "eksik argümanda kayan hedefle bağlanılıyor (connect kilidi)" \
    "s=s.replace('if err := c.checkServerTarget(rawTarget); err != nil {','if err := c.checkServerTarget(c.serverTarget); err != nil {',1)"

mutate_in "$FLG" "ikinci kilit hiçbir hedefi reddetmiyor" \
    "s=s.replace('if c.serverName == \"\" || raw == c.serverTarget {','if true {',1)"

mutate_in "$DOM" "domain check alan adı unutulunca kayıtlı sunucuyu yokluyor" \
    "s=s.replace('if err := c.checkServerTarget(fs.Arg(1)); err != nil {','if err := c.checkServerTarget(c.serverTarget); err != nil {',1)"

echo "== İpuçları (kopyalanıp kabuğa yapıştırılır) =="

mutate_in "$FLG" "boşluklu ad tırnaksız yazılıyor" \
    "s=s.replace('case quotable:','case quotable && false:',1)"

mutate_in "$FLG" "tırnakta da genişleyen ad (\$) çalıştırılacak komuta yazılıyor" \
    "s=s.replace('plain, quotable = false, false','plain = false',1)"

mutate_in "$MAIN" "oturum ipucu kabukta genişleyen adı yazıyor" \
    "s=s.replace('if name, ok := shellName(c.serverName); ok {','if name, _ := shellName(c.serverName); true {',1)"

mutate_in "$MAIN" "fazladan hedefte -s notu yok" \
    "s=s.replace('\tif c.serverName != \"\" {\n\t\tshown, _ := shellName','\tif false {\n\t\tshown, _ := shellName',1)"

mutate_in "$KEY" "-s notu ilgisiz kullanım hatasında da çıkıyor" \
    "s=s.replace('return c.usageError(\"unknown key subcommand','return c.argCountError(\"unknown key subcommand',1)"

mutate_in "$MAIN" "komut yardımı -s'yi söylemiyor" \
    "s=s.replace('if !noTargetCommands[command] {','if !noTargetCommands[command] && false {',1)"

echo "== Masaüstünün listesi (yan süreç) =="

# Kilitsiz ölçüldü: 10 koşunun 10'unda eklemelerin bir kısmı kayboldu.
mutate_in "$SCS" "eşzamanlı eklemeler birbirini siliyor (kilit yok)" \
    "s=s.replace('defer s.serversMu.Unlock()','',1); s=s.replace('s.serversMu.Lock()','',1)"

echo "== Kök yolu =="

# Kök yolunda sayı denetimi ve ikinci kilit üst üste: biri tek başına
# bozulunca öbürü tutar. Kilidi doğrudan çağrıyla sınayan test
# (TestAdminGuardRefusesOtherTarget) her birini ayrı ölçer.
mutate_in "$MAIN" "bootstrap kayıtlı olmayan makinede root olarak çalışıyor" \
    "s=s.replace('c.checkServerTarget(fs.Arg(0))','c.checkServerTarget(c.serverTarget)',1)"

mutate_in "$KEY" "key kayıtlı olmayan makinede çalışıyor" \
    "s=s.replace('if err := c.checkServerTarget(raw); err != nil {','if err := c.checkServerTarget(c.serverTarget); err != nil {',1)"

mutate_in "$TOT" "totp reset kayıtlı olmayan makinede çalışıyor" \
    "s=s.replace('c.checkServerTarget(fs.Arg(0))','c.checkServerTarget(c.serverTarget)',1)"

mutate_in "$FLG" "bootstrap yönetici yerine istemci hedefini alıyor" \
    "s=s.replace('return append(out, srv.Admin), name, nil','return append(out, srv.Target), name, nil',1)"

mutate_in "$FLG" "sudo'lu kayıtta -sudo eklenmiyor" \
    "s=s.replace('if srv.Sudo {','if false && srv.Sudo {',1)"

echo "== Göç (bir kez koşar: kaybolan kart geri gelmez) =="

mutate_in "$SRV" "alınamayan profil sessizce düşüyor" \
    "s=s.replace('skipped = append(skipped, Skipped{Target: p.Target, Reason: err.Error()})','_ = err',1)"

mutate_in "$SRV" "eski masaüstünün bağlandığı boşluklu hedef atlanıyor" \
    "s=s.replace('target := strings.TrimSpace(p.Target)','target := p.Target',1)"

echo "== Dosya =="

mutate_in "$FIL" "okunan kayıtlar denetlenmiyor (-oProxyCommand ssh'a gider)" \
    "s=s.replace('if err := Validate(body.Servers); err != nil {','if err := Validate(nil); err != nil {',1)"

mutate_in "$FIL" "daha yeni sürümün dosyası eziliyor" \
    "s=s.replace('if version > FileVersion {','if version > 99 {',1)"

# İzin bitleri Windows'ta yok: test orada atlanır, mutant sağ kalırdı.
if [[ "$(go env GOOS)" == "windows" ]]; then
    echo "  atlandı (Windows'ta izin bitleri yok, CI Linux'ta ölçer): geçici dosya 0644"
else
    mutate_in "$FIL" "geçici dosya 0644 (başkası okur)" \
        "s=s.replace('tmp.Chmod(0o600)','tmp.Chmod(0o644)',1)"
fi

echo
if [[ "$fail" -ne 0 ]]; then
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
