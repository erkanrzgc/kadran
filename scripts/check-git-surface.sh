#!/usr/bin/env bash
# GitHub şema yüzeyinin değişmezlerini denetler (K-167).
#
# Kural: kadrand'ye ve istemcilere giden GitHub mesajlarında jeton, OAuth
# cihaz kodu, sır ya da imzalı indirme adresi taşıyabilecek bir alan
# TEMSİL EDİLEMEZ. Temsil edilemeyen şey kazara sızdırılamaz.
#
# Taranan yer: git.proto'nun tüm mesajları; api.proto'da adı GitHub,
# StartGitHub, GetGitHub, DisconnectGitHub ya da ResolveGitHub ile başlayan
# mesajlar. Yorumlar taranmaz (check-exec-surface.sh'daki gibi): açıklayıcı
# bir yorum yanlış alarm üretmemeli.
#
# Kullanım:
#   scripts/check-git-surface.sh              temiz ağacı denetler
#   scripts/check-git-surface.sh --self-test  her yasak adın YAKALANDIĞINI kanıtlar
#   scripts/check-git-surface.sh --list-forbidden

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GIT_PROTO="$REPO_ROOT/proto/kadran/v1/git.proto"
API_PROTO="$REPO_ROOT/proto/kadran/v1/api.proto"

# Yasak alan adları. "_token" ile biten her ad da yasaktır.
forbidden_fields=(token device_code secret download_url signed)

if [[ "${1:-}" == "--list-forbidden" ]]; then
    printf '%s\n' "${forbidden_fields[@]}" "*_token"
    exit 0
fi

strip_comments() {
    sed -e 's|//.*$||' "$1"
}

# scan <dosya> <all|github> — ihlal edilen "mesaj.alan" satırlarını basar.
scan() {
    local file="$1" mode="$2"
    strip_comments "$file" | awk -v mode="$mode" -v forbidden="${forbidden_fields[*]}" '
        BEGIN { n = split(forbidden, f, " ") }
        {
            line = $0
            if (depth == 0 && match(line, /^[ \t]*message[ \t]+[A-Za-z0-9_]+/)) {
                name = line
                sub(/^[ \t]*message[ \t]+/, "", name)
                sub(/[^A-Za-z0-9_].*$/, "", name)
                cur = name
                scoped = (mode == "all") || (name ~ /^(GitHub|StartGitHub|GetGitHub|DisconnectGitHub|ResolveGitHub)/)
            }
            if (scoped && cur != "" && match(line, /[A-Za-z0-9_]+[ \t]*=[ \t]*[0-9]+/) && line !~ /^[ \t]*(enum|message)/) {
                pre = line
                sub(/[ \t]*=.*$/, "", pre)
                m = split(pre, parts, /[ \t]+/)
                fld = parts[m]
                bad = (fld ~ /_token$/)
                for (i = 1; i <= n; i++) if (fld == f[i]) bad = 1
                # enum değeri (tek sözcük) alan değildir
                if (m < 2 || (m == 2 && parts[1] == "")) bad = 0
                if (bad) print cur "." fld
            }
            o = gsub(/\{/, "{", line); c = gsub(/\}/, "}", line)
            depth += o - c
            if (depth == 0) { cur = ""; scoped = 0 }
        }'
}

check_tree() {
    local git="$1" api="$2" out rc=0
    out="$(scan "$git" all; scan "$api" github)"
    if [[ -n "$out" ]]; then
        echo "✗ yasak alan bulundu:"
        echo "$out" | sed 's/^/    /'
        rc=1
    fi
    return $rc
}

self_test() {
    local fail=0 name
    tmp="$(mktemp -d)"   # trap EXIT işlev dışında koşar, bu yüzden yerel DEĞİL
    trap 'rm -rf "$tmp"' EXIT
    if ! check_tree "$GIT_PROTO" "$API_PROTO" >/dev/null; then
        echo "✗ temiz ağaç geçmedi"; return 1
    fi
    echo "  ✓ temiz ağaç geçiyor"
    for name in "${forbidden_fields[@]}" my_token; do
        # git.proto içine
        awk -v n="$name" '{print} /^message GitHubRepository \{/ {print "  string " n " = 99;"}' \
            "$GIT_PROTO" > "$tmp/git.proto"
        if check_tree "$tmp/git.proto" "$API_PROTO" >/dev/null; then
            echo "  ✗ git.proto içinde '$name' YAKALANMADI"; fail=1
        else
            echo "  ✓ git.proto: '$name' yakalandı"
        fi
        # api.proto GitHub mesajına
        awk -v n="$name" '{print} /^message StartGitHubConnectResponse \{/ {print "  string " n " = 99;"}' \
            "$API_PROTO" > "$tmp/api.proto"
        if check_tree "$GIT_PROTO" "$tmp/api.proto" >/dev/null; then
            echo "  ✗ api.proto içinde '$name' YAKALANMADI"; fail=1
        else
            echo "  ✓ api.proto: '$name' yakalandı"
        fi
    done
    # Yorumdaki ad yanlış alarm vermemeli.
    awk '{print} /^message GitHubRepository \{/ {print "  // string token = 99;"}' \
        "$GIT_PROTO" > "$tmp/git.proto"
    if check_tree "$tmp/git.proto" "$API_PROTO" >/dev/null; then
        echo "  ✓ yorumdaki ad yok sayıldı"
    else
        echo "  ✗ yorum satırı yanlış alarm verdi"; fail=1
    fi
    return $fail
}

if [[ "${1:-}" == "--self-test" ]]; then
    self_test
    exit $?
fi

if check_tree "$GIT_PROTO" "$API_PROTO"; then
    echo "  ✓ GitHub şemasında jeton/sır taşıyabilecek alan yok"
    exit 0
fi
exit 1
