#!/usr/bin/env bash
# Kurulumun ssh çağrısını ve kaldığı yerden devam eden yüklemeyi koruyan
# testlerin GERÇEKTEN bir şey koruduğunu sınar (K-114, K-127).
#
# K-127 mutasyonlarının bir kısmını yalnızca Linux'taki gerçek-betik
# testleri (upload_linux_test.go) yakalıyor: dd, flock, sha256sum ve sudo
# davranışı sahte ssh'ta yok. Bu betik CI'da Linux'ta ve
# KADRAN_TEST_REAL_SUDO=1 ile koşuyor; Windows'ta o mutasyonlar yeşil kalır.
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
UPLOAD=internal/bootstrap/upload.go
PKG=./internal/bootstrap/
TESTS='TestSSHArgs|TestArchFromUname|OptionLikeHost|OrdinaryHost|TestRemoteExtractionMatchesTheArchiveFormat|TestArchiveCarriesEverythingTheInstallerNeeds|TestArchiveIsDeterministic|TestRemoteTarLine|TestSudo|TestRootMode|TestRootInstall|TestBootstrapRefusesTheClientUser|TestShellQuote|TestUpload|TestALonger|TestACorrupt|TestPersistent|TestFollow|TestInstallFailure|TestAnUnexpected|TestAnInstall|TestAnother|TestADead|TestAShort|TestRealScripts|TestRejectsMultipleKeys|TestArchiveCarriesTheKeyAsOneLine'

BAK_BOOT=$(mktemp)
BAK_UPLOAD=$(mktemp)
cp "$BOOT" "$BAK_BOOT"
cp "$UPLOAD" "$BAK_UPLOAD"
restore() { cp "$BAK_BOOT" "$BOOT"; cp "$BAK_UPLOAD" "$UPLOAD"; }
trap restore EXIT

fail=0

# mutate <ad> <python-ifadesi>: bootstrap.go'ya uygular.
mutate() { mutate_in "$BOOT" "$@"; }

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
        # uğramadan ölçülür (K-127'de conv=notrunc mutantı böyle yeşil kaldı).
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
    if ! build_out=$(go test "$PKG" -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI,"
        echo "     mutasyon derlenebilir olacak şekilde yazılmalı. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    # -timeout: sonsuz döngüye sokan mutantlar (ör. sınırsız yeniden
    # yükleme) testi zaman aşımıyla düşürür; o da yakalanma sayılır.
    if go test "$PKG" -run "$TESTS" -count=1 -timeout 90s >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

# Taban YEŞİL olmalı: kırmızı bir taban (ör. eksik bir araç) her mutantı
# "yakalandı" gösterirdi.
if ! go test "$PKG" -run "$TESTS" -count=1 -timeout 90s >/dev/null 2>&1; then
    echo "!! TABAN KIRMIZI: mutasyonsuz kodda testler düşüyor — ölçüm YAPILMADI"
    exit 1
fi

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
    "s=s.replace('\tif strings.HasPrefix(host, \"-\") {','\tif false {',1)"

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

mutate_in "$UPLOAD" "sunucu gzip açmıyor" \
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

mutate "kadran-client ile kurulum kabul ediliyor" \
    "s=s.replace('ok && user == clientUser {','ok && user == \"hiç\" {',1)"

echo "== Kaldığı yerden devam eden yükleme (K-127) =="

mutate_in "$UPLOAD" "devam yok: her deneme baştan yazıyor" \
    "s=s.replace('strconv.Itoa(have)),\n\t\t\tbytes.NewReader(archive[have:])','strconv.Itoa(0)),\n\t\t\tbytes.NewReader(archive[0:])',1)"

# Ölü oturumun gecikmiş yazması dosyayı KISALTIRDI.
mutate_in "$UPLOAD" "conv=notrunc düştü" \
    "s=s.replace('oflag=seek_bytes conv=notrunc status=none','oflag=seek_bytes status=none',1)"

# Ofset blok (64 KiB) cinsinden olurdu: devam yanlış yere yazar.
mutate_in "$UPLOAD" "oflag=seek_bytes düştü" \
    "s=s.replace('seek=\"\$2\" oflag=seek_bytes conv','seek=\"\$2\" conv',1)"

mutate_in "$UPLOAD" "özet denetlenmiyor" \
    "s=s.replace('sha256sum -c --status 2>/dev/null; then','sha256sum -c --status 2>/dev/null || true; false; then',1)"

mutate_in "$UPLOAD" "deneme sayımı bir eksik (ilk sürümün hatası)" \
    "s=s.replace('if r.failures >= maxTransferAttempts {','if r.failures >= maxTransferAttempts-1 {',1)"

mutate_in "$UPLOAD" "eksik yazma fark edilmiyor (sonsuz döngü)" \
    "s=s.replace('\t\tcase wrote:','\t\tcase wrote && false:',1)"

mutate_in "$UPLOAD" "bozuk paket sınırsız yeniden yükleniyor" \
    "s=s.replace('\t\t\tif reuploaded {','\t\t\tif reuploaded && false {',1)"

# Yalnızca ssh'ın kendi hatası (255) yeniden denenir; özet uyuşmazlığı
# gibi gerçek bir sonuç "kopma" sayılmamalı.
mutate_in "$UPLOAD" "kopma dışı sonuçlar da yeniden deneniyor" \
    "s=s.replace('if code != sshTransportFailure {\n\t\t\treturn code, nil','if code == 0 {\n\t\t\treturn code, nil',1)"

mutate_in "$UPLOAD" "yükleme dizini doğrulanmıyor" \
    "s=s.replace('if err := validUploadDir(fields[0]); err != nil {','if err := validUploadDir(fields[0]); err != nil && false {',1)"

echo "== Oturumdan ayrılan kurulum (K-127) =="

mutate_in "$UPLOAD" "kilit yok (iki kurulum üst üste)" \
    "s=s.replace('if ! flock -n 9; then','if false; then',1)"

mutate_in "$UPLOAD" "başkasının kurulumu izleniyor" \
    "s=s.replace('\t\tcase installBusy:\n','\t\tcase installBusy:\n\t\t\treturn followInstall(ctx, opts, dir, sum)\n',1)"

# Arkada kalan bir alt süreç kilidi sonsuza dek tutardı.
mutate_in "$UPLOAD" "kilit kurulumun alt süreçlerine geçiyor" \
    "s=s.replace(' 9>&- ||',' ||',1)"

mutate_in "$UPLOAD" "izleme ofseti sıfırlanıyor (günlük tekrar)" \
    "s=s.replace('strconv.Itoa(out.n)','strconv.Itoa(0)',1)"

# Bellek yetmeyip ölen kurulumu izleme sonsuza dek beklerdi.
mutate_in "$UPLOAD" "ölen kurulum algılanmıyor" \
    "s=s.replace('if flock -n install.lock true; then','if false; then',1)"

# Sudo'nun umask'ı 077 ise root'un günlüğünü izleyen kullanıcı okuyamaz.
# Yalnızca gerçek sudo testi yakalar (KADRAN_TEST_REAL_SUDO).
mutate_in "$UPLOAD" "umask 022 düştü" \
    "s=s.replace('set -e\numask 022\n','set -e\n',1)"

mutate_in "$UPLOAD" "başlatma sudo'suz koşuyor" \
    "s=s.replace('\tif opts.Sudo {\n\t\treturn \"sudo -n -- \" + cmd','\tif false {\n\t\treturn \"sudo -n -- \" + cmd',1)"

mutate_in "$UPLOAD" "başlatmada sudo -n düştü" \
    "s=s.replace('return \"sudo -n -- \" + cmd','return \"sudo -- \" + cmd',1)"

# Sessizce ölen bağlantıda ssh sonsuza dek beklerdi; yeniden bağlanma
# hiç tetiklenmezdi.
mutate "ServerAliveInterval düştü" \
    "s=s.replace('\t\t\"-o\", \"ServerAliveInterval=15\",\n','',1)"

echo "== Paket belirlenimci (K-127: yarım yükleme yalnızca aynı pakete devam eder) =="

mutate "birim sırası rastgele" \
    "s=s.replace('slices.Sorted(maps.Keys(files))','slices.Collect(maps.Keys(files))',1)"

mutate "dosya zamanı sabit değil" \
    "s=s.replace('ModTime: archiveModTime,','ModTime: time.Unix(time.Now().UnixNano()%1000000, 0),',1)"

echo "== Tek satır anahtar (K-131: ikinci satır kısıtsız anahtar olurdu) =="

mutate "çok satırlı anahtar dosyası kabul ediliyor" \
    "s=s.replace('\tif strings.ContainsAny(text, \"\\\\r\\\\n\") {','\tif false && strings.ContainsAny(text, \"\\\\r\\\\n\") {',1)"

mutate "pakete anahtarın ham hâli gidiyor" \
    "s=s.replace('key = []byte(strings.TrimSpace(string(key)) + \"\\\\n\")','key = normalizeLineEndings(key)',1)"

restore
if [[ $fail -ne 0 ]]; then
    echo
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi

echo
echo "Bütün mutasyonlar yakalandı."
