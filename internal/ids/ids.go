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
)

var known = map[string]bool{
	Func: true, Class: true, Struct: true, File: true, BinFunc: true, Binary: true,
	BB: true, Insn: true, Var: true, Slice: true, Span: true, BinSpan: true, Task: true,
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
