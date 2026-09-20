package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codergag/internal/graph"
	"codergag/internal/ids"
	"codergag/internal/reverse"
)

const cat21 = "package x\n\nfunc parse_cat21() int { return decode_item() }\n\nfunc decode_item() int { return 1 }\n\ntype TargetState struct{ V int }\n"

func fnByStable(t *testing.T, app *Application, sid string) string {
	t.Helper()
	n, _ := app.Refs.Lookup("p", sid)
	if n == nil {
		t.Fatalf("%s not found", sid)
	}
	return n.ID
}

func TestStableIDsAreRelativeAndCarrySpans(t *testing.T) {
	app, root := indexTree(t, map[string]string{"src/cat21.go": cat21})
	res := app.Refs.Resolve("p", "func:src/cat21.go:parse_cat21")
	if res.Status != RefValid || res.Source == nil {
		t.Fatalf("resolve: %+v", res)
	}
	if strings.Contains(res.ID, root) {
		t.Fatal("stable IDs must not contain the absolute checkout path")
	}
	sp := res.Source
	data, _ := os.ReadFile(filepath.Join(root, "src/cat21.go"))
	if got := string(data[sp.StartByte:sp.EndByte]); !strings.HasPrefix(got, "func parse_cat21") || !strings.HasSuffix(got, "}") {
		t.Fatalf("byte span does not cover the function: %q", got)
	}
	if sp.StartLine != 3 || sp.EndLine != 3 || sp.FileID != "file:src/cat21.go" || sp.ContentHash == "" || sp.SymbolID != res.ID {
		t.Fatalf("span: %+v", sp)
	}
	for _, id := range []string{"struct:src/cat21.go:TargetState", "file:src/cat21.go"} {
		if r := app.Refs.Resolve("p", id); r.Status != RefValid {
			t.Errorf("%s: %+v", id, r)
		}
	}
}

func TestSymbolIdentitySurvivesReindexAndKeepsAttachedEdges(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.go": cat21})
	app := ApplicationInMemory()
	app.Index.IndexRepository("p", dir, true, nil, false)
	before := fnByStable(t, app, "func:a.go:parse_cat21")
	gone := fnByStable(t, app, "func:a.go:decode_item")

	// A memory and an evidence-style edge attach to the function.
	app.Memory.Store("p", "note", "parse_cat21 reads the header", map[string]any{"auto_compact": false})
	ev, _ := app.Graph.UpsertNode("Evidence", map[string]any{"project_id": "p", "id": "ev1"}, map[string]any{"description": "trace"})
	app.Graph.Link("REFERENCES", before, ev.ID, nil)

	// Edit the function body and delete decode_item; re-index.
	writeTree(t, dir, map[string]string{"a.go": "package x\n\nfunc parse_cat21() int { return 42 }\n\ntype TargetState struct{ V int }\n"})
	app.Index.IndexRepository("p", dir, true, nil, false)

	if after := fnByStable(t, app, "func:a.go:parse_cat21"); after != before {
		t.Fatalf("node ID changed across re-index: %s -> %s", before, after)
	}
	if n, _ := app.Graph.GetNode(before); n == nil {
		t.Fatal("symbol node vanished")
	}
	if nbrs, _ := app.Graph.Neighbors(before, "REFERENCES", graph.DirOut); len(nbrs) != 1 {
		t.Fatalf("evidence edge lost on re-index: %d", len(nbrs))
	}
	if in, _ := app.Graph.Neighbors(before, "MENTIONS", graph.DirIn); len(in) != 1 {
		t.Fatalf("memory link lost on re-index: %d", len(in))
	}
	if n, _ := app.Graph.GetNode(gone); n != nil {
		t.Fatal("a deleted symbol must be removed")
	}
	if r := app.Refs.Resolve("p", "func:a.go:decode_item"); r.Status != RefInvalid {
		t.Fatalf("deleted symbol must be INVALID_REFERENCE: %+v", r)
	}
}

func TestVerifyDetectsStaleReferencesAndChangedContent(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.go": cat21})
	app := ApplicationInMemory()
	app.Index.IndexRepository("p", dir, true, nil, false)
	const id = "func:a.go:parse_cat21"
	res := app.Refs.Resolve("p", id)
	hash := res.CurrentHash
	if v := app.Refs.Verify("p", VerifyRequest{ID: id, ExpectedContentHash: hash}); v.Status != RefValid {
		t.Fatalf("unchanged code must verify: %+v", v)
	}
	if v := app.Refs.Verify("p", VerifyRequest{ID: id, ExpectedContentHash: "deadbeef"}); v.Status != RefStale ||
		!strings.Contains(v.Reason, "since you read it") {
		t.Fatalf("a hash the caller never saw must be stale: %+v", v)
	}

	// Change the function on disk without re-indexing.
	writeTree(t, dir, map[string]string{"a.go": strings.Replace(cat21, "return decode_item()", "return decode_item() + 1", 1)})
	v := app.Refs.Verify("p", VerifyRequest{ID: id})
	if v.Status != RefStale || v.CurrentHash == v.IndexedHash {
		t.Fatalf("edited span must be REFERENCE_STALE: %+v", v)
	}
	if v := app.Refs.Verify("p", VerifyRequest{ID: id, ExpectedContentHash: hash}); v.Status != RefStale {
		t.Fatalf("caller's old hash no longer matches disk: %+v", v)
	}
	// A caller that read the new text is current even though the index is behind.
	if v := app.Refs.Verify("p", VerifyRequest{ID: id, ExpectedContentHash: v.CurrentHash}); v.Status != RefValid || !strings.Contains(v.Reason, "re-index") {
		t.Fatalf("view is current, index is behind: %+v", v)
	}
	// Whole-file staleness for file references.
	if f := app.Refs.Resolve("p", "file:a.go"); f.Status != RefStale {
		t.Fatalf("changed file must be stale: %+v", f)
	}
	// Re-indexing brings everything back to VALID.
	app.Index.IndexRepository("p", dir, true, nil, false)
	if v := app.Refs.Verify("p", VerifyRequest{ID: id}); v.Status != RefValid {
		t.Fatalf("after re-index: %+v", v)
	}
}

func TestVerifyRevisionMovedButFileUnchangedIsValid(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.go": cat21, "b.go": "package x\nfunc B() {}\n"})
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "one")
	first, _ := git(dir, "rev-parse", "HEAD")
	writeTree(t, dir, map[string]string{"b.go": "package x\nfunc B() { _ = 1 }\n"})
	gitRun(t, dir, "commit", "-am", "two")

	app := ApplicationInMemory()
	app.Index.IndexRepository("p", dir, true, nil, false)
	v := app.Refs.Verify("p", VerifyRequest{ID: "func:a.go:parse_cat21", ExpectedRevision: first})
	if v.Status != RefValid || !strings.Contains(v.Reason, "revision moved") {
		t.Fatalf("a.go did not change between revisions: %+v", v)
	}
	v = app.Refs.Verify("p", VerifyRequest{ID: "func:b.go:B", ExpectedRevision: first})
	if v.Status != RefStale {
		t.Fatalf("b.go changed between revisions: %+v", v)
	}
	if v := app.Refs.Verify("p", VerifyRequest{ID: "func:a.go:parse_cat21", ExpectedRevision: "0000000000000000000000000000000000000000"}); v.Status != RefStale {
		t.Fatalf("an unknown revision must fail closed: %+v", v)
	}
}

func TestHallucinatedReferencesAreRejectedWithCorrections(t *testing.T) {
	app, _ := indexTree(t, map[string]string{"src/cat21.go": cat21})
	res := app.Refs.Resolve("p", "func:src/cat21.go:parse_cat2l")
	if res.Status != RefInvalid || len(res.Candidates) == 0 || res.Candidates[0] != "func:src/cat21.go:parse_cat21" {
		t.Fatalf("typo must be rejected with the real ID: %+v", res)
	}
	if r := app.Refs.Resolve("p", "not-an-id"); r.Status != RefInvalid {
		t.Fatalf("%+v", r)
	}
	if r := app.Refs.Resolve("p", "func:src/nothing.go:zzz"); r.Status != RefInvalid || len(r.Candidates) != 0 {
		t.Fatalf("no plausible candidate should mean no suggestions: %+v", r)
	}
	if r := app.Refs.Resolve("other", "func:src/cat21.go:parse_cat21"); r.Status != RefInvalid {
		t.Fatalf("IDs are project-scoped: %+v", r)
	}
	matches, sug := app.Refs.ResolveSymbol("p", "parse_cat2l")
	if len(matches) != 0 || len(sug) == 0 || sug[0] != "parse_cat21" {
		t.Fatalf("ResolveSymbol: %v %v", matches, sug)
	}
	if m, _ := app.Refs.ResolveSymbol("p", "parse_cat21"); len(m) != 1 || m[0].ID != "func:src/cat21.go:parse_cat21" {
		t.Fatalf("exact symbol lookup: %v", m)
	}
}

func TestResolveSourceSpanNamesEnclosingSymbol(t *testing.T) {
	app, _ := indexTree(t, map[string]string{"src/cat21.go": cat21})
	sp, sym, err := app.Refs.ResolveSourceSpan("p", "src/cat21.go", 5, 5)
	if err != nil || sym != "func:src/cat21.go:decode_item" || sp.ContentHash == "" || sp.StartLine != 5 {
		t.Fatalf("span %+v sym %q err %v", sp, sym, err)
	}
	if _, _, err := app.Refs.ResolveSourceSpan("p", "src/cat21.go", 999, 1000); err == nil || !strings.Contains(err.Error(), RefInvalid) {
		t.Fatalf("out-of-range lines must be INVALID_REFERENCE: %v", err)
	}
	if _, _, err := app.Refs.ResolveSourceSpan("p", "src/missing.go", 1, 2); err == nil {
		t.Fatal("missing file must error")
	}
}

func TestBinaryFunctionsGetStableIDsAndSpans(t *testing.T) {
	app := ApplicationInMemory()
	size := 64
	_, err := app.Reverse.ImportBinary("p", reverse.NormalizedBinary{BinaryID: "fw", Path: "/x/fw.bin", Sha256: "abc123", Tool: "ghidra",
		Functions: []reverse.NormalizedFunction{{Address: "0x401230", Name: "FUN_401230", Size: &size}}})
	if err != nil {
		t.Fatal(err)
	}
	res := app.Refs.Resolve("p", ids.BinFuncID("fw", "0x401230"))
	if res.Status != RefValid || res.Binary == nil || res.Binary.StartAddress != "0x401230" ||
		res.Binary.EndAddress != "0x401270" || res.Binary.BinaryHash != "abc123" || res.Binary.Size != 64 {
		t.Fatalf("binary resolution: %+v %+v", res, res.Binary)
	}
	if r := app.Refs.Resolve("p", "insn:fw:0x401230"); r.Status != RefInvalid {
		t.Fatalf("instruction-level data was not imported: %+v", r)
	}
}

func TestBasicBlockAndInstructionImport(t *testing.T) {
	app := ApplicationInMemory()
	size := 64
	_, err := app.Reverse.ImportBinary("p", reverse.NormalizedBinary{BinaryID: "fw", Path: "/x/fw.bin", Sha256: "abc123", Tool: "ghidra",
		Functions: []reverse.NormalizedFunction{{
			Address: "0x401230", Name: "FUN_401230", Size: &size,
			BasicBlocks: []reverse.NormalizedBasicBlock{{
				Address: "0x401230",
				Instruction: []reverse.NormalizedInstruction{
					{Address: "0x401230", Mnemonic: "push", Operands: "rbp"},
					{Address: "0x401231", Mnemonic: "mov", Operands: "rsp,rbp"},
				},
			}},
		}}})
	if err != nil {
		t.Fatal(err)
	}
	// BB resolves.
	bb := app.Refs.Resolve("p", ids.BasicBlockID("fw", "0x401230", "0x401230"))
	if bb.Status != RefValid {
		t.Fatalf("basic block resolution: %+v", bb)
	}
	// Instruction resolves.
	insn := app.Refs.Resolve("p", ids.InstructionID("fw", "0x401230", "0x401230", "0x401230"))
	if insn.Status != RefValid {
		t.Fatalf("instruction resolution: %+v", insn)
	}
	// Instruction with no data still invalid.
	noop := app.Refs.Resolve("p", ids.InstructionID("fw", "0x401230", "0x401230", "0x401299"))
	if noop.Status != RefInvalid {
		t.Fatalf("missing instruction should be invalid: %+v", noop)
	}
	// Verify the instruction node has properties via graph.
	n, err := app.Graph.GetNode(insn.NodeID)
	if err != nil || n == nil {
		t.Fatalf("instruction node not in graph: %v", err)
	}
	if mn, ok := n.Properties["mnemonic"].(string); !ok || mn != "push" {
		t.Fatalf("instruction mnemonic: %v", n.Properties["mnemonic"])
	}
}

func TestComputeSlice(t *testing.T) {
	i := func(v int) *int { return &v }
	r := ComputeSlice(SliceRequest{Base: "buffer", Offset: 37, Length: i(15), ElementSize: 1, BaseLength: i(60)})
	if !r.Valid || r.End != 52 || r.LastIndex != 51 || r.ByteOffset != 37 || r.ByteEnd != 52 ||
		!strings.Contains(r.Notation, "buffer[37:52]") {
		t.Fatalf("basic: %+v", r)
	}
	r = ComputeSlice(SliceRequest{Base: "w", Offset: 10, End: i(14), ElementSize: 4})
	if r.Length != 4 || r.ByteOffset != 40 || r.ByteLength != 16 || r.ByteEnd != 56 {
		t.Fatalf("element size: %+v", r)
	}
	r = ComputeSlice(SliceRequest{Base: "buffer", Offset: 37, Length: i(15), BaseLength: i(40)})
	if r.Valid || !strings.Contains(strings.Join(r.Problems, ";"), "12 past the end") {
		t.Fatalf("overrun: %+v", r)
	}
	r = ComputeSlice(SliceRequest{Base: "b", Offset: 0, Length: i(11), BaseLength: i(10)})
	if !strings.Contains(strings.Join(r.Problems, ";"), "off by one") {
		t.Fatalf("classic off-by-one must be called out: %+v", r)
	}
	if r = ComputeSlice(SliceRequest{Base: "b", Offset: 5, Length: i(3), End: i(9)}); r.Valid {
		t.Fatalf("length/end disagreement: %+v", r)
	}
	if r = ComputeSlice(SliceRequest{Base: "b", Offset: 5, End: i(3)}); r.Valid || r.Length != -2 {
		t.Fatalf("end before offset: %+v", r)
	}
	if r = ComputeSlice(SliceRequest{Base: "b", Offset: 5}); r.Valid {
		t.Fatalf("length or end is required: %+v", r)
	}
	// Exactly to the end is fine.
	if r = ComputeSlice(SliceRequest{Base: "b", Offset: 5, Length: i(5), BaseLength: i(10)}); !r.Valid {
		t.Fatalf("window ending at base length is valid: %+v", r)
	}
}

func TestEditsAboveASymbolShiftItWithoutMakingItStale(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.go": cat21})
	app := ApplicationInMemory()
	app.Index.IndexRepository("p", dir, true, nil, false)
	orig := app.Refs.Resolve("p", "func:a.go:decode_item")

	// Insert lines above decode_item and change parse_cat21; decode_item itself is untouched.
	edited := strings.Replace(cat21, "func parse_cat21() int { return decode_item() }",
		"// added comment\n// another\nfunc parse_cat21() int { return decode_item() + 1 }", 1)
	writeTree(t, dir, map[string]string{"a.go": edited})

	moved := app.Refs.Resolve("p", "func:a.go:decode_item")
	if moved.Status != RefValid || !strings.Contains(moved.Reason, "moved") {
		t.Fatalf("an unchanged symbol that only shifted must stay VALID: %+v", moved)
	}
	if moved.Source.StartLine != orig.Source.StartLine+2 || moved.Source.StartByte == orig.Source.StartByte {
		t.Fatalf("resolution must report the current location: %d -> %d", orig.Source.StartLine, moved.Source.StartLine)
	}
	if moved.Source.ContentHash != orig.Source.ContentHash {
		t.Fatal("content hash of an unchanged symbol must be stable across moves")
	}
	if v := app.Refs.Verify("p", VerifyRequest{ID: "func:a.go:decode_item", ExpectedContentHash: orig.Source.ContentHash}); v.Status != RefValid {
		t.Fatalf("verify by the hash the caller saw: %+v", v)
	}
	changed := app.Refs.Resolve("p", "func:a.go:parse_cat21")
	if changed.Status != RefStale || changed.CurrentHash == changed.IndexedHash {
		t.Fatalf("the edited symbol is stale: %+v", changed)
	}
	// Deleting the symbol is stale, not a silent success.
	writeTree(t, dir, map[string]string{"a.go": "package x\n"})
	if r := app.Refs.Resolve("p", "func:a.go:decode_item"); r.Status != RefStale || !strings.Contains(r.Reason, "no longer present") {
		t.Fatalf("deleted symbol: %+v", r)
	}
}
