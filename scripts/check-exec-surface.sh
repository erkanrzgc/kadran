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
exec_lines="$(find "$REPO_ROOT/internal/exec" "$REPO_ROOT/cmd/panely-exec" \
    -name '*.go' ! -name '*_test.go' -exec cat {} + 2>/dev/null | wc -l)"
exec_lines="${exec_lines// /}"
if [[ "$exec_lines" -gt "$MAX_EXEC_LINES" ]]; then
    note_failure "ayrıcalıklı kod $exec_lines satır, sınır $MAX_EXEC_LINES — ne eklendiği sorgulanmalı"
else
    note_ok "ayrıcalıklı kod $exec_lines satır (sınır $MAX_EXEC_LINES)"
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
