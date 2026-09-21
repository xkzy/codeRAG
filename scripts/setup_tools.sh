#!/bin/bash
# Auto-install tools needed by codeRAG adapters
# Usage: ./scripts/setup_tools.sh [all|gdb|lldb|retdec|ghidra|objdump|binja|ida]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

log() { echo "[setup-tools] $*"; }

install_retdec() {
    log "Checking for RetDec..."
    if command -v retdec-decompiler.sh &>/dev/null; then
        log "RetDec already installed"
        return 0
    fi
    if command -v docker &>/dev/null; then
        log "Installing RetDec via Docker image..."
        docker pull retdec/retdec:latest
        log "RetDec Docker image ready. Use: docker run -v \$PWD:/mount retdec/retdec retdec-decompiler.sh <binary>"
        return 0
    fi
    log "RetDec not found locally and Docker not available."
    log "Install manually: https://github.com/avast/retdec"
    log "Or use online: https://retdec.com/decompilation/"
    return 1
}

install_gdb() {
    log "Checking for GDB..."
    if command -v gdb &>/dev/null; then
        log "GDB already installed"
        return 0
    fi
    if command -v apt-get &>/dev/null; then
        log "Installing GDB via apt..."
        sudo apt-get update && sudo apt-get install -y gdb python3 gdb-doc
        return 0
    fi
    if command -v brew &>/dev/null; then
        log "Installing GDB via Homebrew..."
        brew install gdb
        return 0
    fi
    log "GDB not found. Install manually from your package manager."
    return 1
}

install_lldb() {
    log "Checking for LLDB..."
    if command -v lldb &>/dev/null; then
        log "LLDB already installed"
        return 0
    fi
    if command -v apt-get &>/dev/null; then
        log "Installing LLDB via apt..."
        sudo apt-get update && sudo apt-get install -y lldb python3-lldb
        return 0
    fi
    if command -v brew &>/dev/null; then
        log "Installing LLDB via Homebrew..."
        brew install llvm
        return 0
    fi
    log "LLDB not found. Install manually from your package manager."
    return 1
}

install_ghidra() {
    log "Checking for Ghidra..."
    if command -v ghidra &>/dev/null || [ -d "$HOME/ghidra" ]; then
        log "Ghidra already installed"
        return 0
    fi
    log "Downloading Ghidra..."
    local ghidra_url="https://ghidra-sre.org/Ghidra_11.0.4_build.zip"
    local ghidra_zip="$SCRIPT_DIR/ghidra.zip"
    if command -v curl &>/dev/null; then
        curl -L -o "$ghidra_zip" "$ghidra_url" || log "Failed to download Ghidra"
    elif command -v wget &>/dev/null; then
        wget -O "$ghidra_zip" "$ghidra_url" || log "Failed to download Ghidra"
    else
        log "Neither curl nor wget available. Download manually from https://ghidra-sre.org/"
        return 1
    fi
    log "Extracting Ghidra..."
    unzip -q "$ghidra_zip" -d "$HOME/" 2>/dev/null || log "Unzip failed, install unzip package"
    rm "$ghidra_zip"
    log "Ghidra installed in $HOME/ghidra*"
    return 0
}

install_objdump() {
    log "Checking for objdump/readelf..."
    if command -v objdump &>/dev/null || command -v readelf &>/dev/null; then
        log "binutils already installed"
        return 0
    fi
    if command -v apt-get &>/dev/null; then
        log "Installing binutils..."
        sudo apt-get update && sudo apt-get install -y binutils
        return 0
    fi
    if command -v brew &>/dev/null; then
        log "Installing binutils..."
        brew install binutils
        return 0
    fi
    log "binutils not found. Install manually from your package manager."
    return 1
}

install_ollama() {
    log "Checking for Ollama..."
    if command -v ollama &>/dev/null; then
        log "Ollama already installed"
        return 0
    fi
    if command -v curl &>/dev/null; then
        log "Installing Ollama..."
        curl -fsSL https://ollama.com/install.sh | sh
        log "Ollama installed. Start with: ollama serve"
        log "Then run: ollama pull phi3:mini"
        return 0
    fi
    log "curl not available. Install Ollama manually from: https://ollama.com/download"
    return 1
}

install_all() {
    local failed=0
    install_retdec || failed=$((failed + 1))
    install_gdb || failed=$((failed + 1))
    install_lldb || failed=$((failed + 1))
    install_ghidra || failed=$((failed + 1))
    install_objdump || failed=$((failed + 1))
    install_ollama || failed=$((failed + 1))
    if [ "$failed" -gt 0 ]; then
        log "WARNING: $failed tool(s) failed to install. Check output above."
        return 1
    fi
    log "All tools installed successfully."
    return 0
}

show_help() {
    cat <<EOF
Usage: $0 [all|retdec|gdb|lldb|ghidra|objdump|binja|ida]

Auto-install tools needed by codeRAG adapters.

Commands:
  all       Install all available tools (default)
  retdec    Install RetDec decompiler
  gdb       Install GDB debugger
  lldb      Install LLDB debugger
  ghidra    Install Ghidra (download)
  objdump   Install binutils (objdump/readelf)
  ollama    Install Ollama for small-model offloading
  binja     Install Binary Ninja (manual - commercial)
  ida       Install IDA Pro (manual - commercial)
  help      Show this help message

Note: Binary Ninja and IDA Pro are commercial tools and require manual installation.
EOF
}

case "${1:-all}" in
    all) install_all ;;
    retdec) install_retdec ;;
    gdb) install_gdb ;;
    lldb) install_lldb ;;
    ghidra) install_ghidra ;;
    objdump) install_objdump ;;
    ollama) install_ollama ;;
    binja)
        log "Binary Ninja is a commercial tool. Download from: https://binary.ninja/"
        log "After installation, set BINJA_PATH environment variable."
        ;;
    ida)
        log "IDA Pro is a commercial tool. Download from: https://hex-rays.com/ida-pro/"
        log "After installation, set IDA_PATH environment variable."
        ;;
    help|-h|--help) show_help ;;
    *)
        echo "Unknown command: $1"
        show_help
        exit 1
        ;;
esac
