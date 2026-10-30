#!/bin/sh
# Kadran istemcisini GitHub yayınından kurar (K-159). sudo istemez.
#
#   curl -fsSL https://raw.githubusercontent.com/erkanrzgc/kadran/main/scripts/get-kadran.sh | sh
#
# Ortam: KADRAN_VERSION (yoksa son yayın), KADRAN_INSTALL_DIR (yoksa
# ~/.local/bin), KADRAN_OS ve KADRAN_ARCH (yoksa uname), KADRAN_RELEASE_BASE
# ve KADRAN_RELEASE_API (yansı ve testler için).
#
# Aynı yayındaki SHA256SUMS'a karşı denetler: bu indirme bozulmasını yakalar,
# sahteciliği yakalamaz. Sunucu paketinin özeti ise bu ikilinin içine gömülü
# ve bootstrap ona bakıyor (internal/bootstrap/release.go).
set -eu

tmp=
new=

die() {
    printf 'get-kadran: %s\n' "$*" >&2
    exit 1
}

cleanup() {
    if [ -n "$tmp" ]; then rm -rf "$tmp"; fi
    if [ -n "$new" ]; then rm -f "$new"; fi
}

# fetch <adres> <hedef>
fetch() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$2" "$1"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$2" "$1"
    else
        die "need curl or wget"
    fi
}

sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | cut -d' ' -f1
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | cut -d' ' -f1
    else
        die "need sha256sum or shasum"
    fi
}

# Sürüm adrese ve dosya adına giriyor: yalnız beklenen karakterler.
valid_version() {
    case "$1" in
        v[0-9]*) ;;
        *) return 1 ;;
    esac
    case "$1" in
        *[!A-Za-z0-9.+_-]*) return 1 ;;
    esac
    return 0
}

latest_version() {
    api="${KADRAN_RELEASE_API:-https://api.github.com/repos/erkanrzgc/kadran/releases/latest}"
    # jq'ya bağlanmamak için: alanlar satırlara bölünür, yanıt tek satır da olabilir.
    if fetch "$api" "$tmp/latest.json"; then
        v="$(tr ',' '\n' < "$tmp/latest.json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\(.*\)".*/\1/p' | head -n 1)"
    else
        v=
    fi
    valid_version "$v" || die "could not determine the latest version; set KADRAN_VERSION (for example v0.9.3)"
    printf '%s\n' "$v"
}

detect_os() {
    case "$(uname -s)" in
        Linux) echo linux ;;
        Darwin) echo darwin ;;
        *) uname -s ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64 | amd64) echo amd64 ;;
        aarch64 | arm64) echo arm64 ;;
        *) uname -m ;;
    esac
}

main() {
    trap cleanup EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM

    os="${KADRAN_OS:-$(detect_os)}"
    arch="${KADRAN_ARCH:-$(detect_arch)}"
    case "$os/$arch" in
        linux/amd64 | linux/arm64 | darwin/amd64 | darwin/arm64) ;;
        *) die "unsupported platform: $os/$arch" ;;
    esac

    base="${KADRAN_RELEASE_BASE:-https://github.com/erkanrzgc/kadran/releases/download}"
    base="${base%/}"
    dir="${KADRAN_INSTALL_DIR:-${HOME:?HOME is not set; set KADRAN_INSTALL_DIR}/.local/bin}"
    dir="${dir%/}"

    tmp="$(mktemp -d)"

    ver="${KADRAN_VERSION:-}"
    if [ -z "$ver" ]; then
        ver="$(latest_version)"
    elif ! valid_version "$ver"; then
        die "invalid KADRAN_VERSION: $ver"
    fi

    file="kadran-$ver-$os-$arch"
    fetch "$base/$ver/$file" "$tmp/$file" || die "could not download $base/$ver/$file"
    fetch "$base/$ver/SHA256SUMS" "$tmp/SHA256SUMS" || die "could not download $base/$ver/SHA256SUMS"

    # sha256sum ikili kipte adın başına * koyar; Windows'ta derlenen yayınlar böyle.
    want="$(awk -v f="$file" '{ n = $2; sub(/^\*/, "", n); if (n == f) { print $1; exit } }' "$tmp/SHA256SUMS")"
    got="$(sha256_of "$tmp/$file")"
    if [ -z "$want" ] || [ "$got" != "$want" ]; then
        die "checksum mismatch for $file; nothing installed"
    fi

    target="$dir/kadran"
    if [ -d "$target" ]; then die "$target is a directory"; fi
    mkdir -p "$dir"
    # Aynı dizinde geçici ad + rename: çalışan eski ikili yarım kalmaz.
    new="$dir/.kadran.new.$$"
    cp "$tmp/$file" "$new"
    chmod 0755 "$new"
    mv -f "$new" "$target"
    new=

    echo "installed kadran $ver to $target"
    case ":$PATH:" in
        *":$dir:"*) ;;
        *) echo "add $dir to your PATH" ;;
    esac
}

# Her şey main'de: `curl | sh` yarıda kesilirse yarım betik koşmaz.
main "$@"
