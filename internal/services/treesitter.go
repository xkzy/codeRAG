package services

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// typeMatch is a class-like or struct-like declaration found in source.
type typeMatch struct {
	name     string
	kind     string // graph node kind: "Class" or "Struct"
	typeKind string // source-level keyword: class, struct, interface, enum, trait, ...
	start    int
	end      int
	refs     []string // types mentioned by fields and members, excluding bases
	span     span
}

func languageFor(suffix string) *sitter.Language {
	switch strings.ToLower(suffix) {
	case ".py":
		return python.GetLanguage()
	case ".go":
		return golang.GetLanguage()
	case ".rs":
		return rust.GetLanguage()
	case ".c", ".h":
		return c.GetLanguage()
	case ".cc", ".cpp", ".hpp", ".cxx", ".hxx", ".hh":
		return cpp.GetLanguage()
	case ".java":
		return java.GetLanguage()
	case ".js", ".jsx", ".mjs", ".cjs":
		return javascript.GetLanguage()
	case ".ts":
		return typescript.GetLanguage()
	case ".tsx":
		return tsx.GetLanguage()
	}
	return nil
}

// treeSitterNodeTypes maps a grammar node type to the graph kind and keyword it declares.
var treeSitterNodeTypes = map[string][2]string{
	"class_definition":           {"Class", "class"},
	"class_declaration":          {"Class", "class"},
	"abstract_class_declaration": {"Class", "class"},
	"class_specifier":            {"Class", "class"},
	"interface_declaration":      {"Class", "interface"},
	"enum_declaration":           {"Class", "enum"},
	"enum_item":                  {"Class", "enum"},
	"enum_specifier":             {"Class", "enum"},
	"trait_item":                 {"Class", "trait"},
	"struct_item":                {"Struct", "struct"},
	"struct_specifier":           {"Struct", "struct"},
	"type_spec":                  {}, // Go: resolved by inspecting the type body
}

// extractTypesTreeSitter parses content with tree-sitter and returns declared
// classes and structs. ok is false when the language is unsupported or parsing
// failed, so callers can fall back to regex extraction.
func extractTypesTreeSitter(content, suffix string) (matches []typeMatch, ok bool) {
	lang := languageFor(suffix)
	if lang == nil {
		return nil, false
	}
	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(lang)
	src := []byte(content)
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil || tree == nil {
		return nil, false
	}
	defer tree.Close()

	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if m, found := typeFromNode(n, src); found {
			m.refs = uniqueStrings(typeFieldRefs(n, src), m.name)
			matches = append(matches, m)
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(tree.RootNode())
	return matches, true
}

func typeFromNode(n *sitter.Node, src []byte) (typeMatch, bool) {
	spec, known := treeSitterNodeTypes[n.Type()]
	if !known {
		return typeMatch{}, false
	}
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return typeMatch{}, false
	}
	m := typeMatch{
		name:  nameNode.Content(src),
		start: int(n.StartPoint().Row) + 1,
		end:   int(n.EndPoint().Row) + 1,
		span:  spanOf(n),
	}
	switch n.Type() {
	case "type_spec": // Go
		body := n.ChildByFieldName("type")
		if body == nil {
			return typeMatch{}, false
		}
		switch body.Type() {
		case "struct_type":
			m.kind, m.typeKind = "Struct", "struct"
		case "interface_type":
			m.kind, m.typeKind = "Class", "interface"
		default:
			return typeMatch{}, false
		}
	case "struct_specifier", "class_specifier", "enum_specifier":
		// Skip forward declarations and type references such as `struct foo *p`.
		if n.ChildByFieldName("body") == nil {
			return typeMatch{}, false
		}
		m.kind, m.typeKind = spec[0], spec[1]
	default:
		if spec[0] == "" {
			return typeMatch{}, false
		}
		m.kind, m.typeKind = spec[0], spec[1]
	}
	return m, true
}

// extractTypes prefers tree-sitter and falls back to regex when unavailable.
func extractTypes(content, suffix string) []typeMatch {
	if matches, ok := extractTypesTreeSitter(content, suffix); ok {
		return matches
	}
	if sp := specFor(suffix); sp != nil {
		return specTypes(sp, content)
	}
	var out []typeMatch
	for _, idx := range typeRe.FindAllStringSubmatchIndex(content, -1) {
		keyword := strings.Fields(content[idx[0]:idx[2]])[0]
		kind := "Class"
		if keyword == "struct" {
			kind = "Struct"
		}
		line := strings.Count(content[:idx[0]], "\n") + 1
		out = append(out, typeMatch{name: content[idx[2]:idx[3]], kind: kind, typeKind: keyword, start: line, end: line,
			span: lineSpan(content, line, line)})
	}
	return out
}

// skipInTypeRefs are subtrees whose type names are inheritance, generics or
// method bodies, which other edges (EXTENDS, IMPLEMENTS, CALLS, USES from the
// method) already describe.
var skipInTypeRefs = map[string]bool{
	"superclass": true, "super_interfaces": true, "extends_interfaces": true, "extends_type_clause": true,
	"implements_clause": true, "class_heritage": true, "base_class_clause": true, "trait_bounds": true,
	"type_parameters": true, "type_parameter_list": true,
	"function_definition": true, "function_declaration": true, "method_declaration": true,
	"method_definition": true, "function_item": true, "constructor_declaration": true,
	"generator_function_declaration": true, "arrow_function": true, "function_expression": true,
}

// typeFieldRefs collects the type names a class or struct mentions in its
// fields and members, so a struct holding a *Service is recorded as using it.
func typeFieldRefs(n *sitter.Node, src []byte) []string {
	nameNode := n.ChildByFieldName("name")
	supers := n.ChildByFieldName("superclasses") // Python base list
	var refs []string
	var walk func(c *sitter.Node)
	walk = func(c *sitter.Node) {
		t := c.Type()
		if skipInTypeRefs[t] {
			return
		}
		if nameNode != nil && c.StartByte() == nameNode.StartByte() && c.EndByte() == nameNode.EndByte() {
			return
		}
		if supers != nil && c.StartByte() == supers.StartByte() && c.EndByte() == supers.EndByte() {
			return
		}
		// A nested type is extracted on its own.
		if c != n && ownerNodeTypes[t] && c.ChildByFieldName("body") != nil {
			return // (a body-less `struct foo *p` is a reference, not a nested definition)
		}
		// Go embedded fields are inheritance-like and already become EXTENDS edges.
		if t == "field_declaration" && c.ChildByFieldName("name") == nil && c.ChildByFieldName("declarator") == nil {
			return
		}
		if name := typeNameOf(c, src); name != "" {
			refs = append(refs, name)
		}
		for i := 0; i < int(c.ChildCount()); i++ {
			walk(c.Child(i))
		}
	}
	walk(n)
	return refs
}

// typeNameOf returns the type a node names in a type position, ignoring
// builtins (int, string, list...) which never resolve to project types.
func typeNameOf(c *sitter.Node, src []byte) string {
	name := ""
	switch c.Type() {
	case "type_identifier":
		name = c.Content(src)
	case "identifier": // Python annotations: `x: Foo`, `x: list[Foo]`
		if p := c.Parent(); p != nil {
			if p.Type() == "type" {
				name = c.Content(src)
			} else if p.Type() == "generic_type" {
				if gp := p.Parent(); gp != nil && gp.Type() == "type" {
					name = c.Content(src)
				}
			}
		}
	}
	if builtinTypes[name] {
		return ""
	}
	return name
}

var builtinTypes = func() map[string]bool {
	m := map[string]bool{}
	for _, n := range strings.Fields(`int int8 int16 int32 int64 uint uint8 uint16 uint32 uint64 uintptr float32 float64
		complex64 complex128 string bool byte rune error any comparable
		i8 i16 i32 i64 i128 isize u8 u16 u32 u64 u128 usize f32 f64 str char
		void long short double float boolean size_t ssize_t uint8_t uint16_t uint32_t uint64_t int8_t int16_t int32_t int64_t
		list dict set tuple frozenset bytes object type None number undefined never unknown`) {
		m[n] = true
	}
	return m
}()
