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

# ── 3. Gerçek yasak alan YAKALANMALI ─────────────────────────────────
cat > "$WORK/privileged.proto" <<'EOF'
syntax = "proto3";
package panely.v1;

message ContainerCreateRequest {
  string image = 1;
  bool privileged = 2;
}
EOF
bash "$CHECKER" "$WORK/privileged.proto" >/dev/null 2>&1
expect "privileged alanı yakalandı" 1 $?

# ── 4. cap_add YAKALANMALI ───────────────────────────────────────────
cat > "$WORK/capadd.proto" <<'EOF'
syntax = "proto3";
package panely.v1;

message ContainerCreateRequest {
  string image = 1;
  repeated string cap_add = 2;
}
EOF
bash "$CHECKER" "$WORK/capadd.proto" >/dev/null 2>&1
expect "cap_add alanı yakalandı" 1 $?

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
