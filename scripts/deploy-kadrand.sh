#!/usr/bin/env bash
# kadrand'yi ve birim dosyasını yerinde günceller.
#
# ── K-049 ───────────────────────────────────────────────────────────
#
# "systemctl is-active" YENİ ikilinin koştuğunu KANITLAMAZ: eski süreç de
# active görünür. Hedef yol birimden okunuyor, sonuç /proc/<pid>/exe'den
# doğrulanıyor.
#
# Sunucuda root olarak, stdin'den çalıştırılmak üzere yazıldı:
#   ssh root@host 'bash -s' < scripts/deploy-kadrand.sh
# Binary ve birim önceden /tmp/kadran-stage'e konmuş olmalı.
set -euo pipefail

STAGE=/tmp/kadran-stage
UNIT=/etc/systemd/system/kadrand.service

[ -f "$STAGE/kadrand" ] || { echo "HATA: $STAGE/kadrand yok" >&2; exit 1; }
[ -f "$STAGE/kadrand.service" ] || { echo "HATA: $STAGE/kadrand.service yok" >&2; exit 1; }

# Hedef yolu BİRİMDEN oku; sabit yazmak, birim değişince sessizce yanlış
# dosyayı güncellemek demekti.
target="$(systemctl cat kadrand | sed -n 's|^ExecStart=\([^ \\]*\).*|\1|p' | head -1)"
[ -n "$target" ] || { echo "HATA: ExecStart hedefi okunamadı" >&2; exit 1; }
echo "hedef  : $target"

want="$(md5sum "$STAGE/kadrand" | cut -d' ' -f1)"
echo "yeni md5: $want"

systemctl stop kadrand
install -m 0755 -o root -g root "$STAGE/kadrand" "$target"
install -m 0644 -o root -g root "$STAGE/kadrand.service" "$UNIT"
systemctl daemon-reload
systemctl start kadrand

# Açılışın oturmasını bekle — "start döndü" ile "servis ayakta" ayrı
# şeyler; sd_notify öncesi ölçmek yanlış pid okurdu.
for _ in $(seq 1 20); do
    state="$(systemctl show -p ActiveState --value kadrand)"
    [ "$state" = "activating" ] || break
    sleep 0.5
done

pid="$(systemctl show -p MainPID --value kadrand)"
[ "$pid" != "0" ] || { echo "HATA: kadrand ayakta değil" >&2; journalctl -u kadrand -n 30 --no-pager >&2; exit 1; }

got="$(md5sum "/proc/$pid/exe" | cut -d' ' -f1)"
echo "koşan md5: $got  (pid=$pid)"
[ "$got" = "$want" ] || { echo "HATA: KOŞAN İKİLİ YENİ DEĞİL" >&2; exit 1; }

# Yeniden başlatma döngüsü sessiz bir arıza biçimi: active görünüp
# saniyede bir yeniden doğan servis de "active"tir.
echo "NRestarts=$(systemctl show -p NRestarts --value kadrand)"
echo "ActiveState=$(systemctl show -p ActiveState --value kadrand)"

echo "── systemd'nin GERÇEKTEN uyguladığı çit ──"
systemctl show kadrand -p RestrictAddressFamilies -p IPAddressDeny -p IPAddressAllow
