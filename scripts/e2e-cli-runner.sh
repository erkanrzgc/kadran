#!/usr/bin/env bash
# CLI'ı gerçek bir kadrand'ye karşı uçtan uca doğrular. LINUX'TA ÇALIŞIR.
#
# # Bu betik neden var?
#
# Birim testleri istemciyi sahte bir sunucuya karşı sınıyor. Sahte sunucu,
# gerçeğinin yaptığı iki şeyi YAPMIYOR: SO_PEERCRED ile çağıranı doğrulamak
# ve kimlik önsözünü okumak. İkisi de yalnızca Linux'ta çalışır — ve K-012'de
# düzeltilen hata tam olarak bu boşlukta yaşıyordu.
#
# Root GEREKTİRMEZ. İstemci grubu olarak kullanıcının kendi birincil grubu
# kullanılıyor; üretimde bu `kadran-client` olur. Ayrıcalık izolasyonunun
# root gerektiren doğrulaması ayrı bir betikte (scripts/e2e-executor.sh).
#
# Kullanım:
#   scripts/e2e-cli-runner.sh <binary-dizini>
#
# Windows iş istasyonundan WSL üzerinden çalıştırmak için:
#   scripts/e2e-cli.sh

set -uo pipefail

BIN="${1:?kullanım: e2e-cli-runner.sh <binary-dizini>}"

WORK="$(mktemp -d /tmp/kadran-e2e.XXXXXX)"
SOCK="$WORK/api.sock"
DB="$WORK/kadran.db"
LOG="$WORK/kadrand.log"

fail=0

check() {
    local name="$1" want="$2" got="$3"
    if [[ "$want" == "$got" ]]; then
        printf '  ✓ %s\n' "$name"
    else
        printf '  ✗ %s — beklenen %q, alınan %q\n' "$name" "$want" "$got" >&2
        fail=1
    fi
}

contains() {
    local name="$1" needle="$2" haystack="$3"
    if [[ "$haystack" == *"$needle"* ]]; then
        printf '  ✓ %s\n' "$name"
    else
        printf '  ✗ %s — %q çıktıda yok\n' "$name" "$needle" >&2
        printf '    çıktı: %s\n' "$haystack" >&2
        fail=1
    fi
}

lacks() {
    local name="$1" needle="$2" haystack="$3"
    if [[ "$haystack" == *"$needle"* ]]; then
        printf '  ✗ %s — %q çıktıda var, olmamalıydı\n' "$name" "$needle" >&2
        fail=1
    else
        printf '  ✓ %s\n' "$name"
    fi
}

cleanup() {
    if [[ -n "${DAEMON_PID:-}" ]]; then
        kill "$DAEMON_PID" 2>/dev/null || true
        wait "$DAEMON_PID" 2>/dev/null || true
    fi
    rm -rf "$WORK"
}
trap cleanup EXIT

chmod +x "$BIN/kadrand" "$BIN/kadran" "$BIN/kadran-connect" "$BIN/kadran-vault"

# Kasa zorunlu (K-123): daemon açık anahtarsız açılmıyor. Anahtarı kurulumun
# kullandığı araç üretiyor; özel anahtar yalnız $WORK'te kalıyor.
"$BIN/kadran-vault" -key "$WORK/vault.key" > "$WORK/vault.pub"

echo "==> kadrand başlatılıyor (kullanıcı: $(id -un), grup: $(id -gn))"
# Executor soketi kasten YOK: erişilemeyen executor'ın DOĞRULANAMADI olarak
# raporlandığını (GEÇERSİZ değil) burada sınıyoruz — K-013.
"$BIN/kadrand" \
    -socket "$SOCK" \
    -db "$DB" \
    -client-group "$(id -gn)" \
    -exec-socket "$WORK/olmayan-exec.sock" \
    -vault-recipient "$WORK/vault.pub" \
    > "$LOG" 2>&1 &
DAEMON_PID=$!

for _ in $(seq 1 50); do
    [[ -S "$SOCK" ]] && break
    sleep 0.1
done
if [[ ! -S "$SOCK" ]]; then
    echo "kadrand soketi açmadı. Günlük:" >&2
    cat "$LOG" >&2
    exit 1
fi

echo
echo "==> kadran status"
out="$("$BIN/kadran" status "unix://$SOCK" 2>&1)"; code=$?
check "çıkış kodu" 0 "$code"
contains "daemon sürümü gösterildi" "Daemon" "$out"
contains "executor erişilemiyor olarak raporlandı" "UNREACHABLE" "$out"
# kadrand root ile başlamayı reddediyor; ekranda da root görünmemeli.
lacks "kadrand yetkisiz kullanıcı olarak çalışıyor" "KURULUM BOZUK" "$out"

echo
echo "==> kadran status --json"
out="$("$BIN/kadran" status --json "unix://$SOCK" 2>&1)"; code=$?
check "çıkış kodu" 0 "$code"
contains "JSON gövdesi" '"daemon_version"' "$out"
contains "proto alan adları korundu" '"executor_reachable"' "$out"

echo
echo "==> kadran audit list"
out="$("$BIN/kadran" audit list "unix://$SOCK" 2>&1)"; code=$?
check "çıkış kodu" 0 "$code"
# kadrand açılışta zincire bir daemon.start kaydı yazar.
contains "başlangıç kaydı zincirde" "daemon.start" "$out"
contains "sonuç sütunu" "SUCCESS" "$out"

echo
echo "==> kadran audit verify"
out="$("$BIN/kadran" audit verify "unix://$SOCK" 2>&1)"; code=$?
# ASIL SORU: executor erişilemezken çıkış kodu 1 mi (doğrulanamadı), yoksa
# 3 mü (kurcalama)? 3 dönerse her yeniden başlatma sahte alarm üretirdi.
check "erişilemeyen executor çıkış kodu (1 = doğrulanamadı, 3 = kırık)" 1 "$code"
contains "daemon zinciri geçerli" "VALID" "$out"
contains "executor zinciri doğrulanamadı" "UNVERIFIABLE" "$out"
lacks "erişilemeyen zincir kurcalama olarak raporlanmadı" "GEÇERSİZ" "$out"

echo
echo "==> kadran app show — canlı sürüm (K-112)"
# Dağıtım Docker istiyor, burada yok; ama canlı sürüm alanının GERÇEK
# kadrand'den tel üzerinden gelip basıldığı, dağıtılmamış bir uygulamada
# da sınanabiliyor: "yok" demeli, boş satır ya da hata değil.
out="$("$BIN/kadran" app create -repo github.com/kadran-e2e/blog e2eblog "unix://$SOCK" 2>&1)"; code=$?
check "app create çıkış kodu" 0 "$code"
out="$("$BIN/kadran" app show e2eblog "unix://$SOCK" 2>&1)"; code=$?
check "app show çıkış kodu" 0 "$code"
contains "canlı sürüm satırı" "Live     : none" "$out"
out="$("$BIN/kadran" app show --json e2eblog "unix://$SOCK" 2>&1)"; code=$?
check "app show --json çıkış kodu" 0 "$code"
contains "JSON'da canlı sürüm alanı" '"active_release_id"' "$out"

echo
echo "==> kadran sidecar (stdio JSON-RPC)"
out="$(printf '%s\n%s\n' \
    '{"jsonrpc":"2.0","id":1,"method":"version"}' \
    "{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"status\",\"params\":{\"target\":\"unix://$SOCK\"}}" \
    | "$BIN/kadran" sidecar 2>&1)"; code=$?
check "çıkış kodu" 0 "$code"
contains "version yanıtı" '"protocol"' "$out"
contains "status yanıtı gerçek sunucudan geldi" '"daemon_version"' "$out"

echo
echo "==> dağıtım anahtarı (K-131): gerçek kadran-connect -deploy=e2eblog"
# Birim testleri sunucuyu api.NewGRPCServer ile kuruyor; kadrand'nin
# main.go'su o kurucuyu kullanmayı bıraksa hepsi yeşil kalırdı. Burada
# GERÇEK kadrand, GERÇEK kadran-connect (argv ayrıştırması dahil) ve
# GERÇEK SO_PEERCRED birlikte sınanıyor.
#
# sshd yerine sahte bir `ssh`: zorlanmış komut gibi istemcinin argümanlarını
# YOK SAYIP kadran-connect'i sshd'nin vereceği ortamla çalıştırıyor.
# authorized_keys'ten sshd ve kabuk üzerinden geçen yol burada ölçülmüyor.
FAKE="$WORK/sahte-ssh"
mkdir -p "$FAKE"
# sshd'nin ExposeAuthInfo ile yaptığı: kullanılan anahtarı bir dosyaya
# yazar, yolunu SSH_USER_AUTH ile verir (K-134).
printf 'publickey ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl\n' > "$WORK/sshauth"
cat > "$FAKE/ssh" <<EOF
#!/usr/bin/env bash
exec env SSH_CONNECTION="203.0.113.9 50000 198.51.100.1 22" \\
    SSH_USER_AUTH="$WORK/sshauth" \\
    "$BIN/kadran-connect" -socket "$SOCK" -deploy=e2eblog
EOF
chmod +x "$FAKE/ssh" "$BIN/kadran-connect"
SHA="$(printf 'a%.0s' $(seq 40))"
dk() { PATH="$FAKE:$PATH" "$BIN/kadran" "$@"; }

out="$(dk status kadran-client@ci-e2e 2>&1)"; code=$?
check "status reddedildi (çıkış kodu)" 1 "$code"
contains "status: yetki reddi" "yalnızca dağıtım yapabilir" "$out"

out="$(dk deploy e2eblog kadran-client@ci-e2e 2>&1)"; code=$?
check "-commit'siz dağıtım reddedildi" 1 "$code"
contains "-commit ipucu" "-commit" "$out"

out="$(dk deploy -commit "$SHA" baska kadran-client@ci-e2e 2>&1)"; code=$?
check "kapsam dışı dağıtım reddedildi" 1 "$code"
contains "kapsam dışı: yetki reddi" "kapsamında değil" "$out"

# Kapsam içi: yetkiyi geçip GERÇEK işleyiciye ulaşmalı. Executor yok, yani
# derleme düşer; ölçülen, sürümün açılıp "derleme başlıyor"un gelmesi.
out="$(dk deploy -commit "$SHA" e2eblog kadran-client@ci-e2e 2>&1)"; code=$?
contains "kapsam içi dağıtım işleyiciye ulaştı" "build starting" "$out"
lacks "kapsam içi: yetki reddi yok" "kapsamında değil" "$out"
lacks "kapsam içi: rol reddi yok" "yalnızca dağıtım yapabilir" "$out"

contains "ret kadrand günlüğünde" "yetki reddedildi" "$(cat "$LOG")"
contains "günlükte rol" "rol=deploy" "$(cat "$LOG")"
# Hangi anahtarın denediği: parmak izi gerçekten taşınıyor mu (K-134;
# canlıda 56 kaydın 0'ında yoktu).
contains "günlükte anahtar parmak izi" "anahtar=SHA256:" "$(cat "$LOG")"

echo
echo "==> soket izinleri"
# Soket 0660 ve grup sahipliğiyle korunuyor; ayrıca SO_PEERCRED her
# bağlantıda grubu yeniden denetliyor. İzinleri kasten gevşetip
# SO_PEERCRED'in tek başına yettiğini göstermek root gerektirdiği için
# scripts/e2e-executor.sh'a bırakıldı.
check "soket modu" "660" "$(stat -c '%a' "$SOCK")"

echo
if [[ $fail -eq 0 ]]; then
    echo "Tüm CLI uçtan uca kontrolleri geçti."
else
    echo "Bazı kontroller BAŞARISIZ." >&2
fi
exit $fail
