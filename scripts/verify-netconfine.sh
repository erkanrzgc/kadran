#!/usr/bin/env bash
# panelyd'nin ağ çitini GERÇEK systemd altında ölçer.
#
# ── Neden cgroup'a girerek? ─────────────────────────────────────────
#
# systemd'nin IPAddressAllow/Deny politikası bir BPF programıdır ve
# birimin CGROUP'una bağlıdır. Aynı cgroup'a konan her süreç aynı
# filtreden geçer. Dolayısıyla oraya bir yoklayıcı sokmak, politikayı
# "yeniden kurup sınamak" değil, ÇALIŞAN BİRİMİN kendi politikasını
# ölçmektir (K-052: doğrulanan yapılandırma, gönderilen yapılandırma
# olmalı).
#
# ── Neden ham TCP, HTTP değil? ──────────────────────────────────────
#
# IPAddressAllow SOKET katmanında çalışıyor. HTTP ile ölçmek araya bir
# çeviri katmanı koyar ve o katmanın kendi arızaları ölçümü kirletir:
# ilk denemede 1.1.1.1'in 301 dönmesi ve 22 numaralı portun HTTP
# konuşmaması yüzünden kontrol grubu "başarısız" göründü. Bağlantının
# KURULUP kurulmadığı, sorulması gereken asıl soru.
#
# ── Kontrol grubu ZORUNLU ───────────────────────────────────────────
#
# "1.1.1.1'e ulaşılamadı" tek başına hiçbir şey kanıtlamaz: sunucunun
# interneti kapalı da olabilir. Her hedef iki kez yoklanıyor — cgroup'un
# İÇİNDE ve DIŞINDA. Anlamlı olan fark.
set -uo pipefail

CG=/sys/fs/cgroup/system.slice/panelyd.service
T=5

[ -d "$CG" ] || { echo "HATA: cgroup yok: $CG" >&2; exit 1; }

# Yoklama hedefi ÇALIŞAN bir konteynerden alınıyor — sabit yazmak, adres
# değişince testi sessizce anlamsızlaştırırdı.
cip="$(docker inspect panely_portfolio_r2_0 \
    --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' 2>/dev/null)"
[ -n "$cip" ] || { echo "HATA: konteyner adresi okunamadı" >&2; exit 1; }

# connect <host> <port> → 0 = TCP bağlantısı kuruldu
connect() { timeout "$T" bash -c "exec 3<>/dev/tcp/$1/$2" 2>/dev/null; }

# inside <host> <port> → aynı şey, ama panelyd'nin cgroup'unda
inside() {
    sh -c "echo \$\$ > $CG/cgroup.procs 2>/dev/null; \
        exec timeout $T bash -c 'exec 3<>/dev/tcp/$1/$2'" 2>/dev/null
}

fail=0
printf '%-32s %-14s %-14s %s\n' 'HEDEF' 'panelyd içi' 'kontrol' 'SONUÇ'
printf '%-32s %-14s %-14s %s\n' '─────' '───────────' '───────' '─────'

check() { # check <ad> <host> <port> <allow|deny>
    local name="$1" host="$2" port="$3" want="$4" i o istr ostr verdict

    inside "$host" "$port" && i=0 || i=1
    connect "$host" "$port" && o=0 || o=1
    [ "$i" -eq 0 ] && istr="BAĞLANDI" || istr="engellendi"
    [ "$o" -eq 0 ] && ostr="BAĞLANDI" || ostr="erişilemez"

    if [ "$want" = allow ]; then
        if [ "$i" -eq 0 ]; then verdict="✓ izin doğru"
        else verdict="✗ İZİNLİ HEDEFE ULAŞILAMADI"; fail=1; fi
    else
        if [ "$o" -ne 0 ]; then
            # Kontrol grubu ulaşamıyorsa çit sınanmamıştır.
            verdict="⚠ KONTROL DA ERİŞEMEDİ — ölçüm geçersiz"; fail=1
        elif [ "$i" -eq 0 ]; then
            verdict="✗ ÇİT SIZDIRIYOR"; fail=1
        else
            verdict="✓ engellendi"
        fi
    fi
    printf '%-32s %-14s %-14s %s\n' "$name" "$istr" "$ostr" "$verdict"
}

# Pozitif kontrol ÖNCE: çitin ölçülebildiğini ve yoklamanın ulaşması
# gereken yere ULAŞTIĞINI kanıtlar. Bu geçmeden negatif iddiaların
# anlamı yok.
check "konteyner $cip:8080" "$cip" 8080 allow

check "1.1.1.1:80 (internet)"       1.1.1.1         80 deny
check "169.254.169.254:80 (metadata)" 169.254.169.254 80 deny
check "127.0.0.1:22 (host sshd)"    127.0.0.1       22 deny

# ⚠ 10.0.0.0/8 gibi "izin listesinde olmayan başka bir özel ağ" hedefi
# KASTEN yok. O aralıkta dinleyen bir şey olmadığı için kontrol grubu da
# bağlanamıyor ve "engellendi" ile "zaten kimse yok" ayırt EDİLEMİYOR.
# Ölçemediğimiz bir şeyi geçmiş saymaktansa hiç sınamamak dürüst.
# İzin listesinin darlığını birim testi (netconfine_test.go) sabitliyor.

echo
if [ "$fail" -eq 0 ]; then
    echo "SONUÇ: çit ölçüldü ve ISIRIYOR."
else
    echo "SONUÇ: bazı satırlar geçmedi — yukarı bak." >&2
    exit 1
fi
