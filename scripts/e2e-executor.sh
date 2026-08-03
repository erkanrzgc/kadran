#!/usr/bin/env bash
# Executor'ın yetki izolasyonunu GERÇEK bir Linux sisteminde doğrular.
#
# Bu betik root gerektirir, çünkü doğruladığı şeyin tamamı yetki
# sınırlarıyla ilgili: sistem kullanıcıları, grup üyelikleri, soket
# sahiplikleri ve SO_PEERCRED reddi.
#
# Kullanım (WSL veya gerçek sunucuda):
#   sudo scripts/e2e-executor.sh
#
# Betik yarattığı her şeyi sonunda temizler.

set -euo pipefail

if [[ $EUID -ne 0 ]]; then
    echo "Bu betik root gerektirir: sudo $0" >&2
    exit 1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EXEC_BIN="${PANELY_EXEC_BIN:-$REPO_ROOT/bin/panely-exec}"

if [[ ! -x "$EXEC_BIN" ]]; then
    echo "panely-exec bulunamadı: $EXEC_BIN" >&2
    echo "Önce derleyin: GOOS=linux GOARCH=amd64 go build -o bin/panely-exec ./cmd/panely-exec" >&2
    exit 1
fi

WORK="$(mktemp -d)"
SOCK_DIR="$WORK/run"
SOCKET="$SOCK_DIR/exec.sock"
JOURNAL="$WORK/exec-audit.log"
EXEC_PID=""

pass=0
fail=0

ok()   { echo "  [GEÇTİ]  $1"; pass=$((pass+1)); }
bad()  { echo "  [KALDI]  $1" >&2; fail=$((fail+1)); }

cleanup() {
    [[ -n "$EXEC_PID" ]] && kill "$EXEC_PID" 2>/dev/null || true
    wait "$EXEC_PID" 2>/dev/null || true
    userdel  panely-e2e-daemon  2>/dev/null || true
    userdel  panely-e2e-intruder 2>/dev/null || true
    groupdel panely-e2e-daemon   2>/dev/null || true
    groupdel panely-e2e-intruder 2>/dev/null || true
    rm -rf "$WORK"
}
trap cleanup EXIT

echo "==> Test kimlikleri oluşturuluyor"
# panelyd'yi taklit eden kullanıcı: executor yalnızca bunu kabul etmeli.
useradd --system --no-create-home --shell /usr/sbin/nologin panely-e2e-daemon
# Yetkisiz üçüncü taraf: reddedilmeli.
useradd --system --no-create-home --shell /usr/sbin/nologin panely-e2e-intruder

DAEMON_UID="$(id -u panely-e2e-daemon)"
DAEMON_GID="$(id -g panely-e2e-daemon)"

mkdir -p "$SOCK_DIR"
# Üretimdeki /run/panely-exec ile aynı: 0750 root:<daemon grubu>.
chown root:"$DAEMON_GID" "$SOCK_DIR"
chmod 0750 "$SOCK_DIR"
chmod 0755 "$WORK"

echo "==> Executor başlatılıyor"

# PANELY_E2E_ALLOW_INTRUDER, testin KENDİSİNİ sınamak içindir.
#
# Ayarlandığında executor davetsiz kullanıcıyı da kabul eder — yani
# SO_PEERCRED politikası kasten "bozulur". Bu durumda 5. doğrulamanın
# BAŞARISIZ olması gerekir. Olmuyorsa test hiçbir şey ölçmüyor demektir.
#
# Hiç ateşlenmeyen bir güvenlik testi, yeşil bir rozet ve sahte bir
# güven duygusundan başka bir şey üretmez.
ALLOW_USER=panely-e2e-daemon
if [[ -n "${PANELY_E2E_ALLOW_INTRUDER:-}" ]]; then
    ALLOW_USER=panely-e2e-intruder
    echo "  (kendini sınama kipi: executor DAVETSİZ kullanıcıyı kabul edecek)"
fi

"$EXEC_BIN" \
    --socket "$SOCKET" \
    --journal "$JOURNAL" \
    --allow-user "$ALLOW_USER" \
    --owner-group panely-e2e-daemon &
EXEC_PID=$!

for _ in $(seq 1 50); do
    [[ -S "$SOCKET" ]] && break
    sleep 0.1
done

if [[ ! -S "$SOCKET" ]]; then
    echo "Executor soketi açmadı — çıkılıyor" >&2
    exit 1
fi

echo
echo "==> Doğrulamalar"

# 1. Soket sahipliği ve izinleri.
sock_perm="$(stat -c '%a' "$SOCKET")"
sock_group="$(stat -c '%G' "$SOCKET")"
if [[ "$sock_perm" == "660" ]]; then
    ok "soket izinleri 0660"
else
    bad "soket izinleri $sock_perm, beklenen 660"
fi
if [[ "$sock_group" == "panely-e2e-daemon" ]]; then
    ok "soket grubu doğru"
else
    bad "soket grubu $sock_group, beklenen panely-e2e-daemon"
fi

# 2. Günlük dosyası panelyd tarafından OKUNABİLİR ama YAZILAMAZ olmalı.
journal_perm="$(stat -c '%a' "$JOURNAL")"
if [[ "$journal_perm" == "640" ]]; then
    ok "denetim günlüğü 0640 (daemon okur, yazamaz)"
else
    bad "denetim günlüğü izinleri $journal_perm, beklenen 640"
fi

# 3. İzinli kullanıcı bağlanabilmeli.
if runuser -u panely-e2e-daemon -- \
       python3 -c "
import socket,sys
s=socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
try:
    s.connect('$SOCKET'); s.close(); sys.exit(0)
except Exception as e:
    print(e, file=sys.stderr); sys.exit(1)
" 2>/dev/null; then
    ok "izinli kullanıcı (panelyd) bağlanabildi"
else
    bad "izinli kullanıcı bağlanamadı"
fi

# 4. ASIL TEST: yetkisiz kullanıcı dizini traverse edemediği için
#    sokete ulaşamamalı. Bu, /run/panely-exec izolasyonunun ta kendisi.
if runuser -u panely-e2e-intruder -- \
       python3 -c "
import socket,sys
s=socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
try:
    s.connect('$SOCKET'); s.close(); sys.exit(0)
except Exception:
    sys.exit(1)
" 2>/dev/null; then
    bad "YETKİSİZ KULLANICI EXECUTOR'A BAĞLANDI — izolasyon kırık"
else
    ok "yetkisiz kullanıcı reddedildi (dizin izinleri)"
fi

# 5. ASIL İDDİA: dizin izinleri gevşetilse BİLE SO_PEERCRED reddetmeli.
#
# # Bu test neden üç durumu ayırıyor?
#
# Naif hâli ("bağlanamadıysa geçti") BOŞ YERE geçebilirdi: connect()
# işletim sistemi izinleri yüzünden başarısız olsaydı SO_PEERCRED hiç
# devreye girmemiş olurdu ama test yine "geçti" derdi. Yani testin
# kanıtladığını sandığı şeyi kanıtlamazdı.
#
# Şimdi connect()'in BAŞARILI olması ZORUNLU (izinler gerçekten
# gevşetildi mi), ve el sıkışmanın reddedilmesi ayrıca sınanıyor.
chmod 0755 "$SOCK_DIR"
chmod 0666 "$SOCKET"

intruder_result="$(runuser -u panely-e2e-intruder -- \
    python3 -c "
import socket
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.settimeout(3)
try:
    s.connect('$SOCKET')
except Exception:
    # İzinler hâlâ engelliyor: SO_PEERCRED sınanamadı.
    print('CONNECT_ENGELLENDI'); raise SystemExit(0)
try:
    s.sendall(b'PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n')
    print('VERI_GELDI' if s.recv(64) else 'KAPATILDI')
except Exception:
    print('KAPATILDI')
" 2>/dev/null)"

case "$intruder_result" in
    KAPATILDI)
        ok "izinler gevşek olsa bile SO_PEERCRED reddetti (connect başarılı, el sıkışma kapandı)"
        ;;
    VERI_GELDI)
        bad "İZİNLER GEVŞEKKEN YETKİSİZ ÇAĞIRAN KABUL EDİLDİ — SO_PEERCRED çalışmıyor"
        ;;
    CONNECT_ENGELLENDI)
        # Testin kendisi kurulamadı; "geçti" demek yanıltıcı olurdu.
        bad "izinler gevşetilemedi, SO_PEERCRED sınanamadı — testin kendisi geçersiz"
        ;;
    *)
        bad "yetkisiz çağıran testi beklenmedik sonuç verdi: '$intruder_result'"
        ;;
esac

# 6. Karşılaştırma: AYNI gevşek izinlerle İZİNLİ kullanıcı kabul edilmeli.
#
# Bu satır olmadan 5. test, executor'ın herkesi reddettiği bozuk bir
# durumda da geçerdi. İki kullanıcı arasındaki TEK fark uid/gid; izinler
# birebir aynı. Farklı sonuç almak, ayrımın gerçekten kimliğe dayandığını
# kanıtlıyor.
allowed_result="$(runuser -u panely-e2e-daemon -- \
    python3 -c "
import socket
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.settimeout(3)
try:
    s.connect('$SOCKET')
    s.sendall(b'PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n')
    print('VERI_GELDI' if s.recv(64) else 'KAPATILDI')
except Exception:
    print('KAPATILDI')
" 2>/dev/null)"

if [[ "$allowed_result" == "VERI_GELDI" ]]; then
    ok "aynı izinlerle izinli kullanıcı kabul edildi — ayrım kimliğe dayanıyor"
else
    bad "izinli kullanıcı da reddedildi ('$allowed_result') — executor herkesi reddediyor olabilir"
fi

echo
echo "==> Sonuç: $pass geçti, $fail kaldı"
[[ $fail -eq 0 ]]
