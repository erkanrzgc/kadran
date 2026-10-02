#!/usr/bin/env bash
# `app show`'un CANLI sürümü doğru gösterdiğini koruyan testlerin GERÇEKTEN
# bir şey koruduğunu sınar (K-112).
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
# Yanlış satırı "canlı" işaretlemek. Geri almadan sonra en üstteki
# "derlendi" satırı trafiği almıyor; onu canlı göstermek, işaretin hiç
# olmamasından DAHA kötü — kullanıcıyı yanlış sürümü incelemeye yollar.
#
# ── K-071/K-080'in dersi ────────────────────────────────────────────
#
# Yeşil kalan bir mutasyon İKİ zıt sonuç doğurabilir: test zayıftır ya da
# MUTASYON zayıftır. Bu yüzden betik, mutasyonun dosyaya uygulanıp
# uygulanmadığını da ayrıca doğruluyor.
set -uo pipefail

cd "$(dirname "$0")/.."
API=internal/api/apps.go
CLI=cmd/kadran/app.go

BAK_API=$(mktemp); BAK_CLI=$(mktemp)
cp "$API" "$BAK_API"; cp "$CLI" "$BAK_CLI"
restore() { cp "$BAK_API" "$API"; cp "$BAK_CLI" "$CLI"; }
trap restore EXIT

fail=0

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

# mutate <ad> <dosya> <test-paketi> <python-ifadesi>
mutate() {
    local name="$1" file="$2" pkg="$3" expr="$4"
    restore
    taban_yesil "$pkg" -count=1
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
p='$file'
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
    # Derlenmeyen bir mutant `go test`'i düşürür ve betik bunu
    # "yakalandı" diye okur (K-096).
    local build_out
    if ! build_out=$(go test "$pkg" -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI,"
        echo "     mutasyon derlenebilir olacak şekilde yazılmalı. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if go test "$pkg" -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Sunucu: canlı sürüm alanı =="

mutate "canlı sürüm hiç doldurulmuyor" "$API" ./internal/api/ \
    "s=s.replace('\t\tactive = d.ReleaseID\n','\t\t_ = d.ReleaseID\n',1)"

# Hiç dağıtılmamış uygulama geçerli bir durum; hata sayılırsa `app show`
# yeni bir uygulamada kırılır.
mutate "aktif dağıtım yokluğu hata sayılıyor" "$API" ./internal/api/ \
    "s=s.replace('\tcase !errors.Is(err, store.ErrNoDeployment):','\tcase err != nil:',1)"

echo "== CLI: işaret =="

# EN PAHALI: yanlış satır.
mutate "ilk satır canlı işaretleniyor" "$CLI" ./cmd/kadran/ \
    "s=s.replace('\t\tcase r.GetReleaseId() == active:','\t\tcase r == releases[0]:',1)"

mutate "her satır canlı işaretleniyor" "$CLI" ./cmd/kadran/ \
    "s=s.replace('\t\tcase r.GetReleaseId() == active:','\t\tcase true:',1)"

mutate "hiçbir satır işaretlenmiyor" "$CLI" ./cmd/kadran/ \
    "s=s.replace('\t\tcase r.GetReleaseId() == active:','\t\tcase false:',1)"

# Geri almadan sonra canlı sürüm kesilmiş listenin dışında kalabilir;
# o zaman bu satır onu gösteren TEK yer.
mutate "listenin dışındaki canlı sürüm söylenmiyor" "$CLI" ./cmd/kadran/ \
    "s=s.replace('\tcase !slices.ContainsFunc(','\tcase false && !slices.ContainsFunc(',1)"

mutate "canlı sürüm yokken susuyor" "$CLI" ./cmd/kadran/ \
    "s=s.replace('\tcase active == \"\":','\tcase false:',1)"

echo "== Eski sunucu: alanın yokluğu (danışman incelemesi) =="

# Alan eklemek protokol sürümünü artırmıyor; yeni CLI eski sunucuyla
# konuşabilir. Yokluğu "boş" okumak, trafik akarken "yönlendirilmiyor"
# demek.
mutate "eski sunucu 'canlı sürüm yok' sayılıyor" "$CLI" ./cmd/kadran/ \
    "s=s.replace('\tknown := resp.ActiveReleaseId != nil\n','\tknown := true\n',1)"

mutate "sunucu alanı boşken göndermiyor" "$API" ./internal/api/ \
    "s=s.replace('ActiveReleaseId: &active,','ActiveReleaseId: func() *string { _ = active; return nil }(),',1)"

restore
if [[ $fail -ne 0 ]]; then
    echo
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi

echo
echo "Bütün mutasyonlar yakalandı."
