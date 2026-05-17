#!/usr/bin/env bash
set -euo pipefail

# termx installer
# Usage: curl -sL https://termx.omscd.com/install.sh | bash

API_URL="https://termx.omscd.com"
BINARY_NAME="termx"
DEFAULT_BIN_DIR="$HOME/.local/bin"
TMP_FILE=""

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

info()  { echo -e "${CYAN}[info]${NC}  $*"; }
ok()    { echo -e "${GREEN}[ok]${NC}    $*"; }
warn()  { echo -e "${YELLOW}[warn]${NC}  $*"; }
error() { echo -e "${RED}[error]${NC} $*" >&2; exit 1; }

cleanup() {
    if [ -n "${TMP_FILE:-}" ] && [ -e "${TMP_FILE:-}" ]; then
        rm -f "$TMP_FILE"
    fi
}

trap cleanup EXIT

detect_os() {
    case "$(uname -s)" in
        Linux*) echo "linux" ;;
        Darwin*) echo "darwin" ;;
        FreeBSD*) echo "freebsd" ;;
        *) error "Unsupported OS: $(uname -s)" ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64) echo "amd64" ;;
        i386|i486|i586|i686) echo "386" ;;
        aarch64|arm64) echo "arm64" ;;
        armv7l|armv7|armhf) echo "arm" ;;
        riscv64) echo "riscv64" ;;
        *) error "Unsupported architecture: $(uname -m)" ;;
    esac
}

check_deps() {
    if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
        error "curl or wget is required but not installed"
    fi
}

fetch() {
    local url="$1"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$url"
    else
        wget -q "$url" -O -
    fi
}

download() {
    local url="$1" output="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --connect-timeout 10 --max-time 120 "$url" -o "$output"
    else
        wget -q --timeout=10 "$url" -O "$output"
    fi
}

download_with_fallback() {
    local output="$1"
    shift
    local urls=("$@")
    local url

    for url in "${urls[@]}"; do
        if download "$url" "$output" 2>/dev/null; then
            if [ "$(wc -c < "$output")" -gt 1000 ]; then
                return 0
            fi
        fi
    done
    return 1
}

json_field() {
    local json="$1" field="$2"
    echo "$json" | grep -o "\"${field}\":[^,}]*" | head -1 | sed "s/\"${field}\"://" | tr -d '"'
}

json_array() {
    local json="$1" field="$2"
    echo "$json" | grep -o "\"${field}\":\[[^]]*\]" | head -1 | \
        sed "s/\"${field}\":\[//" | sed 's/\]$//' | \
        tr ',' '\n' | tr -d '"' | sed 's/^ *//'
}

path_contains_dir() {
    local dir="$1"
    case ":$PATH:" in
        *":$dir:"*) return 0 ;;
        *) return 1 ;;
    esac
}

choose_bin_dir() {
    if [ -d "$HOME/bin" ] || path_contains_dir "$HOME/bin"; then
        echo "$HOME/bin"
    else
        echo "$DEFAULT_BIN_DIR"
    fi
}

install_binary() {
    local os arch api_response has_update version sha256 primary_url
    local -a download_urls=()
    local -a other_urls=()
    local -a all_urls=()
    local tmp_file install_dir target_path current_version

    echo ""
    echo "  ╔══════════════════════════════════════╗"
    echo "  ║         termx installer              ║"
    echo "  ╚══════════════════════════════════════╝"
    echo ""

    os="$(detect_os)"
    arch="$(detect_arch)"
    install_dir="$(choose_bin_dir)"
    target_path="${install_dir}/${BINARY_NAME}"

    info "Detected: ${os}/${arch}"
    info "Install dir: ${install_dir}"

    check_deps

    info "Fetching latest version..."
    api_response="$(fetch "${API_URL}/api/agent/check-update?version=0.0.0&os=${os}&arch=${arch}")"

    has_update="$(json_field "$api_response" "update")"
    if [ "$has_update" != "true" ]; then
        error "No release available for ${os}/${arch}"
    fi

    version="$(json_field "$api_response" "version")"
    sha256="$(json_field "$api_response" "sha256")"
    primary_url="$(json_field "$api_response" "downloadUrl")"

    [ -n "$primary_url" ] && [ "$primary_url" != "null" ] && all_urls+=("$primary_url")
    while IFS= read -r url; do
        [ -n "$url" ] && [ "$url" != "$primary_url" ] && all_urls+=("$url")
    done < <(json_array "$api_response" "mirrors")

    local url
    for url in "${all_urls[@]}"; do
        case "$url" in
            *gitee.com*) download_urls+=("$url") ;;
            *) other_urls+=("$url") ;;
        esac
    done
    download_urls+=("${other_urls[@]}")

    [ ${#download_urls[@]} -eq 0 ] && error "No download URL available"

    if command -v "$BINARY_NAME" >/dev/null 2>&1; then
        current_version="$("$BINARY_NAME" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' || true)"
        if [ -n "$current_version" ]; then
            info "Current version: v${current_version}"
        fi
    fi
    info "Latest version: v${version}"

    mkdir -p "$install_dir"

    tmp_file="$(mktemp)"
    TMP_FILE="$tmp_file"

    info "Downloading ${BINARY_NAME}..."
    if ! download_with_fallback "$tmp_file" "${download_urls[@]}"; then
        error "All download sources failed"
    fi

    if [ -n "$sha256" ] && [ "$sha256" != "null" ]; then
        info "Verifying checksum..."
        local actual
        if command -v sha256sum >/dev/null 2>&1; then
            actual="$(sha256sum "$tmp_file" | awk '{print $1}')"
        elif command -v shasum >/dev/null 2>&1; then
            actual="$(shasum -a 256 "$tmp_file" | awk '{print $1}')"
        else
            warn "sha256 tool not found, skipping verification"
            actual="$sha256"
        fi
        [ "$actual" != "$sha256" ] && error "SHA256 mismatch"
        ok "Checksum verified"
    fi

    chmod +x "$tmp_file"
    mv "$tmp_file" "$target_path"
    TMP_FILE=""

    ok "${BINARY_NAME} v${version} installed to ${target_path}"

    if path_contains_dir "$install_dir"; then
        ok "${install_dir} is already in PATH"
    else
        warn "${install_dir} is not in PATH yet"
        echo ""
        echo "  Add this to your shell profile:"
        echo ""
        echo "    export PATH=\"${install_dir}:\$PATH\""
        echo ""
    fi

    print_next_steps "$target_path"
}

print_next_steps() {
    local target_path="$1"

    echo ""
    echo "  ╔══════════════════════════════════════╗"
    echo "  ║       Installation complete!         ║"
    echo "  ╚══════════════════════════════════════╝"
    echo ""
    echo "  Binary:"
    echo "    ${target_path}"
    echo ""
    echo "  Start it manually:"
    echo "    ${BINARY_NAME}"
    echo ""
    echo "  Enable hub remote access:"
    echo "    ${BINARY_NAME} remote enable --mode hub --token <your_token>"
    echo ""
    echo "  Keep hub remote online:"
    echo "    ${BINARY_NAME} daemon"
    echo ""
    echo "  Keep it running with nohup:"
    echo "    nohup ${BINARY_NAME} daemon > ~/.termx.log 2>&1 &"
    echo ""
    echo "  Daemonize it with your OS service manager:"
    echo "    - macOS: create a LaunchAgent with ProgramArguments starting from ${target_path}"
    echo "    - Linux: create a systemd user service with ExecStart=${target_path} [args...]"
    echo "    - FreeBSD: create an rc.d service or use daemon(8) to supervise ${target_path}"
    echo ""
    echo "  Docs:"
    echo "    ${API_URL}/docs"
    echo ""
}

main() {
    install_binary
}

main "$@"
