#!/usr/bin/env bash
# Kadran sunucu kurulumu. `kadran bootstrap root@sunucu` tarafından
# uzak makinede root olarak çalıştırılır.
#
# # Bu betik neyi kuruyor?
#
# Üç binary, iki kullanıcı, iki grup ve bir SSH zorlanmış komutu. Kurulum
# bittiğinde root erişimi bir daha GEREKMEZ: günlük kullanım yetkisiz
# `kadran-client` kullanıcısı üzerinden yürür.
#
# # İdempotent
#
# Baştan sona tekrar çalıştırılabilir. `hcloud server rebuild` ile temiz
# imajdan defalarca koşturularak sınanıyor.
#
# Kullanım: install.sh <hazırlık-dizini>

set -euo pipefail

STAGE="${1:?usage: install.sh <staging-directory>}"

LIB_DIR=/usr/local/lib/kadran
STATE_DIR=/var/lib/kadran
CLIENT_HOME=/var/lib/kadran-client
SSHD_DROPIN=/etc/ssh/sshd_config.d/60-kadran.conf

say()  { printf '  %s\n' "$*"; }
step() { printf '\n==> %s\n' "$*"; }
die()  { printf '\nERROR: %s\n' "$*" >&2; exit 1; }

# calisan_ayni_mi <birim> <ikili> — birimin ÇALIŞAN imajı kurulan ikiliyle
# içerikçe aynı mı. Kanıt /proc/<pid>/exe'den: `systemctl is-active` eski
# sürecin ayakta olduğunu da "active" diye gösterir (K-049, K-112).
calisan_ayni_mi() {
    local pid calisan kurulan
    pid="$(systemctl show -p MainPID --value "$1" 2>/dev/null || echo 0)"
    [ "${pid:-0}" -gt 0 ] 2>/dev/null || return 1
    calisan="$(md5sum "/proc/$pid/exe" 2>/dev/null | cut -d' ' -f1)"
    kurulan="$(md5sum "$2" 2>/dev/null | cut -d' ' -f1)"
    [ -n "$calisan" ] && [ "$calisan" = "$kurulan" ]
}

# vekil_parmak_izi — ters vekilin yapılandırma ve birim dosyalarının özeti.
# Yeniden kurulumda bunlar değişmediyse (ve ikili aynıysa) ters vekil
# yeniden başlatılmıyor: o trafiğin yolu (K-112).
#
# İlk kurulumda bu dosyalar YOK ve cat 1 döner; betik `set -euo pipefail`
# ile koştuğu için korumasız bir `x="$(vekil_parmak_izi)"` taze kurulumu
# sessizce durdururdu. `|| true` bu yüzden.
vekil_parmak_izi() {
    { cat /etc/kadran/caddy.json /etc/tmpfiles.d/kadran-caddy.conf \
          /etc/systemd/system/kadran-caddy.service \
          /etc/systemd/system/kadran-caddy-admin.socket \
          /etc/systemd/system/kadran-caddy.service.d/*.conf 2>/dev/null || true; } |
        md5sum | cut -d' ' -f1
}

# istemci_olarak <kullanıcı> <grup> <komut|fonksiyon> [argüman…] — komutu
# o kullanıcının kimliğiyle koşturur (K-139).
#
# Root kadran-client'ın ev dizininde yol üzerinden iş yapınca, o kullanıcı
# denetim ile kullanım arasında bir adı bağa çevirebilir; root da bağın
# hedefinde çalışır (K-137'nin kalan penceresi). Aynı iş o kullanıcının
# kimliğiyle yapılınca bir bağ, onun zaten erişebildiğinden fazlasını açmaz.
#
# Fonksiyon verilirse `declare -f` ile yeni bir bash'e taşınır. Yalnız o
# fonksiyon gider, `die` gitmez: çağrılan fonksiyon çıkış koduyla konuşur.
# Ortam `env -i` ile boşaltılır ve çalışma dizini `/` olur; root'un
# dizinleri (`$STAGE` 0700) o kullanıcıya kapalı.
#
# Root'suzken yalnız kendi kimliğine "geçer" (sınama düzeneği:
# scripts/check-install-sh.sh). Başka bir kullanıcı istenirse 1 döner.
istemci_olarak() {
    local kullanici="$1" grup="$2" onek=() fonk
    shift 2
    if [ "$(id -u)" -eq 0 ]; then
        onek=(setpriv --reuid "$kullanici" --regid "$grup" --clear-groups --)
    elif [ "$kullanici" != "$(id -u)" ] && [ "$kullanici" != "$(id -un 2>/dev/null)" ]; then
        return 1
    fi
    if declare -F "$1" >/dev/null; then
        fonk="$1"
        shift
        set -- bash -c "$(declare -f "$fonk"); $fonk \"\$@\"" "$fonk" "$@"
    fi
    (cd / && ${onek[@]+"${onek[@]}"} env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin "$@")
}

# ak_yaz_istemci <authorized_keys> <anahtar gövdesi> <satır> — kadran-client
# olarak koşar (istemci_olarak). Gövdeyi taşıyan eski satırı atar, satırı
# ekler ve dosyayı yerine koyar.
#
# Bağ denetimini ÇAĞIRAN yapar. Denetimden sonra konan bir bağ burada yalnız
# o kullanıcının erişebildiği bir dosyaya ulaşır; root'un dosyasında okuma
# düşer (5). Yeni içerik bütünüyle bir `mktemp` kopyasına (O_EXCL, tahmin
# edilemez ad, 0600) yazılır ve `mv -T` ile yerine konur: dosya o kullanıcının
# olarak doğar, sahiplik aktarmaya gerek kalmaz.
#
# Çıkış: 0 tamam, 4 geçici dosya açılamadı, 5 okunamadı, 6 yazılamadı,
# 7 yerine konamadı.
ak_yaz_istemci() {
    local f="$1" govde="$2" satir="$3" gecici rc=0
    gecici="$(mktemp "$f.XXXXXX")" || return 4
    if [ -e "$f" ]; then
        # grep'in 1'i "hiç satır kalmadı" (dosyada yalnız bu anahtar
        # vardı); 2 okuma hatası. Yutulsa dağıtım satırları sessizce düşerdi.
        grep -vF -- "$govde" "$f" > "$gecici" || rc=$?
        [ "$rc" -le 1 ] || { rm -f "$gecici"; return 5; }
    fi
    printf '%s\n' "$satir" >> "$gecici" || { rm -f "$gecici"; return 6; }
    mv -fT "$gecici" "$f" || { rm -f "$gecici"; return 7; }
}

# yonetici_satiri_yaz <authorized_keys> <açık anahtar dosyası> <LIB_DIR>
#                     <kullanıcı> <grup>
# — yönetici anahtarının zorlanmış komutlu satırını yazar.
#
# Anahtar dosyası TEK satır olmalı, yoksa 1 döner ve dosyaya dokunmaz:
# satır `command=...,restrict $(cat …)` diye kuruluyor ve ikinci bir satır
# authorized_keys'e AYRI ve KISITSIZ bir anahtar olarak düşerdi (K-131).
# İstemci de denetliyor; bu ikinci kat, elle hazırlanmış bir paket için.
#
# İdempotanlık: aynı anahtar gövdesini (tip + base64) taşıyan satır
# yenisiyle DEĞİŞTİRİLİR, yorum alanı değişebilir. Başka anahtarların
# satırları — `kadran key` ile eklenen dağıtım anahtarları dahil —
# KORUNUR. Tek-satır denetimi de sınanıyor: scripts/check-install-sh.sh.
#
# authorized_keys kadran-client'ın dizininde. Root yalnız anahtar dosyasını
# okur (`$STAGE` ona kapalı) ve bağ denetimini yapar: dosya bağsa ya da
# düzenli dosya değilse DURUR (K-137). Yazma o kullanıcı olarak yapılır
# (ak_yaz_istemci, K-139); denetimden sonra konan bir bağ root'a hiçbir şey
# yaptıramaz. Root'a ait bir authorized_keys'i o kullanıcı okuyamaz: kurulum
# durur ve dosyaya dokunulmaz.
#
# Fonksiyon kurulumda `|| die` bağlamında çağrılıyor; orada `set -e`
# KAPALI. Bu yüzden her adım kendi hatasını denetliyor.
yonetici_satiri_yaz() {
    local auth_file="$1" key_file="$2" lib_dir="$3" kullanici="$4" grup="$5" key key_body rc=0
    [ "$(grep -c '' "$key_file")" -eq 1 ] || return 1
    key="$(cat "$key_file")"
    # CR denetimi Linux'ta anlamlı (kurulum ve CI orada). Git Bash hem
    # grep'te hem `$(…)`'da CR'yi siliyor (ölçüldü); orada bu senaryo
    # sınanamaz.
    case "$key" in *$'\r'*) return 1 ;; esac
    key_body="$(printf '%s' "$key" | awk '{print $1" "$2}')"
    if [ -L "$auth_file" ] || { [ -e "$auth_file" ] && [ ! -f "$auth_file" ]; }; then
        die "$auth_file is a symlink or not a regular file; not written"
    fi
    istemci_olarak "$kullanici" "$grup" ak_yaz_istemci "$auth_file" "$key_body" \
        "command=\"$lib_dir/kadran-connect\",restrict $key" || rc=$?
    case "$rc" in
    0) ;;
    5) die "$auth_file could not be read as $kullanici; left untouched (if it is not owned by $kullanici, fix it: chown $kullanici: $auth_file)" ;;
    *) die "$auth_file could not be written as $kullanici (exit $rc); left untouched" ;;
    esac
}

# ssh_dizini_hazirla <dizin> <kullanıcı> <grup> — `.ssh`'yi kurar ya da
# iznini düzeltir.
#
# Dizin kadran-client'ın ev dizininde; o kullanıcı `.ssh`'nin yerine bir
# bağ koyabilir. Root'un `install -d`'si bağı İZLER ve HEDEF dizini o
# kullanıcıya 0700 ile devreder (Debian 13'te ölçüldü). Bu yüzden bağsa
# DURUR (K-137) ve dizini o kullanıcı olarak kurar (K-139): denetimden sonra
# konan bir bağın hedefi onun değilse izin değişikliği düşer. Dizin o
# kullanıcının olarak doğar; sahibi başkasıysa kurulum durur.
ssh_dizini_hazirla() {
    local dizin="$1" kullanici="$2" grup="$3"
    if [ -L "$dizin" ]; then
        die "$dizin is a symlink; .ssh not set up"
    fi
    istemci_olarak "$kullanici" "$grup" install -d -m 0700 "$dizin" \
        || die "$dizin could not be set up as $kullanici (see the error above; if it is not owned by $kullanici: chown $kullanici: $dizin)"
}

# kisitsiz_satir_sayisi <authorized_keys> <LIB_DIR> — kadran-connect'e
# zorlanmamış ya da `restrict` taşımayan anahtar satırlarının sayısı.
#
# Tek bir böyle satır kadran-client'a kabuk açar. Eski denetim "herhangi
# bir satırda command= var mı" diye bakıyordu ve iki satırlı anahtar
# dosyasının ürettiği kısıtsız ikinci satırı GEÇİRİRDİ (K-131).
kisitsiz_satir_sayisi() {
    { grep -vE '^[[:space:]]*(#|$)' "$1" || true; } |
        { grep -cvE "^command=\"$2/kadran-connect( -deploy=[a-z0-9,-]+)?\",restrict " || true; }
}

# ── Ön koşullar ──────────────────────────────────────────────────────

step "Prerequisites"

[ "$(id -u)" -eq 0 ] || die "this script must run as root (if SSH as root is disabled: kadran bootstrap -sudo user@server)"
command -v systemctl >/dev/null || die "systemd not found — not supported"
command -v sshd >/dev/null || command -v /usr/sbin/sshd >/dev/null \
    || die "sshd not found"

SSHD_BIN="$(command -v sshd || echo /usr/sbin/sshd)"

# Docker README'de ön koşul. Taze sunucu testinde (K-112) betik ona hiç
# bakmıyordu: Docker'sız bir sunucuda kullanıcılar ve birimler kuruluyor,
# hata ancak sonda, sebebi belirsiz bir kontrolle görünebilirdi.
command -v docker >/dev/null \
    || die "Docker Engine not found — install it first (Ubuntu: apt-get install -y docker.io)"
docker version --format '{{.Server.Version}}' >/dev/null 2>&1 \
    || die "Docker is installed but its daemon does not answer — systemctl status docker"

# nologin yolu dağıtıma göre değişiyor.
NOLOGIN="$(command -v nologin || echo /usr/sbin/nologin)"

say "system OK ($(uname -m), $( (. /etc/os-release && echo "$PRETTY_NAME") 2>/dev/null || echo unknown ))"

# ── Eski adlı kurulum (panely → kadran, K-136) ───────────────────────
#
# Göç kodu ayrı dosyada; kurulum paketinde install.sh'ın yanında gelir.
# shellcheck source=goc.sh
. "$STAGE/goc.sh"
GOC=0
if goc_gerekli; then
    GOC=1
    goc_1
fi

# ── Gruplar ve kullanıcılar ──────────────────────────────────────────

step "Groups and users"

getent group kadran >/dev/null || groupadd --system kadran
getent group kadran-client >/dev/null || groupadd --system kadran-client

# kadrand'nin çalıştığı yetkisiz kullanıcı. Giriş yapmaz.
if ! id -u kadran >/dev/null 2>&1; then
    useradd --system --gid kadran \
        --home-dir "$STATE_DIR" --no-create-home \
        --shell "$NOLOGIN" \
        --comment "Kadran control plane" kadran
fi

# SSH istemci kullanıcısı.
#
# KRİTİK: birincil grup `kadran-client` OLMAK ZORUNDA (-g), ek grup (-G)
# DEĞİL. SO_PEERCRED yalnızca sürecin BİRİNCİL grubunu bildirir; ek grup
# üyeliklerini görmez. Yanlış yapılırsa hiçbir hata mesajı çıkmaz, her
# bağlantı sessizce reddedilir.
#
# Kabuk `nologin` DEĞİL: sshd zorlanmış komutu kullanıcının giriş kabuğu
# üzerinden çalıştırır ve nologin onu reddeder. Hesabı kısıtlayan şey
# kabuk değil, authorized_keys'teki `command=...,restrict` ikilisi.
if ! id -u kadran-client >/dev/null 2>&1; then
    useradd --system --gid kadran-client \
        --home-dir "$CLIENT_HOME" --create-home \
        --shell /bin/sh \
        --comment "Kadran client access" kadran-client
fi

# ── Değişmez doğrulaması ─────────────────────────────────────────────
#
# Kullanıcılar önceden (yanlış) oluşturulmuş olabilir. Sessizce kabul
# etmek yerine kontrol ediyoruz: bu iki koşul modelin dayandığı yer.

step "Privilege invariants"

primary="$(id -gn kadran-client)"
[ "$primary" = "kadran-client" ] || die \
"the primary group of user kadran-client is '$primary', it must be 'kadran-client'.
SO_PEERCRED reports only the primary group; as it is, every connection
is silently refused. To fix:  usermod -g kadran-client kadran-client"

if id -nG kadran-client | tr ' ' '\n' | grep -qx kadran; then
    die \
"user kadran-client is in the 'kadran' group. As it is, it reaches exec.sock
directly and kadrand can be bypassed entirely — privilege separation collapses.
To fix:  gpasswd -d kadran-client kadran"
fi

if id -nG kadran | tr ' ' '\n' | grep -qx docker; then
    die \
"user kadran is in the 'docker' group. Access to the Docker socket is root
in practice; as it is, the executor separation is decorative.
To fix:  gpasswd -d kadran docker"
fi

say "kadran-client primary group: $primary"
say "kadran-client groups: $(id -nG kadran-client)"
say "kadran groups: $(id -nG kadran)"

# ── Binary'ler ───────────────────────────────────────────────────────

step "Binary'ler"

install -d -m 0755 -o root -g root "$LIB_DIR"

for binary in kadrand kadran-exec kadran-connect kadran-caddy kadran-vault; do
    [ -f "$STAGE/$binary" ] || die "$binary is missing from the staging directory"
done

# kadran-exec root çalışır ve yalnızca root yazabilmeli.
install -m 0755 -o root -g root "$STAGE/kadrand"       "$LIB_DIR/kadrand"
install -m 0755 -o root -g root "$STAGE/kadran-exec"   "$LIB_DIR/kadran-exec"
# kadran-connect'i kadran-client çalıştırır; yazma yetkisi yine yalnızca root.
install -m 0755 -o root -g root "$STAGE/kadran-connect" "$LIB_DIR/kadran-connect"
# Ters vekil. Yazma yetkisi yalnızca root: bu binary bir GÜVENLİK SINIRI
# taşıyor (K-050) ve çalıştıran kullanıcının onu değiştirebilmesi sınırı
# anlamsız kılardı.
install -m 0755 -o root -g root "$STAGE/kadran-caddy"  "$LIB_DIR/kadran-caddy"
# Kasa anahtarını üretir (K-123). Yalnız kurulumda, root olarak koşuyor.
install -m 0755 -o root -g root "$STAGE/kadran-vault"  "$LIB_DIR/kadran-vault"
# Kasanın geri dönüşü (v0.4.x'e inmeden önce elle çalıştırılır).
install -m 0755 -o root -g root "$STAGE/kasa-coz.sh"   "$LIB_DIR/kadran-kasa-coz.sh"

say "installed into $LIB_DIR"
"$LIB_DIR/kadrand" -version || die "could not run kadrand — the architecture may not match"

# ── Dizinler ─────────────────────────────────────────────────────────

step "Directories"

install -m 0644 -o root -g root "$STAGE/kadran-tmpfiles.conf" /etc/tmpfiles.d/kadran.conf
systemd-tmpfiles --create /etc/tmpfiles.d/kadran.conf

# /var/lib/kadran tmpfiles ile yaratılıyor ama yeniden başlatma arasında
# kalıcı olması gerekiyor; burada da garantiye alıyoruz.
install -d -m 0750 -o kadran -g kadran "$STATE_DIR"

say "/run/kadran, /run/kadran-exec, $STATE_DIR ready"

# ── Kasa (K-123) ─────────────────────────────────────────────────────
#
# Ortam değişkeni değerleri executor'ın anahtarıyla mühürleniyor. Anahtar
# yoksa kadran-vault üretiyor (0600, var olanın üstüne asla yazmıyor) ve
# yalnız AÇIK anahtarı basıyor: özel anahtar bu betiğin çıktısına ve
# günlüğe hiç girmiyor. Daemon açık anahtarı /etc/kadran/vault.pub'dan
# okuyor (root 0644; daemon yazamıyor).

step "Kasa"

VAULT_KEY=/var/lib/kadran-exec/vault.key
VAULT_PUB=/etc/kadran/vault.pub
vault_yeni=0
[ -e "$VAULT_KEY" ] || vault_yeni=1
vault_alici="$("$LIB_DIR/kadran-vault" -key "$VAULT_KEY")" \
    || die "could not prepare the vault key (see above). If it is corrupt, restore it from your copy: $VAULT_KEY — no new key was written over it."
case "$vault_alici" in
    age1*) ;;
    *) die "kadran-vault printed unexpected output" ;;
esac
# Yeni anahtar diske inmeden daemon değerleri ona mühürlerse ve makine o
# arada çökerse anahtar boş kalabilir: değerler kurtarılamazdı.
sync
install -d -m 0755 -o root -g root /etc/kadran
vault_gecici="$(mktemp)"
printf '%s\n' "$vault_alici" > "$vault_gecici"
install -m 0644 -o root -g root "$vault_gecici" "$VAULT_PUB"
rm -f "$vault_gecici"
say "vault recipient: $vault_alici"
if [ "$vault_yeni" -eq 1 ]; then
    say "⚠ NEW VAULT KEY created. Environment variables are encrypted with it;"
    say "  if the key is lost the values CANNOT BE RECOVERED. Save it in your password manager:"
    say "    sudo cat $VAULT_KEY"
    say "  Do not keep it in the same place as the offsite backup key."
fi

# ── systemd birimleri ────────────────────────────────────────────────

step "systemd units"

install -m 0644 -o root -g root "$STAGE/kadran-exec.service" /etc/systemd/system/kadran-exec.service
install -m 0644 -o root -g root "$STAGE/kadrand.service"     /etc/systemd/system/kadrand.service
install -m 0644 -o root -g root "$STAGE/var-lib-kadran-volumes.mount" \
    /etc/systemd/system/var-lib-kadran-volumes.mount
systemctl daemon-reload

# ── Executor denetim günlüğü daemon'un dizininden ÇIKARILIR ──────────
#
# Günlük eskiden $STATE_DIR içindeydi. Dosya root'undu ama dizin
# kadran'ın; daemon onu silip yerine kendi zincirini koyabiliyordu
# (K-100, canlıda ölçüldü). Yeni yeri /var/lib/kadran-exec (0700 root,
# tmpfiles yukarıda yarattı).
old_journal="$STATE_DIR/exec-audit.log"
new_journal=/var/lib/kadran-exec/exec-audit.log

# Birimi güncellemek yetmez: operatörün `systemctl edit` ile eklediği
# bir drop-in (ör. --allow-repo için) ExecStart'ı TAMAMEN yeniden yazar
# ve eski yolu taşıyor olabilir. O durumda günlük taşınır, executor
# eski yerde BOŞ bir zincir başlatır ve geçmiş sessizce kopar. Önce
# etkin ExecStart'a bakılıyor, taşımaya ondan sonra geçiliyor.
#
# Çıktı önce değişkene alınıyor: `set -o pipefail` altında `… | grep -q`
# yarışa açık — grep eşleşince erken çıkar, sol taraf SIGPIPE alır ve
# boru hattı başarısız sayılır. Yani eşleşme "yok" okunabilirdi.
exec_start="$(systemctl show -p ExecStart kadran-exec.service)"
if [[ "$exec_start" == *"$old_journal"* ]]; then
    die "kadran-exec's effective ExecStart still points the log at $old_journal.
Most likely a drop-in (systemctl cat kadran-exec). Change it to:
  --journal $new_journal
then run the install again. The log was NOT moved."
fi

if [ -e "$old_journal" ]; then
    [ -e "$new_journal" ] && die "both logs exist: $old_journal and $new_journal
Decide by hand which one is the real chain; neither was deleted."

    # Executor günlüğü açılışta bir kez açıp tanımlayıcıyı tutuyor.
    # Çalışırken taşınırsa eski inode'a yazmaya devam eder; bu yüzden
    # önce durdurulur. Aşağıdaki `enable --now` onu yeni yolla başlatır.
    systemctl stop kadran-exec.service 2>/dev/null || true
    mv "$old_journal" "$new_journal"
    chown root:root "$new_journal"
    chmod 0640 "$new_journal"
    say "executor audit log moved to $new_journal"
fi

# Hacim kökü nodev,nosuid ile bağlanır. Birim ÖNCE etkinleştirilir ki
# yeniden başlatmadan sonra da bağlansın; `enable` tek başına şimdi
# bağlamaz, bu yüzden `start` da çağrılır (ikisi de idempotent).
systemctl enable var-lib-kadran-volumes.mount >/dev/null 2>&1 || true
systemctl restart var-lib-kadran-volumes.mount \
    || die "could not harden the volume root — app volumes would be mounted without nodev,nosuid"

# Birimin AKTİF olması yetmez: `Options=` sessizce yok sayılsaydı birim
# yine "active" görünürdü. Etkin bayraklar ÇEKİRDEKTEN okunur.
#
# Bu kontrolün var olma sebebi ölçülmüş bir yanlıştır: Docker'ın local
# sürücüsüne aynı seçenekler verildiğinde hacim sertleştirilmeden
# bağlanıyor ve hiçbir hata üretmiyor (docs/decisions.md K-038).
vol_opts="$(awk '$5=="/var/lib/kadran/volumes"{print $6}' /proc/self/mountinfo | head -1)"
for flag in nodev nosuid; do
    printf '%s' "$vol_opts" | tr ',' '\n' | grep -qx "$flag" \
        || die "$flag is NOT ACTIVE on the volume root (active: ${vol_opts:-<not mounted>})"
done
say "volume root hardened ($vol_opts)"

# ── Ters vekil (kadran-caddy) ────────────────────────────────────────
#
# Dağıtımın `caddy` paketine BAĞLANILMIYOR; gerekçe
# deploy/systemd/kadran-caddy.service'in başında. Kurulan her şey depodan
# geliyor: birim, soket, tmpfiles kuralı ve yol açıcı yapılandırma.

# ters_vekil_hazirla / ters_vekil_baslat — ters vekili kurar ya da yükseltir.
#
# İki parça çünkü göçte (K-136) arada eski vekil duruyor: ESKİ vekil siteyi
# sunarken yapılabilen her şey (K-050 denetimi, dosyalar, enable) hazırlıkta;
# kullanıcıya ve porta bağlı olanlar başlatmada. GCP provasında ilk sürümün
# 6,5 sn'lik vekil geçişinin ~3 sn'si modül denetimiydi. Taze kurulum ve
# yükseltmede ikisi art arda çalışır (ters_vekil_kur), davranış aynı.
ters_vekil_hazirla() {
    step "Ters vekil"

    # ── K-050 SINIRI: binary'de dosya servis eden modül var mı? ──────────
    #
    # Bu, kurulumun en önemli ölçümü. Sınır bir yapılandırmada değil,
    # BINARY'DE: kadrand admin soketine yazabildiği için, stok Caddy'de o
    # yetki "alan adının TLS özel anahtarını okuyabilme"yi de kapsıyordu
    # (ölçüldü, varsayılmadı).
    #
    # ⚠ ÖNCE POZİTİF KONTROL. Doğrudan "file_server var mı" diye sormak,
    # binary hiç çalışmasa bile "yok" cevabı üretirdi — cevapsızlığı istenen
    # cevap diye okumak bu projede üç kez yanlış sonuç ürettirdi (K-051).
    # Bu yüzden önce beklenen bir modülün VARLIĞI kanıtlanıyor.
    caddy_modules="$("$LIB_DIR/kadran-caddy" list-modules 2>/dev/null)" \
        || die "could not run kadran-caddy — the architecture may not match"

    printf '%s\n' "$caddy_modules" | grep -qx 'http.handlers.reverse_proxy' || die \
"reverse_proxy is MISSING from kadran-caddy's module list. The check is void: this
is either not the expected binary or list-modules returned nothing.
The file-serving check below would mean nothing like this."

    serving_modules="$(printf '%s\n' "$caddy_modules" \
        | grep -E 'file_server|templates|caddyfs' || true)"
    [ -z "$serving_modules" ] || die \
"kadran-caddy contains FILE-SERVING modules:
$serving_modules
With this binary the reverse proxy could serve the directory holding
the TLS private keys. The build does not follow the exclusion list in
build/caddy/main.go — the K-050 boundary is VOID."

    say "K-050 boundary verified ($(printf '%s\n' "$caddy_modules" | grep -c '^') modules, no file serving)"

    # ── Yapılandırma ve birimler ────────────────────────────────────────

    vekil_once="$(vekil_parmak_izi)"

    install -d -m 0755 -o root -g root /etc/kadran
    install -m 0644 -o root -g root "$STAGE/caddy.json" /etc/kadran/caddy.json
    install -m 0644 -o root -g root "$STAGE/kadran-caddy.service" \
        /etc/systemd/system/kadran-caddy.service
    install -m 0644 -o root -g root "$STAGE/kadran-caddy-admin.socket" \
        /etc/systemd/system/kadran-caddy-admin.socket
    systemctl daemon-reload

    # ── :80/:443'ü başkası tutuyor mu? ──────────────────────────────────
    #
    # Dağıtımın kendi caddy'si ya da bir nginx çalışıyorsa kadran-caddy
    # bağlanamaz ve "address already in use" ile ölür. Sebebi günlüğün
    # içinde kaybolmasın diye ÖNCEDEN ve açıkça söyleniyor.
    for other in caddy nginx apache2 httpd lighttpd; do
        if systemctl is-active --quiet "$other.service" 2>/dev/null; then
            die \
"$other.service is running and may hold ports 80/443.
kadran-caddy cannot bind them. To continue:
  systemctl disable --now $other.service"
        fi
    done

    # Soket ÖNCE: Caddy onu fd/3 olarak devralıyor.
    #
    # `enable` ile `start` AYRI şeyler — yalnızca başlatmak, birimi yeniden
    # başlatmadan sonra geri getirmez. Bu ayrım gerçek bir kurulumda
    # atlandı ve ancak reboot testinde ortaya çıktı; ikisi de yapılıyor ve
    # ikisi de aşağıda DOĞRULANIYOR.
    systemctl enable kadran-caddy-admin.socket
    systemctl enable kadran-caddy.service
}

ters_vekil_baslat() {
    getent group kadran-caddy >/dev/null || groupadd --system kadran-caddy

    if ! id -u kadran-caddy >/dev/null 2>&1; then
        useradd --system --gid kadran-caddy \
            --home-dir /var/lib/kadran-caddy --no-create-home \
            --shell "$NOLOGIN" \
            --comment "Kadran reverse proxy" kadran-caddy
    fi

    # Değişmez: ters vekil `kadran` GRUBUNDA OLAMAZ.
    #
    # Girseydi /run/kadran-exec/exec.sock'a (0660 root:kadran) ulaşırdı; yani
    # internete bakan süreç ayrıcalıklı executor'a konuşabilirdi. kadrand'nin
    # admin soketine erişimi grup ÜYELİĞİYLE değil, SOKETİN grup sahipliğiyle
    # sağlanıyor.
    if id -nG kadran-caddy | tr ' ' '\n' | grep -qx kadran; then
        die \
"user kadran-caddy is in the 'kadran' group. As it is, it can reach exec.sock
— the internet-facing process could talk to the privileged executor.
To fix:  gpasswd -d kadran-caddy kadran"
    fi

    install -m 0644 -o root -g root "$STAGE/kadran-caddy-tmpfiles.conf" \
        /etc/tmpfiles.d/kadran-caddy.conf
    systemd-tmpfiles --create /etc/tmpfiles.d/kadran-caddy.conf

    # Yeniden kurulumda HİÇBİR ŞEY değişmediyse ters vekile dokunulmuyor.
    # Taze sunucu testinde (K-112) ikinci kurulum onu koşulsuz yeniden
    # başlattı ve Caddy rotasız açıldı; site kadrand yeniden başlayana kadar
    # KAPALI kaldı (bir sonraki kurulumda kadrand yeniden başlayınca döndü).
    # Rotaları geri getirmek kadrand'nin işi (K-055, vekil izleyicisi); ama
    # gereksiz yeniden başlatma yine de kesinti demek. İkili, yapılandırma
    # ya da birim değiştiyse yeniden başlatma şart. İkili commit'ten BAĞIMSIZ
    # derleniyor (scripts/build-caddy.sh, -buildvcs=false); öyle olmasaydı
    # Caddy'ye dokunmayan her yükseltme de onu "değişmiş" sayardı (ölçüldü).
    if [ "$vekil_once" = "$(vekil_parmak_izi)" ] \
            && systemctl is-active --quiet kadran-caddy-admin.socket \
            && systemctl is-active --quiet kadran-caddy.service \
            && calisan_ayni_mi kadran-caddy.service "$LIB_DIR/kadran-caddy"; then
        say "reverse proxy unchanged — not restarted, traffic not interrupted"
    else
        systemctl stop kadran-caddy.service 2>/dev/null || true
        systemctl restart kadran-caddy-admin.socket
        systemctl restart kadran-caddy.service
    fi
}

ters_vekil_kur() {
    ters_vekil_hazirla
    ters_vekil_baslat
}

if [ "$GOC" -eq 0 ]; then
    ters_vekil_kur
fi

# ── SSH yapılandırması ───────────────────────────────────────────────

step "SSH configuration"

[ -f "$STAGE/client_key.pub" ] || die "the client public key is missing from the staging directory"

ssh_dizini_hazirla "$CLIENT_HOME/.ssh" kadran-client kadran-client

# authorized_keys satırı:
#
#   command="..."  → istemci ne isterse istesin YALNIZCA bu çalışır.
#                    SSH_ORIGINAL_COMMAND kadran-connect tarafından
#                    kasten yok sayılıyor.
#   restrict       → pty, port yönlendirme, ajan yönlendirme, X11,
#                    user-rc: hepsi kapalı.
#
# Soket yönlendirmesi yerine zorlanmış komut kullanılmasının gerekçesi
# docs/decisions.md K-003'te: unix soketi yönlendirmesini açmak
# `port-forwarding` iznini gerektirir ve bu, istemciye sunucudaki HER TCP
# portuna tünel açma yetkisi verirdi.
#
# Yazma kadran-client olarak yapılıyor; dosya o kullanıcının ve 0600 doğuyor
# (K-139). Root burada artık `chown -R` ve `chmod` koşturmuyor: `chmod` bağı
# izliyordu ve denetimden sonraki pencerede ulaşılabilirdi (K-137).
auth_file="$CLIENT_HOME/.ssh/authorized_keys"
yonetici_satiri_yaz "$auth_file" "$STAGE/client_key.pub" "$LIB_DIR" kadran-client kadran-client \
    || die "the client public key must be a single line — a second line would become a key without a forced command"

# sshd drop-in.
#
# ExposeAuthInfo, kimlik doğrulamada kullanılan anahtarı geçici bir dosyaya
# yazar ve yolunu oturuma SSH_USER_AUTH ile verir. kadran-connect denetim
# kaydının aktör kimliğini ORADAN okuyor; kapalıysa parmak izi sessizce boş
# kalır ve denetim izi "kim yaptı" sorusunu yanıtlayamaz. (Kod eskiden
# SSH_AUTH_INFO_0'ı okuyordu: PAM'in iç değişkeni, oturuma gelmiyor; canlıda
# parmak izi hiç kaydedilmemişti — K-134.)
if [ -d /etc/ssh/sshd_config.d ] && grep -qE '^\s*Include\s+/etc/ssh/sshd_config\.d/' /etc/ssh/sshd_config; then
    cat > "$SSHD_DROPIN" <<'SSHD'
# Kadran tarafından yönetiliyor. Elle düzenlemeyin.

# ── Denetim kimliğinin taklit edilmesini engelleyen satır ────────────
#
# kadran-connect, aktörün SSH parmak izini SSH_USER_AUTH'ın gösterdiği
# dosyadan okur. O değişkeni istemci belirleyebilirse denetim izi yalan
# söyler.
#
# authorized_keys'teki `environment="AD=deger"` seçeneği tam olarak bunu
# yapardı: sshd(8) bu seçenek için "override other default environment
# values" diyor — yani SSH_USER_AUTH sahte bir dosyayı gösterebilirdi.
# Kapatan ayar PermitUserEnvironment'tır.
#
# İKİ İNCE NOKTA:
#
#   1. Bunu `restrict` KAPATMAZ. sshd(8) restrict'i "disable port, agent
#      and X11 forwarding, as well as disabling PTY allocation and
#      execution of ~/.ssh/rc" diye tanımlar; ortam işleme o listede yok.
#      (docs/decisions.md K-031)
#
#   2. PermitUserEnvironment bir `Match` bloğunun İÇİNDE KULLANILAMAZ —
#      Match'in izin verdiği anahtar kelimeler arasında değildir. Match
#      altına yazmak sshd yapılandırmasını GEÇERSİZ kılar. Bu yüzden
#      genel kapsamda, Match'ten ÖNCE duruyor.
#
# Varsayılanı zaten `no`; yine de açıkça yazılıyor çünkü bu değer
# denetim izinin doğruluğunu taşıyor ve bir dağıtımın genel
# yapılandırmasına bırakılamaz.
PermitUserEnvironment no

# İstemcinin SendEnv ile gönderdiği değişkenler AcceptEnv ile süzülür.
# Varsayılanı "hiçbirini kabul etme" olduğu ve AcceptEnv eklemeli
# (additive) çalıştığı için — boş bir değerle sıfırlanamaz — burada
# BİLEREK hiç AcceptEnv satırı yazılmıyor. Yazılacak her isim yüzeyi
# yalnızca genişletirdi.

# ExposeAuthInfo olmadan denetim kaydındaki SSH parmak izi boş kalır.
Match User kadran-client
    ExposeAuthInfo yes
    PermitTTY no
    X11Forwarding no
    AllowAgentForwarding no
    AllowTcpForwarding no
    PermitTunnel no
SSHD
    chmod 0644 "$SSHD_DROPIN"
else
    die "no sshd_config.d directory or no Include line — \
configure it by hand (Match User kadran-client + ExposeAuthInfo yes)"
fi

# Yapılandırma BOZUKSA sshd'yi yeniden yüklemek bizi dışarıda bırakır.
# Önce doğrula.
"$SSHD_BIN" -t || die "invalid sshd configuration — the change was not applied"
systemctl reload ssh 2>/dev/null || systemctl reload sshd

say "forced command and ExposeAuthInfo configured"

# ── Servisler ────────────────────────────────────────────────────────

step "Services"

# Göçte executor BAŞLAMADAN: depo beyaz listesi eskisiyle aynı mı (K-136).
if [ "$GOC" -eq 1 ]; then
    goc_izinli_depo_dogrula
fi

systemctl enable kadran-exec.service kadrand.service

# Yeniden kurulum aynı zamanda YÜKSELTME yolu. `enable --now` ÇALIŞAN
# birimi yeniden başlatmıyor: taze sunucu testinde (K-112) ikinci
# kurulumdan sonra /proc/<pid>/exe → "…/kadrand (deleted)" — süreç
# diskten silinmiş ESKİ ikiliyi çalıştırıyordu ve kurulum "tamamlandı"
# diyordu. Kontrol düzlemi her kurulumda yeniden başlatılıyor; uygulama
# trafiği etkilenmez (ters vekil ve konteynerler ayrı). Executor ÖNCE:
# daemon açılışta ona bağlanıyor.
systemctl restart kadran-exec.service
systemctl restart kadrand.service

# Soketlerin belirmesi için kısa bir pencere.
for _ in $(seq 1 50); do
    [ -S /run/kadran/api.sock ] && break
    sleep 0.1
done

systemctl is-active --quiet kadran-exec.service || {
    journalctl -u kadran-exec.service -n 30 --no-pager >&2
    die "kadran-exec did not start"
}
systemctl is-active --quiet kadrand.service || {
    journalctl -u kadrand.service -n 30 --no-pager >&2
    die "kadrand did not start"
}

say "kadran-exec and kadrand are running"

# ── Göçün ikinci yarısı (K-136) ─────────────────────────────────────
#
# Yeni kontrol düzlemi çalışıyor ve eski konteynerlerin karşılıklarını
# kuruyor. Onlar ayağa kalkınca eski ters vekil yenisiyle değişir;
# kesinti yalnız bu adımda.
if [ "$GOC" -eq 1 ]; then
    goc_bekle
    ters_vekil_hazirla
    # kadrand eski vekil DURMADAN önce durur ve yenisi açılınca başlar:
    # açılış uzlaştırması rotaları hemen yazar. Çalışır bırakılsaydı rotaları
    # vekil izleyicisinin 10 sn'lik turu yazardı (GCP provası: 16 sn'lik
    # kesintinin 9,5 sn'si bu bekleyişti).
    systemctl stop kadrand.service
    goc_vekil
    ters_vekil_baslat
    systemctl start kadrand.service
    goc_secimli
    goc_rota_bekle
    goc_bitir
else
    secimli_guncelle
fi

# ── Kurulum sonrası doğrulama ────────────────────────────────────────
#
# Ürünün merkezî iddiası burada sınanıyor. Bu kontroller geçmiyorsa
# kurulum "başarılı" sayılmamalı.

step "Post-install verification"

fail=0
check_fail() { printf '  ✗ %s\n' "$*" >&2; fail=1; }
check_ok()   { printf '  ✓ %s\n' "$*"; }

# 1. kadrand root ÇALIŞMAMALI.
daemon_user="$(ps -o user= -C kadrand | head -1 | tr -d ' ')"
if [ "$daemon_user" = "kadran" ]; then
    check_ok "kadrand runs as an unprivileged user ($daemon_user)"
else
    check_fail "kadrand runs as '$daemon_user', expected 'kadran'"
fi

# 2. kadrand Docker'a ERİŞEMEMELİ — ama önce ölçümün ölçebildiği
#    kanıtlanıyor. `setpriv … docker ps` Docker HİÇ yokken de başarısız
#    oluyor (komut bulunamadı) ve bu kontrol "erişemiyor" diye GEÇİYORDU
#    (taze sunucu testi, K-112). Root ulaşamıyorsa kadran'ın ulaşamaması
#    hiçbir şey kanıtlamaz.
if ! docker ps >/dev/null 2>&1; then
    check_fail "root cannot reach Docker either — privilege separation COULD NOT BE MEASURED"
elif setpriv --reuid kadran --regid kadran --clear-groups docker ps >/dev/null 2>&1; then
    check_fail "user kadran can reach Docker — privilege separation has COLLAPSED"
else
    check_ok "user kadran cannot reach Docker"
fi

# 3. Soket izinleri.
api_mode="$(stat -c '%a %U:%G' /run/kadran/api.sock 2>/dev/null || echo missing)"
if [ "$api_mode" = "660 kadran:kadran-client" ]; then
    check_ok "api.sock: $api_mode"
else
    check_fail "api.sock unexpected: $api_mode (expected 660 kadran:kadran-client)"
fi

exec_mode="$(stat -c '%a %U:%G' /run/kadran-exec/exec.sock 2>/dev/null || echo missing)"
if [ "$exec_mode" = "660 root:kadran" ]; then
    check_ok "exec.sock: $exec_mode"
else
    check_fail "exec.sock unexpected: $exec_mode (expected 660 root:kadran)"
fi

# 4. İstemci kullanıcısı exec.sock'a ULAŞAMAMALI.
if setpriv --reuid kadran-client --regid kadran-client --clear-groups \
        test -r /run/kadran-exec/exec.sock 2>/dev/null; then
    check_fail "kadran-client can read exec.sock — kadrand can be bypassed"
else
    check_ok "kadran-client cannot reach exec.sock"
fi

# 4b. Daemon, executor'ın denetim günlüğünün DİZİNİNE yazamamalı.
#
# Dosyanın izni yetmez: yazılabilir bir dizindeki root dosyası silinip
# yerine başkası konabilir (K-100). Çalışan executor'ın açık tuttuğu
# günlük de sınanıyor — bir drop-in eski yolu geri getirmiş olabilir.
if setpriv --reuid kadran --regid kadran --clear-groups \
        test -w "$(dirname "$new_journal")" 2>/dev/null; then
    check_fail "kadran can write to the executor log's directory — the privileged record can be changed"
else
    check_ok "kadran cannot write to the executor log's directory"
fi
exec_pid="$(systemctl show -p MainPID --value kadran-exec.service)"
exec_fds="$(ls -l "/proc/$exec_pid/fd" 2>/dev/null || true)"
if [[ "$exec_fds" == *"-> $new_journal"$'\n'* || "$exec_fds" == *"-> $new_journal" ]]; then
    check_ok "the executor keeps its log at $new_journal"
else
    check_fail "the running executor does not hold $new_journal open"
fi

# 5. İstemci kullanıcısı kabuk ALMAMALI: HER satır zorlanmış komutlu.
kisitsiz="$(kisitsiz_satir_sayisi "$auth_file" "$LIB_DIR")"
if ! grep -q 'command="' "$auth_file"; then
    check_fail "no forced command in authorized_keys — the client could get a shell"
elif [ "$kisitsiz" != 0 ]; then
    check_fail "$kisitsiz lines in authorized_keys are not forced to kadran-connect — the client could get a shell"
else
    check_ok "every line in authorized_keys has a forced command"
fi

# ── Ters vekil ──────────────────────────────────────────────────────

# 6. Birimler ETKİN olmalı, yalnızca çalışıyor olmaları YETMEZ.
#
# Bu ayrım gerçek bir kurulumda atlandı: soket başlatılmış ama
# etkinleştirilmemişti ve yalnızca REBOOT testinde ortaya çıktı. "Şu an
# çalışıyor" bir kabul ölçütü değil; ölçüt "yeniden başlatmadan sonra da
# çalışır".
for unit in kadran-caddy-admin.socket kadran-caddy.service; do
    state="$(systemctl is-enabled "$unit" 2>/dev/null || echo missing)"
    if [ "$state" = "enabled" ]; then
        check_ok "$unit enabled (survives a reboot)"
    else
        check_fail "$unit NOT ENABLED ($state) — it will not come back after a reboot"
    fi
done

# 6b. Kontrol düzleminin çalışan imajı da kurulan ikili olmalı (K-112).
#     Önceden yalnızca ters vekil için ölçülüyordu; kadrand ve executor
#     yeniden kurulumda silinmiş eski ikiliyle çalışmaya devam ediyordu
#     ve hiçbir kontrol bunu görmedi.
calisan_ikili_dogrula() {
    if calisan_ayni_mi "$1.service" "$LIB_DIR/$1"; then
        check_ok "running $1 is the installed binary ($(md5sum "$LIB_DIR/$1" | cut -c1-12))"
    else
        check_fail "running $1 is NOT the installed binary — the old process is up or nothing runs"
    fi
}
calisan_ikili_dogrula kadrand
calisan_ikili_dogrula kadran-exec

# 7. Çalışan İMAJ, kurduğumuz binary olmalı (K-049).
#
# `systemctl is-active` yeni ikilinin çalıştığını KANITLAMAZ: eski süreç
# ayakta kalmışsa birim yine "active" görünür. Kanıt /proc/<pid>/exe'den
# okunuyor — çalışan imajın kendisi.
caddy_pid="$(systemctl show -p MainPID --value kadran-caddy.service 2>/dev/null || echo 0)"
if [ "${caddy_pid:-0}" -gt 0 ] 2>/dev/null \
        && running_sum="$(md5sum "/proc/$caddy_pid/exe" 2>/dev/null | cut -d' ' -f1)" \
        && [ -n "$running_sum" ]; then
    installed_sum="$(md5sum "$LIB_DIR/kadran-caddy" | cut -d' ' -f1)"
    if [ "$running_sum" = "$installed_sum" ]; then
        check_ok "running reverse proxy is the installed binary (${running_sum:0:12})"
    else
        check_fail "running reverse proxy is ANOTHER binary (running $running_sum, installed $installed_sum)"
    fi
else
    check_fail "could not read the reverse proxy's running image (pid=${caddy_pid:-none})"
fi

# 8. Ters vekil root ÇALIŞMAMALI.
caddy_user="$(ps -o user= -p "${caddy_pid:-0}" 2>/dev/null | tr -d ' ')"
if [ "$caddy_user" = "kadran-caddy" ]; then
    check_ok "reverse proxy runs as an unprivileged user ($caddy_user)"
else
    check_fail "reverse proxy runs as '$caddy_user', expected 'kadran-caddy'"
fi

# 9. Admin soketinin izinleri.
admin_mode="$(stat -c '%a %U:%G' /run/kadran-caddy/admin.sock 2>/dev/null || echo missing)"
if [ "$admin_mode" = "660 kadran-caddy:kadran" ]; then
    check_ok "admin.sock: $admin_mode"
else
    check_fail "admin.sock unexpected: $admin_mode (expected 660 kadran-caddy:kadran)"
fi

# 10. kadrand admin soketine ULAŞABİLMELİ — K-050'nin dayandığı erişim.
if setpriv --reuid kadran --regid kadran --clear-groups \
        test -w /run/kadran-caddy/admin.sock 2>/dev/null; then
    check_ok "user kadran can write to the admin socket"
else
    check_fail "user kadran CANNOT write to the admin socket — the reverse proxy cannot be managed"
fi

# 11. Ters vekil executor'a ULAŞAMAMALI. Modelin can alıcı noktası:
#     internete bakan süreç ayrıcalıklı soketi görmemeli.
if setpriv --reuid kadran-caddy --regid kadran-caddy --clear-groups \
        test -r /run/kadran-exec/exec.sock 2>/dev/null; then
    check_fail "the reverse proxy can read exec.sock — the privileged executor faces the internet"
else
    check_ok "the reverse proxy cannot reach exec.sock"
fi

# 12. Kasa anahtarı yalnız root'un (K-123). Daemon okuyabilseydi kasa
#     hiçbir şeyi korumazdı. Kontrol: root okuyabiliyor, yani "kadran
#     okuyamıyor" sonucu dosyanın yokluğundan gelmiyor.
vault_kip="$(stat -c '%a %U:%G' "$VAULT_KEY" 2>/dev/null || echo missing)"
if [ "$vault_kip" = "600 root:root" ] && test -r "$VAULT_KEY" \
   && ! setpriv --reuid kadran --regid kadran --clear-groups test -r "$VAULT_KEY" 2>/dev/null; then
    check_ok "the vault key is root's only ($vault_kip), kadran cannot read it"
else
    check_fail "vault key is $vault_kip — it must be 600 root:root and unreadable to kadran"
fi

# 13. Kasanın açık anahtarı root'un; daemon okuyabiliyor ama değiştiremiyor.
#     Değiştirebilseydi yeni değerleri kendi anahtarına mühürletebilirdi.
#     Dosyanın izni yetmez: dizinine yazabilen dosyayı silip yerine
#     başkasını koyar (K-100), o yüzden /etc/kadran da sınanıyor.
pub_kip="$(stat -c '%a %U:%G' "$VAULT_PUB" 2>/dev/null || echo missing)"
if [ "$pub_kip" = "644 root:root" ] \
   && setpriv --reuid kadran --regid kadran --clear-groups test -r "$VAULT_PUB" 2>/dev/null \
   && ! setpriv --reuid kadran --regid kadran --clear-groups test -w "$VAULT_PUB" 2>/dev/null \
   && ! setpriv --reuid kadran --regid kadran --clear-groups test -w "$(dirname "$VAULT_PUB")" 2>/dev/null; then
    check_ok "vault public key is $pub_kip, kadran reads it but can change neither it nor its directory"
else
    check_fail "vault public key is $pub_kip — it must be 644 root:root and kadran must not write to its directory"
fi

[ "$fail" -eq 0 ] || die "post-install verification failed — see above"

# Eski adlı Docker kalıntıları (konteyner, ağ, imaj etiketi; K-136) yalnızca
# hiçbiri trafik almıyorsa kaldırılır. Göçten AYRI: o kurulumda
# kaldırılamadıysa sonraki her kurulum yeniden dener. Doğrulamadan SONRA:
# temizlik kontrollerin önüne geçmesin, doğrulama düşerse eski konteynerler
# (son çalışan sürüm) yerinde kalsın.
if goc_artik_var; then
    goc_temizle
fi

# ── Kasanın dayandığı iki koşul: uyarı, hata değil (K-123) ──────────
#
# Kasa geçmiş değerleri ele geçirilmiş bir daemon'a karşı ancak daemon'un
# derleyebildiği depolar kısıtlıyken koruyor: kısıt yoksa kendi seçtiği bir
# imajı bir uygulama olarak başlatıp o uygulamanın değerlerini günlüğe
# yazdırabilir (güvenlik incelemesi). Taze kurulumda depo listesi henüz yok;
# kurulumu düşürmek yerine söyleniyor.
exec_bayraklari="$(systemctl show -p ExecStart kadran-exec.service)"
if [[ "$exec_bayraklari" != *--allow-repo* ]]; then
    printf '\n⚠ kadran-exec has no --allow-repo list. The vault does not protect past values\n'
    printf '  if a compromised daemon installs an image from another repository.\n'
    printf '  Add one: see the note at the top of deploy/systemd/kadran-exec.service.\n'
fi
# Kasadan önceki göç kopyaları düz değer taşıyor ve kadran'ın: daemon onları
# okuyabilir. Kurulum silmiyor (geri alma yolu olabilirler), her kurulumda
# hatırlatıyor.
duz_kopyalar="$(find "$STATE_DIR" -maxdepth 1 -name 'kadran.db.pre-*' -type f 2>/dev/null | sort)"
if [ -n "$duz_kopyalar" ]; then
    printf '\n⚠ These migration copies may hold PLAIN values from before the vault, and the\n'
    printf '  daemon can read them. If your sites work, delete them:\n'
    printf '    sudo rm %s\n' $duz_kopyalar
fi

printf '\nInstall complete.\n'
printf 'Root access is no longer needed; to connect:\n'
printf '  kadran status kadran-client@<server>\n'
