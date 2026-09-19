# Ghidra adapter contract

Exporters should transform Ghidra results into `NormalizedBinary` / `NormalizedFunction` and pass them to `ReverseEngineeringService.import_binary`. Required stable identity is the binary SHA-256 plus function address. Optional fields include basic blocks, instructions, data references, types, and decompiler text; add them as normalized records rather than coupling graph services to Ghidra APIs.

The MCP `index_binary` tool accepts this portable JSON shape:

```json
{"binary_id":"firmware-1.0","path":"/samples/fw.bin","sha256":"…","tool":"ghidra","functions":[{"address":"0x401230","name":"FUN_00401230","calls":["0x402100"],"strings":["CAT48"],"decompiler_output":"…"}]}
```
