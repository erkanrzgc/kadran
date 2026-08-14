#!/usr/bin/env bash
# panelyd'yi ve birim dosyasını yerinde günceller.
#
# ── K-049 ───────────────────────────────────────────────────────────
#
# "systemctl is-active" YENİ ikilinin koştuğunu KANITLAMAZ: eski süreç de
# active görünür. Hedef yol birimden okunuyor, sonuç /proc/<pid>/exe'den
# doğrulanıyor.
#
# Sunucuda root olarak, stdin'den çalıştırılmak üzere yazıldı:
#   ssh root@host 'bash -s' < scripts/deploy-panelyd.sh
# Binary ve birim önceden /tmp/panely-stage'e konmuş olmalı.
set -euo pipefail

STAGE=/tmp/panely-stage
UNIT=/etc/systemd/system/panelyd.service

[ -f "$STAGE/panelyd" ] || { echo "HATA: $STAGE/panelyd yok" >&2; exit 1; }
[ -f "$STAGE/panelyd.service" ] || { echo "HATA: $STAGE/panelyd.service yok" >&2; exit 1; }

# Hedef yolu BİRİMDEN oku; sabit yazmak, birim değişince sessizce yanlış
# dosyayı güncellemek demekti.
target="$(systemctl cat panelyd | sed -n 's|^ExecStart=\([^ \\]*\).*|\1|p' | head -1)"
[ -n "$target" ] || { echo "HATA: ExecStart hedefi okunamadı" >&2; exit 1; }
echo "hedef  : $target"

want="$(md5sum "$STAGE/panelyd" | cut -d' ' -f1)"
echo "yeni md5: $want"

systemctl stop panelyd
install -m 0755 -o root -g root "$STAGE/panelyd" "$target"
install -m 0644 -o root -g root "$STAGE/panelyd.service" "$UNIT"
systemctl daemon-reload
systemctl start panelyd

# Açılışın oturmasını bekle — "start döndü" ile "servis ayakta" ayrı
# şeyler; sd_notify öncesi ölçmek yanlış pid okurdu.
for _ in $(seq 1 20); do
    state="$(systemctl show -p ActiveState --value panelyd)"
    [ "$state" = "activating" ] || break
    sleep 0.5
done

pid="$(systemctl show -p MainPID --value panelyd)"
[ "$pid" != "0" ] || { echo "HATA: panelyd ayakta değil" >&2; journalctl -u panelyd -n 30 --no-pager >&2; exit 1; }

got="$(md5sum "/proc/$pid/exe" | cut -d' ' -f1)"
echo "koşan md5: $got  (pid=$pid)"
[ "$got" = "$want" ] || { echo "HATA: KOŞAN İKİLİ YENİ DEĞİL" >&2; exit 1; }

# Yeniden başlatma döngüsü sessiz bir arıza biçimi: active görünüp
# saniyede bir yeniden doğan servis de "active"tir.
echo "NRestarts=$(systemctl show -p NRestarts --value panelyd)"
echo "ActiveState=$(systemctl show -p ActiveState --value panelyd)"

echo "── systemd'nin GERÇEKTEN uyguladığı çit ──"
systemctl show panelyd -p RestrictAddressFamilies -p IPAddressDeny -p IPAddressAllow
