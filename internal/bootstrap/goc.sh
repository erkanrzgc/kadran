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
    # Bitmiş göç bir daha başlamaz; kalıntı temizliği ayrı (goc_temizle).
    [ -e "$GOC_DIR/tamam" ] && return 1
    [ -e "$GOC_DIR/asama1" ] && return 0
    local ad yol
    for ad in panely panely-client panely-caddy; do
        goc_kullanici_var "$ad" && return 0
    done
    for yol in "$ESKI_LIB" "$KOK/var/lib/panely" "$KOK/etc/panely" \
               "$KOK/etc/systemd/system/panelyd.service" \
               "$KOK/etc/systemd/system/panely-caddy.service"; do
        [ -e "$yol" ] && return 0
    done
    return 1
}

# goc_kullanici_var <ad> — kullanıcı var mı. Sahte kökte (testler) kökün
# /etc/passwd'ına bakar: testi koşturan makinenin GERÇEK kullanıcıları
# (ör. eski kurulumlu bir sunucu) sonucu karıştırmasın.
goc_kullanici_var() {
    if [ -n "$KOK" ]; then
        grep -q "^$1:" "$KOK/etc/passwd" 2>/dev/null
    else
        getent passwd "$1" >/dev/null 2>&1
    fi
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
            die "migration: both $eski and $yeni exist and $yeni is not empty.
Decide by hand which one is real; neither was deleted."
        fi
    fi
    mv "$eski" "$yeni"
}

# goc_onek <dizin> <eski önek> <yeni önek> — dizindeki (alt dizinlere
# inmeden) adı eski önekle başlayan girdileri yeni önekle yeniden
# adlandırır. Hedef ad zaten varsa DURUR. Dizin sembolik bağsa DURUR:
# backups daemon'un yazabildiği /var/lib/kadran içinde ve root bağın
# gösterdiği başka bir dizindeki dosyaları yeniden adlandırırdı.
goc_onek() {
    local dizin="$1" eski="$2" yeni="$3" f ad hedef
    if [ -L "$dizin" ]; then
        die "migration: $dizin is a symlink; the files in it were not renamed"
    fi
    [ -d "$dizin" ] || return 0
    for f in "$dizin/$eski"*; do
        [ -e "$f" ] || continue
        ad="${f##*/}"
        hedef="$dizin/$yeni${ad#"$eski"}"
        [ -e "$hedef" ] && die "migration: $hedef already exists ($f could not be moved)"
        mv "$f" "$hedef"
    done
}

# goc_yerinde_sed <dosya> <sed ifadesi> — dosyayı geçici kopyayla yeniden
# yazar; sahiplik ve izin korunur. İçerik ekrana basılmaz (sır taşıyabilir).
#
# Dosya sembolik bağsa DURUR. Geçici kopya mktemp ile açılır: adı tahmin
# edilemez (authorized_keys kadran-client'ın dizininde; o kullanıcı sabit
# adlı kopyanın yerine bir bağ koyup root'a istediği dosyanın üstüne
# yazdırabiliyordu) ve 0600 doğar (rclone.conf'un anahtarı umask'la 0644
# açılan kopyada bir an herkese okunurdu). Güvenlik incelemesi, K-136.
#
# Kalan dar pencere: dizinin sahibi mktemp ile sed arasında kopyayı bağla
# değiştirebilir. Kapatmak kopyayı o kullanıcı olarak yazmayı gerektirir
# (install.sh bunu K-139'dan beri yapıyor: istemci_olarak). Burada KABUL:
# kadran-client'ın tek yetkisi zorlanmış komut, göç yalnız v0.3.x ve
# öncesinden yükseltmede bir kez koşuyor ve göç sınaması sahte kökte,
# root'suz (kullanıcı değiştiremiyor).
goc_yerinde_sed() {
    local f="$1" ifade="$2" gecici
    if [ -L "$f" ]; then
        die "migration: $f is a symlink; not rewritten in place"
    fi
    gecici="$(mktemp "$f.goc.XXXXXX")"
    sed "$ifade" "$f" > "$gecici"
    chown --reference="$f" "$gecici"
    chmod --reference="$f" "$gecici"
    mv -fT "$gecici" "$f"
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
# Kova adı ve yol KORUNUR: kova kullanıcının hesabında.
#
# Önce rclone.conf, sonra offsite.conf; her biri kendi durumuna bakar. İki
# sed arasında kesilen bir koşu (rclone.conf çevrilmiş, offsite.conf değil)
# yeniden koşunca tamamlanır. Eskiden burada DURUYORDU ve o noktada eski
# kontrol düzlemi çoktan durmuş olurdu (güvenlik incelemesi, K-136).
goc_uzak_yedek() {
    local rc="$1/rclone.conf" oc="$1/offsite.conf"
    [ -f "$oc" ] && [ -f "$rc" ] || return 0
    grep -q '^OFFSITE_REMOTE=panely-offsite:' "$oc" || return 0
    if grep -qx '\[panely-offsite\]' "$rc"; then
        if grep -qx '\[kadran-offsite\]' "$rc"; then
            die "migration: rclone.conf has both [panely-offsite] and [kadran-offsite]"
        fi
        goc_yerinde_sed "$rc" 's/^\[panely-offsite\]$/[kadran-offsite]/'
    elif ! grep -qx '\[kadran-offsite\]' "$rc"; then
        die "migration: offsite.conf points at panely-offsite but rclone.conf has neither [panely-offsite] nor [kadran-offsite]"
    fi
    goc_yerinde_sed "$oc" 's/^OFFSITE_REMOTE=panely-offsite:/OFFSITE_REMOTE=kadran-offsite:/'
}

# goc_kullanici <eski> <yeni> <ev> <açıklama> — grubu ve kullanıcıyı
# yeniden adlandırır; uid/gid aynı kalır. Ev dizini alanı da güncellenir
# (dizinin kendisi goc_tasi ile taşınır).
goc_kullanici() {
    local eski="$1" yeni="$2" ev="$3" aciklama="$4"
    if getent group "$eski" >/dev/null; then
        getent group "$yeni" >/dev/null && die "migration: both groups $eski and $yeni exist"
        groupmod -n "$yeni" "$eski"
    fi
    if getent passwd "$eski" >/dev/null; then
        getent passwd "$yeni" >/dev/null && die "migration: both users $eski and $yeni exist"
        # usermod, kullanıcının süreci varken reddeder; o sürecin birimi
        # buraya gelmeden durdurulmuş olmalı.
        usermod -l "$yeni" -d "$ev" -c "$aciklama" "$eski" \
            || die "migration: could not rename $eski (does it have a running process? ps -u $eski)"
    fi
}

# goc_birimleri_kaldir <birim…> — eski birim dosyalarını ve drop-in
# dizinlerini GOC_DIR'e taşır (geri dönüş betiği onları geri koyar).
goc_birimleri_kaldir() {
    local b
    install -d -m 0700 "$GOC_DIR/eski-birimler"
    for b in "$@"; do
        goc_dropin_tasi "$b"
        goc_tasi "$KOK/etc/systemd/system/$b" "$GOC_DIR/eski-birimler/$b"
        goc_tasi "$KOK/etc/systemd/system/$b.d" "$GOC_DIR/eski-birimler/$b.d"
    done
}

# goc_dropin_tasi <eski birim> — operatörün drop-in'lerini (`systemctl edit`)
# yeni birim adına KOPYALAR; içerikteki eski adlar (yollar, kullanıcılar,
# ikililer) yeniye çevrilir. Eski dizin geri dönüş için goc_birimleri_kaldir
# tarafından saklanır.
#
# Neden şart: canlıda kadran-exec'in depo beyaz listesi (--allow-repo,
# K-056) bir drop-in'de. Taşınmasaydı yeni executor kısıtsız açılırdı:
# sessiz bir güvenlik gerilemesi. goc_izinli_depo_dogrula bunu ayrıca
# denetler. Hedefte aynı adlı dosya varsa dokunulmaz (yeniden koşu).
goc_dropin_tasi() {
    local eski="$1" yeni d f hedef
    d="$KOK/etc/systemd/system/$eski.d"
    [ -d "$d" ] || return 0
    yeni="${eski//panely/kadran}"
    install -d -m 0755 "$KOK/etc/systemd/system/$yeni.d"
    for f in "$d"/*; do
        [ -f "$f" ] || continue
        hedef="$KOK/etc/systemd/system/$yeni.d/${f##*/}"
        [ -e "$hedef" ] && continue
        sed 's/panely/kadran/g' "$f" > "$hedef"
        chmod --reference="$f" "$hedef"
        say "drop-in moved, old names rewritten (review it): $yeni.d/${f##*/}"
    done
}

# goc_izinli_depo <systemctl show -p ExecStart çıktısı> — etkin komut
# satırındaki her `--allow-repo` değerini satır satır yazar (yoksa boş).
# `--allow-repo x` ve `--allow-repo=x` ikisi de yakalanır.
#
# Pozitif kontrol: çıktıda komut satırı (`argv[]=`) yoksa DURUR. systemctl
# okunamadığında boş çıktı "beyaz liste yok" sayılsaydı, iki uç birden
# okunamadığında karşılaştırma eşit görünür ve kısıtsız bir executor
# geçerdi (güvenlik incelemesi, K-136).
goc_izinli_depo() {
    if [[ "$1" != *'argv[]='* ]]; then
        die "migration: could not read the executor's command line (systemctl show); cannot verify the repository allow-list"
    fi
    printf '%s\n' "$1" | { grep -oE -- '--allow-repo[ =][^ ;]*' || true; }
}

# goc_izinli_depo_dogrula — yeni executor'ın etkin depo beyaz listesi,
# göçten önceki executor'ınkiyle AYNI olmalı. Değilse DURUR: yeni
# executor henüz başlamadı, site eskiyle açık.
goc_izinli_depo_dogrula() {
    local once simdi
    [ -e "$GOC_DIR/izinli-depo" ] || return 0
    once="$(cat "$GOC_DIR/izinli-depo")"
    simdi="$(goc_izinli_depo "$(systemctl show -p ExecStart kadran-exec.service)")"
    if [ "$once" != "$simdi" ]; then
        die "migration: the executor's repository allow-list changed.
  before: ${once:-<none>}
  after : ${simdi:-<none>}
That would be a security regression; kadran-exec was NOT STARTED.
Check the drop-in: systemctl cat kadran-exec.service"
    fi
    say "repository allow-list kept: ${simdi:-<none>}"
}

# goc_1 — kontrol düzlemini durdurur ve ters vekil DIŞINDAKİ her şeyi yeni
# ada taşır. Eski ters vekil ve eski konteynerler bu sırada siteyi sunmaya
# devam eder.
goc_1() {
    step "Migration: panely → kadran (K-136)"
    install -d -m 0700 -o root -g root "$GOC_DIR"

    # Geri dönüş betiği İLK iş kurulur: göç yarıda kalsa da sunucuda hazır.
    install -d -m 0755 -o root -g root "$LIB_DIR"
    install -m 0644 -o root -g root "$STAGE/goc.sh" "$LIB_DIR/goc.sh"
    install -m 0755 -o root -g root "$STAGE/geri.sh" "$LIB_DIR/kadran-geri-donus.sh"
    say "rollback script: $LIB_DIR/kadran-geri-donus.sh"

    # Süren bir istemci oturumu (ör. bir CI dağıtımı) yarıda kesilmesin.
    if getent passwd panely-client >/dev/null && pgrep -u panely-client >/dev/null; then
        die "panely-client has an open session (a deploy may be running).
Wait for it to finish and run the install again."
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
    if [ ! -e "$GOC_DIR/kurulu" ]; then
        goc_kurulu_secimliler > "$GOC_DIR/kurulu.yeni"
        mv "$GOC_DIR/kurulu.yeni" "$GOC_DIR/kurulu"
    fi
    if [ ! -e "$GOC_DIR/beklenen" ]; then
        docker ps --filter label=panely.app_id --filter status=running \
            --format '{{.Label "panely.app_id"}} {{.Label "panely.release_id"}} {{.Label "panely.replica"}}' \
            > "$GOC_DIR/beklenen.yeni"
        mv "$GOC_DIR/beklenen.yeni" "$GOC_DIR/beklenen"
    fi
    say "running old replicas: $(grep -c '' "$GOC_DIR/beklenen"), enabled timers: $(tr '\n' ' ' < "$GOC_DIR/zamanlayicilar")"

    # İmajlar yeni adla etiketlenir: yeni konteynerler yeniden derlemeden
    # kurulur. Eski etiketler göç bitene kadar kalır (geri dönüş yolu).
    local imaj n=0
    while read -r imaj; do
        [ -n "$imaj" ] || continue
        docker tag "$imaj" "kadran/${imaj#panely/}"
        n=$((n + 1))
    done < <(docker images --filter reference='panely/*' --format '{{.Repository}}:{{.Tag}}')
    say "$n images tagged under kadran/"

    # Eski executor'ın ETKİN depo beyaz listesi (drop-in dahil); yenisi
    # başlamadan aynısı istenir (goc_izinli_depo_dogrula).
    if [ ! -e "$GOC_DIR/izinli-depo" ] && [ -e "$KOK/etc/systemd/system/panely-exec.service" ]; then
        goc_izinli_depo "$(systemctl show -p ExecStart panely-exec.service)" > "$GOC_DIR/izinli-depo.yeni"
        mv "$GOC_DIR/izinli-depo.yeni" "$GOC_DIR/izinli-depo"
    fi

    # Kontrol düzlemi ve zamanlayıcılar durur. Ters vekil ÇALIŞMAYA DEVAM
    # eder; konteynerler zaten kontrol düzleminden bağımsız.
    local b
    for b in "${ESKI_BIRIMLER[@]}"; do
        case "$b" in *@.service) continue ;; esac
        [ -e "$KOK/etc/systemd/system/$b" ] || continue
        systemctl disable --now "$b" >/dev/null 2>&1 || systemctl stop "$b" 2>/dev/null || true
    done
    if findmnt -n "$KOK/var/lib/panely/volumes" >/dev/null 2>&1; then
        umount "$KOK/var/lib/panely/volumes" || die "migration: could not unmount the old volume root"
    fi

    # Veritabanının daemon DURMUŞKEN alınmış kopyası (-wal ve -shm dahil).
    if [ -f "$KOK/var/lib/panely/panely.db" ] && [ ! -e "$GOC_DIR/veritabani" ]; then
        install -d -m 0700 "$GOC_DIR/veritabani.yeni"
        cp -a "$KOK/var/lib/panely/panely.db"* "$GOC_DIR/veritabani.yeni/"
        mv "$GOC_DIR/veritabani.yeni" "$GOC_DIR/veritabani"
        say "database copy: $GOC_DIR/veritabani"
    fi

    goc_kullanici panely kadran /var/lib/kadran "Kadran control plane"
    goc_kullanici panely-client kadran-client /var/lib/kadran-client "Kadran client access"

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
    [ -s "$GOC_DIR/beklenen" ] || { say "no old replicas expected"; return 0; }
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
        [ $((SECONDS - bas)) -ge "$sure" ] && die "migration: $eksik replicas did not come up under the new name within ${sure}s.
The site is up on the OLD reverse proxy and OLD containers. For the reason:
  journalctl -u kadrand -n 50
Fix it and run the install again; the migration resumes where it stopped."
        sleep 2
    done
    say "new replicas running ($((SECONDS - bas))s)"
}

# goc_vekil — eski ters vekili durdurur ve yeni ada taşır. KESİNTİ BURADA
# başlar ve install.sh'ın ters_vekil_kur'u kadran-caddy'yi başlatınca biter
# (rotaları kadrand'ın vekil izleyicisi yazar, K-055).
goc_vekil() {
    local b
    # Kesinti raporu için: eski vekilin GERÇEKTEN rotaladığı uygulamalar.
    # Alan adı olmayan bir uygulamanın rotası yoktur; onu beklemek anlamsız.
    goc_rotali_kaydet
    local mevcut=()
    for b in "${ESKI_VEKIL_BIRIMLERI[@]}"; do
        [ -e "$KOK/etc/systemd/system/$b" ] && mevcut+=("$b")
    done
    if [ "${#mevcut[@]}" -gt 0 ]; then
        # ÖNCE devre dışı (systemd'yi yeniden yükler, eski vekil hâlâ
        # sunuyor), SONRA durdur: yeniden yükleme kesinti penceresine girmez.
        systemctl disable "${mevcut[@]}" >/dev/null 2>&1 || true
        date +%s > "$GOC_DIR/vekil-durdu"
        systemctl stop "${mevcut[@]}" 2>/dev/null || true
    else
        date +%s > "$GOC_DIR/vekil-durdu"
    fi
    goc_kullanici panely-caddy kadran-caddy /var/lib/kadran-caddy "Kadran reverse proxy"
    goc_tasi "$KOK/var/lib/panely-caddy" "$KOK/var/lib/kadran-caddy"
    rm -f "$KOK/etc/tmpfiles.d/panely-caddy.conf"
    # Birim dosyaları kenara alınır; systemd'yi yeniden yüklemek goc_bitir'de
    # (pencerenin DIŞINDA). Yüklü kalan eski tanımlar durmuş ve devre dışı.
    goc_birimleri_kaldir "${ESKI_VEKIL_BIRIMLERI[@]}"
    rm -rf "$KOK/run/panely-caddy"
}

# goc_secimli — göçten önce KURULU olan seçimli birimlerin yeni adlı
# birimlerini ve betiklerini kurar; yalnızca önceden ETKİN olan
# zamanlayıcıları açar.
goc_secimli() {
    local s liste
    liste="$(goc_secimli_listesi)"
    [ -n "$liste" ] || return 0
    while read -r s; do
        [ -n "$s" ] || continue
        secimli_dosyalari_kur "$s"
        if grep -qx "$s" "$GOC_DIR/zamanlayicilar"; then
            say "optional unit installed: kadran-$s"
        else
            say "optional unit installed, its timer stays DISABLED as before: kadran-$s"
        fi
    done <<< "$liste"
    systemctl daemon-reload
    while read -r s; do
        [ -n "$s" ] || continue
        systemctl enable --now "kadran-$s.timer"
    done < "$GOC_DIR/zamanlayicilar"
}

# secimli_dosyalari_kur <ad> — bir seçimli birimin betiğini, belgesini ve
# systemd birimlerini paketten kurar. Zamanlayıcıyı açmaz, kapatmaz.
#
# Dosya adları AÇIK yazılıyor (değişkenden kurulmuyor): paket testi
# (TestArchiveCarriesEveryStageFileTheScriptsRead) her `$STAGE/<ad>`'ı
# pakette arıyor; kurulan adı göremeseydi eksik dosyayı da göremezdi.
secimli_dosyalari_kur() {
    case "$1" in
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
        *) die "unknown optional unit: $1" ;;
    esac
}

# secimli_guncelle — göç olmayan kurulumda (yükseltme) KURULU seçimli
# birimleri pakettekiyle değiştirir (K-138). Seçimli birimleri kullanıcı elle
# ya da v0.4.0 göçü kuruyor; bu adım yokken yükseltme onları eski sürümde
# bırakıyordu: v0.4.1'in çapaları canlıda bu yüzden yüklenmedi.
#
# Kurulu = zamanlayıcı dosyası var. Zamanlayıcı açıksa açık, kapalıysa
# kapalı kalır. Maskelenmiş birim (/dev/null'a bağ) atlanır: üstüne gerçek
# birim yazmak maskeyi kaldırırdı. Operatörün değişiklikleri drop-in'de
# durduğu için korunur; birim dosyasının kendisini düzenleyen, değişikliğini
# kaybeder (göçle aynı).
secimli_guncelle() {
    local s z guncel=0
    for s in notify offsite volume-backup; do
        z="$KOK/etc/systemd/system/kadran-$s.timer"
        [ -e "$z" ] || continue
        if [ "$(readlink "$z")" = /dev/null ]; then
            say "optional unit is masked, not updated: kadran-$s"
            continue
        fi
        secimli_dosyalari_kur "$s"
        say "optional unit updated (timer left alone): kadran-$s"
        guncel=1
    done
    [ "$guncel" -eq 0 ] || systemctl daemon-reload
}

# goc_kurulu_secimliler — eski adla KURULU seçimli birimler, zamanlayıcısı
# etkin olsun olmasın. Kapalı bir zamanlayıcı da kullanıcının kurduğu bir
# birim: göç onu yeni adla kurar ama açmaz (rc6 provası: kapalı bildirim
# birimi sessizce düşüyordu).
goc_kurulu_secimliler() {
    local s
    for s in "${SECIMLI[@]}"; do
        if [ -e "$KOK/etc/systemd/system/panely-$s.timer" ]; then echo "$s"; fi
    done
}

# goc_secimli_listesi — yeni adla kurulacak seçimliler: kurulu ∪ etkin.
# Etkinler ayrıca eklenir: kurulu kaydı olmayan (bu düzeltmeden önceki bir
# sürümle başlamış) bir göçte de düşmesinler.
goc_secimli_listesi() {
    { cat "$GOC_DIR/kurulu" 2>/dev/null || true; cat "$GOC_DIR/zamanlayicilar" 2>/dev/null || true; } |
        { grep -v '^$' || true; } | sort -u
}

# goc_birim_kur <ad> — hazırlık dizinindeki birimi systemd dizinine kurar.
goc_birim_kur() {
    [ -f "$STAGE/$1" ] || die "migration: $1 is missing from the install package"
    install -m 0644 -o root -g root "$STAGE/$1" "$KOK/etc/systemd/system/$1"
}

# goc_ip_var <yapılandırma> <ip…> — Caddy yapılandırmasında verilen IP'lerden
# biri upstream olarak geçiyor mu. Eşleşme `"IP:` biçiminde: 172.21.0.2,
# 172.21.0.20'nin ÖNEKİ olarak sayılmasın.
goc_ip_var() {
    local yapi="$1" ip
    shift
    for ip in "$@"; do
        [ -n "$ip" ] || continue
        [[ "$yapi" == *"\"$ip:"* ]] && return 0
    done
    return 1
}

# goc_temizlenebilir <yapılandırma> <eski ip…> — eski konteynerler silinebilir
# mi: yapılandırma OKUNABİLDİ ve eski konteynerlerin hiçbiri trafik almıyor.
# Yeni konteynerlerin rotalı olup olmadığı burada sorulmuyor: eski bir
# konteyneri silmenin güvenliği yalnızca onun trafik alıp almadığına bağlı.
goc_temizlenebilir() {
    local yapi="$1"
    shift
    [ -n "$yapi" ] || return 1
    ! goc_ip_var "$yapi" "$@"
}

# goc_ipler <docker filtresi> — süzgece uyan ÇALIŞAN konteynerlerin IP'leri.
goc_ipler() {
    docker ps -q --filter "$1" --filter status=running |
        xargs -r docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}'
}

# goc_vekil_yapisi <admin soketi> — ters vekilin canlı yapılandırması (boş:
# okunamadı).
goc_vekil_yapisi() {
    command -v curl >/dev/null || return 0
    curl -sf --unix-socket "$1" http://localhost/config/ 2>/dev/null || true
}

# goc_rotali_kaydet — eski vekilin yapılandırmasında IP'si geçen uygulamalar
# (her satırda bir uygulama). Okunamazsa liste boş: kesinti yine ölçülür ama
# bekleme yapılmaz.
goc_rotali_kaydet() {
    local yapi uyg
    [ -e "$GOC_DIR/rotali" ] && return 0
    yapi="$(goc_vekil_yapisi /run/panely-caddy/admin.sock)"
    while read -r uyg; do
        [ -n "$uyg" ] || continue
        # shellcheck disable=SC2046
        if goc_ip_var "$yapi" $(goc_ipler "label=panely.app_id=$uyg"); then
            echo "$uyg"
        fi
    done < <(docker ps --filter label=panely.app_id --filter status=running \
                 --format '{{.Label "panely.app_id"}}' | sort -u) > "$GOC_DIR/rotali.yeni"
    mv "$GOC_DIR/rotali.yeni" "$GOC_DIR/rotali"
    say "apps routed by the old proxy: $(grep -c '' "$GOC_DIR/rotali")"
}

# goc_rota_bekle — rotalı her uygulamanın yeni konteynerlerinden biri yeni
# vekilde görünene kadar bekler ve kesintiyi raporlar. Görünmezse UYARIR
# (durmaz): yeni vekil çalışıyor, sebep kadrand'ın günlüğünde.
goc_rota_bekle() {
    local sure="${GOC_ROTA_SN:-60}" bas yapi uyg eksik
    [ -s "$GOC_DIR/rotali" ] || { say "no routed apps; not waiting"; return 0; }
    bas=$SECONDS
    while :; do
        yapi="$(goc_vekil_yapisi /run/kadran-caddy/admin.sock)"
        eksik=0
        while read -r uyg; do
            [ -n "$uyg" ] || continue
            # shellcheck disable=SC2046
            goc_ip_var "$yapi" $(goc_ipler "label=kadran.app_id=$uyg") || eksik=$((eksik + 1))
        done < "$GOC_DIR/rotali"
        [ "$eksik" -eq 0 ] && break
        if [ $((SECONDS - bas)) -ge "$sure" ]; then
            say "⚠ the routes of $eksik apps did not appear on the new proxy within ${sure}s: journalctl -u kadrand -n 50"
            return 0
        fi
        sleep 1
    done
    say "reverse proxy gap (old proxy stopped → new routes): ~$(( $(date +%s) - $(cat "$GOC_DIR/vekil-durdu") ))s"
}

# goc_bitir — göç tamam. Eski ikililer kenara alınır (eski vekil artık
# durdu, onlara gerek yok). Temizlik AYRI: goc_temizle.
goc_bitir() {
    systemctl daemon-reload
    goc_tasi "$ESKI_LIB" "$GOC_DIR/eski-lib"
    touch "$GOC_DIR/tamam"
    say "migration complete; old binaries and database copy: $GOC_DIR"
}

# goc_artik_var — eski adlı Docker kalıntısı var mı (konteyner, ağ, imaj).
goc_artik_var() {
    local aglar
    [ -n "$(docker ps -aq --filter label=panely.app_id)" ] && return 0
    # Çıktı önce değişkene: `… | grep -q` pipefail altında yarışa açık
    # (grep erken çıkar, sol taraf SIGPIPE alır; install.sh'taki not).
    aglar="$(docker network ls --format '{{.Name}}')"
    [[ $'\n'"$aglar" == *$'\n'panely-* ]] && return 0
    [ -n "$(docker images -q --filter reference='panely/*')" ]
}

# goc_temizle — eski konteynerleri, ağları ve imaj etiketlerini kaldırır;
# ancak hiçbiri trafik almıyorsa. Göçten bağımsız ve tekrar denenebilir:
# her kurulumda kalıntı varsa çağrılır.
#
# Hiçbir adım kurulumu DÜŞÜRMEZ: kullanımdaki bir ağ ya da imaj set -e
# altında kurulumu öldürürdü ve kalıntı durduğu için sonraki her kurulum
# aynı yerde ölürdü (güvenlik incelemesi, K-136). Kalan, sonraki kurulumda
# yeniden denenir.
goc_temizle() {
    local yapi eksik=0
    yapi="$(goc_vekil_yapisi /run/kadran-caddy/admin.sock)"
    # shellcheck disable=SC2046
    if ! goc_temizlenebilir "$yapi" $(goc_ipler label=panely.app_id); then
        say "⚠ old containers not removed: the reverse proxy's configuration could not be read or they still get traffic."
        say "  The next install tries again. By hand:"
        goc_temizle_yazdir
        return 0
    fi
    docker ps -aq --filter label=panely.app_id | xargs -r docker rm -f >/dev/null ||
        { say "⚠ some old containers could not be removed"; eksik=1; }
    docker network ls --format '{{.Name}}' | { grep '^panely-' || true; } |
        xargs -r docker network rm >/dev/null ||
        { say "⚠ some old networks could not be removed"; eksik=1; }
    docker images --filter reference='panely/*' --format '{{.Repository}}:{{.Tag}}' |
        xargs -r docker rmi >/dev/null ||
        { say "⚠ some old image tags could not be removed"; eksik=1; }
    if [ "$eksik" -eq 0 ]; then
        say "old leftovers removed (containers, networks, image tags)"
    else
        say "  The next install retries the rest. By hand:"
        goc_temizle_yazdir
    fi
}

goc_temizle_yazdir() {
    cat <<EOF
  The old containers may still be RUNNING. If the site is up under the new names, by hand:
    docker ps -aq --filter label=panely.app_id | xargs -r docker rm -f
    docker network ls --format '{{.Name}}' | grep '^panely-' | xargs -r docker network rm
    docker images --filter reference='panely/*' --format '{{.Repository}}:{{.Tag}}' | xargs -r docker rmi
EOF
}
