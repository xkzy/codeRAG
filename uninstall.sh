#!/usr/bin/env bash
# codeRAG uninstaller - reverses install.sh
# Usage: ./uninstall.sh [--agent <agent>] [--dir <dir>] [--bin-dir <dir>] [--purge] [--dry-run]
#
# By default your indexed data (~/.codergag/*.db) and config are kept.
# Pass --purge to delete them too.

set -euo pipefail

INSTALL_DIR="${HOME}/.codergag"
CONFIG_DIR="${HOME}/.config/codergag"
BIN_DIR="${HOME}/.local/bin"
AGENT="all"
PURGE=false
DRY_RUN=false
AGENTS_ONLY=false

usage() {
    cat <<EOF
codeRAG uninstaller

Usage: $0 [options]

Options:
  --agent AGENT       Agent(s) to unconfigure: claude-code, codex, opencode, kilo, cursor, vscode, continue,
                      a comma list, "all" (every agent found, default) or none
  --dir DIR           Installation directory (default: ~/.codergag)
  --bin-dir DIR       Binary directory (default: ~/.local/bin)
  --purge             Also delete the database/data dir and config dir
  --agents-only       Only remove codergag from agent configs; keep binary, PATH and data
  --dry-run           Show what would be removed; change nothing
  --help              Show this help
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
        --purge) PURGE=true; shift ;;
        --agents-only) AGENTS_ONLY=true; shift ;;
        --dry-run) DRY_RUN=true; shift ;;
        --help) usage; exit 0 ;;
        *) err "Unknown option: $1"; usage; exit 1 ;;
    esac
done

# Agents present on this machine, one per line (same detection as install.sh).
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

backup_file() {
    [[ -f "$1" ]] || return 0
    local b; b="$1.codergag-bak-$(date +%Y%m%d%H%M%S)"
    cp -p "$1" "$b" && log "  backup: $b"
}

# json_remove FILE JQ_FILTER - drop codergag from a JSON config, only if present.
json_remove() {
    local file="$1" filter="$2"
    [[ -f "$file" ]] || { log "  no config: $file"; return 0; }
    if ! command -v jq &>/dev/null; then warn "jq is required to edit $file - skipped"; return 1; fi
    if ! jq -e . "$file" &>/dev/null; then
        warn "$file is not plain JSON (comments?) - remove the codergag server manually"
        return 1
    fi
    if ! grep -q codergag "$file"; then log "  not configured: $file"; return 0; fi
    local out
    out="$(jq "$filter" "$file")" || return 1
    if [[ "$DRY_RUN" == "true" ]]; then log "  [dry-run] would remove codergag from $file"; return 0; fi
    backup_file "$file"
    printf '%s\n' "$out" > "$file"   # write in place so permissions are kept
    log "  removed: $file"
}

# run_cli DESCRIPTION CMD... - execute, or print under --dry-run.
run_cli() {
    local what="$1"; shift
    if [[ "$DRY_RUN" == "true" ]]; then log "  [dry-run] would run: $*"; return 0; fi
    "$@" &>/dev/null && log "  removed: $what" || warn "  nothing to remove (or failed): $*"
}

unconfigure_claude_code() {
    if command -v claude &>/dev/null; then
        run_cli "Claude Code (user scope)" claude mcp remove --scope user codergag
    else
        json_remove "$HOME/.claude.json" 'if .mcpServers then .mcpServers |= del(.codergag) else . end'
    fi
}
unconfigure_codex() {
    if command -v codex &>/dev/null; then
        run_cli "Codex" codex mcp remove codergag
    else
        warn "codex CLI not found - skipped"
    fi
}
unconfigure_opencode() { json_remove "$HOME/.config/opencode/opencode.json" 'if .mcp then .mcp |= del(.codergag) else . end'; }
unconfigure_kilo() {
    local f="$HOME/.config/kilo/kilo.jsonc"; [[ -f "$f" ]] || f="$HOME/.config/kilo/kilo.json"
    json_remove "$f" 'if .mcp then .mcp |= del(.codergag) else . end'
}
unconfigure_cursor() { json_remove "$HOME/.cursor/mcp.json" 'if .mcpServers then .mcpServers |= del(.codergag) else . end'; }
unconfigure_vscode() {
    local d="$HOME/.config/Code/User"; [[ -d "$d" ]] || d="$HOME/Library/Application Support/Code/User"
    json_remove "$d/mcp.json" 'if .servers then .servers |= del(.codergag) else . end'
}
unconfigure_continue() {
    json_remove "$HOME/.continue/config.json" 'if .mcpServers then .mcpServers |= map(select(.name != "codergag")) else . end'
}

unconfigure_agents() {
    [[ "$AGENT" == "none" ]] && { log "Skipping agent configs"; return; }

    local list="$AGENT"
    if [[ "$AGENT" == "all" ]]; then
        list="$(installed_agents | paste -sd, -)"
        [[ -z "$list" ]] && { log "No supported agents found"; return; }
        log "Agents found: ${list//,/ }"
    fi

    local a
    IFS=',' read -ra AGENTS <<< "$list"
    for a in "${AGENTS[@]}"; do
        log "Unconfiguring: $a"
        case "$a" in
            claude-code) unconfigure_claude_code ;;
            codex)       unconfigure_codex ;;
            opencode)    unconfigure_opencode ;;
            kilo)        unconfigure_kilo ;;
            cursor)      unconfigure_cursor ;;
            vscode|copilot) unconfigure_vscode ;;
            continue)    unconfigure_continue ;;
            gemini|aider) warn "  $a: installer never configured it automatically - check manually" ;;
            *)           warn "  unknown agent: $a (skipped)" ;;
        esac
    done
}

# Remove the "# codeRAG" + PATH export block that install.sh appended.
remove_path() {
    local rc
    for rc in "$HOME/.zshrc" "$HOME/.bashrc" "$HOME/.profile"; do
        [[ -f "$rc" ]] || continue
        grep -q '^# codeRAG$' "$rc" || continue
        if [[ "$DRY_RUN" == "true" ]]; then log "[dry-run] would remove PATH block from $rc"; continue; fi
        backup_file "$rc"
        # Drop the marker line and the export line right after it (and the blank line before).
        awk '
            { lines[NR] = $0 }
            END {
                for (i = 1; i <= NR; i++) {
                    if (lines[i] == "# codeRAG" && lines[i+1] ~ /^export PATH=/) {
                        if (out > 0 && buf[out] == "") out--
                        i++
                        continue
                    }
                    buf[++out] = lines[i]
                }
                for (i = 1; i <= out; i++) print buf[i]
            }' "$rc" > "${rc}.codergag-tmp" && cat "${rc}.codergag-tmp" > "$rc"
        rm -f "${rc}.codergag-tmp"
        log "Removed PATH entry from $rc"
    done
}

rm_path() {
    local p="$1"
    [[ -e "$p" || -L "$p" ]] || return 0
    if [[ "$DRY_RUN" == "true" ]]; then log "[dry-run] would delete $p"; return 0; fi
    rm -rf -- "$p"
    log "Deleted $p"
}

main() {
    log "codeRAG uninstaller"
    [[ "$DRY_RUN" == "true" ]] && log "DRY RUN: nothing will be changed"

    # Stop a running server / service so the binary can be removed cleanly.
    if command -v systemctl &>/dev/null && systemctl --user list-unit-files codergag.service &>/dev/null; then
        if systemctl --user list-unit-files codergag.service 2>/dev/null | grep -q codergag; then
            if [[ "$DRY_RUN" == "true" ]]; then
                log "[dry-run] would stop/disable user service codergag"
            else
                systemctl --user disable --now codergag.service &>/dev/null || true
                log "Stopped codergag user service (unit file left in place)"
            fi
        fi
    fi

    unconfigure_agents
    [[ "$AGENTS_ONLY" == "true" ]] && { log "Agent configs done (--agents-only)"; return; }
    rm_path "${BIN_DIR}/codergag"
    remove_path
    rm_path "${INSTALL_DIR}/src"

    if [[ "$PURGE" == "true" ]]; then
        rm_path "$INSTALL_DIR"
        rm_path "$CONFIG_DIR"
    else
        warn "Kept data and config (use --purge to delete):"
        warn "  ${INSTALL_DIR}"
        warn "  ${CONFIG_DIR}"
    fi

    echo ""
    log "Uninstall complete. Restart your shell and any configured agents."
}

main "$@"
