#!/usr/bin/env bash
# check-exec-surface.sh'ın gerçekten yakaladığını doğrular.
#
# # Neden bir kontrolün testi?
#
# Hiçbir zaman ateşlenmeyen bir güvenlik kontrolü, hiç olmayandan daha
# kötüdür: yeşil bir CI rozeti verir ve kimse bakmaz. Bu betik kasten
# bozulmuş şemalar üretip kontrolün onları YAKALADIĞINI doğruluyor.
#
# Kullanım:
#   scripts/check-exec-surface-test.sh

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHECKER="$REPO_ROOT/scripts/check-exec-surface.sh"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

fail=0
expect() {
    local name="$1" want="$2" got="$3"
    if [[ "$want" == "$got" ]]; then
        printf '  ✓ %s\n' "$name"
    else
        printf '  ✗ %s — beklenen çıkış %s, alınan %s\n' "$name" "$want" "$got" >&2
        fail=1
    fi
}

# ── 1. Gerçek şema geçmeli ───────────────────────────────────────────
bash "$CHECKER" >/dev/null 2>&1
expect "gerçek exec.proto temiz" 0 $?

# ── 2. Yorum içindeki örnek YAKALANMAMALI ────────────────────────────
#
# Bu tam olarak ilk sürümün düştüğü tuzak: exec.proto gelecekteki alanları
# yorum içinde örnekliyor ve tarama onları gerçek alan sanıyordu.
cat > "$WORK/yorumlu.proto" <<'EOF'
syntax = "proto3";
package panely.v1;

// Faz 1'de eklenecek şekil:
//
//   message ContainerCreateRequest {
//     repeated string command = 4;  // argv — konteyner İÇİNDE
//     bool privileged = 9;          // ASLA: temsil edilemez olmalı
//   }
message Placeholder {
  string name = 1;
}
EOF
bash "$CHECKER" "$WORK/yorumlu.proto" >/dev/null 2>&1
expect "yorumdaki örnekler yanlış alarm üretmiyor" 0 $?

# ── 3. YASAK LİSTEDEKİ HER ALAN YAKALANMALI ──────────────────────────
#
# Liste elle kopyalanmaz; kontrolün kendisinden okunur. Böylece listeye
# eklenen bir desenin kanıtsız kalması imkânsızdır — daha önce iki kez
# bedeli ödenen "hiç ateşlenmeyen kontrol" sınıfı budur.
mapfile -t forbidden < <(bash "$CHECKER" --list-forbidden)

if [[ ${#forbidden[@]} -eq 0 ]]; then
    echo "  ✗ yasak alan listesi boş — test bir şey ölçmüyor" >&2
    fail=1
fi

for field in "${forbidden[@]}"; do
    cat > "$WORK/tek.proto" <<EOF
syntax = "proto3";
package panely.v1;

message ContainerCreateRequest {
  string app_id = 1;
  string ${field} = 2;
}
EOF
    bash "$CHECKER" "$WORK/tek.proto" >/dev/null 2>&1
    expect "yasak alan yakalandı: $field" 1 $?
done

# ── 4. Alan tanımının HER ŞEKLİ yakalanmalı ──────────────────────────
#
# 3. adım her alanı yalnızca `string <ad>` şeklinde sınıyor. Asıl risk
# TİP kısmında: `map<...>` ve `optional` etiketi deseni kırıyordu ve
# ikisi de gerçek şemada kullanılıyor.
shape_case() {
    local name="$1" line="$2"
    cat > "$WORK/sekil.proto" <<EOF
syntax = "proto3";
package panely.v1;

message ContainerCreateRequest {
  string app_id = 1;
  ${line}
}
EOF
    bash "$CHECKER" "$WORK/sekil.proto" >/dev/null 2>&1
    expect "$name" 1 $?
}

shape_case "düz tip: bool privileged"          "bool privileged = 2;"
shape_case "repeated: repeated string cap_add" "repeated string cap_add = 2;"
shape_case "map tipi: map<string,string> sysctls" "map<string, string> sysctls = 2;"
shape_case "optional etiketi: optional bool privileged" "optional bool privileged = 2;"

# ── 5. Serbest argv YAKALANMALI ──────────────────────────────────────
cat > "$WORK/argv.proto" <<'EOF'
syntax = "proto3";
package panely.v1;

message RunRequest {
  repeated string argv = 1;
}
EOF
bash "$CHECKER" "$WORK/argv.proto" >/dev/null 2>&1
expect "serbest argv yakalandı" 1 $?

# ── 6. Serbest kabuk alanı YAKALANMALI ───────────────────────────────
cat > "$WORK/shell.proto" <<'EOF'
syntax = "proto3";
package panely.v1;

message RunRequest {
  string shell = 1;
}
EOF
bash "$CHECKER" "$WORK/shell.proto" >/dev/null 2>&1
expect "serbest kabuk alanı yakalandı" 1 $?

# ── 7. Satır sınırı gerçekten uygulanıyor mu? ────────────────────────
#
# Sınırı 1'e indirip mevcut kodun aşmasını bekliyoruz. Sınır hiç
# uygulanmıyor olsaydı bu da geçerdi ve kimse fark etmezdi.
MAX_EXEC_LINES=1 bash "$CHECKER" >/dev/null 2>&1
expect "satır sınırı uygulanıyor" 1 $?

echo
if [[ $fail -eq 0 ]]; then
    echo "Kontrol betiği hem temizi geçiriyor hem ihlali yakalıyor."
else
    echo "Kontrol betiğinin kendisi bozuk." >&2
fi
exit $fail
