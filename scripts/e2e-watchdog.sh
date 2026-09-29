#!/usr/bin/env bash
# K-115: panelyd'nin watchdog'unu GERÇEK systemd altında ölçer.
#
# ROOT olarak koşar (CI'da sudo); panelyd'yi sudo'yu çağıran kullanıcıyla
# koşturur, çünkü panelyd root'ta başlamayı reddediyor.
#
# ── Üretim birimi DEĞİL, aynı watchdog ayarları ─────────────────────
#
# deploy/systemd/panelyd.service panely kullanıcısını, executor'ı ve
# sertleştirmeyi istiyor; runner'da hiçbiri yok. Burada sınanan şey
# mekanizma: WATCHDOG_USEC'in panelyd'ye ulaşması, pinglerin gelmesi,
# pingler kesilince systemd'nin öldürüp geri getirmesi. WatchdogSec
# burada 10 sn (üretimde 60 sn): sayı değil mekanizma ölçülüyor ve CI
# dakikalarca beklemesin.
#
# Birim /run/systemd/system'e YAZILIYOR, systemd-run ile değil: geçici
# birim durunca boşaltılıyor ve "durdurma watchdog sayılmadı" kontrolü
# okunacak bir Result bulamazdı.
#
# ── Kontrol grupları ────────────────────────────────────────────────
#
# 1. Normal koşu, 6 × WatchdogSec: yeniden başlatma SIFIR. Tek başına
#    bir şey kanıtlamaz; watchdog hiç kurulmamış olsa da sıfır çıkardı.
# 2. SIGSTOP: süreç donunca systemd öldürüp geri getiriyor. 1'i
#    anlamlı kılan pozitif kontrol bu: watchdog kurulu, yani 1'deki
#    sıfır pinglerin GELDİĞİ demek.
# 3. SIGABRT: Go'nun bastığı yığın dökümünde döngülerin goroutine'leri
#    görünüyor mu. Birimde WatchdogSignal değiştirilmemesinin gerekçesi
#    bu döküm; iddia burada ölçülüyor.
# 4. systemctl stop: sonuç "success", "watchdog" değil.
set -uo pipefail

BIN="${1:?kullanım: sudo bash scripts/e2e-watchdog.sh <panelyd dizini, mutlak>}"
cd "$(dirname "$0")/.."
# Ayıklayıcı K-110'un bildirim betiğinden: systemd'nin GERÇEK watchdog
# satırı aynı koddan geçsin (check-notify-format.sh türetilmiş bir satırla
# sınıyor).
# shellcheck source=/dev/null
source deploy/notify/panely-notify.sh

UNIT=panelyd-wd
KULLANICI="${SUDO_USER:?sudo ile koşturulmalı}"
GRUP="$(id -gn "$KULLANICI")"
WORK="$(mktemp -d)"
chown "$KULLANICI:$GRUP" "$WORK"
WDSEC=10

fail=0
ok()  { echo "  ✓ $1"; }
bad() { echo "  ✗ $1"; fail=1; }
ozellik() { systemctl show -p "$1" --value "$UNIT.service"; }
gunluk() { journalctl --sync; journalctl -u "$UNIT.service" --no-pager -o cat "$@"; }

# bekle <saniye> <koşul-komutu…> — koşul doğru olana kadar yoklar.
bekle() {
    local sure="$1"; shift
    local son=$((SECONDS + sure))
    while (( SECONDS < son )); do
        "$@" && return 0
        sleep 1
    done
    return 1
}
yeniden_baslatma_en_az() { (( $(ozellik NRestarts) >= $1 )) && [[ "$(ozellik ActiveState)" == active ]]; }

temizle() {
    if (( fail )); then
        echo "── günlük (son 60 satır) ──"
        gunluk -n 60
    fi
    systemctl stop "$UNIT.service" 2>/dev/null
    rm -f "/run/systemd/system/$UNIT.service"
    systemctl daemon-reload
    rm -rf "$WORK"
}
trap temizle EXIT

cat > "/run/systemd/system/$UNIT.service" <<EOF
[Service]
Type=notify
NotifyAccess=main
User=$KULLANICI
Group=$GRUP
ExecStart=$BIN/panelyd -socket $WORK/api.sock -exec-socket $WORK/olmayan-exec.sock -caddy-socket $WORK/olmayan-caddy.sock -db $WORK/panely.db -client-group $GRUP
Restart=on-failure
RestartSec=1s
WatchdogSec=${WDSEC}s
TimeoutAbortSec=5s
EOF
systemctl daemon-reload

echo "== Başlangıç =="
if ! systemctl start "$UNIT.service"; then
    bad "panelyd systemd altında başlamadı"
    exit 1
fi
ok "READY geldi ($(ozellik ActiveState))"

# main'in bir döngüye damga vermeyi unutmadığının tek kanıtı bu satır
# (Beat.Mark nil'de sessiz).
if gunluk | grep -q 'izlenen_donguler=gözetmen,vekil-izleyici,disk,yedek'; then
    ok "dört döngü izleniyor"
else
    bad "izlenen döngü listesi eksik ya da yok: $(gunluk | grep 'watchdog' | tail -2)"
fi
if gunluk | grep -q "ping_araligi=$((WDSEC / 2))s"; then
    ok "ping aralığı WatchdogSec'in yarısı ($((WDSEC / 2))s)"
else
    bad "ping aralığı beklenen değil: $(gunluk | grep 'watchdog açık' | tail -1)"
fi

echo "== 1. Kontrol: normal koşu, $((6 * WDSEC)) sn =="
sleep $((6 * WDSEC))
if [[ "$(ozellik NRestarts)" == 0 && "$(ozellik ActiveState)" == active ]]; then
    ok "yeniden başlatma yok (NRestarts=0)"
else
    bad "normal koşuda yeniden başladı: NRestarts=$(ozellik NRestarts) $(ozellik ActiveState)"
fi
if gunluk | grep -qi 'watchdog timeout'; then
    bad "normal koşuda watchdog zaman aşımı"
fi

echo "== 2. SIGSTOP: watchdog kurulu mu =="
pid="$(ozellik MainPID)"
kill -STOP "$pid"
if bekle $((WDSEC + 30)) yeniden_baslatma_en_az 1; then
    ok "donan süreç öldürülüp geri getirildi (NRestarts=$(ozellik NRestarts), eski PID $pid, yeni $(ozellik MainPID))"
else
    bad "donan süreç öldürülmedi: NRestarts=$(ozellik NRestarts) $(ozellik ActiveState)"
fi
journalctl --sync
olay="$(journalctl -o json --no-pager _PID=1 "MESSAGE_ID=$OLAY_BASARISIZ" "UNIT=$UNIT.service" | servis_olay_ayikla)"
if grep -qx "$UNIT.service watchdog" <<<"$olay"; then
    ok "systemd'nin gerçek olayı ayıklayıcıdan 'watchdog' olarak geçti"
else
    bad "ayıklayıcı watchdog olayını görmedi: $(printf '%q' "$olay")"
fi

echo "== 3. SIGABRT: yığın dökümü =="
since="$(date +%s)"
kill -ABRT "$(ozellik MainPID)"
if bekle 30 yeniden_baslatma_en_az 2; then
    ok "SIGABRT sonrası geri geldi (NRestarts=$(ozellik NRestarts))"
else
    bad "SIGABRT sonrası geri gelmedi: NRestarts=$(ozellik NRestarts) $(ozellik ActiveState)"
fi
dokum="$(gunluk --since "@$since")"
adet="$(grep -c '^goroutine ' <<<"$dokum")"
echo "  döküm: $adet goroutine"
for iz in 'main.watchProxy' 'main.runBackupScheduler' 'liveness.(\*Watchdog).Run'; do
    if grep -q "$iz" <<<"$dokum"; then
        ok "dökümde $iz"
    else
        bad "dökümde $iz YOK — SIGABRT takılan döngüyü göstermiyor"
    fi
done

echo "== 4. Durdurma watchdog sayılmıyor =="
systemctl stop "$UNIT.service"
sonuc="$(ozellik Result)"
if [[ "$sonuc" == success ]]; then
    ok "durdurma sonucu success"
else
    bad "durdurma sonucu $sonuc"
fi

echo
if (( fail )); then
    echo "BAŞARISIZ: watchdog gerçek systemd altında beklendiği gibi davranmadı."
    exit 1
fi
echo "Watchdog gerçek systemd altında ölçüldü."
