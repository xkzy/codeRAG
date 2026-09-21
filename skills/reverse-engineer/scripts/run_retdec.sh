#!/bin/bash
# Run RetDec decompiler on a binary and output JSON
# Usage: ./run_retdec.sh <binary_path> <output_dir>
set -euo pipefail

BINARY_PATH="${1:-}"
OUTPUT_DIR="${2:-./retdec_output}"

if [ -z "$BINARY_PATH" ]; then
    echo "Usage: $0 <binary_path> [output_dir]"
    exit 1
fi

mkdir -p "$OUTPUT_DIR"

# Try local retdec first, fall back to Docker
if command -v retdec-decompiler.sh &>/dev/null; then
    retdec-decompiler.sh --json "$BINARY_PATH" -o "$OUTPUT_DIR/$(basename "$BINARY_PATH").json"
elif command -v docker &>/dev/null; then
    docker run --rm -v "$PWD:/workspace" retdec/retdec \
        retdec-decompiler.sh --json "/workspace/$BINARY_PATH" \
        -o "/workspace/$OUTPUT_DIR/$(basename "$BINARY_PATH").json"
else
    echo "RetDec not found. Install from https://github.com/avast/retdec"
    echo "Or use online at https://retdec.com/decompilation/"
    exit 1
fi

echo "Output written to: $OUTPUT_DIR/$(basename "$BINARY_PATH").json"
