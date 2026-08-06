#!/usr/bin/env bash
# Ayrıcalıklı yüzeyin değişmezlerini denetler.
#
# # Neden ayrı bir betik?
#
# Bu kontroller CI YAML'ının içine gömülüydü ve biri sessizce yanlış
# çalışıyordu: yorum satırlarını da tarıyordu, yani exec.proto'daki
# açıklayıcı bir örnek yüzünden temiz ağaçta bile hata veriyordu. Yanlış
# alarm veren bir kontrol, kapatılmaya mahkûmdur.
#
# Betik olarak yerelde çalıştırılabiliyor ve — daha önemlisi — kendisi
# sınanabiliyor: scripts/check-exec-surface-test.sh hem temiz ağacın
# geçtiğini hem de kasten bozulmuş bir şemanın YAKALANDIĞINI doğruluyor.
#
# Kullanım:
#   scripts/check-exec-surface.sh [exec.proto yolu]
#   scripts/check-exec-surface.sh --list-forbidden

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# forbidden_fields — ŞEMADA TEMSİL EDİLEMEYECEK alan adları.
#
# Bu seçeneklerin temsil edilememesi, doğrulanmasından daha güçlü bir
# garantidir: temsil edilemeyen bir şey kazara kabul edilemez.
#
# Liste betiğin en üstündedir çünkü `--list-forbidden` ile dışa verilir ve
# check-exec-surface-test.sh her bir öğe için ayrı bir kanıt üretir. Böylece
# listeye eklenen bir desenin kanıtsız kalması MÜMKÜN DEĞİLDİR — ateşlenmeyen
# bir kontrol, hiç olmayandan kötüdür.
forbidden_fields=(
    # Ayrıcalık yükseltme
    privileged
    cap_add
    capabilities
    security_opt
    devices
    sysctls
    # Ad alanı kaçışı
    host_network
    host_pid
    host_ipc
    network_mode
    pid_mode
    ipc_mode
    userns_mode
    cgroup_parent
    # Serbest tutamaç: hostta Panely'nin yönetmediği nesnelere işaretçi
    container_id
    image
    host_path
)

if [[ "${1:-}" == "--list-forbidden" ]]; then
    printf '%s\n' "${forbidden_fields[@]}"
    exit 0
fi

SCHEMA="${1:-$REPO_ROOT/proto/panely/v1/exec.proto}"

# MAX_EXEC_LINES, ayrıcalıklı kodun üst sınırıdır.
#
# Plandaki açık risk maddesinden geliyor: ayrıcalıklı yüzey ne kadar
# büyürse "en az yetki" o kadar anlamsızlaşır. Sınırı yükseltmek bir
# çözüm değil, bir kararın kendisidir — gerekiyorsa ne eklendiği
# tartışılarak yükseltilmeli.
#
# ── Ölçüm KAPSAMI: sabit yol listesi değil, içe aktarma grafiği ──────
#
# Kapsam bir dönem `internal/exec` + `cmd/panely-exec` olarak
# sabitlenmişti ve sayaç 1267 gösteriyordu. O sayı YANLIŞTI: root süreç
# bunlardan fazlasını çalıştırıyor, yani bütçe sessizce dolanılabiliyordu
# — bir paket yazıp panely-exec'ten içe aktarmak yeterliydi. Tam bu oldu:
# `internal/logutil` root binary'ye girdi ve sayaç kıpırdamadı (K-034).
#
# Kapsam artık `go list -deps` ile DERLEYİCİDEN türetiliyor.
#
# ── Sayılan ŞEY: yorum ve boş satır HARİÇ kod ────────────────────────
#
# İlk düzeltmede ham satır sayılıyordu ve bu, metriğin kendisinde ikinci
# bir sorun yarattı: ayrıcalıklı yüzeyin %38'i yorum (2631 ham / 1608
# kod). Bu projede ağır yorumlama BİLİNÇLİ bir karar — K-002'nin amacı
# yüzeyi DENETLENEBİLİR tutmak ve açıklama denetlenebilirliği artırır.
# Ham satır saymak, bütçede kalmak için yorum silmeyi ödüllendirirdi:
# metrik, var olma sebebinin tersine çalışırdı.
#
# Planın kendi hedefi de zaten koddu — "denetlenebilir ~1500 satır".
# Yorum hariç ölçüm 1608 veriyor, tam o mertebe. Bu yüzden sınır
# ORİJİNAL 2000'e geri alındı (K-036).
#
# Çıktı HER ZAMAN iki sayıyı da basar; ham sayı gizlenmiyor.
MAX_EXEC_LINES="${MAX_EXEC_LINES:-2000}"

fail=0
note_failure() {
    printf '  ✗ %s\n' "$1" >&2
    fail=1
}
note_ok() {
    printf '  ✓ %s\n' "$1"
}

# stripComments, yorumları çıkarır.
#
# Kritik: taramalar YALNIZCA gerçek alan tanımlarına bakmalı. exec.proto,
# gelecekteki şekilleri yorum içinde örnekliyor; onları yasak alan sanmak
# kontrolü kullanılamaz hale getirirdi.
strip_comments() {
    sed -e 's://.*::' "$1"
}

if [[ ! -f "$SCHEMA" ]]; then
    echo "şema bulunamadı: $SCHEMA" >&2
    exit 2
fi

schema_body="$(strip_comments "$SCHEMA")"

echo "==> Ayrıcalıklı kod boyutu"

# Sayılan küme, SABİT BİR YOL LİSTESİ DEĞİL — root binary'nin GERÇEK
# içe aktarma grafiğinden türetilir.
#
# # Neden?
#
# Liste `internal/exec` + `cmd/panely-exec` olarak sabitlenmişti. Bu,
# bütçenin sessizce dolanılabilmesi demekti: yeni bir paket yazıp
# panely-exec'ten içe aktarmak, kodu ayrıcalıklı sürecin içine sokar ama
# sayaca hiç dokunmazdı. Tam olarak bu oldu — `internal/logutil` root
# binary'ye girdi ve bütçe 1267'de kaldı.
#
# `go list -deps` binary'nin gerçekten derlediği her paketi verir. Modül
# içindekileri süzüp sayıyoruz; standart kütüphane ve dış bağımlılıklar
# bu bütçenin konusu değil (onlar ayrı bir sorun ve govulncheck'in işi).
#
# go yoksa sabit listeye düşülür ama bu AÇIKÇA duyurulur — sessiz bir
# geri düşüş, kontrolün kendisini yalancı yapardı.
module_path="$(cd "$REPO_ROOT" && go list -m 2>/dev/null || echo "")"

if [[ -n "$module_path" ]]; then
    # `go list -deps` HEDEF PAKETİ DE listeler; ayrıca eklemek onu iki kez
    # saydırır. (İlk yazımda tam bu oldu: cmd/panely-exec 174 satır olarak
    # iki kere toplandı.)
    mapfile -t priv_dirs < <(
        cd "$REPO_ROOT" &&
        go list -deps ./cmd/panely-exec 2>/dev/null |
            grep "^${module_path}/" |
            sed "s|^${module_path}/||"
    )

    exec_lines=0
    total_lines=0
    counted=0
    for d in "${priv_dirs[@]}"; do
        [[ -d "$REPO_ROOT/$d" ]] || continue

        # ÜRETİLEN protobuf kodu bütçenin dışında.
        #
        # Gerekçe: bu kod exec.proto'dan mekanik olarak türetiliyor, elle
        # yazılmıyor ve elle denetlenmiyor. Bütçenin amacı "root süreçte
        # kaç satır İNSAN YAZIMI kod var" sorusunu yanıtlamak; 4000 satır
        # üretilmiş marshalling kodu bu sayıyı anlamsız kılardı. Şemanın
        # kendisi zaten aşağıdaki yasak-alan taramalarıyla korunuyor.
        #
        # Dışlama KÖRÜ KÖRÜNE yapılmıyor: dosyaların gerçekten üretilmiş
        # olduğu doğrulanıyor. Aksi hâlde bu dizin, denetimden kaçmak
        # isteyen birinin elle kod saklayabileceği bir yer olurdu.
        if [[ "$d" == internal/pb/* ]]; then
            handwritten="$(grep -LE '^// Code generated .* DO NOT EDIT\.$' \
                "$REPO_ROOT/$d"/*.go 2>/dev/null || true)"
            if [[ -n "$handwritten" ]]; then
                note_failure "üretilen kod dizininde elle yazılmış dosya var (bütçeden kaçış): $handwritten"
            fi
            continue
        fi

        raw="$(find "$REPO_ROOT/$d" -maxdepth 1 -name '*.go' ! -name '*_test.go' \
            -exec cat {} + 2>/dev/null | wc -l)"
        # Yorum ve boş satırlar sayılmaz — aşağıdaki gerekçeye bakın.
        # Sezgisel: yorum GİBİ görünmeyen her şey kod sayılır, yani hata
        # payı kodu FAZLA saymaya doğrudur (blok yorum gövdeleri).
        code="$(find "$REPO_ROOT/$d" -maxdepth 1 -name '*.go' ! -name '*_test.go' \
            -exec cat {} + 2>/dev/null | grep -vcE '^[[:space:]]*(//|/\*|\*|$)')"
        exec_lines=$((exec_lines + ${code// /}))
        total_lines=$((total_lines + ${raw// /}))
        counted=$((counted + 1))
    done
    echo "    ($counted paket, panely-exec'in içe aktarma grafiğinden; üretilen kod hariç)"
    echo "    ham satır: $total_lines · yorum/boş hariç: $exec_lines"
else
    # ÖLÇEMİYORSAK ONAYLAMAYIZ.
    #
    # Buradaki cazip seçenek, sabit yol listesine düşüp bir uyarı basmaktı.
    # Ama o liste yüzeyin yarısını sayıyor (K-034) — yani çıktı yeşil bir
    # onay ve düşük bir sayı olurdu. Ölçemediğini onaylayan bir güvenlik
    # kontrolü, tam olarak bu projede dört kez yakalanan sınıf.
    #
    # CI'da go daima var (.github/actions/setup). Yerelde yoksa doğru
    # yanıt "geçti" değil, "ölçemedim".
    note_failure "go bulunamadı — ayrıcalıklı yüzey ÖLÇÜLEMEDİ.
  Bütçe, cmd/panely-exec'in içe aktarma grafiğinden türetiliyor ve bu
  \`go list -deps\` gerektiriyor. Sabit yol listesine düşmek yüzeyin
  yaklaşık yarısını sayardı; eksik ölçüme dayalı bir onay vermiyoruz."
    exec_lines=""
fi

# Ölçüm yapılamadıysa karşılaştırma da yapılmaz. Aksi hâlde çıktı kendi
# kendisiyle çelişirdi: bir ✗ hemen ardından "✓ 0 satır" gelirdi.
if [[ -n "$exec_lines" ]]; then
    if [[ "$exec_lines" -gt "$MAX_EXEC_LINES" ]]; then
        note_failure "ayrıcalıklı kod $exec_lines satır, sınır $MAX_EXEC_LINES — ne eklendiği sorgulanmalı"
    else
        note_ok "ayrıcalıklı kod $exec_lines satır (sınır $MAX_EXEC_LINES)"
    fi
fi

echo
echo "==> Şemada yasak alanlar"
# Alan tanımı: "[etiket] <tip> <ad> = <numara>;"
#
# Tip kısmı üç şekli de kapsamalıdır, yoksa kontrol sessizce boşa düşer:
#
#   bool privileged = 1;                 → düz tip
#   map<string,string> sysctls = 2;      → `map` sonrası `<` geliyor; eski
#                                          desen `[A-Za-z0-9_.]+` ile eşleşip
#                                          boşluk beklediği için KAÇIRIYORDU
#   optional bool privileged = 3;        → `optional`ı tip sanıp asıl tipte
#                                          takılıyordu; KAÇIRIYORDU
#
# İkisi de teorik değil: şemada artık `optional uint32 replica` ve
# `map<string,string> env` var, yani her iki şekil de gerçekten kullanılıyor.
field_type='(map<[^>]*>|[A-Za-z0-9_.]+)'
field_label='((repeated|optional)[[:space:]]+)?'
for field in "${forbidden_fields[@]}"; do
    if grep -Eqi "^[[:space:]]*${field_label}${field_type}[[:space:]]+${field}[[:space:]]*=[[:space:]]*[0-9]+" \
        <<<"$schema_body"; then
        note_failure "exec.proto içinde yasak alan: $field"
    fi
done
[[ $fail -eq 0 ]] && note_ok "yasak alan yok (${#forbidden_fields[@]} desen tarandı)"

echo
echo "==> Şemada serbest komut alanı"
# Serbest argv sızması beyaz liste modelini tamamen anlamsız kılar:
# executor'a "şunu çalıştır" diyebilen biri zaten root'tur.
before_argv=$fail
if grep -Eqi "^[[:space:]]*repeated[[:space:]]+string[[:space:]]+(argv|args|cmd|command|entrypoint)[[:space:]]*=" \
    <<<"$schema_body"; then
    note_failure "exec.proto serbest argv alanı içeriyor"
fi
if grep -Eqi "^[[:space:]]*string[[:space:]]+(shell|script|raw_command)[[:space:]]*=" \
    <<<"$schema_body"; then
    note_failure "exec.proto serbest kabuk/komut alanı içeriyor"
fi
[[ $fail -eq $before_argv ]] && note_ok "serbest komut alanı yok"

echo
if [[ $fail -eq 0 ]]; then
    echo "Ayrıcalıklı yüzey değişmezleri korunuyor."
else
    echo "Ayrıcalıklı yüzey değişmezleri İHLAL EDİLDİ." >&2
fi
exit $fail
