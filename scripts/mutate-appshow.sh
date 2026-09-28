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
CLI=cmd/panely/app.go

BAK_API=$(mktemp); BAK_CLI=$(mktemp)
cp "$API" "$BAK_API"; cp "$CLI" "$BAK_CLI"
restore() { cp "$BAK_API" "$API"; cp "$BAK_CLI" "$CLI"; }
trap restore EXIT

fail=0

# mutate <ad> <dosya> <test-paketi> <python-ifadesi>
mutate() {
    local name="$1" file="$2" pkg="$3" expr="$4"
    restore
    if ! python -c "
import io,sys
class _S(str):
    def replace(self,a,b,*r):
        out=str.replace(self,a,b,*r)
        if out==self:
            sys.stderr.write('REPLACE ESLESMEDI: '+repr(a[:70])+chr(10))
            sys.exit(8)
        return _S(out)
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
mutate "ilk satır canlı işaretleniyor" "$CLI" ./cmd/panely/ \
    "s=s.replace('\t\tif r.GetReleaseId() == active {','\t\tif r == releases[0] {',1)"

mutate "her satır canlı işaretleniyor" "$CLI" ./cmd/panely/ \
    "s=s.replace('\t\tif r.GetReleaseId() == active {','\t\tif true {',1)"

mutate "hiçbir satır işaretlenmiyor" "$CLI" ./cmd/panely/ \
    "s=s.replace('\t\tif r.GetReleaseId() == active {','\t\tif false {',1)"

# Geri almadan sonra canlı sürüm kesilmiş listenin dışında kalabilir;
# o zaman bu satır onu gösteren TEK yer.
mutate "listenin dışındaki canlı sürüm söylenmiyor" "$CLI" ./cmd/panely/ \
    "s=s.replace('\tcase !slices.ContainsFunc(','\tcase false && !slices.ContainsFunc(',1)"

mutate "canlı sürüm yokken susuyor" "$CLI" ./cmd/panely/ \
    "s=s.replace('\tcase active == \"\":','\tcase false:',1)"

restore
if [[ $fail -ne 0 ]]; then
    echo
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi

echo
echo "Bütün mutasyonlar yakalandı."
