#!/usr/bin/env bash
# Birim dosyası testlerinin GERÇEKTEN bir şey koruduğunu sınar.
#
# Her mutasyon korunan bir özelliği KASTEN bozar; test kırmızıya
# dönmezse o özellik korunmuyor demektir.
#
# ── Neden birim dosyaları için ayrı bir betik ───────────────────────
#
# Buradaki iki özellik Go kodunda değil, birim DOSYALARI arasındaki
# ilişkide yaşıyor (K-100, K-101, K-102):
#
#   - executor'ın denetim günlüğü daemon'un yazabildiği bir dizinde
#     durmamalı; dursa, dosyanın sahibi root olsa bile daemon onu silip
#     yerine kendi zincirini koyabilir (canlıda ölçüldü).
#   - uzak yedek yükleyicisinin rclone yapılandırması da öyle; rclone
#     yapılandırması komut çalıştırabilir ve yükleyici ağ gören tek birim.
#
# İlişkiyi okuyan testler ancak bir dosyayı bozunca kırmızıya dönüyorsa
# bir şey koruyor demektir.
set -uo pipefail

cd "$(dirname "$0")/.."
FILES=(
    deploy/systemd/kadran-exec.service
    deploy/systemd/kadran-tmpfiles.conf
    deploy/systemd/kadran-offsite.service
    deploy/systemd/kadran-notify.service
    deploy/systemd/kadran-notify-failure@.service
    deploy/systemd/kadran-volume-backup.service
    deploy/systemd/kadran-volume-backup.timer
    deploy/systemd/kadran-caddy.service
    deploy/systemd/kadrand.service
)
BAK=$(mktemp -d)
for f in "${FILES[@]}"; do cp "$f" "$BAK/$(basename "$f")"; done
restore() { for f in "${FILES[@]}"; do cp "$BAK/$(basename "$f")" "$f"; done; }
trap 'restore; rm -rf "$BAK"' EXIT

fail=0
WANT='TestExecutorJournalOutsideDaemonDirs|TestOwnedPathsAreActuallyCreated|TestOffsiteRcloneConfigOutsideDaemonDirs|TestUnitsDoNotHardRequireForeignPaths|TestOffsiteUploaderCanResolveNamesButNotReachLocalhost|TestNotify|TestOffsiteFailureIsNotified|TestVolumeArchive|TestReverseProxyHasNoReload|TestDaemonHasAWatchdog'

# ── Taban YEŞİL olmalı ───────────────────────────────────────────────
#
# Mutasyonsuz kodda düşen bir test her mutantı "yakalandı" gösterirdi:
# SESSİZ sahte geçiş. Her farklı test komutu, ilk mutantından önce bir kez
# mutasyonsuz kodda koşturuluyor.
declare -A TABAN=()
taban_yesil() {
    local key="$*"
    [[ -n "${TABAN[$key]:-}" ]] && return 0
    restore
    if ! go test "$@" >/dev/null 2>&1; then
        echo "!! TABAN KIRMIZI: mutasyonsuz kodda 'go test $*' düşüyor — ölçüm YAPILMADI"
        exit 1
    fi
    TABAN[$key]=1
}

# mutate <ad> <dosya> <python-ifadesi>
mutate() {
    local name="$1" src="$2" expr="$3"
    restore
    taban_yesil ./internal/bootstrap/ -run "$WANT" -count=1
    if ! python -c "
import io,sys
class _S(str):
    def replace(self,a,b,*r):
        # TEK eşleşme şart: aranan metin bir yorumda da geçiyorsa
        # replace(…,1) İLKİNİ, yani yorumu değiştirir ve kod hiç mutasyona
        # uğramadan ölçülür (K-127).
        n=self.count(a)
        if n!=1:
            sys.stderr.write('REPLACE '+str(n)+' KEZ ESLESTI (1 olmali): '+repr(a[:70])+chr(10))
            sys.exit(8)
        return _S(str.replace(self,a,b,*r))
p='$src'
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
    # Birim dosyası değişikliği Go derlemesini bozmaz, ama kapı yine de
    # burada: test paketi derlenmiyorsa `go test` düşer ve betik bunu
    # "yakalandı" diye okurdu (K-096). CI her betikte bu kapıyı arıyor.
    local build_out
    if ! build_out=$(go test ./internal/bootstrap/ -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI,"
        echo "     mutasyon derlenebilir olacak şekilde yazılmalı. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if go test ./internal/bootstrap/ -run "$WANT" -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Executor denetim günlüğü =="

# Satır başındaki `\n` ŞART. Birimin yorum bloğunda aynı yolu taşıyan
# bir ÖRNEK satır var (`#       --journal …`); çapasız `replace` ilk
# eşleşmeyi, yani yorumu değiştiriyordu. Test yorumları okumadığı için
# yeşil kaldı ve betik "KIRMIZI OLMADI" dedi — zayıf olan test değil
# mutasyondu (K-080'in ikinci sebebi).
mutate "günlük daemon'un dizinine geri taşındı" deploy/systemd/kadran-exec.service \
    "s=s.replace('\n    --journal /var/lib/kadran-exec/exec-audit.log','\n    --journal /var/lib/kadran/exec-audit.log',1)"

mutate "günlük dizini grup-yazılabilir" deploy/systemd/kadran-tmpfiles.conf \
    "s=s.replace('d /var/lib/kadran-exec        0700 root   root   -','d /var/lib/kadran-exec        0770 root   kadran -',1)"

mutate "günlük dizininin sahibi kadran" deploy/systemd/kadran-tmpfiles.conf \
    "s=s.replace('d /var/lib/kadran-exec        0700 root   root   -','d /var/lib/kadran-exec        0700 kadran root   -',1)"

mutate "günlük dizinini kimse yaratmıyor" deploy/systemd/kadran-tmpfiles.conf \
    "s=s.replace('d /var/lib/kadran-exec        0700 root   root   -\n','',1)"

echo "== Uzak yedek rclone yapılandırması =="

mutate "RCLONE_CONFIG tanımlanmıyor" deploy/systemd/kadran-offsite.service \
    "s=s.replace('Environment=RCLONE_CONFIG=/etc/kadran/rclone.conf\n','',1)"

mutate "rclone yapılandırması daemon'un dizininde" deploy/systemd/kadran-offsite.service \
    "s=s.replace('Environment=RCLONE_CONFIG=/etc/kadran/rclone.conf','Environment=RCLONE_CONFIG=/var/lib/kadran/.config/rclone/rclone.conf',1)"

echo "== Uzak yedek DNS istisnası (K-107) =="

mutate "DNS çözücüsü istisnası kaldırıldı" deploy/systemd/kadran-offsite.service \
    "s=s.replace('\nIPAddressAllow=127.0.0.53\n','\n',1)"

mutate "istisna tüm localhost'a genişletildi" deploy/systemd/kadran-offsite.service \
    "s=s.replace('\nIPAddressAllow=127.0.0.53\n','\nIPAddressAllow=localhost\n',1)"

mutate "istisna 127.0.0.0/8'e genişletildi" deploy/systemd/kadran-offsite.service \
    "s=s.replace('\nIPAddressAllow=127.0.0.53\n','\nIPAddressAllow=127.0.0.53 127.0.0.0/8\n',1)"

echo "== Alarm göndericisi (K-108) =="

N=deploy/systemd/kadran-notify.service
NF=deploy/systemd/kadran-notify-failure@.service

mutate "gönderici kadran kullanıcısıyla koşuyor" "$N" \
    "s=s.replace('\nDynamicUser=yes\n','\nUser=kadran\n',1)"

mutate "hata birimi DynamicUser'ı kaybetti" "$NF" \
    "s=s.replace('\nDynamicUser=yes\n','\n',1)"

mutate "anahtar dosyası daemon'un dizininde" "$N" \
    "s=s.replace('\nLoadCredential=notify:/etc/kadran/notify.conf\n','\nLoadCredential=notify:/var/lib/kadran/notify.conf\n',1)"

mutate "journal grubu yok (alarmlar SESSİZCE görünmez)" "$N" \
    "s=s.replace('\nSupplementaryGroups=systemd-journal\n','\n',1)"

mutate "hata biriminde journal grubu yok" "$NF" \
    "s=s.replace('\nSupplementaryGroups=systemd-journal\n','\n',1)"

mutate "hata biriminde DNS istisnası yok" "$NF" \
    "s=s.replace('\nIPAddressAllow=127.0.0.53\n','\n',1)"

mutate "gönderici IPv6'ya çıkamıyor" "$N" \
    "s=s.replace('\nRestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX\n','\nRestrictAddressFamilies=AF_INET AF_UNIX\n',1)"

mutate "uzak yedek arızası bildirilmiyor" deploy/systemd/kadran-offsite.service \
    "s=s.replace('\nOnFailure=kadran-notify-failure@%n.service\n','\n',1)"

mutate "OnFailure olmayan bir birimi adlandırıyor" deploy/systemd/kadran-offsite.service \
    "s=s.replace('\nOnFailure=kadran-notify-failure@%n.service\n','\nOnFailure=kadran-notify-fail@%n.service\n',1)"

mutate "gönderici journal'ı dolduruyor" "$N" \
    "s=s.replace('\nLogLevelMax=notice\n','\n',1)"

echo "== Hacim arşivleyicisi (K-111) =="

H=deploy/systemd/kadran-volume-backup.service

mutate "arşivleyici YAZMA yetkisi de aldı" "$H" \
    "s=s.replace('\nCapabilityBoundingSet=CAP_DAC_READ_SEARCH\n','\nCapabilityBoundingSet=CAP_DAC_READ_SEARCH CAP_DAC_OVERRIDE\n',1)"

mutate "arşivleyici Docker soketine bağlanabilir" "$H" \
    "s=s.replace('\nRestrictAddressFamilies=none\n','\n',1)"

mutate "arşivleyici ağa çıkabilir" "$H" \
    "s=s.replace('\nPrivateNetwork=yes\n','\n',1)"

mutate "arşivleyici kadran kullanıcısıyla (daemon arşivi silebilir)" "$H" \
    "s=s.replace('\nGroup=kadran\n','\nUser=kadran\nGroup=kadran\n',1)"

mutate "arşivler daemon'un dizininde" "$H" \
    "s=s.replace('\nStateDirectory=kadran-volume-backup\n','\nStateDirectory=kadran/volume-backup\n',1)"

mutate "arşivleyici başka yere de yazabilir" "$H" \
    "s=s.replace('\nProtectSystem=strict\n','\nProtectSystem=strict\nReadWritePaths=/var/lib/kadran\n',1)"

mutate "arşivlere kadran grubu yazabilir" "$H" \
    "s=s.replace('\nUMask=0027\n','\nUMask=0007\n',1)"

mutate "hacim yedeği arızası bildirilmiyor" "$H" \
    "s=s.replace('\nOnFailure=kadran-notify-failure@%n.service\n','\n',1)"

mutate "zamanlayıcı kaçan koşuyu atlıyor" deploy/systemd/kadran-volume-backup.timer \
    "s=s.replace('\nPersistent=true\n','\nPersistent=false\n',1)"

echo "== Ters vekilde reload yok (K-112) =="

C=deploy/systemd/kadran-caddy.service

mutate "ExecReload geri eklendi" "$C" \
    "s=s.replace('\nExecStart=/usr/local/lib/kadran/kadran-caddy run --config /etc/kadran/caddy.json\n','\nExecStart=/usr/local/lib/kadran/kadran-caddy run --config /etc/kadran/caddy.json\nExecReload=/usr/local/lib/kadran/kadran-caddy reload --config /etc/kadran/caddy.json --force\n',1)"

echo "== Daemon watchdog'u (K-115) =="

P=deploy/systemd/kadrand.service

mutate "WatchdogSec yok (tespit sessizce kapalı)" "$P" \
    "s=s.replace('\nWatchdogSec=60s\n','\n',1)"

mutate "WatchdogSec çok kısa" "$P" \
    "s=s.replace('\nWatchdogSec=60s\n','\nWatchdogSec=10s\n',1)"

mutate "watchdog öldürmesi geri getirilmiyor" "$P" \
    "s=s.replace('\nRestart=on-failure\n','\nRestart=no\n',1)"

mutate "yığın dökümü SIGKILL ile yok ediliyor" "$P" \
    "s=s.replace('\nWatchdogSec=60s\n','\nWatchdogSec=60s\nWatchdogSignal=SIGKILL\n',1)"

restore
echo
if [ "$fail" -ne 0 ]; then
    echo "BAŞARISIZ: en az bir mutasyon yakalanmadı ya da uygulanamadı."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
