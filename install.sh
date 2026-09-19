#!/usr/bin/env bash
# codeRAG installer - works with any coding agent
# Usage: curl -fsSL https://raw.githubusercontent.com/xkzy/codeRAG/main/install.sh | bash
# Or: ./install.sh [--agent <agent>] [--dir <dir>] [--config <path>]

set -euo pipefail

VERSION="1.1.1"
REPO="xkzy/codeRAG"
INSTALL_DIR="${HOME}/.codergag"
CONFIG_DIR="${HOME}/.config/codergag"
BIN_DIR="${HOME}/.local/bin"
AGENT=""
CUSTOM_CONFIG=""
SKIP_BUILD=false
SKIP_CONFIG=false
FORCE=false
DRY_RUN=false

usage() {
    cat <<EOF
codeRAG installer v${VERSION}

Usage: $0 [options]

Options:
  --agent AGENT       Agent(s) to configure: claude-code, codex, opencode, kilo, cursor, vscode, continue,
                      a comma list, "all" (every agent found on this machine) or none
  --dir DIR           Installation directory (default: ~/.codergag)
  --bin-dir DIR       Binary directory (default: ~/.local/bin)
  --config PATH       Use custom config file
  --skip-build        Skip building from source (use prebuilt)
  --skip-config       Skip agent configuration
  --force             Overwrite existing installation
  --dry-run           Show which agent configs would change; write nothing
  --help              Show this help

Examples:
  $0 --agent claude-code
  $0 --agent kilo --dir /opt/codergag --bin-dir /usr/local/bin
  $0 --skip-build --agent copilot
EOF
}

log() { echo -e "\033[1;32m[codeRAG]\033[0m $*"; }
warn() { echo -e "\033[1;33m[codeRAG]\033[0m $*"; }
err() { echo -e "\033[1;31m[codeRAG]\033[0m $*" >&2; }

while [[ $# -gt 0 ]]; do
    case $1 in
        --agent) AGENT="$2"; shift 2 ;;
        --dir) INSTALL_DIR="$2"; shift 2 ;;
        --bin-dir) BIN_DIR="$2"; shift 2 ;;
        --config) CUSTOM_CONFIG="$2"; shift 2 ;;
        --skip-build) SKIP_BUILD=true; shift ;;
        --skip-config) SKIP_CONFIG=true; shift ;;
        --force) FORCE=true; shift ;;
        --dry-run) DRY_RUN=true; shift ;;
        --help) usage; exit 0 ;;
        *) err "Unknown option: $1"; usage; exit 1 ;;
    esac
done

# Agents present on this machine, one per line.
installed_agents() {
    command -v claude &>/dev/null || [[ -f "$HOME/.claude.json" ]] && echo claude-code
    command -v codex &>/dev/null || [[ -d "$HOME/.codex" ]] && echo codex
    command -v opencode &>/dev/null || [[ -d "$HOME/.config/opencode" ]] && echo opencode
    command -v kilo &>/dev/null || [[ -d "$HOME/.config/kilo" ]] && echo kilo
    command -v cursor &>/dev/null || [[ -d "$HOME/.cursor" ]] && echo cursor
    [[ -d "$HOME/.config/Code/User" || -d "$HOME/Library/Application Support/Code/User" ]] && echo vscode
    [[ -d "$HOME/.continue" ]] && echo continue
    return 0
}

# Detect agent if not specified
detect_agent() {
    if [[ -n "$AGENT" ]]; then return; fi
    AGENT="$(installed_agents | head -n1)"
    [[ -z "$AGENT" ]] && AGENT="none"
    log "Detected agent: $AGENT (use --agent all to configure every agent found)"
}

# Check prerequisites
check_prereqs() {
    log "Checking prerequisites..."
    
    if ! command -v go &>/dev/null; then
        err "Go is required but not installed. Install from https://golang.org/dl/"
        exit 1
    fi
    
    if ! command -v git &>/dev/null; then
        err "Git is required but not installed."
        exit 1
    fi
    
    GO_VERSION=$(go version | grep -oE 'go[0-9]+\.[0-9]+' | sed 's/go//')
    REQUIRED="1.22"
    if [[ "$(printf '%s\n' "$REQUIRED" "$GO_VERSION" | sort -V | head -n1)" != "$REQUIRED" ]]; then
        err "Go $REQUIRED+ required, found $GO_VERSION"
        exit 1
    fi
    
    log "Go $GO_VERSION ✓"
    log "Git $(git --version | cut -d' ' -f3) ✓"
}

# Download a prebuilt release binary; returns non-zero to fall back to a source build.
download_prebuilt() {
    local os arch asset url tmp
    case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; *) return 1 ;; esac
    case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) return 1 ;; esac
    asset="codergag_${os}_${arch}.tar.gz"
    url="https://github.com/${REPO}/releases/latest/download/${asset}"
    tmp="$(mktemp -d)"
    log "Trying prebuilt binary (${asset})..."
    if ! curl -fsSL "$url" -o "${tmp}/${asset}" 2>/dev/null; then
        warn "No prebuilt binary available, building from source"
        rm -rf "$tmp"
        return 1
    fi
    if curl -fsSL "https://github.com/${REPO}/releases/latest/download/SHA256SUMS" -o "${tmp}/SHA256SUMS" 2>/dev/null; then
        local want got
        want="$(grep " ${asset}\$" "${tmp}/SHA256SUMS" | cut -d' ' -f1)"
        got="$( (sha256sum "${tmp}/${asset}" 2>/dev/null || shasum -a 256 "${tmp}/${asset}") | cut -d' ' -f1)"
        if [[ -n "$want" && "$want" != "$got" ]]; then
            err "Checksum mismatch for ${asset}"
            rm -rf "$tmp"
            exit 1
        fi
    fi
    mkdir -p "$BIN_DIR"
    tar xzf "${tmp}/${asset}" -C "$tmp" codergag
    install -m 0755 "${tmp}/codergag" "${BIN_DIR}/codergag"
    rm -rf "$tmp"
    log "Binary installed to ${BIN_DIR}/codergag (prebuilt)"
    return 0
}

# Build from source
build_from_source() {
    if [[ "$SKIP_BUILD" == "true" ]]; then
        log "Skipping build (--skip-build)"
        return
    fi
    
    if download_prebuilt; then
        return
    fi

    log "Building codeRAG from source..."
    
    local build_dir="${INSTALL_DIR}/src"
    
    if [[ -d "$build_dir" && "$FORCE" != "true" ]]; then
        log "Source exists, updating..."
        cd "$build_dir"
        git fetch --quiet
        git reset --hard origin/main --quiet
    else
        log "Cloning repository..."
        rm -rf "$build_dir"
        git clone --depth 1 --quiet "https://github.com/${REPO}.git" "$build_dir"
        cd "$build_dir"
    fi
    
    log "Compiling..."
    CGO_ENABLED=1 go build -o "${BIN_DIR}/codergag" ./cmd/codergag
    
    log "Binary installed to ${BIN_DIR}/codergag"
}

# Create default config
create_config() {
    log "Creating configuration..."
    
    mkdir -p "$CONFIG_DIR"
    
    local config_path="${CONFIG_DIR}/config.yaml"
    
    if [[ -n "$CUSTOM_CONFIG" ]]; then
        cp "$CUSTOM_CONFIG" "$config_path"
        log "Using custom config: $CUSTOM_CONFIG"
        return
    fi
    
    if [[ -f "$config_path" && "$FORCE" != "true" ]]; then
        log "Config exists, skipping (use --force to overwrite)"
        return
    fi
    
    cat > "$config_path" <<'YAML'
# codeRAG configuration
database:
  path: "~/.codergag/codergag.db"

projects: []

indexing:
  incremental: true
  ignore:
    - ".git"
    - "build"
    - "node_modules"
    - "dist"
    - "*.min.js"
    - "*.pb.go"
    - "vendor"

storage: "sqlite"

cache:
  enabled: true
  exact:
    enabled: true
  semantic:
    enabled: true
    threshold: 0.92
    max_results: 5
  tool:
    enabled: true
  analysis:
    enabled: true
  llm:
    enabled: true
    cache_responses: true
  ttl:
    enabled: false
    seconds: 86400
  invalidation:
    git: true
    binary_hash: true
    dependency: true
    tool_version: true
  concurrency:
    prevent_duplicate_work: true
  privacy:
    cache_llm_responses: true
    cache_source_content: false
    cache_binary_content: false
    cache_tool_results: true

verification:
  enabled: false
  allowed_commands:
    - "go"
    - "python"
    - "npm"
    - "cargo"
    - "make"
  timeout_seconds: 120
  max_output_bytes: 65536
YAML
    
    log "Config written to $config_path"
}

# Configure agent
# --- agent configuration -------------------------------------------------
# Every change merges into the agent's existing config (never overwrites other
# servers) and backs the file up first. Agents with a native "mcp add" CLI use it.

BIN_PATH() { echo "${BIN_DIR}/codergag"; }

backup_file() {
    [[ -f "$1" ]] || return 0
    local b; b="$1.codergag-bak-$(date +%Y%m%d%H%M%S)"
    cp -p "$1" "$b" && log "  backup: $b"
}

# json_merge FILE JQ_FILTER [jq args...] - merge codergag into a JSON config.
json_merge() {
    local file="$1" filter="$2"; shift 2
    if ! command -v jq &>/dev/null; then warn "jq is required to edit $file - skipped"; return 1; fi
    local base='{}'
    if [[ -f "$file" ]]; then
        if ! jq -e . "$file" &>/dev/null; then
            warn "$file is not plain JSON (comments?) - add the codergag server manually"
            return 1
        fi
        base="$(cat "$file")"
    fi
    local out
    out="$(printf '%s' "$base" | jq --arg bin "$(BIN_PATH)" --arg cfg "${CONFIG_DIR}/config.yaml" "$@" "$filter")" || return 1
    if [[ "$DRY_RUN" == "true" ]]; then
        log "  [dry-run] would update $file (servers after: $(printf '%s' "$out" | jq -c '[(.mcp // .mcpServers // .servers) | keys[]]'))"
        return 0
    fi
    if [[ -f "$file" && "$(printf '%s' "$out" | jq -S .)" == "$(jq -S . "$file")" ]]; then
        log "  already configured: $file"
        return 0
    fi
    backup_file "$file"
    mkdir -p "$(dirname "$file")"
    printf '%s\n' "$out" > "$file"   # write in place so permissions are kept
    log "  configured: $file"
}

# run_cli DESCRIPTION CMD... - execute, or print under --dry-run.
run_cli() {
    local what="$1"; shift
    if [[ "$DRY_RUN" == "true" ]]; then log "  [dry-run] would run: $*"; return 0; fi
    "$@" &>/dev/null && log "  configured: $what" || { warn "  failed: $*"; return 1; }
}

MCP_LOCAL='{codergag:{type:"local",command:[$bin,"serve"],environment:{CODERAG_CONFIG:$cfg},enabled:true}}'
MCP_STDIO='{codergag:{command:$bin,args:["serve"],env:{CODERAG_CONFIG:$cfg}}}'

configure_claude_code() {
    if command -v claude &>/dev/null; then
        # The CLI owns ~/.claude.json (it is rewritten constantly while Claude Code runs).
        claude mcp remove --scope user codergag &>/dev/null || true
        run_cli "Claude Code (user scope)" claude mcp add codergag --scope user --env "CODERAG_CONFIG=${CONFIG_DIR}/config.yaml" -- "$(BIN_PATH)" serve
    else
        json_merge "$HOME/.claude.json" ".mcpServers = ((.mcpServers // {}) + $MCP_STDIO)"
    fi
}
configure_codex() {
    if command -v codex &>/dev/null; then
        codex mcp remove codergag &>/dev/null || true
        run_cli "Codex" codex mcp add codergag --env "CODERAG_CONFIG=${CONFIG_DIR}/config.yaml" -- "$(BIN_PATH)" serve
    else
        warn "codex CLI not found - skipped"
    fi
}
configure_opencode() { json_merge "$HOME/.config/opencode/opencode.json" ".mcp = ((.mcp // {}) + $MCP_LOCAL)"; }
configure_kilo() {
    local f="$HOME/.config/kilo/kilo.jsonc"; [[ -f "$f" ]] || f="$HOME/.config/kilo/kilo.json"
    json_merge "$f" ".mcp = ((.mcp // {}) + $MCP_LOCAL)"
}
configure_cursor() { json_merge "$HOME/.cursor/mcp.json" ".mcpServers = ((.mcpServers // {}) + $MCP_STDIO)"; }
configure_vscode() {
    local d="$HOME/.config/Code/User"; [[ -d "$d" ]] || d="$HOME/Library/Application Support/Code/User"
    json_merge "$d/mcp.json" ".servers = ((.servers // {}) + {codergag:{type:\"stdio\",command:\$bin,args:[\"serve\"],env:{CODERAG_CONFIG:\$cfg}}})"
}
configure_continue() {
    local f="$HOME/.continue/config.json"
    json_merge "$f" '.mcpServers = ((.mcpServers // []) | map(select(.name != "codergag")) + [{name:"codergag",command:$bin,args:["serve"],env:{CODERAG_CONFIG:$cfg}}])'
}

configure_agent() {
    [[ "$SKIP_CONFIG" == "true" ]] && return
    [[ "$AGENT" == "none" ]] && { log "No agent to configure"; return; }

    local list="$AGENT"
    if [[ "$AGENT" == "all" ]]; then
        list="$(installed_agents | paste -sd, -)"
        [[ -z "$list" ]] && { log "No supported agents found"; return; }
        log "Agents found: ${list//,/ }"
    fi

    local a
    IFS=',' read -ra AGENTS <<< "$list"
    for a in "${AGENTS[@]}"; do
        log "Configuring: $a"
        case "$a" in
            claude-code) configure_claude_code ;;
            codex)       configure_codex ;;
            opencode)    configure_opencode ;;
            kilo)        configure_kilo ;;
            cursor)      configure_cursor ;;
            vscode|copilot) configure_vscode ;;
            continue)    configure_continue ;;
            gemini)      warn "  gemini: no stable MCP config location - add '$(BIN_PATH) serve' manually" ;;
            aider)       warn "  aider has no MCP support - run '$(BIN_PATH) serve' separately" ;;
            *)           warn "  unknown agent: $a (skipped)" ;;
        esac
    done
    CONFIGURED_AGENTS="$list"
}

# Add binary to PATH
setup_path() {
    local shell_rc=""
    
    if [[ -n "${ZSH_VERSION:-}" ]] || [[ "$SHELL" == */zsh ]]; then
        shell_rc="${HOME}/.zshrc"
    elif [[ -n "${BASH_VERSION:-}" ]] || [[ "$SHELL" == */bash ]]; then
        shell_rc="${HOME}/.bashrc"
    else
        shell_rc="${HOME}/.profile"
    fi
    
    local path_entry="export PATH=\"${BIN_DIR}:\$PATH\""
    
    if ! grep -q "$BIN_DIR" "$shell_rc" 2>/dev/null; then
        echo "" >> "$shell_rc"
        echo "# codeRAG" >> "$shell_rc"
        echo "$path_entry" >> "$shell_rc"
        log "Added ${BIN_DIR} to PATH in $shell_rc"
        log "Run 'source $shell_rc' or restart your shell"
    else
        log "PATH already configured in $shell_rc"
    fi
}

# Verify installation
verify_install() {
    log "Verifying installation..."
    
    if [[ "$SKIP_BUILD" == "true" ]]; then
        log "Skipping binary verification (--skip-build)"
        return
    fi
    
    if [[ ! -x "${BIN_DIR}/codergag" ]]; then
        err "Binary not found at ${BIN_DIR}/codergag"
        exit 1
    fi
    
    "${BIN_DIR}/codergag" status --json >/dev/null 2>&1 || {
        err "Binary test failed"
        exit 1
    }
    
    log "Installation verified ✓"
}

# Main
main() {
    log "codeRAG installer v${VERSION}"
    log "Install directory: $INSTALL_DIR"
    log "Binary directory: $BIN_DIR"
    
    detect_agent
    if [[ "$DRY_RUN" == "true" ]]; then
        log "DRY RUN: reporting agent changes only; nothing is written"
        configure_agent
        return 0
    fi
    check_prereqs

    mkdir -p "$BIN_DIR" "$INSTALL_DIR" "$CONFIG_DIR"

    build_from_source
    create_config
    configure_agent
    setup_path
    verify_install
    
    echo ""
    log "Installation complete!"
    echo ""
    echo "Next steps:"
    echo "  1. Restart your shell or run: source ~/.bashrc (or ~/.zshrc)"
    echo "  2. Index a project: codergag serve (in another terminal) then use index_repository tool"
    echo "  3. Run evaluation: codergag eval -project <id>"
    echo ""
    echo "Config: ${CONFIG_DIR}/config.yaml"
    echo "Binary: ${BIN_DIR}/codergag"
    echo "Data:   ${INSTALL_DIR}/codergag.db"
    echo ""
    
    if [[ -n "${CONFIGURED_AGENTS:-}" && "$DRY_RUN" != "true" ]]; then
        echo "Restart these agents to load the codergag MCP server: ${CONFIGURED_AGENTS//,/ }"
    fi
}

main "$@"