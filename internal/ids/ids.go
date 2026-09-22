// Package ids defines stable, human-readable identifiers for graph entities.
//
// Stable IDs are built from repo-relative paths and symbol names, never from line
// numbers or retrieval order, so they survive re-indexing, moving a checkout and
// context-window changes. Locations (line, column, byte, address) are metadata.
package ids

import (
	"path/filepath"
	"strings"
)

// Kind prefixes. The prefix is the entity type, so an ID is self-describing.
const (
	Func    = "func"
	Class   = "class"
	Struct  = "struct"
	File    = "file"
	BinFunc = "binfunc"
	Binary  = "binary"
	BB      = "bb"
	Insn    = "insn"
	Var     = "var"
	Slice   = "slice"
	Span    = "span"
	BinSpan = "binspan"
	Task    = "task"
	Observation = "obs"

	BinModule         = "binmodule"
	BinSection        = "binsection"
	BinGlobal         = "binglobal"
	BinImport         = "binimport"
	BinExport         = "binexport"
	BinSymbol         = "binsymbol"
	BinType           = "bintype"
	BinRegister       = "binregister"
	BinMemLoc         = "binmemloc"
	BinConstant       = "binconst"
	BinCallSite       = "bincallsite"
	BinJump           = "binjump"
	BinDataFlow       = "bindataflow"
	BinControlFlow    = "bincontrolflow"
	BinExternalAPI    = "binextapi"
	BinArtifact       = "binartifact"
	BinHypothesis     = "binhypothesis"
	BinPortingMap     = "binportmap"
	BinVerification   = "binverify"
	BinContextPackage = "binctxpkg"
)

var known = map[string]bool{
	Func: true, Class: true, Struct: true, File: true, BinFunc: true, Binary: true,
	BB: true, Insn: true, Var: true, Slice: true, Span: true, BinSpan: true, Task: true,
	Observation: true,
	BinModule: true, BinSection: true, BinGlobal: true, BinImport: true, BinExport: true,
	BinSymbol: true, BinType: true, BinRegister: true, BinMemLoc: true, BinConstant: true,
	BinCallSite: true, BinJump: true, BinDataFlow: true, BinControlFlow: true,
	BinExternalAPI: true, BinArtifact: true, BinHypothesis: true, BinPortingMap: true,
	BinVerification: true, BinContextPackage: true,
}

// Rel returns p relative to root using forward slashes, or p unchanged when it
// is not under root.
func Rel(root, p string) string {
	if root == "" {
		return filepath.ToSlash(p)
	}
	rel, err := filepath.Rel(root, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(rel)
}

// Symbol builds func:/class:/struct: IDs: kind:relpath:Label (Label is Owner.name
// for methods).
func Symbol(kind, rel, label string) string { return kind + ":" + rel + ":" + label }

func FileID(rel string) string        { return File + ":" + rel }
func BinaryID(binaryID string) string { return Binary + ":" + binaryID }
func BinFuncID(binaryID, addr string) string {
	return BinFunc + ":" + binaryID + ":" + addr
}
func BasicBlockID(binaryID, fnAddr, bbAddr string) string {
	return BB + ":" + binaryID + ":" + fnAddr + ":" + bbAddr
}
func InstructionID(binaryID, fnAddr, bbAddr, insnAddr string) string {
	return Insn + ":" + binaryID + ":" + fnAddr + ":" + bbAddr + ":" + insnAddr
}

func BinModuleID(binaryID, name string) string {
	return BinModule + ":" + binaryID + ":" + name
}

func BinSectionID(binaryID, name string) string {
	return BinSection + ":" + binaryID + ":" + name
}

func BinGlobalID(binaryID, addr string) string {
	return BinGlobal + ":" + binaryID + ":" + addr
}

func BinImportID(binaryID, name string) string {
	return BinImport + ":" + binaryID + ":" + name
}

func BinExportID(binaryID, name string) string {
	return BinExport + ":" + binaryID + ":" + name
}

func BinSymbolID(binaryID, addr string) string {
	return BinSymbol + ":" + binaryID + ":" + addr
}

func BinTypeID(binaryID, name string) string {
	return BinType + ":" + binaryID + ":" + name
}

func BinRegisterID(binaryID, name string) string {
	return BinRegister + ":" + binaryID + ":" + name
}

func BinMemLocID(binaryID, addr string) string {
	return BinMemLoc + ":" + binaryID + ":" + addr
}

func BinConstantID(binaryID, value string) string {
	return BinConstant + ":" + binaryID + ":" + value
}

func BinCallSiteID(binaryID, fnAddr, insnAddr string) string {
	return BinCallSite + ":" + binaryID + ":" + fnAddr + ":" + insnAddr
}

func BinJumpID(binaryID, fromAddr, toAddr string) string {
	return BinJump + ":" + binaryID + ":" + fromAddr + ":" + toAddr
}

func BinDataFlowID(binaryID, fnAddr, name string) string {
	return BinDataFlow + ":" + binaryID + ":" + fnAddr + ":" + name
}

func BinControlFlowID(binaryID, fnAddr string) string {
	return BinControlFlow + ":" + binaryID + ":" + fnAddr
}

func BinExternalAPIID(binaryID, name string) string {
	return BinExternalAPI + ":" + binaryID + ":" + name
}

func BinArtifactID(binaryID, artifactType, name string) string {
	return BinArtifact + ":" + binaryID + ":" + artifactType + ":" + name
}

func BinHypothesisID(binaryID, subjectAddr, claimHash string) string {
	return BinHypothesis + ":" + binaryID + ":" + subjectAddr + ":" + claimHash
}

func BinPortingMapID(binaryID, binaryFnAddr, targetLang string) string {
	return BinPortingMap + ":" + binaryID + ":" + binaryFnAddr + ":" + targetLang
}

func BinVerificationID(binaryID, binaryFnAddr, testName string) string {
	return BinVerification + ":" + binaryID + ":" + binaryFnAddr + ":" + testName
}

func BinContextPackageID(binaryID, queryHash string) string {
	return BinContextPackage + ":" + binaryID + ":" + queryHash
}

// Parse splits "kind:rest". ok is false when the prefix is not a known kind.
func Parse(id string) (kind, rest string, ok bool) {
	i := strings.IndexByte(id, ':')
	if i <= 0 {
		return "", id, false
	}
	kind = id[:i]
	if !known[kind] {
		return "", id, false
	}
	return kind, id[i+1:], true
}

// Looks reports whether s has the shape of a stable ID (known prefix).
func Looks(s string) bool { _, _, ok := Parse(s); return ok }

// SplitSymbol splits the rest of a func:/class:/struct: ID into path and label.
// Paths may contain colons only on Windows drive letters, which Rel avoids.
func SplitSymbol(rest string) (rel, label string) {
	i := strings.LastIndexByte(rest, ':')
	if i < 0 {
		return rest, ""
	}
	return rest[:i], rest[i+1:]
}
