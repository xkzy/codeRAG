# RetDec API Reference

## REST API

Base URL: `https://retdec.com/api/v1/`

### Decompilation

```
POST /api/v1/decompilation
Headers: Authorization: Bearer <API_KEY>
Body: { "input_file": "<binary_path>" }
Returns: { "decompilation_id": "..." }
```

```
GET /api/v1/decompilation/{decompilation_id}
Returns: { "status": "finished", "output_file": "..." }
```

```
GET /api/v1/decompilations/{decompilation_id}/decompiled
Returns: C source code
```

### File Info

```
POST /api/v1/fileinfo
Body: { "input_file": "<binary_path>" }
Returns: File metadata, architecture, format, entry point
```

## Local CLI

```bash
# Decompile with JSON output
retdec-decompiler.sh --json <binary_file>

# Decompile specific function
retdec-decompiler.sh --function <address> <binary_file>

# Output format options
retdec-decompiler.sh --output-format c|py|json <binary_file>
```

## JSON Output Fields

| Field | Description |
|-------|-------------|
| `binary_id` | Unique binary identifier |
| `path` | File system path to binary |
| `sha256` | SHA-256 hash of binary |
| `functions[]` | Array of function objects |
| `functions[].address` | Function start address (hex string) |
| `functions[].name` | Function name (auto-generated if stripped) |
| `functions[].size` | Function size in bytes |
| `functions[].calls[]` | Addresses of called functions |
| `functions[].strings[]` | Strings referenced by function |
| `functions[].decompiler_output` | Decompiled C pseudocode |
| `functions[].basic_blocks[]` | Control flow basic blocks |
| `functions[].basic_blocks[].instructions[]` | Assembly instructions |

## Docker Usage

```bash
docker run --rm -v $PWD:/mount retdec/retdec \
    retdec-decompiler.sh --json /mount/binary
```
