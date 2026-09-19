#!/usr/bin/env bash
# codeRAG installer - works with any coding agent
# Usage: curl -fsSL https://raw.githubusercontent.com/xkzy/codeRAG/main/install.sh | bash
# Or: ./install.sh [--agent <agent>] [--dir <dir>] [--config <path>]

set -euo pipefail

VERSION="1.0.0"
REPO="xkzy/codeRAG"
INSTALL_DIR="${HOME}/.codergag"
CONFIG_DIR="${HOME}/.config/codergag"
BIN_DIR="${HOME}/.local/bin"
AGENT=""
CUSTOM_CONFIG=""
SKIP_BUILD=false
SKIP_CONFIG=false
FORCE=false

usage() {
    cat <<EOF
codeRAG installer v${VERSION}

Usage: $0 [options]

Options:
  --agent AGENT       Coding agent to configure (claude-code, copilot, cursor, kilo, opencode, continue, aider, none)
  --dir DIR           Installation directory (default: ~/.codergag)
  --bin-dir DIR       Binary directory (default: ~/.local/bin)
  --config PATH       Use custom config file
  --skip-build        Skip building from source (use prebuilt)
  --skip-config       Skip agent configuration
  --force             Overwrite existing installation
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
        --help) usage; exit 0 ;;
        *) err "Unknown option: $1"; usage; exit 1 ;;
    esac
done

# Detect agent if not specified
detect_agent() {
    if [[ -n "$AGENT" ]]; then return; fi
    
    if command -v claude &>/dev/null; then
        AGENT="claude-code"
    elif command -v cursor &>/dev/null; then
        AGENT="cursor"
    elif command -v kilo &>/dev/null; then
        AGENT="kilo"
    elif command -v opencode &>/dev/null; then
        AGENT="opencode"
    elif command -v continue &>/dev/null; then
        AGENT="continue"
    elif command -v aider &>/dev/null; then
        AGENT="aider"
    elif [[ -d "$HOME/.config/claude" ]]; then
        AGENT="claude-code"
    elif [[ -d "$HOME/.cursor" ]]; then
        AGENT="cursor"
    elif [[ -d "$HOME/.config/kilo" ]]; then
        AGENT="kilo"
    else
        AGENT="none"
    fi
    log "Detected agent: $AGENT"
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

# Build from source
build_from_source() {
    if [[ "$SKIP_BUILD" == "true" ]]; then
        log "Skipping build (--skip-build)"
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
configure_agent() {
    [[ "$SKIP_CONFIG" == "true" ]] && return
    [[ "$AGENT" == "none" ]] && { log "No agent to configure"; return; }
    
    log "Configuring for agent: $AGENT"
    
    local config_path="${CONFIG_DIR}/config.yaml"
    local mcp_config=""
    
    case "$AGENT" in
        claude-code)
            configure_claude_code "$config_path"
            ;;
        copilot)
            configure_copilot "$config_path"
            ;;
        cursor)
            configure_cursor "$config_path"
            ;;
        kilo)
            configure_kilo "$config_path"
            ;;
        opencode)
            configure_opencode "$config_path"
            ;;
        continue)
            configure_continue "$config_path"
            ;;
        aider)
            configure_aider "$config_path"
            ;;
        *)
            warn "Unknown agent: $AGENT, skipping configuration"
            ;;
    esac
}

configure_claude_code() {
    local config_path="$1"
    local mcp_file="${HOME}/.config/claude/mcp_servers.json"
    
    mkdir -p "$(dirname "$mcp_file")"
    
    if [[ -f "$mcp_file" ]]; then
        # Update existing
        if command -v jq &>/dev/null; then
            jq --arg cmd "codergag serve" --arg config "$config_path" \
               '. + {"codergag": {"command": $cmd, "args": [], "env": {"CODERAG_CONFIG": $config}}}' \
               "$mcp_file" > "${mcp_file}.tmp" && mv "${mcp_file}.tmp" "$mcp_file"
        else
            warn "jq not found, please add manually to $mcp_file"
            return
        fi
    else
        cat > "$mcp_file" <<EOF
{
  "codergag": {
    "command": "codergag",
    "args": ["serve"],
    "env": {
      "CODERAG_CONFIG": "$config_path"
    }
  }
}
EOF
    fi
    
    log "Claude Code MCP configured: $mcp_file"
}

configure_copilot() {
    warn "GitHub Copilot doesn't support MCP servers directly."
    warn "Use the VS Code extension or continue with another agent."
}

configure_cursor() {
    local config_path="$1"
    local mcp_file="${HOME}/.cursor/mcp.json"
    
    mkdir -p "$(dirname "$mcp_file")"
    
    cat > "$mcp_file" <<EOF
{
  "mcpServers": {
    "codergag": {
      "command": "codergag",
      "args": ["serve"],
      "env": {
        "CODERAG_CONFIG": "$config_path"
      }
    }
  }
}
EOF
    
    log "Cursor MCP configured: $mcp_file"
}

configure_kilo() {
    local config_path="$1"
    local kilo_dir="${HOME}/.config/kilo"
    local mcp_file="${kilo_dir}/mcp.json"
    
    mkdir -p "$kilo_dir"
    
    cat > "$mcp_file" <<EOF
{
  "mcpServers": {
    "codergag": {
      "command": "codergag",
      "args": ["serve"],
      "env": {
        "CODERAG_CONFIG": "$config_path"
      }
    }
  }
}
EOF
    
    log "Kilo MCP configured: $mcp_file"
}

configure_opencode() {
    local config_path="$1"
    local opencode_dir="${HOME}/.config/opencode"
    local mcp_file="${opencode_dir}/mcp.json"
    
    mkdir -p "$opencode_dir"
    
    cat > "$mcp_file" <<EOF
{
  "mcpServers": {
    "codergag": {
      "command": "codergag",
      "args": ["serve"],
      "env": {
        "CODERAG_CONFIG": "$config_path"
      }
    }
  }
}
EOF
    
    log "OpenCode MCP configured: $mcp_file"
}

configure_continue() {
    local config_path="$1"
    local continue_dir="${HOME}/.continue"
    local config_file="${continue_dir}/config.json"
    
    mkdir -p "$continue_dir"
    
    if [[ -f "$config_file" ]]; then
        warn "Continue config exists at $config_file"
        warn "Please add codergag MCP server manually to your config.json"
    else
        cat > "$config_file" <<EOF
{
  "mcpServers": [
    {
      "name": "codergag",
      "command": "codergag",
      "args": ["serve"],
      "env": {
        "CODERAG_CONFIG": "$config_path"
      }
    }
  ]
}
EOF
        log "Continue config created: $config_file"
    fi
}

configure_aider() {
    warn "Aider doesn't support MCP servers directly."
    warn "Run 'codergag serve' in a separate terminal and use aider with --mcp flag if available."
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
    
    case "$AGENT" in
        claude-code)
            echo "Claude Code: Restart Claude Code to load the MCP server"
            ;;
        cursor)
            echo "Cursor: Restart Cursor to load the MCP server"
            ;;
        kilo)
            echo "Kilo: Restart Kilo to load the MCP server"
            ;;
        opencode)
            echo "OpenCode: Restart OpenCode to load the MCP server"
            ;;
        continue)
            echo "Continue: Restart Continue to load the MCP server"
            ;;
    esac
}

main "$@"