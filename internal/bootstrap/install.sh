#!/usr/bin/env bash
# Panely sunucu kurulumu. `panely bootstrap root@sunucu` tarafından
# uzak makinede root olarak çalıştırılır.
#
# # Bu betik neyi kuruyor?
#
# Üç binary, iki kullanıcı, iki grup ve bir SSH zorlanmış komutu. Kurulum
# bittiğinde root erişimi bir daha GEREKMEZ: günlük kullanım yetkisiz
# `panely-client` kullanıcısı üzerinden yürür.
#
# # İdempotent
#
# Baştan sona tekrar çalıştırılabilir. `hcloud server rebuild` ile temiz
# imajdan defalarca koşturularak sınanıyor.
#
# Kullanım: install.sh <hazırlık-dizini>

set -euo pipefail

STAGE="${1:?kullanım: install.sh <hazırlık-dizini>}"

LIB_DIR=/usr/local/lib/panely
STATE_DIR=/var/lib/panely
CLIENT_HOME=/var/lib/panely-client
SSHD_DROPIN=/etc/ssh/sshd_config.d/60-panely.conf

say()  { printf '  %s\n' "$*"; }
step() { printf '\n==> %s\n' "$*"; }
die()  { printf '\nHATA: %s\n' "$*" >&2; exit 1; }

# ── Ön koşullar ──────────────────────────────────────────────────────

step "Ön koşullar"

[ "$(id -u)" -eq 0 ] || die "bu betik root olarak çalışmalı"
command -v systemctl >/dev/null || die "systemd bulunamadı — desteklenmiyor"
command -v sshd >/dev/null || command -v /usr/sbin/sshd >/dev/null \
    || die "sshd bulunamadı"

SSHD_BIN="$(command -v sshd || echo /usr/sbin/sshd)"

# nologin yolu dağıtıma göre değişiyor.
NOLOGIN="$(command -v nologin || echo /usr/sbin/nologin)"

say "sistem uygun ($(uname -m), $( (. /etc/os-release && echo "$PRETTY_NAME") 2>/dev/null || echo bilinmiyor ))"

# ── Gruplar ve kullanıcılar ──────────────────────────────────────────

step "Gruplar ve kullanıcılar"

getent group panely >/dev/null || groupadd --system panely
getent group panely-client >/dev/null || groupadd --system panely-client

# panelyd'nin çalıştığı yetkisiz kullanıcı. Giriş yapmaz.
if ! id -u panely >/dev/null 2>&1; then
    useradd --system --gid panely \
        --home-dir "$STATE_DIR" --no-create-home \
        --shell "$NOLOGIN" \
        --comment "Panely kontrol düzlemi" panely
fi

# SSH istemci kullanıcısı.
#
# KRİTİK: birincil grup `panely-client` OLMAK ZORUNDA (-g), ek grup (-G)
# DEĞİL. SO_PEERCRED yalnızca sürecin BİRİNCİL grubunu bildirir; ek grup
# üyeliklerini görmez. Yanlış yapılırsa hiçbir hata mesajı çıkmaz, her
# bağlantı sessizce reddedilir.
#
# Kabuk `nologin` DEĞİL: sshd zorlanmış komutu kullanıcının giriş kabuğu
# üzerinden çalıştırır ve nologin onu reddeder. Hesabı kısıtlayan şey
# kabuk değil, authorized_keys'teki `command=...,restrict` ikilisi.
if ! id -u panely-client >/dev/null 2>&1; then
    useradd --system --gid panely-client \
        --home-dir "$CLIENT_HOME" --create-home \
        --shell /bin/sh \
        --comment "Panely istemci erişimi" panely-client
fi

# ── Değişmez doğrulaması ─────────────────────────────────────────────
#
# Kullanıcılar önceden (yanlış) oluşturulmuş olabilir. Sessizce kabul
# etmek yerine kontrol ediyoruz: bu iki koşul modelin dayandığı yer.

step "Yetki değişmezleri"

primary="$(id -gn panely-client)"
[ "$primary" = "panely-client" ] || die \
"panely-client kullanıcısının birincil grubu '$primary', 'panely-client' olmalı.
SO_PEERCRED yalnızca birincil grubu bildirir; bu hâliyle her bağlantı
sessizce reddedilir. Düzeltmek için:  usermod -g panely-client panely-client"

if id -nG panely-client | tr ' ' '\n' | grep -qx panely; then
    die \
"panely-client kullanıcısı 'panely' grubunda. Bu hâliyle exec.sock'a
doğrudan ulaşır ve panelyd tamamen atlanabilir — ayrıcalık ayrımı çöker.
Düzeltmek için:  gpasswd -d panely-client panely"
fi

if id -nG panely | tr ' ' '\n' | grep -qx docker; then
    die \
"panely kullanıcısı 'docker' grubunda. Docker soketine erişim pratikte
root yetkisidir; bu hâliyle executor ayrımı dekoratif kalır.
Düzeltmek için:  gpasswd -d panely docker"
fi

say "panely-client birincil grubu: $primary"
say "panely-client ek grupları: $(id -nG panely-client)"
say "panely ek grupları: $(id -nG panely)"

# ── Binary'ler ───────────────────────────────────────────────────────

step "Binary'ler"

install -d -m 0755 -o root -g root "$LIB_DIR"

for binary in panelyd panely-exec panely-connect; do
    [ -f "$STAGE/$binary" ] || die "$binary hazırlık dizininde yok"
done

# panely-exec root çalışır ve yalnızca root yazabilmeli.
install -m 0755 -o root -g root "$STAGE/panelyd"       "$LIB_DIR/panelyd"
install -m 0755 -o root -g root "$STAGE/panely-exec"   "$LIB_DIR/panely-exec"
# panely-connect'i panely-client çalıştırır; yazma yetkisi yine yalnızca root.
install -m 0755 -o root -g root "$STAGE/panely-connect" "$LIB_DIR/panely-connect"

say "$LIB_DIR içine kuruldu"
"$LIB_DIR/panelyd" -version || die "panelyd çalıştırılamadı — mimari uyuşmuyor olabilir"

# ── Dizinler ─────────────────────────────────────────────────────────

step "Dizinler"

install -m 0644 -o root -g root "$STAGE/panely-tmpfiles.conf" /etc/tmpfiles.d/panely.conf
systemd-tmpfiles --create /etc/tmpfiles.d/panely.conf

# /var/lib/panely tmpfiles ile yaratılıyor ama yeniden başlatma arasında
# kalıcı olması gerekiyor; burada da garantiye alıyoruz.
install -d -m 0750 -o panely -g panely "$STATE_DIR"

say "/run/panely, /run/panely-exec, $STATE_DIR hazır"

# ── systemd birimleri ────────────────────────────────────────────────

step "systemd birimleri"

install -m 0644 -o root -g root "$STAGE/panely-exec.service" /etc/systemd/system/panely-exec.service
install -m 0644 -o root -g root "$STAGE/panelyd.service"     /etc/systemd/system/panelyd.service
systemctl daemon-reload

# ── SSH yapılandırması ───────────────────────────────────────────────

step "SSH yapılandırması"

[ -f "$STAGE/client_key.pub" ] || die "istemci açık anahtarı hazırlık dizininde yok"

install -d -m 0700 -o panely-client -g panely-client "$CLIENT_HOME/.ssh"

# authorized_keys satırı:
#
#   command="..."  → istemci ne isterse istesin YALNIZCA bu çalışır.
#                    SSH_ORIGINAL_COMMAND panely-connect tarafından
#                    kasten yok sayılıyor.
#   restrict       → pty, port yönlendirme, ajan yönlendirme, X11,
#                    user-rc: hepsi kapalı.
#
# Soket yönlendirmesi yerine zorlanmış komut kullanılmasının gerekçesi
# docs/decisions.md K-003'te: unix soketi yönlendirmesini açmak
# `port-forwarding` iznini gerektirir ve bu, istemciye sunucudaki HER TCP
# portuna tünel açma yetkisi verirdi.
client_key="$(cat "$STAGE/client_key.pub")"
auth_line="command=\"$LIB_DIR/panely-connect\",restrict $client_key"

auth_file="$CLIENT_HOME/.ssh/authorized_keys"
touch "$auth_file"

# İdempotanlık: aynı anahtar için satır varsa yenisiyle DEĞİŞTİRİLİR.
# Anahtar gövdesi (tip + base64) eşleşme ölçütü; yorum alanı değişebilir.
key_body="$(printf '%s' "$client_key" | awk '{print $1" "$2}')"
if [ -s "$auth_file" ] && grep -qF "$key_body" "$auth_file"; then
    grep -vF "$key_body" "$auth_file" > "$auth_file.yeni" || true
    mv "$auth_file.yeni" "$auth_file"
fi
printf '%s\n' "$auth_line" >> "$auth_file"

chown -R panely-client:panely-client "$CLIENT_HOME/.ssh"
chmod 0600 "$auth_file"

# sshd drop-in.
#
# ExposeAuthInfo, kimlik doğrulamada kullanılan anahtarın parmak izini
# SSH_AUTH_INFO_0'a yazar. panely-connect denetim kaydının aktör kimliğini
# ORADAN okuyor; kapalıysa parmak izi sessizce boş kalır ve denetim izi
# "kim yaptı" sorusunu yanıtlayamaz.
if [ -d /etc/ssh/sshd_config.d ] && grep -qE '^\s*Include\s+/etc/ssh/sshd_config\.d/' /etc/ssh/sshd_config; then
    cat > "$SSHD_DROPIN" <<'SSHD'
# Panely tarafından yönetiliyor. Elle düzenlemeyin.

# ── Denetim kimliğinin taklit edilmesini engelleyen satır ────────────
#
# panely-connect, aktörün SSH parmak izini SSH_AUTH_INFO_0'dan okur.
# O değişkeni istemci belirleyebilirse denetim izi yalan söyler.
#
# authorized_keys'teki `environment="AD=deger"` seçeneği tam olarak bunu
# yapardı: sshd(8) bu seçenek için "override other default environment
# values" diyor — yani sshd'nin KENDİ yazdığı SSH_AUTH_INFO_0'ı ezerdi.
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
Match User panely-client
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
elle yapılandırma gerekiyor (Match User panely-client + ExposeAuthInfo yes)"
fi

# Yapılandırma BOZUKSA sshd'yi yeniden yüklemek bizi dışarıda bırakır.
# Önce doğrula.
"$SSHD_BIN" -t || die "sshd yapılandırması geçersiz — değişiklik uygulanmadı"
systemctl reload ssh 2>/dev/null || systemctl reload sshd

say "zorlanmış komut ve ExposeAuthInfo yapılandırıldı"

# ── Servisler ────────────────────────────────────────────────────────

step "Servisler"

systemctl enable --now panely-exec.service
systemctl enable --now panelyd.service

# Soketlerin belirmesi için kısa bir pencere.
for _ in $(seq 1 50); do
    [ -S /run/panely/api.sock ] && break
    sleep 0.1
done

systemctl is-active --quiet panely-exec.service || {
    journalctl -u panely-exec.service -n 30 --no-pager >&2
    die "panely-exec başlamadı"
}
systemctl is-active --quiet panelyd.service || {
    journalctl -u panelyd.service -n 30 --no-pager >&2
    die "panelyd başlamadı"
}

say "panely-exec ve panelyd çalışıyor"

# ── Kurulum sonrası doğrulama ────────────────────────────────────────
#
# Ürünün merkezî iddiası burada sınanıyor. Bu kontroller geçmiyorsa
# kurulum "başarılı" sayılmamalı.

step "Kurulum sonrası doğrulama"

fail=0
check_fail() { printf '  ✗ %s\n' "$*" >&2; fail=1; }
check_ok()   { printf '  ✓ %s\n' "$*"; }

# 1. panelyd root ÇALIŞMAMALI.
daemon_user="$(ps -o user= -C panelyd | head -1 | tr -d ' ')"
if [ "$daemon_user" = "panely" ]; then
    check_ok "panelyd yetkisiz kullanıcı olarak çalışıyor ($daemon_user)"
else
    check_fail "panelyd '$daemon_user' olarak çalışıyor, 'panely' bekleniyordu"
fi

# 2. panelyd Docker'a ERİŞEMEMELİ.
if setpriv --reuid panely --regid panely --clear-groups docker ps >/dev/null 2>&1; then
    check_fail "panely kullanıcısı Docker'a erişebiliyor — ayrıcalık ayrımı ÇÖKMÜŞ"
else
    check_ok "panely kullanıcısı Docker'a erişemiyor"
fi

# 3. Soket izinleri.
api_mode="$(stat -c '%a %U:%G' /run/panely/api.sock 2>/dev/null || echo yok)"
if [ "$api_mode" = "660 panely:panely-client" ]; then
    check_ok "api.sock: $api_mode"
else
    check_fail "api.sock beklenmedik: $api_mode (660 panely:panely-client bekleniyordu)"
fi

exec_mode="$(stat -c '%a %U:%G' /run/panely-exec/exec.sock 2>/dev/null || echo yok)"
if [ "$exec_mode" = "660 root:panely" ]; then
    check_ok "exec.sock: $exec_mode"
else
    check_fail "exec.sock beklenmedik: $exec_mode (660 root:panely bekleniyordu)"
fi

# 4. İstemci kullanıcısı exec.sock'a ULAŞAMAMALI.
if setpriv --reuid panely-client --regid panely-client --clear-groups \
        test -r /run/panely-exec/exec.sock 2>/dev/null; then
    check_fail "panely-client exec.sock'u okuyabiliyor — panelyd atlanabilir"
else
    check_ok "panely-client exec.sock'a erişemiyor"
fi

# 5. İstemci kullanıcısı kabuk ALMAMALI (zorlanmış komut).
if grep -q 'command="' "$auth_file"; then
    check_ok "authorized_keys zorlanmış komut içeriyor"
else
    check_fail "authorized_keys'te zorlanmış komut yok — istemci kabuk alabilir"
fi

[ "$fail" -eq 0 ] || die "kurulum sonrası doğrulama başarısız — yukarıya bakın"

printf '\nKurulum tamamlandı.\n'
printf 'Artık root erişimine gerek yok; bağlanmak için:\n'
printf '  panely status panely-client@<sunucu>\n'
