#!/usr/bin/env bash
# Kurulumun ssh çağrısını koruyan testlerin GERÇEKTEN bir şey koruduğunu
# sınar (K-114).
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
# Parola istemi. BatchMode=yes düşerse ssh terminalde parola ister ve
# "parola ya da özel anahtar istenmez" iddiası sessizce delinir. Hemen
# arkasından argüman enjeksiyonu: `-` ile başlayan hedefi ssh seçenek
# sanar (`-oProxyCommand=…` iş istasyonunda komut çalıştırır).
#
# ── K-071/K-080'in dersi ────────────────────────────────────────────
#
# Yeşil kalan bir mutasyon İKİ zıt sonuç doğurabilir: test zayıftır ya da
# MUTASYON zayıftır. Bu yüzden betik, mutasyonun dosyaya uygulanıp
# uygulanmadığını da ayrıca doğruluyor.
set -uo pipefail

cd "$(dirname "$0")/.."
BOOT=internal/bootstrap/bootstrap.go
PKG=./internal/bootstrap/

BAK=$(mktemp)
cp "$BOOT" "$BAK"
restore() { cp "$BAK" "$BOOT"; }
trap restore EXIT

fail=0

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
p='$BOOT'
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
    if ! build_out=$(go test "$PKG" -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI,"
        echo "     mutasyon derlenebilir olacak şekilde yazılmalı. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if go test "$PKG" -run 'TestSSHArgs|TestArchFromUname|OptionLikeHost|OrdinaryHost|TestRemoteExtractionMatchesTheArchiveFormat|TestArchiveCarriesEverythingTheInstallerNeeds|TestSudo|TestRootMode|TestRootInstall|TestBootstrapRefusesTheClientUser|TestShellQuote' -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== ssh çağrısı =="

mutate "BatchMode düştü (ssh parola sorabilir)" \
    "s=s.replace('\t\t\"-o\", \"BatchMode=yes\",\n','',1)"

mutate "hedef seçeneklerin başına da kondu" \
    "s=s.replace('\targs := []string{\n\t\t\"-T\",','\targs := []string{\n\t\topts.Host,\n\t\t\"-T\",',1)"

mutate "port verilmese de -p ekleniyor" \
    "s=s.replace('\tif opts.Port != 0 {','\tif true {',1)"

mutate "-T düştü" \
    "s=s.replace('\t\t\"-T\",\n','',1)"

echo "== Argüman enjeksiyonu =="

mutate "- ile başlayan hedef kabul ediliyor" \
    "s=s.replace('\tif strings.HasPrefix(opts.Host, \"-\") {','\tif false {',1)"

echo "== Mimari eşlemesi =="

mutate "aarch64 amd64 sanılıyor" \
    "s=s.replace('\tcase \"aarch64\", \"arm64\":\n\t\treturn \"arm64\", nil','\tcase \"aarch64\", \"arm64\":\n\t\treturn \"amd64\", nil',1)"

mutate "tanınmayan mimari amd64 sayılıyor" \
    "s=s.replace('\t\treturn \"\", fmt.Errorf(\"bootstrap: desteklenmeyen mimari: %q\", machine)','\t\treturn \"amd64\", nil',1)"

echo "== Paket biçimi ile açma komutu (K-119) =="

# İki taraf ayrı yerde yazılıyor; biri değişip öbürü değişmezse kurulum
# sunucuda "not in gzip format" ile düşer.
mutate "paket gzip'lenmiyor" \
    "s=s.replace('\ttw := tar.NewWriter(gz)','\ttw := tar.NewWriter(&buf)',1)"

mutate "sunucu gzip açmıyor" \
    "s=s.replace('tar -x -z -m -C','tar -x -m -C',1)"

echo "== Sudo kipi (K-122) =="

# Önkontrol yüklemeden ÖNCE: parola isteyen sudo ya da root olmayan hedef
# 28 MB'ı boşuna yüklerdi.
mutate "yetki önkontrolü yok" \
    "s=s.replace('\tif err := checkPrivilege(ctx, opts); err != nil {\n\t\treturn err\n\t}\n','',1)"

mutate "uid denetlenmiyor" \
    "s=s.replace('if uid := strings.TrimSpace(out); uid != \"0\" {','if uid := strings.TrimSpace(out); uid == \"hiç\" {',1)"

mutate "sudo'nun kendi mesajı taşınmıyor" \
    "s=s.replace('(-sudo kipi parola SORMAZ; sudoers\\'ta NOPASSWD gerekir): %w\", opts.Host, err)','(-sudo kipi parola SORMAZ; sudoers\\'ta NOPASSWD gerekir)\", opts.Host)',1)"

mutate "sudo kipinde betik sudo'suz koşuyor" \
    "s=s.replace('\tif !opts.Sudo {\n\t\treturn script\n\t}','\tif true {\n\t\treturn script\n\t}',1)"

# -n olmadan sudo parola sormaya kalkar: sırrı görmeme ilkesi delinir.
mutate "sudo -n düştü" \
    "s=s.replace('\"sudo -n -- bash -c \"','\"sudo -- bash -c \"',1)"

# Tırnaklar kaçış derdi olmasın diye chr() ile: ReplaceAll'ın aradığı
# tek tırnak başka bir metne çevriliyor, yani hiçbir tırnak kaçırılmıyor.
mutate "tek tırnak kaçırılmıyor" \
    "s=s.replace('strings.ReplaceAll(s, '+chr(34)+chr(39)+chr(34)+',','strings.ReplaceAll(s, '+chr(34)+'YOK'+chr(34)+',',1)"

mutate "panely-client ile kurulum kabul ediliyor" \
    "s=s.replace('ok && user == clientUser {','ok && user == \"hiç\" {',1)"

restore
if [[ $fail -ne 0 ]]; then
    echo
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi

echo
echo "Bütün mutasyonlar yakalandı."
