# shellcheck shell=bash
# panely → kadran yerinde göç (K-136).
#
# install.sh bu dosyayı `source` eder; tek başına çalıştırılmaz. say, step
# ve die install.sh'tan gelir.
#
# # Neden yerinde?
#
# Kullanıcılar `usermod -l` ile yeniden adlandırılıyor: uid/gid değişmez,
# dosya sahipliği olduğu gibi kalır. Yeni kullanıcı açıp chown etmek her
# dosyaya dokunmak demekti ve yarıda kalırsa sahiplik karışırdı.
#
# # Sitenin kesintisi neden kısa?
#
# Eski ters vekil ve eski konteynerler, yeni kontrol düzlemi kendi
# konteynerlerini kurana kadar çalışmaya devam eder. Kesinti yalnızca ters
# vekil değişirken olur (goc_vekil). Yeni konteynerler ayağa kalkmazsa göç
# o noktada DURUR ve site eskiyle açık kalır.
#
# # Yarıda kalırsa
#
# Her adım durumuna bakarak karar verir: eski ad var mı, yeni ad var mı.
# Kurulum yeniden koşturulunca kaldığı yerden sürer. İki ad birden varsa
# ve yenisi boş değilse DURUR; hiçbir şey silinmez.
#
# KOK: testlerin sahte kök dizini (scripts/check-goc-sh.sh). Kurulumda boş.

KOK="${KOK:-}"
GOC_DIR="$KOK/var/lib/kadran-goc"
ESKI_LIB="$KOK/usr/local/lib/panely"
ESKI_CONNECT='command="/usr/local/lib/panely/panely-connect'
YENI_CONNECT='command="/usr/local/lib/kadran/kadran-connect'

# Kontrol düzlemi ve seçimli birimler. Ters vekilinkiler AYRI: onlar en son
# durur (goc_vekil).
ESKI_BIRIMLER=(
    panelyd.service panely-exec.service var-lib-panely-volumes.mount
    panely-notify.service panely-notify.timer panely-notify-failure@.service
    panely-offsite.service panely-offsite.timer
    panely-volume-backup.service panely-volume-backup.timer
)
ESKI_VEKIL_BIRIMLERI=(panely-caddy.service panely-caddy-admin.socket)
SECIMLI=(notify offsite volume-backup)

# goc_gerekli — sunucuda eski adlı kurulumun izi var mı. Yarıda kalmış bir
# göç de "gerekli" sayılır.
goc_gerekli() {
    [ -e "$GOC_DIR/asama1" ] && [ ! -e "$GOC_DIR/tamam" ] && return 0
    local ad yol
    for ad in panely panely-client panely-caddy; do
        getent passwd "$ad" >/dev/null 2>&1 && return 0
    done
    for yol in "$ESKI_LIB" "$KOK/var/lib/panely" "$KOK/etc/panely" \
               "$KOK/etc/systemd/system/panelyd.service" \
               "$KOK/etc/systemd/system/panely-caddy.service"; do
        [ -e "$yol" ] && return 0
    done
    return 1
}

# goc_tasi <eski> <yeni> — eskiyi yeniye taşır. Yeni yalnızca BOŞ bir
# dizinse (ör. tmpfiles yarattı) onun yerine geçilir; başka her durumda
# DURUR. Eski yoksa bir şey yapmaz (zaten taşınmış).
goc_tasi() {
    local eski="$1" yeni="$2"
    [ -e "$eski" ] || [ -L "$eski" ] || return 0
    if [ -e "$yeni" ] || [ -L "$yeni" ]; then
        if [ -d "$yeni" ] && [ ! -L "$yeni" ] && [ -z "$(ls -A "$yeni")" ]; then
            rmdir "$yeni"
        else
            die "göç: hem $eski hem $yeni var ve $yeni boş değil.
Hangisinin gerçek olduğuna elle karar verilmeli; hiçbiri silinmedi."
        fi
    fi
    mv "$eski" "$yeni"
}

# goc_onek <dizin> <eski önek> <yeni önek> — dizindeki (alt dizinlere
# inmeden) adı eski önekle başlayan girdileri yeni önekle yeniden
# adlandırır. Hedef ad zaten varsa DURUR.
goc_onek() {
    local dizin="$1" eski="$2" yeni="$3" f ad hedef
    [ -d "$dizin" ] || return 0
    for f in "$dizin/$eski"*; do
        [ -e "$f" ] || continue
        ad="${f##*/}"
        hedef="$dizin/$yeni${ad#"$eski"}"
        [ -e "$hedef" ] && die "göç: $hedef zaten var ($f taşınamadı)"
        mv "$f" "$hedef"
    done
}

# goc_yerinde_sed <dosya> <sed ifadesi> — dosyayı geçici kopyayla yeniden
# yazar; sahiplik ve izin korunur. İçerik ekrana basılmaz (sır taşıyabilir).
goc_yerinde_sed() {
    local f="$1" ifade="$2"
    sed "$ifade" "$f" > "$f.goc"
    chown --reference="$f" "$f.goc"
    chmod --reference="$f" "$f.goc"
    mv "$f.goc" "$f"
}

# goc_ak <authorized_keys> — zorlanmış komutun YOLUNU yeni ada çevirir.
# Yalnız satır başındaki tam eski yol değişir; seçenekler (-deploy=…),
# anahtar ve yorum aynen kalır. Kurulum sonrası denetim (kisitsiz_satir_sayisi)
# yeni yola zorlanmamış her satırı yakalar.
goc_ak() {
    local f="$1"
    [ -f "$f" ] || return 0
    grep -qF "$ESKI_CONNECT" "$f" || return 0
    goc_yerinde_sed "$f" "s#^$ESKI_CONNECT#$YENI_CONNECT#"
}

# goc_uzak_yedek <etc dizini> — rclone hedefinin ADINI çevirir
# ([panely-offsite] → [kadran-offsite]) ve offsite.conf'taki başvuruyu.
# İkisi birlikte değişir ya da hiç değişmez: yalnız biri değişse uzak yedek
# hedefi bulamazdı. Kova adı ve yol KORUNUR: kova kullanıcının hesabında.
goc_uzak_yedek() {
    local etc="$1"
    [ -f "$etc/offsite.conf" ] && [ -f "$etc/rclone.conf" ] || return 0
    grep -q '^OFFSITE_REMOTE=panely-offsite:' "$etc/offsite.conf" || return 0
    grep -qx '\[panely-offsite\]' "$etc/rclone.conf" \
        || die "göç: offsite.conf panely-offsite hedefini gösteriyor ama rclone.conf'ta [panely-offsite] yok"
    if grep -qx '\[kadran-offsite\]' "$etc/rclone.conf"; then
        die "göç: rclone.conf'ta hem [panely-offsite] hem [kadran-offsite] var"
    fi
    goc_yerinde_sed "$etc/rclone.conf" 's/^\[panely-offsite\]$/[kadran-offsite]/'
    goc_yerinde_sed "$etc/offsite.conf" 's/^OFFSITE_REMOTE=panely-offsite:/OFFSITE_REMOTE=kadran-offsite:/'
}

# goc_kullanici <eski> <yeni> <ev> <açıklama> — grubu ve kullanıcıyı
# yeniden adlandırır; uid/gid aynı kalır. Ev dizini alanı da güncellenir
# (dizinin kendisi goc_tasi ile taşınır).
goc_kullanici() {
    local eski="$1" yeni="$2" ev="$3" aciklama="$4"
    if getent group "$eski" >/dev/null; then
        getent group "$yeni" >/dev/null && die "göç: hem $eski hem $yeni grubu var"
        groupmod -n "$yeni" "$eski"
    fi
    if getent passwd "$eski" >/dev/null; then
        getent passwd "$yeni" >/dev/null && die "göç: hem $eski hem $yeni kullanıcısı var"
        # usermod, kullanıcının süreci varken reddeder; o sürecin birimi
        # buraya gelmeden durdurulmuş olmalı.
        usermod -l "$yeni" -d "$ev" -c "$aciklama" "$eski" \
            || die "göç: $eski yeniden adlandırılamadı (açık süreci var mı? ps -u $eski)"
    fi
}

# goc_birimleri_kaldir <birim…> — eski birim dosyalarını ve drop-in
# dizinlerini GOC_DIR'e taşır (geri dönüş betiği onları geri koyar).
goc_birimleri_kaldir() {
    local b
    install -d -m 0700 "$GOC_DIR/eski-birimler"
    for b in "$@"; do
        goc_tasi "$KOK/etc/systemd/system/$b" "$GOC_DIR/eski-birimler/$b"
        goc_tasi "$KOK/etc/systemd/system/$b.d" "$GOC_DIR/eski-birimler/$b.d"
    done
}

# goc_1 — kontrol düzlemini durdurur ve ters vekil DIŞINDAKİ her şeyi yeni
# ada taşır. Eski ters vekil ve eski konteynerler bu sırada siteyi sunmaya
# devam eder.
goc_1() {
    step "Göç: panely → kadran (K-136)"
    install -d -m 0700 -o root -g root "$GOC_DIR"

    # Süren bir istemci oturumu (ör. bir CI dağıtımı) yarıda kesilmesin.
    if getent passwd panely-client >/dev/null && pgrep -u panely-client >/dev/null; then
        die "panely-client'ın açık oturumu var (süren bir dağıtım olabilir).
Bitmesini bekleyip kurulumu yeniden çalıştır."
    fi

    # İlk koşunun gözlemleri saklanır; yeniden koşuda üzerine yazılmaz.
    if [ ! -e "$GOC_DIR/zamanlayicilar" ]; then
        local s
        for s in "${SECIMLI[@]}"; do
            if [ "$(systemctl is-enabled "panely-$s.timer" 2>/dev/null)" = enabled ]; then
                echo "$s"
            fi
        done > "$GOC_DIR/zamanlayicilar.yeni"
        mv "$GOC_DIR/zamanlayicilar.yeni" "$GOC_DIR/zamanlayicilar"
    fi
    if [ ! -e "$GOC_DIR/beklenen" ]; then
        docker ps --filter label=panely.app_id --filter status=running \
            --format '{{.Label "panely.app_id"}} {{.Label "panely.release_id"}} {{.Label "panely.replica"}}' \
            > "$GOC_DIR/beklenen.yeni"
        mv "$GOC_DIR/beklenen.yeni" "$GOC_DIR/beklenen"
    fi
    say "çalışan eski replika: $(grep -c '' "$GOC_DIR/beklenen"), etkin zamanlayıcı: $(tr '\n' ' ' < "$GOC_DIR/zamanlayicilar")"

    # İmajlar yeni adla etiketlenir: yeni konteynerler yeniden derlemeden
    # kurulur. Eski etiketler göç bitene kadar kalır (geri dönüş yolu).
    local imaj n=0
    while read -r imaj; do
        [ -n "$imaj" ] || continue
        docker tag "$imaj" "kadran/${imaj#panely/}"
        n=$((n + 1))
    done < <(docker images --filter reference='panely/*' --format '{{.Repository}}:{{.Tag}}')
    say "$n imaj kadran/ adıyla etiketlendi"

    # Kontrol düzlemi ve zamanlayıcılar durur. Ters vekil ÇALIŞMAYA DEVAM
    # eder; konteynerler zaten kontrol düzleminden bağımsız.
    local b
    for b in "${ESKI_BIRIMLER[@]}"; do
        case "$b" in *@.service) continue ;; esac
        [ -e "$KOK/etc/systemd/system/$b" ] || continue
        systemctl disable --now "$b" >/dev/null 2>&1 || systemctl stop "$b" 2>/dev/null || true
    done
    if findmnt -n "$KOK/var/lib/panely/volumes" >/dev/null 2>&1; then
        umount "$KOK/var/lib/panely/volumes" || die "göç: eski hacim kökü ayrılamadı"
    fi

    # Veritabanının daemon DURMUŞKEN alınmış kopyası (-wal ve -shm dahil).
    if [ -f "$KOK/var/lib/panely/panely.db" ] && [ ! -e "$GOC_DIR/veritabani" ]; then
        install -d -m 0700 "$GOC_DIR/veritabani.yeni"
        cp -a "$KOK/var/lib/panely/panely.db"* "$GOC_DIR/veritabani.yeni/"
        mv "$GOC_DIR/veritabani.yeni" "$GOC_DIR/veritabani"
        say "veritabanının kopyası: $GOC_DIR/veritabani"
    fi

    goc_kullanici panely kadran /var/lib/kadran "Kadran kontrol düzlemi"
    goc_kullanici panely-client kadran-client /var/lib/kadran-client "Kadran istemci erişimi"

    goc_tasi "$KOK/var/lib/panely" "$KOK/var/lib/kadran"
    goc_onek "$KOK/var/lib/kadran" panely.db kadran.db
    goc_onek "$KOK/var/lib/kadran/backups" panely- kadran-
    goc_tasi "$KOK/var/lib/panely-exec" "$KOK/var/lib/kadran-exec"
    goc_tasi "$KOK/var/lib/panely-client" "$KOK/var/lib/kadran-client"
    goc_ak "$KOK/var/lib/kadran-client/.ssh/authorized_keys"
    goc_tasi "$KOK/var/lib/panely-volume-backup" "$KOK/var/lib/kadran-volume-backup"
    goc_onek "$KOK/var/lib/kadran-volume-backup" panely-hacim- kadran-hacim-
    # DynamicUser'ın durumu private/ altında; üstteki bağlantıyı systemd
    # birimi başlatırken yeniden kurar. Eski bağlantı artık boşluğa bakar.
    goc_tasi "$KOK/var/lib/private/panely-notify" "$KOK/var/lib/private/kadran-notify"
    if [ -L "$KOK/var/lib/panely-notify" ]; then rm -f "$KOK/var/lib/panely-notify"; fi

    # /etc/panely: caddy.json'ı çalışan eski vekil yalnız açılışta okudu;
    # taşımak onu etkilemez. Yeni caddy.json kurulumda yeniden yazılır.
    goc_tasi "$KOK/etc/panely" "$KOK/etc/kadran"
    goc_uzak_yedek "$KOK/etc/kadran"
    rm -f "$KOK/etc/ssh/sshd_config.d/60-panely.conf" "$KOK/etc/tmpfiles.d/panely.conf"

    goc_birimleri_kaldir "${ESKI_BIRIMLER[@]}"
    systemctl daemon-reload
    rm -rf "$KOK/run/panely" "$KOK/run/panely-exec"
    touch "$GOC_DIR/asama1"
}

# goc_bekle — her eski çalışan replikanın yeni etiketli karşılığı çalışana
# kadar bekler. Gelmezse DURUR: site eski vekil ve eski konteynerlerle açık.
goc_bekle() {
    local sure="${GOC_BEKLE_SN:-300}" bas uyg sur rep eksik
    [ -s "$GOC_DIR/beklenen" ] || { say "beklenen eski replika yok"; return 0; }
    bas=$SECONDS
    while :; do
        eksik=0
        while read -r uyg sur rep; do
            [ -n "$uyg" ] || continue
            [ -n "$(docker ps -q --filter "label=kadran.app_id=$uyg" \
                --filter "label=kadran.release_id=$sur" \
                --filter "label=kadran.replica=$rep" --filter status=running)" ] \
                || eksik=$((eksik + 1))
        done < "$GOC_DIR/beklenen"
        [ "$eksik" -eq 0 ] && break
        [ $((SECONDS - bas)) -ge "$sure" ] && die "göç: $eksik replika ${sure} sn içinde yeni adla ayağa kalkmadı.
Site ESKİ ters vekil ve ESKİ konteynerlerle açık. Sebep için:
  journalctl -u kadrand -n 50
Düzeltip kurulumu yeniden çalıştır; göç kaldığı yerden sürer."
        sleep 2
    done
    say "yeni replikalar çalışıyor ($((SECONDS - bas)) sn)"
}

# goc_vekil — eski ters vekili durdurur ve yeni ada taşır. KESİNTİ BURADA
# başlar ve install.sh'ın ters_vekil_kur'u kadran-caddy'yi başlatınca biter
# (rotaları kadrand'ın vekil izleyicisi yazar, K-055).
goc_vekil() {
    local b
    for b in "${ESKI_VEKIL_BIRIMLERI[@]}"; do
        [ -e "$KOK/etc/systemd/system/$b" ] || continue
        systemctl disable --now "$b" >/dev/null 2>&1 || systemctl stop "$b" 2>/dev/null || true
    done
    date +%s > "$GOC_DIR/vekil-durdu"
    goc_kullanici panely-caddy kadran-caddy /var/lib/kadran-caddy "Kadran ters vekili"
    goc_tasi "$KOK/var/lib/panely-caddy" "$KOK/var/lib/kadran-caddy"
    rm -f "$KOK/etc/tmpfiles.d/panely-caddy.conf"
    goc_birimleri_kaldir "${ESKI_VEKIL_BIRIMLERI[@]}"
    systemctl daemon-reload
    rm -rf "$KOK/run/panely-caddy"
}

# goc_secimli — göçten önce etkin olan seçimli zamanlayıcıların yeni
# birimlerini ve betiklerini kurar, aynı zamanlayıcıları etkinleştirir.
goc_secimli() {
    local s
    [ -s "$GOC_DIR/zamanlayicilar" ] || return 0
    # Dosya adları AÇIK yazılıyor (değişkenden kurulmuyor): paket testi
    # (TestArchiveCarriesEveryStageFileTheScriptsRead) her `$STAGE/<ad>`'ı
    # pakette arıyor; kurulan adı göremeseydi eksik dosyayı da göremezdi.
    while read -r s; do
        [ -n "$s" ] || continue
        case "$s" in
            notify)
                install -d -m 0755 -o root -g root "$LIB_DIR/notify"
                install -m 0755 -o root -g root "$STAGE/kadran-notify.sh" "$LIB_DIR/notify/kadran-notify.sh"
                install -m 0644 -o root -g root "$STAGE/notify-README.md" "$LIB_DIR/notify/README.md"
                goc_birim_kur kadran-notify-failure@.service
                goc_birim_kur kadran-notify.service
                goc_birim_kur kadran-notify.timer ;;
            offsite)
                install -d -m 0755 -o root -g root "$LIB_DIR/offsite"
                install -m 0755 -o root -g root "$STAGE/kadran-offsite.sh" "$LIB_DIR/offsite/kadran-offsite.sh"
                install -m 0644 -o root -g root "$STAGE/offsite-README.md" "$LIB_DIR/offsite/README.md"
                goc_birim_kur kadran-offsite.service
                goc_birim_kur kadran-offsite.timer ;;
            volume-backup)
                install -d -m 0755 -o root -g root "$LIB_DIR/offsite"
                install -m 0755 -o root -g root "$STAGE/kadran-volume-backup.sh" \
                    "$LIB_DIR/offsite/kadran-volume-backup.sh"
                goc_birim_kur kadran-volume-backup.service
                goc_birim_kur kadran-volume-backup.timer ;;
            *) die "göç: tanınmayan zamanlayıcı kaydı: $s" ;;
        esac
        say "seçimli birim kuruldu: kadran-$s"
    done < "$GOC_DIR/zamanlayicilar"
    systemctl daemon-reload
    while read -r s; do
        [ -n "$s" ] || continue
        systemctl enable --now "kadran-$s.timer"
    done < "$GOC_DIR/zamanlayicilar"
}

# goc_birim_kur <ad> — hazırlık dizinindeki birimi systemd dizinine kurar.
goc_birim_kur() {
    [ -f "$STAGE/$1" ] || die "göç: $1 kurulum paketinde yok"
    install -m 0644 -o root -g root "$STAGE/$1" "$KOK/etc/systemd/system/$1"
}

# goc_temizle — ters vekilin rotaları YENİ konteynerlere gidiyorsa eski
# konteynerleri, ağları ve etiketleri kaldırır. Doğrulanamazsa eskilere
# DOKUNMAZ ve ne yapılacağını yazar.
goc_temizle() {
    local sure="${GOC_ROTA_SN:-60}" bas yapi ipler ip eski_ipler ok=0
    if ! command -v curl >/dev/null; then
        say "⚠ curl yok: rotalar doğrulanamadı, eski konteynerlere dokunulmadı"
        goc_temizle_yazdir; return 0
    fi
    ipler="$(docker ps -q --filter label=kadran.app_id --filter status=running |
        xargs -r docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}')"
    eski_ipler="$(docker ps -q --filter label=panely.app_id --filter status=running |
        xargs -r docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}')"
    bas=$SECONDS
    while [ $((SECONDS - bas)) -lt "$sure" ]; do
        yapi="$(curl -sf --unix-socket /run/kadran-caddy/admin.sock http://localhost/config/ 2>/dev/null || true)"
        ok=1
        for ip in $ipler; do [[ "$yapi" == *"\"$ip:"* ]] || ok=0; done
        for ip in $eski_ipler; do [[ "$yapi" == *"\"$ip:"* ]] && ok=0; done
        [ "$ok" -eq 1 ] && break
        sleep 1
    done
    if [ "$ok" -ne 1 ]; then
        say "⚠ ters vekilin rotaları $sure sn içinde yeni konteynerlere geçmedi; eski konteynerlere dokunulmadı"
        goc_temizle_yazdir; return 0
    fi
    if [ -e "$GOC_DIR/vekil-durdu" ]; then
        say "ters vekil kesintisi (durdurma → yeni rotalar): ~$(( $(date +%s) - $(cat "$GOC_DIR/vekil-durdu") )) sn"
    fi

    docker ps -aq --filter label=panely.app_id | xargs -r docker rm -f >/dev/null
    docker network ls --format '{{.Name}}' | { grep '^panely-' || true; } |
        xargs -r docker network rm >/dev/null
    docker images --filter reference='panely/*' --format '{{.Repository}}:{{.Tag}}' |
        xargs -r docker rmi >/dev/null
    goc_tasi "$ESKI_LIB" "$GOC_DIR/eski-lib"
    touch "$GOC_DIR/tamam"
    say "eski konteynerler, ağlar ve imaj etiketleri kaldırıldı; eski ikililer $GOC_DIR/eski-lib"
}

goc_temizle_yazdir() {
    cat <<EOF
  Eski konteynerler ÇALIŞIYOR olabilir. Site yeni adlarla açıksa elle:
    docker ps -aq --filter label=panely.app_id | xargs -r docker rm -f
    docker network ls --format '{{.Name}}' | grep '^panely-' | xargs -r docker network rm
    docker images --filter reference='panely/*' --format '{{.Repository}}:{{.Tag}}' | xargs -r docker rmi
EOF
}
