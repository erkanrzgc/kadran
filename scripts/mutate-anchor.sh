#!/usr/bin/env bash
# Zincir çapasını (K-126 C) koruyan denetimlerin GERÇEKTEN bir şey
# koruduğunu sınar.
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
# Çapa denetiminin tek işi, sonradan yazılmış bir geçmişi görmek. En
# pahalı hata ona rağmen YEŞİL demek: sunucunun gönderdiği hash alanına
# güvenmek, "bir çapa tutuyorsa yeter" demek, hiç çapa denetlenmemişken
# geçmek, bozuk çapayı sessizce atlamak. Sonra üretim tarafı: daemon çapa
# yazmazsa ya da uzak yedek onu yüklemezse koruma sessizce biter.
#
# Düzenek mutate-goc.sh'tan: tek eşleşme şartı (K-127), yeşil taban
# şartı, derleme kapısı (K-096; kabuk için `bash -n`), tam yoldan yedek.
# Uzak yedek testi root istiyor (K-100): root değilsek `sudo -n`; ikisi
# de yoksa ölçüm YAPILMADI denir, sessizce geçilmez.
set -uo pipefail

cd "$(dirname "$0")/.."
ANC=internal/anchor/anchor.go
CLI=cmd/kadran/audit_anchor.go
SNAP=internal/store/snapshot.go
OFF=deploy/offsite/kadran-offsite.sh
FILES=("$ANC" "$CLI" "$SNAP" "$OFF")

BAKDIR=$(mktemp -d)
bak() { printf '%s/%s' "$BAKDIR" "${1//\//__}"; }
for f in "${FILES[@]}"; do cp "$f" "$(bak "$f")"; done
restore() { for f in "${FILES[@]}"; do cp "$(bak "$f")" "$f"; done; }
trap restore EXIT

if [[ "$(id -u)" == 0 ]]; then
    KOK=()
elif sudo -n true 2>/dev/null; then
    KOK=(sudo -n)
else
    echo "!! root ya da parolasız sudo yok: uzak yedek testi koşamaz — ölçüm YAPILMADI"
    exit 1
fi

fail=0

olc() {
    go test ./internal/anchor/ -count=1 -timeout 60s >/dev/null 2>&1 &&
        go test ./cmd/kadran/ -count=1 -timeout 60s \
            -run 'Anchor|FetchAll|AuditRecordFromProto|AuditVerify' >/dev/null 2>&1 &&
        go test ./internal/store/ -count=1 -timeout 120s -run 'Anchor|Snapshot' >/dev/null 2>&1 &&
        "${KOK[@]}" bash scripts/check-offsite.sh >/dev/null 2>&1
}

# mutate_in <dosya> <ad> <python-ifadesi>
mutate_in() {
    local file="$1" name="$2" expr="$3"
    restore
    if ! python -c "
import io,sys
class _S(str):
    def replace(self,a,b,*r):
        # TEK eşleşme şart: aranan metin bir yorumda da geçiyorsa
        # replace(…,1) İLKİNİ, yani yorumu değiştirir (K-127).
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

    # ── MUTANT DERLENMELİ (K-096) ───────────────────────
    local build_out
    if ! build_out=$(bash -n "$OFF" 2>&1 &&
        go test ./internal/anchor/ ./cmd/kadran/ ./internal/store/ -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI. Çıktı:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if olc; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

# Taban YEŞİL olmalı: kırmızı bir taban her mutantı "yakalandı" gösterirdi.
if ! olc; then
    echo "!! TABAN KIRMIZI: mutasyonsuz kodda denetimler düşüyor — ölçüm YAPILMADI"
    exit 1
fi

echo "== Denetim: sonradan yazılmış geçmişi görmek =="

mutate_in "$ANC" "zincir yeniden hesaplanmıyor, dönen hash alanına güveniliyor" \
    "s=s.replace('\t\tprev = audit.ComputeHash(r)\n','\t\tprev = r.Hash\n',1)"

mutate_in "$ANC" "yalnız ilk çapa denetleniyor (biri tutuyorsa yeter)" \
    "s=s.replace('\t\tcase hashes[a.Seq-1] != a.Hash:','\t\tcase hashes[a.Seq-1] != a.Hash && r.Checked == 1:',1)"

mutate_in "$ANC" "hiç çapa denetlenmeden yeşil" \
    "s=s.replace('return r.Checked > 0 && len(r.Conflicts) == 0','return len(r.Conflicts) == 0',1)"

mutate_in "$ANC" "kısaltılmış zincir çelişki sayılmıyor" \
    "s=s.replace('\t\tcase a.Seq > uint64(len(hashes)):','\t\tcase a.Seq > uint64(len(hashes))+1000:',1)"

mutate_in "$ANC" "kanonik olmayan çapa kabul ediliyor" \
    "s=s.replace('\tif !bytes.Equal(Format(a.Seq, a.Hash), data) {','\tif bytes.Equal(nil, []byte{1}) {',1)"

mutate_in "$ANC" "-anchors-since eski çapaları ayırmıyor" \
    "s=s.replace('\t\tif !since.IsZero() && a.Taken.Before(since) {','\t\tif false && a.Taken.Before(since) {',1)"

echo "== CLI =="

mutate_in "$CLI" "sayfalama ilk kaydı atlıyor (after_seq dahil sanıldı)" \
    "s=s.replace('\t\trecs, err := page(ctx, after)','\t\trecs, err := page(ctx, after+1)',1)"

mutate_in "$CLI" "sırayı geri saran sunucu döngüye sokuyor" \
    "s=s.replace('\t\t\tif p.GetSeq() <= after {','\t\t\tif false {',1)"

mutate_in "$CLI" "bozuk çapa sessizce atlanıyor" \
    "s=s.replace('\t\t\treturn nil, fmt.Errorf(\"%w: %w\", errBadAnchor, err)','\t\t\tcontinue',1)"

mutate_in "$CLI" "çelişkide çıkış kodu başarı" \
    "s=s.replace('araştırılmalıdır.\")\n\t\treturn exitChainInvalid','araştırılmalıdır.\")\n\t\treturn exitOK',1)"

echo "== Üretim: daemon ve uzak yedek =="

mutate_in "$SNAP" "daemon yedeğin yanına çapa yazmıyor" \
    "s=s.replace('\t\tif err := s.writeAnchor(ctx, dir, stamp); err != nil {','\t\tif err := error(nil); err != nil {',1)"

mutate_in "$SNAP" "budama çapayı yedeğiyle birlikte silmiyor" \
    "s=s.replace('\t\t_ = os.Remove(strings.TrimSuffix(old, snapshotExt) + anchor.Ext)\n','',1)"

mutate_in "$OFF" "uzak yedek çapayı yüklemiyor" \
    "s=s.replace('    yukle_dogrula \"\$capa\" \"\$(basename \"\$capa\")\"','    :',1)"

mutate_in "$OFF" "budama deseni çapaları da seçiyor" \
    "s=s.replace(\"uzak_buda \\\"veritabanı yedeği\\\" '^kadran-.*\\\\.db\\\\.age\$'\",\"uzak_buda \\\"veritabanı yedeği\\\" '^kadran-[0-9]'\",1)"

echo
if [[ "$fail" -ne 0 ]]; then
    echo "En az bir mutasyon yakalanmadı — denetimler iddia ettikleri şeyi korumuyor."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
