#!/usr/bin/env bash
# Göç betiğinin (internal/bootstrap/goc.sh, K-136) dosya adımlarını
# GERÇEKTEN çalıştırır: sahte bir kök dizinde (KOK), root gerekmeden.
#
# Kullanıcı yeniden adlandırma ve systemctl burada sınanmıyor; onlar gerçek
# sunucuda ölçülüyor (K-136, GCP provası). Burada sınanan, yanlış yapıldığında
# VERİ kaybettirecek, erişimi kıracak ya da kurulumu takılı bırakacak
# adımlar: taşıma, önek değiştirme, authorized_keys, uzak yedek
# yapılandırması, root'un dosya yazarken bağ izlememesi ve eski kalıntıların
# temizliği (docker sahte bir betikle).
set -uo pipefail
cd "$(dirname "$0")/.."

fail=0
dene() {
    local ad="$1" kod="$2"
    if bash -c "$kod" >/dev/null 2>&1; then echo "  ✓ $ad"; else echo "  ✗ $ad"; fail=1; fi
}

KOK="$(mktemp -d)"
trap 'rm -rf "$KOK"' EXIT
GOC="$(pwd)/internal/bootstrap/goc.sh"
[ -f "$GOC" ] || { echo "goc.sh yok — ölçüm geçersiz" >&2; exit 1; }

# Her senaryo kendi kökünde, taze bir kabukta: önceki senaryonun durumu
# sonrakini etkilemesin.
on() {
    printf 'set -euo pipefail; export KOK=%q; say(){ :; }; step(){ :; }; die(){ echo "DIE: $*" >&2; exit 97; }; source %q; ' \
        "$KOK/$1" "$GOC"
}
kok() { rm -rf "${KOK:?}/$1"; mkdir -p "$KOK/$1"; printf '%s' "$KOK/$1"; }

echo "== goc_tasi =="
k="$(kok t1)"; mkdir -p "$k/var/lib/panely/backups"; echo veri > "$k/var/lib/panely/panely.db"
dene "eski dizin yeniye taşınıyor, içerik aynı" \
    "$(on t1) goc_tasi \$KOK/var/lib/panely \$KOK/var/lib/kadran; [ ! -e \$KOK/var/lib/panely ] && [ \"\$(cat \$KOK/var/lib/kadran/panely.db)\" = veri ]"
k="$(kok t2)"; mkdir -p "$k/var/lib/panely" "$k/var/lib/kadran"; echo veri > "$k/var/lib/panely/a"
dene "yeni BOŞ dizinse (tmpfiles) eski onun yerine geçiyor" \
    "$(on t2) goc_tasi \$KOK/var/lib/panely \$KOK/var/lib/kadran; [ -f \$KOK/var/lib/kadran/a ]"
k="$(kok t3)"; mkdir -p "$k/var/lib/panely" "$k/var/lib/kadran"; echo eski > "$k/var/lib/panely/a"; echo yeni > "$k/var/lib/kadran/b"
dene "iki taraf da doluysa DURUYOR ve hiçbir şey silinmiyor" \
    "! bash -c '$(on t3) goc_tasi \$KOK/var/lib/panely \$KOK/var/lib/kadran'; [ -f '$k/var/lib/panely/a' ] && [ -f '$k/var/lib/kadran/b' ]"
k="$(kok t4)"
dene "eski yoksa sessizce geçiyor (zaten taşınmış)" \
    "$(on t4) goc_tasi \$KOK/yok \$KOK/var/lib/kadran; [ ! -e \$KOK/var/lib/kadran ]"
k="$(kok t5)"; mkdir -p "$k/var/lib/private/panely-notify"; ln -s private/panely-notify "$k/var/lib/panely-notify"
dene "DynamicUser durumu taşınıyor, boşa bakan bağlantı kalkıyor" \
    "$(on t5) goc_tasi \$KOK/var/lib/private/panely-notify \$KOK/var/lib/private/kadran-notify; if [ -L \$KOK/var/lib/panely-notify ]; then rm -f \$KOK/var/lib/panely-notify; fi; [ -d \$KOK/var/lib/private/kadran-notify ] && [ ! -e \$KOK/var/lib/panely-notify ] && [ ! -L \$KOK/var/lib/panely-notify ]"

echo
echo "== goc_onek =="
k="$(kok o1)"; d="$k/b"; mkdir -p "$d"
for f in panely.db panely.db-wal panely.db-shm panely.db.pre-0008_alarms; do echo "$f" > "$d/$f"; done
dene "veritabanı ve yan dosyaları birlikte yeniden adlanıyor" \
    "$(on o1) goc_onek \$KOK/b panely.db kadran.db; for f in kadran.db kadran.db-wal kadran.db-shm kadran.db.pre-0008_alarms; do [ -f \$KOK/b/\$f ] || exit 1; done; [ -z \"\$(ls \$KOK/b | grep panely)\" ]"
k="$(kok o2)"; d="$k/y"; mkdir -p "$d"
echo a > "$d/panely-20261001T233220Z.db"; echo b > "$d/panely-hacim-web-20261001T000000Z.tar.zst.age"; echo c > "$d/baska.txt"
dene "yalnız önekli dosyalar değişiyor, içerik korunuyor" \
    "$(on o2) goc_onek \$KOK/y panely- kadran-; [ \"\$(cat \$KOK/y/kadran-20261001T233220Z.db)\" = a ] && [ \"\$(cat \$KOK/y/kadran-hacim-web-20261001T000000Z.tar.zst.age)\" = b ] && [ -f \$KOK/y/baska.txt ]"
k="$(kok o3)"; d="$k/y"; mkdir -p "$d"; echo eski > "$d/panely-1.db"; echo yeni > "$d/kadran-1.db"
dene "hedef ad zaten varsa DURUYOR, iki dosya da yerinde" \
    "! bash -c '$(on o3) goc_onek \$KOK/y panely- kadran-'; [ \"\$(cat '$d/panely-1.db')\" = eski ] && [ \"\$(cat '$d/kadran-1.db')\" = yeni ]"
# backups, daemon'un yazabildiği /var/lib/kadran içinde: daemon onu bir
# sembolik bağla değiştirirse root başka bir dizindeki dosyaları yeniden
# adlandırırdı (güvenlik incelemesi, K-136).
k="$(kok o4)"; mkdir -p "$k/baska"; echo onemli > "$k/baska/panely-onemli.conf"; ln -s "$k/baska" "$k/backups"
dene "dizin sembolik bağsa DURUYOR, bağın gösterdiği dosyalar yeniden adlanmıyor" \
    "! bash -c '$(on o4) goc_onek \$KOK/backups panely- kadran-'; [ -f '$k/baska/panely-onemli.conf' ]"

echo
echo "== goc_yerinde_sed: root yeniden yazarken bağ izlemiyor, sırrı açmıyor =="
# authorized_keys kadran-client'ın .ssh dizininde: o kullanıcı orada dosya
# açabilir. Sabit adlı geçici kopyaya önceden konan bağ, root'a istenen
# dosyanın üstüne yazdırıyordu (güvenlik incelemesi, K-136).
k="$(kok s1)"; echo kurban > "$k/kurban"; printf '%s\n' 'command="/usr/local/lib/panely/panely-connect",restrict ssh-ed25519 AAAA x' > "$k/ak"
ln -s "$k/kurban" "$k/ak.goc"
dene "önceden konmuş .goc bağı izlenmiyor: kurban dosyası aynı, ak düz dosya ve çevrilmiş" \
    "$(on s1) goc_ak \$KOK/ak; [ \"\$(cat \$KOK/kurban)\" = kurban ] && [ ! -L \$KOK/ak ] && grep -q kadran-connect \$KOK/ak"
k="$(kok s2)"; printf '%s\n' 'command="/usr/local/lib/panely/panely-connect",restrict ssh-ed25519 AAAA x' > "$k/kurban"; ln -s "$k/kurban" "$k/ak"
dene "dosyanın kendisi sembolik bağsa DURUYOR, bağ ve hedefi değişmiyor" \
    "! bash -c '$(on s2) goc_ak \$KOK/ak'; [ -L '$k/ak' ] && grep -q panely-connect '$k/kurban'"
# rclone.conf 0640 ve /etc/kadran 0755: geçici kopya umask'la (0644)
# açılsaydı anahtar bir an herkese okunur olurdu.
k="$(kok s3)"; printf 'gizli\n' > "$k/rc"; chmod 0640 "$k/rc"
dene "geçici kopya yazılırken yalnız sahibine açık (0600), sonuç eski izinde (0640)" \
    "$(on s3) chown(){ stat -c %a \"\${@: -1}\" > \$KOK/kip; command chown \"\$@\"; }; goc_yerinde_sed \$KOK/rc s/gizli/x/; [ \"\$(cat \$KOK/kip)\" = 600 ] && [ \"\$(stat -c %a \$KOK/rc)\" = 640 ] && [ \"\$(cat \$KOK/rc)\" = x ]"
dene "geride geçici dosya kalmıyor" "[ -z \"\$(ls '$k' | grep goc)\" ]"

echo
echo "== goc_ak: authorized_keys (K-131 satırları) =="
YON='command="/usr/local/lib/panely/panely-connect",restrict ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIYonetici erkan@dizustu'
CI='command="/usr/local/lib/panely/panely-connect -deploy=portfolio",restrict ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDagitim ci'
YABANCI='ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIYabanci panely-connect geçen yorum'
k="$(kok a1)"; printf '%s\n%s\n%s\n' "$YON" "$CI" "$YABANCI" > "$k/ak"; chmod 0600 "$k/ak"
dene "iki satırın yolu yeni ada geçiyor, -deploy kapsamı korunuyor" \
    "$(on a1) goc_ak \$KOK/ak; grep -qxF 'command=\"/usr/local/lib/kadran/kadran-connect\",restrict ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIYonetici erkan@dizustu' \$KOK/ak && grep -qxF 'command=\"/usr/local/lib/kadran/kadran-connect -deploy=portfolio\",restrict ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDagitim ci' \$KOK/ak"
dene "zorlanmamış satıra dokunulmuyor (denetim onu ayrıca yakalar)" \
    "grep -qxF '$YABANCI' '$k/ak' && [ \"\$(grep -c '' '$k/ak')\" = 3 ]"
dene "izin korunuyor (0600)" "[ \"\$(stat -c %a '$k/ak')\" = 600 ]"
# Elle yazılmış, başka seçenekle başlayan bir satır: kurulum böyle satır
# YAZMAZ. Dokunulmaz; kurulum sonrası denetim (kisitsiz_satir_sayisi) onu
# yeni yola zorlanmamış diye yakalar ve kurulum başarısız sayılır.
ELLE='from="10.0.0.1",command="/usr/local/lib/panely/panely-connect",restrict ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIElle elle'
k="$(kok a3)"; printf '%s\n%s\n' "$YON" "$ELLE" > "$k/ak"
dene "yalnız kurulumun yazdığı biçim değişiyor; elle yazılmış satır aynen kalıyor" \
    "$(on a3) goc_ak \$KOK/ak; grep -qxF '$ELLE' \$KOK/ak"
k="$(kok a2)"; printf '%s\n' "$YON" > "$k/ak"; cp "$k/ak" "$k/once"
dene "ikinci koşu dosyayı değiştirmiyor (idempotent)" \
    "$(on a2) goc_ak \$KOK/ak; cp \$KOK/ak \$KOK/bir; goc_ak \$KOK/ak; cmp -s \$KOK/ak \$KOK/bir && ! cmp -s \$KOK/ak \$KOK/once"

echo
echo "== goc_uzak_yedek: rclone hedefi =="
uy() {
    local k; k="$(kok "$1")"
    printf '[panely-offsite]\ntype = s3\nprovider = Cloudflare\naccess_key_id = GIZLI\n' > "$k/rclone.conf"
    printf 'OFFSITE_REMOTE=panely-offsite:panely-yedek\nOFFSITE_KEEP=48\n' > "$k/offsite.conf"
    chmod 0640 "$k/rclone.conf" "$k/offsite.conf"
}
uy u1
dene "hedefin adı iki dosyada birlikte değişiyor, kova adı KORUNUYOR" \
    "$(on u1) goc_uzak_yedek \$KOK; grep -qx '\\[kadran-offsite\\]' \$KOK/rclone.conf && grep -qx 'OFFSITE_REMOTE=kadran-offsite:panely-yedek' \$KOK/offsite.conf && grep -qx 'access_key_id = GIZLI' \$KOK/rclone.conf"
dene "izinler korunuyor (0640)" "[ \"\$(stat -c %a '$KOK/u1/rclone.conf')\" = 640 ] && [ \"\$(stat -c %a '$KOK/u1/offsite.conf')\" = 640 ]"
uy u2; printf 'OFFSITE_REMOTE=benim-hedefim:kova\n' > "$KOK/u2/offsite.conf"
dene "kullanıcının kendi adlı hedefine dokunulmuyor" \
    "$(on u2) goc_uzak_yedek \$KOK; grep -qx '\\[panely-offsite\\]' \$KOK/rclone.conf && grep -qx 'OFFSITE_REMOTE=benim-hedefim:kova' \$KOK/offsite.conf"
uy u3; printf '[kadran-offsite]\ntype = s3\n' >> "$KOK/u3/rclone.conf"
dene "iki hedef birden varsa DURUYOR, dosyalar değişmiyor" \
    "! bash -c '$(on u3) goc_uzak_yedek \$KOK'; grep -qx 'OFFSITE_REMOTE=panely-offsite:panely-yedek' '$KOK/u3/offsite.conf'"
# İki sed arasında kesilen koşu: rclone.conf çevrilmiş, offsite.conf değil.
# Eski sürüm her yeniden koşuda burada DURUYORDU; o noktada eski kontrol
# düzlemi çoktan durmuş olurdu (güvenlik incelemesi, K-136).
uy u4; sed -i 's/^\[panely-offsite\]$/[kadran-offsite]/' "$KOK/u4/rclone.conf"
dene "yarıda kalmış koşu (rclone.conf çevrilmiş, offsite.conf değil) yeniden koşunca TAMAMLANIYOR" \
    "$(on u4) goc_uzak_yedek \$KOK; grep -qx '\\[kadran-offsite\\]' \$KOK/rclone.conf && grep -qx 'OFFSITE_REMOTE=kadran-offsite:panely-yedek' \$KOK/offsite.conf"
uy u5; printf '[baska]\ntype = s3\n' > "$KOK/u5/rclone.conf"
dene "offsite.conf eski hedefi gösteriyor ama rclone.conf'ta ikisi de yoksa DURUYOR" \
    "! bash -c '$(on u5) goc_uzak_yedek \$KOK'; grep -qx 'OFFSITE_REMOTE=panely-offsite:panely-yedek' '$KOK/u5/offsite.conf'"

echo
echo "== goc_dropin_tasi: operatör drop-in'leri (K-056 beyaz listesi) =="
DROPIN='[Service]
ExecStart=
ExecStart=/usr/local/lib/panely/panely-exec --socket /run/panely-exec/exec.sock --journal /var/lib/panely-exec/exec-audit.log --allow-user panely --owner-group panely --allow-repo erkanrzgc/portfolio,crccheck/docker-hello-world'
k="$(kok d1)"; mkdir -p "$k/etc/systemd/system/panely-exec.service.d"
printf '%s\n' "$DROPIN" > "$k/etc/systemd/system/panely-exec.service.d/10-allow-repo.conf"
dene "drop-in yeni birim adına kopyalanıyor, yollar ve kullanıcılar çevriliyor, beyaz liste aynen kalıyor" \
    "$(on d1) goc_dropin_tasi panely-exec.service; f=\$KOK/etc/systemd/system/kadran-exec.service.d/10-allow-repo.conf; grep -qxF 'ExecStart=/usr/local/lib/kadran/kadran-exec --socket /run/kadran-exec/exec.sock --journal /var/lib/kadran-exec/exec-audit.log --allow-user kadran --owner-group kadran --allow-repo erkanrzgc/portfolio,crccheck/docker-hello-world' \$f && ! grep -q panely \$f"
dene "eski drop-in yerinde (saklamak goc_birimleri_kaldir'ın işi)" \
    "[ -f '$k/etc/systemd/system/panely-exec.service.d/10-allow-repo.conf' ]"
k="$(kok d2)"; mkdir -p "$k/etc/systemd/system/panely-exec.service.d" "$k/etc/systemd/system/kadran-exec.service.d"
printf '%s\n' "$DROPIN" > "$k/etc/systemd/system/panely-exec.service.d/10-allow-repo.conf"
echo elle-duzeltildi > "$k/etc/systemd/system/kadran-exec.service.d/10-allow-repo.conf"
dene "hedefte aynı adlı dosya varsa üstüne yazılmıyor (yeniden koşu)" \
    "$(on d2) goc_dropin_tasi panely-exec.service; [ \"\$(cat \$KOK/etc/systemd/system/kadran-exec.service.d/10-allow-repo.conf)\" = elle-duzeltildi ]"
k="$(kok d3)"; mkdir -p "$k/etc/systemd/system/panely-exec.service.d"
printf '%s\n' "$DROPIN" > "$k/etc/systemd/system/panely-exec.service.d/10-allow-repo.conf"
dene "goc_birimleri_kaldir: drop-in hem yeniye geçiyor hem eskisi geri dönüş için saklanıyor" \
    "$(on d3) goc_birimleri_kaldir panely-exec.service; [ -f \$KOK/etc/systemd/system/kadran-exec.service.d/10-allow-repo.conf ] && [ -f \$KOK/var/lib/kadran-goc/eski-birimler/panely-exec.service.d/10-allow-repo.conf ] && [ ! -e \$KOK/etc/systemd/system/panely-exec.service.d ]"

echo
echo "== goc_izinli_depo: etkin beyaz liste =="
GOSTER='ExecStart={ path=/usr/local/lib/panely/panely-exec ; argv[]=/usr/local/lib/panely/panely-exec --socket /run/panely-exec/exec.sock --allow-user panely --allow-repo erkanrzgc/portfolio,crccheck/docker-hello-world ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }'
k="$(kok i1)"
dene "systemctl show çıktısından değer çıkıyor" \
    "$(on i1) [ \"\$(goc_izinli_depo '$GOSTER')\" = '--allow-repo erkanrzgc/portfolio,crccheck/docker-hello-world' ]"
dene "beyaz liste yoksa boş, kurulum DURMUYOR" \
    "$(on i1) x=\"\$(goc_izinli_depo 'ExecStart={ argv[]=/x --socket /y ; }')\"; [ -z \"\$x\" ]"
dene "= biçimi ve birden çok bayrak yakalanıyor" \
    "$(on i1) [ \"\$(goc_izinli_depo 'argv[]=/x --allow-repo=a/b --allow-repo c/d ;')\" = \"\$(printf -- '--allow-repo=a/b\n--allow-repo c/d')\" ]"
# Pozitif kontrol: systemctl okunamadığında boş çıktı "beyaz liste yok"
# sayılırsa iki uç da boş görünür ve karşılaştırma geçerdi.
dene "systemctl çıktısı boşsa 'beyaz liste yok' SAYILMIYOR, DURUYOR" \
    "! bash -c '$(on i1) goc_izinli_depo \"\" >/dev/null'"
dene "komut satırı taşımayan çıktı (dbus hatası) da DURUYOR" \
    "! bash -c '$(on i1) goc_izinli_depo \"Failed to connect to bus\" >/dev/null'"

echo
echo "== goc_ip_var / goc_temizlenebilir: eski konteynerler ne zaman silinir =="
YAPI='{"apps":{"http":{"servers":{"srv0":{"routes":[{"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"172.21.0.20:8000"}]}]}]}}}}}'
k="$(kok r1)"
dene "upstream'deki IP bulunuyor" "$(on r1) goc_ip_var '$YAPI' 172.21.0.20"
dene "bir IP başka bir IP'nin ÖNEKİ olarak sayılmıyor (172.21.0.2 ≠ 172.21.0.20)" \
    "! bash -c '$(on r1) goc_ip_var '\"'\"'$YAPI'\"'\"' 172.21.0.2'"
dene "eski konteyner trafik alıyorsa SİLİNMİYOR" \
    "! bash -c '$(on r1) goc_temizlenebilir '\"'\"'$YAPI'\"'\"' 172.18.0.2 172.21.0.20'"
dene "eski konteynerlerin hiçbiri yapılandırmada yoksa siliniyor (rotasız uygulama engel değil)" \
    "$(on r1) goc_temizlenebilir '$YAPI' 172.18.0.2 172.18.0.3"
dene "yapılandırma OKUNAMADIYSA silinmiyor" \
    "! bash -c '$(on r1) goc_temizlenebilir \"\" 172.18.0.2'"
dene "eski konteyner hiç yoksa ve yapılandırma okunduysa siliniyor" \
    "$(on r1) goc_temizlenebilir '$YAPI'"

echo
echo "== goc_temizle: bir kalıntı silinemese de kurulum DÜŞMÜYOR =="
# Kullanımdaki bir ağ ya da imaj set -e altında kurulumu düşürüyordu; kalıntı
# kaldığı için sonraki her kurulum aynı satırda ölürdü (güvenlik incelemesi,
# K-136). Sahte docker: konteyner silinir, ağ ve imaj silinemez.
k="$(kok c1)"; mkdir -p "$k/bin"
cat > "$k/bin/docker" <<'EOF'
#!/bin/sh
case "$1 $2" in
    "ps -aq") echo eskikonteyner ;;
    "network ls") echo panely-web ;;
    "network rm") echo "ağ kullanımda" >&2; exit 1 ;;
    "images --filter") echo panely/web:abc ;;
    rmi*) echo "imaj kullanımda" >&2; exit 1 ;;
esac
exit 0
EOF
chmod +x "$k/bin/docker"
dene "ağ ve imaj silinemezse uyarı verip sürüyor" \
    "$(on c1) PATH=\$KOK/bin:\$PATH; goc_vekil_yapisi(){ echo '{\"apps\":{}}'; }; goc_ipler(){ :; }; say(){ echo \"\$*\"; }; goc_temizle > \$KOK/cikti 2>&1; echo sonrasi >> \$KOK/cikti; grep -q 'ağlar kaldırılamadı' \$KOK/cikti && grep -q 'imaj etiketleri kaldırılamadı' \$KOK/cikti && grep -q sonrasi \$KOK/cikti && ! grep -q 'kalıntılar kaldırıldı' \$KOK/cikti"

echo
echo "== install.sh: eski kalıntılar ancak doğrulama GEÇİNCE temizleniyor =="
# Temizlik doğrulamadan önce koşuyordu: düşerse 17 kontrol hiç koşmazdı ve
# doğrulama düşse de eski konteynerler (son çalışan sürüm) silinirdi.
IS=internal/bootstrap/install.sh
dene "goc_temizle çağrısı 'doğrulama başarısız' çıkışından SONRA, tek yerde" \
    "d=\$(grep -n 'kurulum sonrası doğrulama başarısız' $IS | cut -d: -f1); t=\$(grep -n '^    goc_temizle\$' $IS | cut -d: -f1); [ -n \"\$d\" ] && [ -n \"\$t\" ] && [ \"\$(printf '%s\n' \"\$t\" | wc -l)\" = 1 ] && [ \"\$t\" -gt \"\$d\" ]"

echo
echo "== goc_gerekli =="
k="$(kok g1)"
dene "temiz kökte göç GEREKMİYOR" "! bash -c '$(on g1) goc_gerekli'"
k="$(kok g2)"; mkdir -p "$k/usr/local/lib/panely"
dene "eski ikili dizini göçü tetikliyor" "$(on g2) goc_gerekli"
k="$(kok g4)"; mkdir -p "$k/etc"; echo 'panely-client:x:996:987::/var/lib/panely-client:/bin/sh' > "$k/etc/passwd"
dene "yalnız eski kullanıcı kalmışsa da göç tetikleniyor" "$(on g4) goc_gerekli"
k="$(kok g3)"; mkdir -p "$k/var/lib/kadran-goc"; touch "$k/var/lib/kadran-goc/asama1"
dene "yarıda kalmış göç (asama1 var, tamam yok) yeniden tetikliyor" "$(on g3) goc_gerekli"
touch "$k/var/lib/kadran-goc/tamam"
dene "bitmiş göç tetiklemiyor" "! bash -c '$(on g3) goc_gerekli'"
mkdir -p "$k/usr/local/lib/panely"
dene "bitmiş göç, eski bir iz kalsa da yeniden BAŞLAMIYOR (bayat kayıtla göç yok)" \
    "! bash -c '$(on g3) goc_gerekli'"

echo
echo "== KONTROL: düzenek kırmızıyı görebiliyor =="
# goc_tasi'yi "iki taraf doluysa üstüne yaz" diye bozan bir sürüm yukarıdaki
# ölçümü kızartmalı; kızartmazsa ölçüm bir şey kanıtlamıyordu.
BOZUK="$(mktemp)"; trap 'rm -rf "$KOK" "$BOZUK"' EXIT
sed 's/            die "göç: hem \$eski hem \$yeni var ve \$yeni boş değil./            rm -rf "$yeni"; mv "$eski" "$yeni"; return 0; die "x/' "$GOC" > "$BOZUK"
grep -q 'rm -rf "$yeni"; mv' "$BOZUK" || { echo "  ✗ KONTROL: mutant uygulanamadı — ölçüm geçersiz"; fail=1; }
k="$(kok m1)"; mkdir -p "$k/a" "$k/b"; echo eski > "$k/a/x"; echo yeni > "$k/b/y"
dene "KONTROL: bozuk goc_tasi dolu hedefi SİLİYOR (yani ölçüm bunu görür)" \
    "bash -c 'set -euo pipefail; export KOK=$k; say(){ :; }; die(){ exit 97; }; source $BOZUK; goc_tasi \$KOK/a \$KOK/b'; [ ! -e '$k/b/y' ]"

echo
[ "$fail" -eq 0 ] && echo "goc.sh: hepsi geçti" || { echo "goc.sh: BAŞARISIZ"; exit 1; }
