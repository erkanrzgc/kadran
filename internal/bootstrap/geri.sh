#!/usr/bin/env bash
# kadran → panely GERİ DÖNÜŞ (K-136). Sunucuda root olarak:
#
#   /usr/local/lib/kadran/kadran-geri-donus.sh
#
# Göç (goc.sh) başlarken bu betiği ve goc.sh'ı /usr/local/lib/kadran'a
# kurar; göç yarıda kalsa da sunucuda hazırdır. Bittiğinde v0.3.0 ağacıyla
# `bootstrap` çalıştırılır: o, eski adlı birimleri kurar ve konteynerleri
# eski adlarla yeniden açar.
#
# ⚠ KESİNTİ: ters vekil hemen durur; site v0.3.0 bootstrap'ı bitip
# panelyd konteynerleri iyileştirene kadar kapalıdır. Bu bir acil durum
# yoludur, göç gibi kesintisiz değildir.
#
# Her adım durumuna bakar: yarıda kalırsa yeniden çalıştırılabilir.
set -euo pipefail

say()  { printf '  %s\n' "$*"; }
step() { printf '\n==> %s\n' "$*"; }
die()  { printf '\nERROR: %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "run as root"

BURASI="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=goc.sh
. "$BURASI/goc.sh"

[ -d "$GOC_DIR" ] || die "$GOC_DIR does not exist: this server was never migrated, there is nothing to roll back"

step "Rollback: kadran → panely (K-136)"

# İmajlar eski adla yeniden etiketlenir: v0.3.0 panelyd konteynerleri
# panely/<uyg>:<sha>'dan kurar.
n=0
while read -r imaj; do
    [ -n "$imaj" ] || continue
    docker tag "$imaj" "panely/${imaj#kadran/}"
    n=$((n + 1))
done < <(docker images --filter reference='kadran/*' --format '{{.Repository}}:{{.Tag}}')
# Yeni adlı etiketler kalkar (imajın kendisi panely/ etiketiyle duruyor;
# `rmi` yalnızca etiketi siler). GCP provasında kalıntı olarak görüldü.
docker images --filter reference='kadran/*' --format '{{.Repository}}:{{.Tag}}' |
    xargs -r docker rmi >/dev/null
say "$n images tagged under panely/, kadran/ tags removed"

# Her şey durur; ters vekil dahil (KESİNTİ burada başlar).
for b in kadrand.service kadran-exec.service kadran-caddy.service kadran-caddy-admin.socket \
         kadran-notify.timer kadran-offsite.timer kadran-volume-backup.timer \
         kadran-notify.service kadran-offsite.service kadran-volume-backup.service \
         var-lib-kadran-volumes.mount; do
    [ -e "/etc/systemd/system/$b" ] || continue
    systemctl disable --now "$b" >/dev/null 2>&1 || systemctl stop "$b" 2>/dev/null || true
done
if findmnt -n /var/lib/kadran/volumes >/dev/null 2>&1; then
    umount /var/lib/kadran/volumes || die "could not unmount the volume root"
fi
say "kadran units stopped"

# Yeni adlı konteynerler ve ağlar kaldırılır; v0.3.0 panelyd eski
# adlarla yeniden kurar. Hacim verisi konteynerlerde DEĞİL, dizinde.
docker ps -aq --filter label=kadran.app_id | xargs -r docker rm -f >/dev/null
docker network ls --format '{{.Name}}' | { grep '^kadran-' || true; } |
    xargs -r docker network rm >/dev/null

goc_kullanici kadran-caddy panely-caddy /var/lib/panely-caddy "Panely reverse proxy"
goc_kullanici kadran-client panely-client /var/lib/panely-client "Panely client access"
goc_kullanici kadran panely /var/lib/panely "Panely control plane"

goc_tasi /var/lib/kadran /var/lib/panely
goc_onek /var/lib/panely kadran.db panely.db
goc_onek /var/lib/panely/backups kadran- panely-
goc_tasi /var/lib/kadran-exec /var/lib/panely-exec
goc_tasi /var/lib/kadran-client /var/lib/panely-client
goc_tasi /var/lib/kadran-caddy /var/lib/panely-caddy
goc_tasi /var/lib/kadran-volume-backup /var/lib/panely-volume-backup
goc_onek /var/lib/panely-volume-backup kadran-hacim- panely-hacim-
goc_tasi /var/lib/private/kadran-notify /var/lib/private/panely-notify
if [ -L /var/lib/kadran-notify ]; then rm -f /var/lib/kadran-notify; fi

ak=/var/lib/panely-client/.ssh/authorized_keys
if [ -f "$ak" ] && grep -qF "$YENI_CONNECT" "$ak"; then
    goc_yerinde_sed "$ak" "s#^$YENI_CONNECT#$ESKI_CONNECT#"
fi

goc_tasi /etc/kadran /etc/panely
if [ -f /etc/panely/offsite.conf ] && grep -q '^OFFSITE_REMOTE=kadran-offsite:' /etc/panely/offsite.conf; then
    goc_yerinde_sed /etc/panely/rclone.conf 's/^\[kadran-offsite\]$/[panely-offsite]/'
    goc_yerinde_sed /etc/panely/offsite.conf 's/^OFFSITE_REMOTE=kadran-offsite:/OFFSITE_REMOTE=panely-offsite:/'
fi

# Yeni birimler ve kurallar kalkar; eskiler göçün sakladığı yerden döner.
# Yeni drop-in'ler SİLİNMEZ, göç kaydına alınır: göçten sonra elle
# değiştirilmiş olabilirler (ör. daraltılmış --allow-repo) ve geri dönen
# eski drop-in onları sessizce geri alırdı (güvenlik incelemesi, K-136).
for b in kadrand.service kadran-exec.service kadran-caddy.service kadran-caddy-admin.socket \
         var-lib-kadran-volumes.mount kadran-notify.service kadran-notify.timer \
         kadran-notify-failure@.service kadran-offsite.service kadran-offsite.timer \
         kadran-volume-backup.service kadran-volume-backup.timer; do
    rm -f "/etc/systemd/system/$b"
    if [ -d "/etc/systemd/system/$b.d" ]; then
        install -d -m 0700 "$GOC_DIR/yeni-dropin"
        mv "/etc/systemd/system/$b.d" "$GOC_DIR/yeni-dropin/"
    fi
done
rm -f /etc/tmpfiles.d/kadran.conf /etc/tmpfiles.d/kadran-caddy.conf \
      /etc/ssh/sshd_config.d/60-kadran.conf
if [ -d "$GOC_DIR/eski-birimler" ]; then
    for f in "$GOC_DIR/eski-birimler"/*; do
        [ -e "$f" ] || continue
        goc_tasi "$f" "/etc/systemd/system/${f##*/}"
    done
fi
goc_tasi "$GOC_DIR/eski-lib" /usr/local/lib/panely
systemctl daemon-reload
rm -rf /run/kadran /run/kadran-exec /run/kadran-caddy

# Göç kaydı kenara alınır: aynı sunucuda yeniden göç, eski gözlemlerle
# (beklenen replikalar, zamanlayıcılar) değil taze başlamalı.
kenar="$GOC_DIR.geri-$(date -u +%Y%m%dT%H%M%SZ)"
zamanlayicilar="$(cat "$GOC_DIR/zamanlayicilar" 2>/dev/null || true)"
mv "$GOC_DIR" "$kenar"

# Kurulu ikililer en son silinir: bu betik oradan çalışıyor (Linux açık
# dosyayı silmeye izin verir; bash betiği okumaya devam eder).
rm -rf /usr/local/lib/kadran

say "old names restored; migration record and database copy: $kenar"
if [ -d "$kenar/yeni-dropin" ]; then
    say "⚠ the kadran drop-ins are under $kenar/yeni-dropin. If you changed them"
    say "  after the migration, compare them with the restored old ones:"
    say "  systemctl cat panely-exec.service | grep -- --allow-repo"
fi
if [ -n "$zamanlayicilar" ]; then
    say "timers enabled before the migration (re-enable them AFTER bootstrap):"
    for s in $zamanlayicilar; do say "  systemctl enable --now panely-$s.timer"; done
fi
cat <<'EOF'

Now install v0.3.0 from your workstation (the site stays DOWN until then):
  kadran bootstrap -repo <v0.3.0 tree> -binaries <v0.3.0 binaries> root@server
  (if SSH as root is disabled: -sudo user@server)
EOF
