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

STAGE="${1:?kullanım: install.sh <hazırlık-dizini>}"

LIB_DIR=/usr/local/lib/kadran
STATE_DIR=/var/lib/kadran
CLIENT_HOME=/var/lib/kadran-client
SSHD_DROPIN=/etc/ssh/sshd_config.d/60-kadran.conf

say()  { printf '  %s\n' "$*"; }
step() { printf '\n==> %s\n' "$*"; }
die()  { printf '\nHATA: %s\n' "$*" >&2; exit 1; }

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

# yonetici_satiri_yaz <authorized_keys> <açık anahtar dosyası> <LIB_DIR>
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
yonetici_satiri_yaz() {
    local auth_file="$1" key_file="$2" lib_dir="$3" key key_body
    [ "$(grep -c '' "$key_file")" -eq 1 ] || return 1
    key="$(cat "$key_file")"
    # CR denetimi Linux'ta anlamlı (kurulum ve CI orada). Git Bash hem
    # grep'te hem `$(…)`'da CR'yi siliyor (ölçüldü); orada bu senaryo
    # sınanamaz.
    case "$key" in *$'\r'*) return 1 ;; esac
    key_body="$(printf '%s' "$key" | awk '{print $1" "$2}')"
    touch "$auth_file"
    if [ -s "$auth_file" ] && grep -qF "$key_body" "$auth_file"; then
        grep -vF "$key_body" "$auth_file" > "$auth_file.yeni" || true
        mv "$auth_file.yeni" "$auth_file"
    fi
    printf '%s\n' "command=\"$lib_dir/kadran-connect\",restrict $key" >> "$auth_file"
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

step "Ön koşullar"

[ "$(id -u)" -eq 0 ] || die "bu betik root olarak çalışmalı (root'a SSH kapalıysa: kadran bootstrap -sudo kullanıcı@sunucu)"
command -v systemctl >/dev/null || die "systemd bulunamadı — desteklenmiyor"
command -v sshd >/dev/null || command -v /usr/sbin/sshd >/dev/null \
    || die "sshd bulunamadı"

SSHD_BIN="$(command -v sshd || echo /usr/sbin/sshd)"

# Docker README'de ön koşul. Taze sunucu testinde (K-112) betik ona hiç
# bakmıyordu: Docker'sız bir sunucuda kullanıcılar ve birimler kuruluyor,
# hata ancak sonda, sebebi belirsiz bir kontrolle görünebilirdi.
command -v docker >/dev/null \
    || die "Docker Engine bulunamadı — önce kurun (Ubuntu: apt-get install -y docker.io)"
docker version --format '{{.Server.Version}}' >/dev/null 2>&1 \
    || die "Docker kurulu ama daemon cevap vermiyor — systemctl status docker"

# nologin yolu dağıtıma göre değişiyor.
NOLOGIN="$(command -v nologin || echo /usr/sbin/nologin)"

say "sistem uygun ($(uname -m), $( (. /etc/os-release && echo "$PRETTY_NAME") 2>/dev/null || echo bilinmiyor ))"

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

step "Gruplar ve kullanıcılar"

getent group kadran >/dev/null || groupadd --system kadran
getent group kadran-client >/dev/null || groupadd --system kadran-client

# kadrand'nin çalıştığı yetkisiz kullanıcı. Giriş yapmaz.
if ! id -u kadran >/dev/null 2>&1; then
    useradd --system --gid kadran \
        --home-dir "$STATE_DIR" --no-create-home \
        --shell "$NOLOGIN" \
        --comment "Kadran kontrol düzlemi" kadran
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
        --comment "Kadran istemci erişimi" kadran-client
fi

# ── Değişmez doğrulaması ─────────────────────────────────────────────
#
# Kullanıcılar önceden (yanlış) oluşturulmuş olabilir. Sessizce kabul
# etmek yerine kontrol ediyoruz: bu iki koşul modelin dayandığı yer.

step "Yetki değişmezleri"

primary="$(id -gn kadran-client)"
[ "$primary" = "kadran-client" ] || die \
"kadran-client kullanıcısının birincil grubu '$primary', 'kadran-client' olmalı.
SO_PEERCRED yalnızca birincil grubu bildirir; bu hâliyle her bağlantı
sessizce reddedilir. Düzeltmek için:  usermod -g kadran-client kadran-client"

if id -nG kadran-client | tr ' ' '\n' | grep -qx kadran; then
    die \
"kadran-client kullanıcısı 'kadran' grubunda. Bu hâliyle exec.sock'a
doğrudan ulaşır ve kadrand tamamen atlanabilir — ayrıcalık ayrımı çöker.
Düzeltmek için:  gpasswd -d kadran-client kadran"
fi

if id -nG kadran | tr ' ' '\n' | grep -qx docker; then
    die \
"kadran kullanıcısı 'docker' grubunda. Docker soketine erişim pratikte
root yetkisidir; bu hâliyle executor ayrımı dekoratif kalır.
Düzeltmek için:  gpasswd -d kadran docker"
fi

say "kadran-client birincil grubu: $primary"
say "kadran-client ek grupları: $(id -nG kadran-client)"
say "kadran ek grupları: $(id -nG kadran)"

# ── Binary'ler ───────────────────────────────────────────────────────

step "Binary'ler"

install -d -m 0755 -o root -g root "$LIB_DIR"

for binary in kadrand kadran-exec kadran-connect kadran-caddy; do
    [ -f "$STAGE/$binary" ] || die "$binary hazırlık dizininde yok"
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

say "$LIB_DIR içine kuruldu"
"$LIB_DIR/kadrand" -version || die "kadrand çalıştırılamadı — mimari uyuşmuyor olabilir"

# ── Dizinler ─────────────────────────────────────────────────────────

step "Dizinler"

install -m 0644 -o root -g root "$STAGE/kadran-tmpfiles.conf" /etc/tmpfiles.d/kadran.conf
systemd-tmpfiles --create /etc/tmpfiles.d/kadran.conf

# /var/lib/kadran tmpfiles ile yaratılıyor ama yeniden başlatma arasında
# kalıcı olması gerekiyor; burada da garantiye alıyoruz.
install -d -m 0750 -o kadran -g kadran "$STATE_DIR"

say "/run/kadran, /run/kadran-exec, $STATE_DIR hazır"

# ── systemd birimleri ────────────────────────────────────────────────

step "systemd birimleri"

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
    die "kadran-exec'in etkin ExecStart'ı günlüğü hâlâ $old_journal olarak gösteriyor.
Büyük ihtimalle bir drop-in (systemctl cat kadran-exec). Düzelt:
  --journal $new_journal
sonra kurulumu yeniden çalıştır. Günlük TAŞINMADI."
fi

if [ -e "$old_journal" ]; then
    [ -e "$new_journal" ] && die "iki günlük birden var: $old_journal ve $new_journal
Hangisinin gerçek zincir olduğuna elle karar verilmeli; hiçbiri silinmedi."

    # Executor günlüğü açılışta bir kez açıp tanımlayıcıyı tutuyor.
    # Çalışırken taşınırsa eski inode'a yazmaya devam eder; bu yüzden
    # önce durdurulur. Aşağıdaki `enable --now` onu yeni yolla başlatır.
    systemctl stop kadran-exec.service 2>/dev/null || true
    mv "$old_journal" "$new_journal"
    chown root:root "$new_journal"
    chmod 0640 "$new_journal"
    say "executor denetim günlüğü $new_journal konumuna taşındı"
fi

# Hacim kökü nodev,nosuid ile bağlanır. Birim ÖNCE etkinleştirilir ki
# yeniden başlatmadan sonra da bağlansın; `enable` tek başına şimdi
# bağlamaz, bu yüzden `start` da çağrılır (ikisi de idempotent).
systemctl enable var-lib-kadran-volumes.mount >/dev/null 2>&1 || true
systemctl restart var-lib-kadran-volumes.mount \
    || die "hacim kökü sertleştirilemedi — uygulama hacimleri nodev,nosuid olmadan bağlanırdı"

# Birimin AKTİF olması yetmez: `Options=` sessizce yok sayılsaydı birim
# yine "active" görünürdü. Etkin bayraklar ÇEKİRDEKTEN okunur.
#
# Bu kontrolün var olma sebebi ölçülmüş bir yanlıştır: Docker'ın local
# sürücüsüne aynı seçenekler verildiğinde hacim sertleştirilmeden
# bağlanıyor ve hiçbir hata üretmiyor (docs/decisions.md K-038).
vol_opts="$(awk '$5=="/var/lib/kadran/volumes"{print $6}' /proc/self/mountinfo | head -1)"
for flag in nodev nosuid; do
    printf '%s' "$vol_opts" | tr ',' '\n' | grep -qx "$flag" \
        || die "hacim kökünde $flag ETKİN DEĞİL (etkin: ${vol_opts:-<bağlı değil>})"
done
say "hacim kökü sertleştirildi ($vol_opts)"

# ── Ters vekil (kadran-caddy) ────────────────────────────────────────
#
# Dağıtımın `caddy` paketine BAĞLANILMIYOR; gerekçe
# deploy/systemd/kadran-caddy.service'in başında. Kurulan her şey depodan
# geliyor: birim, soket, tmpfiles kuralı ve yol açıcı yapılandırma.

# ters_vekil_kur — ters vekili kurar ya da yükseltir. Taze kurulum ve
# yükseltmede yerinde çağrılır. Göçte (K-136) kontrol düzlemi yeni
# konteynerleri kurduktan SONRA çağrılır: eski vekil o ana kadar siteyi
# sunmaya devam eder (bkz. goc.sh).
ters_vekil_kur() {
    step "Ters vekil"

    getent group kadran-caddy >/dev/null || groupadd --system kadran-caddy

    if ! id -u kadran-caddy >/dev/null 2>&1; then
        useradd --system --gid kadran-caddy \
            --home-dir /var/lib/kadran-caddy --no-create-home \
            --shell "$NOLOGIN" \
            --comment "Kadran ters vekili" kadran-caddy
    fi

    # Değişmez: ters vekil `kadran` GRUBUNDA OLAMAZ.
    #
    # Girseydi /run/kadran-exec/exec.sock'a (0660 root:kadran) ulaşırdı; yani
    # internete bakan süreç ayrıcalıklı executor'a konuşabilirdi. kadrand'nin
    # admin soketine erişimi grup ÜYELİĞİYLE değil, SOKETİN grup sahipliğiyle
    # sağlanıyor.
    if id -nG kadran-caddy | tr ' ' '\n' | grep -qx kadran; then
        die \
"kadran-caddy kullanıcısı 'kadran' grubunda. Bu hâliyle exec.sock'a
ulaşabilir — internete bakan süreç ayrıcalıklı executor'a konuşabilir.
Düzeltmek için:  gpasswd -d kadran-caddy kadran"
    fi

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
        || die "kadran-caddy çalıştırılamadı — mimari uyuşmuyor olabilir"

    printf '%s\n' "$caddy_modules" | grep -qx 'http.handlers.reverse_proxy' || die \
"kadran-caddy modül listesinde reverse_proxy YOK. Ölçüm geçersiz: bu
binary ya beklenen ikili değil ya da list-modules bir şey döndürmedi.
Aşağıdaki dosya-servisi kontrolü bu hâliyle anlamsız olurdu."

    serving_modules="$(printf '%s\n' "$caddy_modules" \
        | grep -E 'file_server|templates|caddyfs' || true)"
    [ -z "$serving_modules" ] || die \
"kadran-caddy DOSYA SERVİS EDEN modüller içeriyor:
$serving_modules
Bu binary ile ters vekil, TLS özel anahtarlarının durduğu dizini
servis edebilir. Derleme build/caddy/main.go'daki dışlama listesine
uymuyor — K-050 sınırı ETKİSİZ."

    say "K-050 sınırı doğrulandı ($(printf '%s\n' "$caddy_modules" | grep -c '^') modül, dosya servisi yok)"

    # ── Yapılandırma ve birimler ────────────────────────────────────────

    vekil_once="$(vekil_parmak_izi)"

    install -d -m 0755 -o root -g root /etc/kadran
    install -m 0644 -o root -g root "$STAGE/caddy.json" /etc/kadran/caddy.json

    install -m 0644 -o root -g root "$STAGE/kadran-caddy-tmpfiles.conf" \
        /etc/tmpfiles.d/kadran-caddy.conf
    systemd-tmpfiles --create /etc/tmpfiles.d/kadran-caddy.conf

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
"$other.service çalışıyor ve 80/443 portlarını tutuyor olabilir.
kadran-caddy bu portlara bağlanamaz. Devam etmek için:
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
        say "ters vekil değişmedi — yeniden başlatılmadı, trafik kesilmedi"
    else
        systemctl stop kadran-caddy.service 2>/dev/null || true
        systemctl restart kadran-caddy-admin.socket
        systemctl restart kadran-caddy.service
    fi
}

if [ "$GOC" -eq 0 ]; then
    ters_vekil_kur
fi

# ── SSH yapılandırması ───────────────────────────────────────────────

step "SSH yapılandırması"

[ -f "$STAGE/client_key.pub" ] || die "istemci açık anahtarı hazırlık dizininde yok"

install -d -m 0700 -o kadran-client -g kadran-client "$CLIENT_HOME/.ssh"

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
auth_file="$CLIENT_HOME/.ssh/authorized_keys"
yonetici_satiri_yaz "$auth_file" "$STAGE/client_key.pub" "$LIB_DIR" \
    || die "istemci açık anahtarı tek satır olmalı — ikinci satır zorlanmış komutsuz bir anahtar olurdu"

chown -R kadran-client:kadran-client "$CLIENT_HOME/.ssh"
chmod 0600 "$auth_file"

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
    die "sshd_config.d dizini yok veya Include satırı bulunamadı — \
elle yapılandırma gerekiyor (Match User kadran-client + ExposeAuthInfo yes)"
fi

# Yapılandırma BOZUKSA sshd'yi yeniden yüklemek bizi dışarıda bırakır.
# Önce doğrula.
"$SSHD_BIN" -t || die "sshd yapılandırması geçersiz — değişiklik uygulanmadı"
systemctl reload ssh 2>/dev/null || systemctl reload sshd

say "zorlanmış komut ve ExposeAuthInfo yapılandırıldı"

# ── Servisler ────────────────────────────────────────────────────────

step "Servisler"

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
    die "kadran-exec başlamadı"
}
systemctl is-active --quiet kadrand.service || {
    journalctl -u kadrand.service -n 30 --no-pager >&2
    die "kadrand başlamadı"
}

say "kadran-exec ve kadrand çalışıyor"

# ── Göçün ikinci yarısı (K-136) ─────────────────────────────────────
#
# Yeni kontrol düzlemi çalışıyor ve eski konteynerlerin karşılıklarını
# kuruyor. Onlar ayağa kalkınca eski ters vekil yenisiyle değişir;
# kesinti yalnız bu adımda.
if [ "$GOC" -eq 1 ]; then
    goc_bekle
    goc_vekil
    ters_vekil_kur
    goc_secimli
    goc_temizle
fi

# ── Kurulum sonrası doğrulama ────────────────────────────────────────
#
# Ürünün merkezî iddiası burada sınanıyor. Bu kontroller geçmiyorsa
# kurulum "başarılı" sayılmamalı.

step "Kurulum sonrası doğrulama"

fail=0
check_fail() { printf '  ✗ %s\n' "$*" >&2; fail=1; }
check_ok()   { printf '  ✓ %s\n' "$*"; }

# 1. kadrand root ÇALIŞMAMALI.
daemon_user="$(ps -o user= -C kadrand | head -1 | tr -d ' ')"
if [ "$daemon_user" = "kadran" ]; then
    check_ok "kadrand yetkisiz kullanıcı olarak çalışıyor ($daemon_user)"
else
    check_fail "kadrand '$daemon_user' olarak çalışıyor, 'kadran' bekleniyordu"
fi

# 2. kadrand Docker'a ERİŞEMEMELİ — ama önce ölçümün ölçebildiği
#    kanıtlanıyor. `setpriv … docker ps` Docker HİÇ yokken de başarısız
#    oluyor (komut bulunamadı) ve bu kontrol "erişemiyor" diye GEÇİYORDU
#    (taze sunucu testi, K-112). Root ulaşamıyorsa kadran'ın ulaşamaması
#    hiçbir şey kanıtlamaz.
if ! docker ps >/dev/null 2>&1; then
    check_fail "root da Docker'a ulaşamıyor — ayrıcalık ayrımı ÖLÇÜLEMEDİ"
elif setpriv --reuid kadran --regid kadran --clear-groups docker ps >/dev/null 2>&1; then
    check_fail "kadran kullanıcısı Docker'a erişebiliyor — ayrıcalık ayrımı ÇÖKMÜŞ"
else
    check_ok "kadran kullanıcısı Docker'a erişemiyor"
fi

# 3. Soket izinleri.
api_mode="$(stat -c '%a %U:%G' /run/kadran/api.sock 2>/dev/null || echo yok)"
if [ "$api_mode" = "660 kadran:kadran-client" ]; then
    check_ok "api.sock: $api_mode"
else
    check_fail "api.sock beklenmedik: $api_mode (660 kadran:kadran-client bekleniyordu)"
fi

exec_mode="$(stat -c '%a %U:%G' /run/kadran-exec/exec.sock 2>/dev/null || echo yok)"
if [ "$exec_mode" = "660 root:kadran" ]; then
    check_ok "exec.sock: $exec_mode"
else
    check_fail "exec.sock beklenmedik: $exec_mode (660 root:kadran bekleniyordu)"
fi

# 4. İstemci kullanıcısı exec.sock'a ULAŞAMAMALI.
if setpriv --reuid kadran-client --regid kadran-client --clear-groups \
        test -r /run/kadran-exec/exec.sock 2>/dev/null; then
    check_fail "kadran-client exec.sock'u okuyabiliyor — kadrand atlanabilir"
else
    check_ok "kadran-client exec.sock'a erişemiyor"
fi

# 4b. Daemon, executor'ın denetim günlüğünün DİZİNİNE yazamamalı.
#
# Dosyanın izni yetmez: yazılabilir bir dizindeki root dosyası silinip
# yerine başkası konabilir (K-100). Çalışan executor'ın açık tuttuğu
# günlük de sınanıyor — bir drop-in eski yolu geri getirmiş olabilir.
if setpriv --reuid kadran --regid kadran --clear-groups \
        test -w "$(dirname "$new_journal")" 2>/dev/null; then
    check_fail "kadran, executor günlüğünün dizinine yazabiliyor — ayrıcalıklı kayıt değiştirilebilir"
else
    check_ok "kadran executor günlüğünün dizinine yazamıyor"
fi
exec_pid="$(systemctl show -p MainPID --value kadran-exec.service)"
exec_fds="$(ls -l "/proc/$exec_pid/fd" 2>/dev/null || true)"
if [[ "$exec_fds" == *"-> $new_journal"$'\n'* || "$exec_fds" == *"-> $new_journal" ]]; then
    check_ok "executor günlüğü $new_journal konumunda tutuyor"
else
    check_fail "çalışan executor $new_journal dosyasını açık tutmuyor"
fi

# 5. İstemci kullanıcısı kabuk ALMAMALI: HER satır zorlanmış komutlu.
kisitsiz="$(kisitsiz_satir_sayisi "$auth_file" "$LIB_DIR")"
if ! grep -q 'command="' "$auth_file"; then
    check_fail "authorized_keys'te zorlanmış komut yok — istemci kabuk alabilir"
elif [ "$kisitsiz" != 0 ]; then
    check_fail "authorized_keys'te $kisitsiz satır kadran-connect'e zorlanmamış — istemci kabuk alabilir"
else
    check_ok "authorized_keys'in her satırı zorlanmış komutlu"
fi

# ── Ters vekil ──────────────────────────────────────────────────────

# 6. Birimler ETKİN olmalı, yalnızca çalışıyor olmaları YETMEZ.
#
# Bu ayrım gerçek bir kurulumda atlandı: soket başlatılmış ama
# etkinleştirilmemişti ve yalnızca REBOOT testinde ortaya çıktı. "Şu an
# çalışıyor" bir kabul ölçütü değil; ölçüt "yeniden başlatmadan sonra da
# çalışır".
for unit in kadran-caddy-admin.socket kadran-caddy.service; do
    state="$(systemctl is-enabled "$unit" 2>/dev/null || echo yok)"
    if [ "$state" = "enabled" ]; then
        check_ok "$unit etkin (yeniden başlatmayı geçer)"
    else
        check_fail "$unit ETKİN DEĞİL ($state) — reboot sonrası geri gelmez"
    fi
done

# 6b. Kontrol düzleminin çalışan imajı da kurulan ikili olmalı (K-112).
#     Önceden yalnızca ters vekil için ölçülüyordu; kadrand ve executor
#     yeniden kurulumda silinmiş eski ikiliyle çalışmaya devam ediyordu
#     ve hiçbir kontrol bunu görmedi.
calisan_ikili_dogrula() {
    if calisan_ayni_mi "$1.service" "$LIB_DIR/$1"; then
        check_ok "çalışan $1 kurulan binary ($(md5sum "$LIB_DIR/$1" | cut -c1-12))"
    else
        check_fail "çalışan $1 kurulan binary DEĞİL — eski süreç ayakta ya da hiç çalışmıyor"
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
        check_ok "çalışan ters vekil kurulan binary (${running_sum:0:12})"
    else
        check_fail "çalışan ters vekil BAŞKA bir binary (çalışan $running_sum, kurulan $installed_sum)"
    fi
else
    check_fail "ters vekilin çalışan imajı okunamadı (pid=${caddy_pid:-yok})"
fi

# 8. Ters vekil root ÇALIŞMAMALI.
caddy_user="$(ps -o user= -p "${caddy_pid:-0}" 2>/dev/null | tr -d ' ')"
if [ "$caddy_user" = "kadran-caddy" ]; then
    check_ok "ters vekil yetkisiz kullanıcı olarak çalışıyor ($caddy_user)"
else
    check_fail "ters vekil '$caddy_user' olarak çalışıyor, 'kadran-caddy' bekleniyordu"
fi

# 9. Admin soketinin izinleri.
admin_mode="$(stat -c '%a %U:%G' /run/kadran-caddy/admin.sock 2>/dev/null || echo yok)"
if [ "$admin_mode" = "660 kadran-caddy:kadran" ]; then
    check_ok "admin.sock: $admin_mode"
else
    check_fail "admin.sock beklenmedik: $admin_mode (660 kadran-caddy:kadran bekleniyordu)"
fi

# 10. kadrand admin soketine ULAŞABİLMELİ — K-050'nin dayandığı erişim.
if setpriv --reuid kadran --regid kadran --clear-groups \
        test -w /run/kadran-caddy/admin.sock 2>/dev/null; then
    check_ok "kadran kullanıcısı admin soketine yazabiliyor"
else
    check_fail "kadran kullanıcısı admin soketine YAZAMIYOR — ters vekil yönetilemez"
fi

# 11. Ters vekil executor'a ULAŞAMAMALI. Modelin can alıcı noktası:
#     internete bakan süreç ayrıcalıklı soketi görmemeli.
if setpriv --reuid kadran-caddy --regid kadran-caddy --clear-groups \
        test -r /run/kadran-exec/exec.sock 2>/dev/null; then
    check_fail "ters vekil exec.sock'u okuyabiliyor — ayrıcalıklı executor internete bakıyor"
else
    check_ok "ters vekil exec.sock'a erişemiyor"
fi

[ "$fail" -eq 0 ] || die "kurulum sonrası doğrulama başarısız — yukarıya bakın"

printf '\nKurulum tamamlandı.\n'
printf 'Artık root erişimine gerek yok; bağlanmak için:\n'
printf '  kadran status kadran-client@<sunucu>\n'
