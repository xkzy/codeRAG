package objdump

import (
	"context"
	"encoding/json"
	"testing"

)

func TestObjdumpAdapter(t *testing.T) {
	adapter := ObjdumpAdapter{}
	if adapter.Name() != "objdump" {
		t.Fatalf("expected name 'objdump', got %s", adapter.Name())
	}

	// Test with objdump -d output
	objdumpOut := `0000000000401000 <main>:
  401000:       55                      push   %rbp
  401001:       48 89 e5                mov    %rsp,%rbp
  401004:       bf 00 00 00 00          mov    $0x0,%edi
  401009:       e8 00 00 00 00          call   40100e <puts@plt>
  40100e:       5d                      pop    %rbp
  40100f:       c3                      ret`

	readelfOut := `Symbol table '.symtab' contains 10 entries:
   Num:    Value          Size Type    Bind   Vis      Ndx Name
     0: 0000000000000000     0 NOTYPE  LOCAL  DEFAULT  UND
     1: 0000000000401000    43 FUNC    GLOBAL DEFAULT   14 main
     2: 0000000000401200    10 FUNC    GLOBAL DEFAULT   14 foo`

	input := ObjdumpExport{
		BinaryID:      "fw-1.0",
		Path:          "/samples/fw.bin",
		SHA256:        "abc123",
		ObjdumpOutput: objdumpOut,
		ReadelfOutput: readelfOut,
	}

	data, _ := json.Marshal(input)
	result, err := adapter.Convert(context.Background(), data)
	if err != nil {
		t.Fatalf("Convert failed: %v", err)
	}

	if result.Tool != "objdump" {
		t.Errorf("expected tool objdump, got %s", result.Tool)
	}
	if len(result.Functions) != 1 {
		t.Errorf("expected 1 function, got %d", len(result.Functions))
	}
	fn := result.Functions[0]
	if fn.Name != "main" {
		t.Errorf("expected function name main, got %s", fn.Name)
	}
	if len(fn.BasicBlocks) == 0 {
		t.Errorf("expected basic blocks to be parsed")
	}
}