package services

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

const (
	relExtends    = "EXTENDS"
	relImplements = "IMPLEMENTS"
)

// typeRelation says that type sub extends or implements the type named super.
type typeRelation struct {
	sub, rel, super string
}

// baseName reduces a type expression to the simple name it refers to
// (`pkg.Base`, `Base<T>`, `*Base`, `Base[T]` all become `Base`).
func baseName(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "identifier", "type_identifier", "property_identifier", "field_identifier", "namespace_identifier":
		return n.Content(src)
	case "scoped_type_identifier", "scoped_identifier", "nested_type_identifier", "qualified_identifier",
		"qualified_type", "member_expression", "attribute", "nested_identifier":
		if nm := n.ChildByFieldName("name"); nm != nil {
			return baseName(nm, src)
		}
		if c := n.NamedChildCount(); c > 0 {
			return baseName(n.NamedChild(int(c)-1), src)
		}
	case "generic_type", "template_type", "subscript", "generic_name":
		if c := n.NamedChildCount(); c > 0 {
			return baseName(n.NamedChild(0), src)
		}
	case "pointer_type", "reference_type", "type_parameter_list":
		if c := n.NamedChildCount(); c > 0 {
			return baseName(n.NamedChild(int(c)-1), src)
		}
	}
	return ""
}

// collectBases gathers the base names referenced anywhere below n. Nodes that
// resolve to a name are not descended into, so `Outer<Inner>` yields `Outer`.
func collectBases(n *sitter.Node, src []byte) []string {
	if n == nil {
		return nil
	}
	if name := baseName(n, src); name != "" {
		return []string{name}
	}
	var out []string
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if c.Type() == "type_arguments" || c.Type() == "type_parameters" {
			continue
		}
		out = append(out, collectBases(c, src)...)
	}
	return out
}

func relationsFromNode(n *sitter.Node, src []byte) []typeRelation {
	name := ""
	if nm := n.ChildByFieldName("name"); nm != nil {
		name = nm.Content(src)
	}
	add := func(sub, rel string, supers ...string) []typeRelation {
		var out []typeRelation
		for _, s := range supers {
			if sub != "" && s != "" && s != sub {
				out = append(out, typeRelation{sub, rel, s})
			}
		}
		return out
	}

	switch n.Type() {
	case "class_definition": // Python
		var supers []string
		if sc := n.ChildByFieldName("superclasses"); sc != nil {
			for i := 0; i < int(sc.NamedChildCount()); i++ {
				c := sc.NamedChild(i)
				if c.Type() == "keyword_argument" { // metaclass=...
					continue
				}
				supers = append(supers, baseName(c, src))
			}
		}
		return add(name, relExtends, supers...)

	case "class_declaration", "abstract_class_declaration", "interface_declaration", "enum_declaration": // Java, TS, JS
		var out []typeRelation
		for i := 0; i < int(n.NamedChildCount()); i++ {
			c := n.NamedChild(i)
			switch c.Type() {
			case "superclass": // Java
				out = append(out, add(name, relExtends, collectBases(c, src)...)...)
			case "super_interfaces", "implements_clause": // Java, TS
				out = append(out, add(name, relImplements, collectBases(c, src)...)...)
			case "extends_interfaces", "extends_type_clause": // Java / TS interface extends
				out = append(out, add(name, relExtends, collectBases(c, src)...)...)
			case "class_heritage": // JS, TS
				for j := 0; j < int(c.NamedChildCount()); j++ {
					h := c.NamedChild(j)
					switch h.Type() {
					case "implements_clause":
						out = append(out, add(name, relImplements, collectBases(h, src)...)...)
					case "extends_clause":
						out = append(out, add(name, relExtends, extendsClauseBases(h, src)...)...)
					default: // JS: `extends Base` puts the expression directly in the heritage
						out = append(out, add(name, relExtends, baseName(h, src))...)
					}
				}
			}
		}
		return out

	case "class_specifier", "struct_specifier": // C++
		var out []typeRelation
		for i := 0; i < int(n.NamedChildCount()); i++ {
			if c := n.NamedChild(i); c.Type() == "base_class_clause" {
				out = append(out, add(name, relExtends, collectBases(c, src)...)...)
			}
		}
		return out

	case "impl_item": // Rust: impl Trait for Type
		tr := n.ChildByFieldName("trait")
		if tr == nil {
			return nil
		}
		return add(baseName(n.ChildByFieldName("type"), src), relImplements, baseName(tr, src))

	case "trait_item": // Rust: trait A: B + C
		return add(name, relExtends, collectBases(n.ChildByFieldName("bounds"), src)...)

	case "type_spec": // Go: embedded structs and interfaces
		body := n.ChildByFieldName("type")
		if body == nil {
			return nil
		}
		var out []typeRelation
		switch body.Type() {
		case "struct_type":
			for i := 0; i < int(body.NamedChildCount()); i++ {
				list := body.NamedChild(i) // field_declaration_list
				for j := 0; j < int(list.NamedChildCount()); j++ {
					f := list.NamedChild(j)
					if f.Type() != "field_declaration" || f.ChildByFieldName("name") != nil {
						continue
					}
					out = append(out, add(name, relExtends, baseName(f.ChildByFieldName("type"), src))...)
				}
			}
		case "interface_type":
			for i := 0; i < int(body.NamedChildCount()); i++ {
				c := body.NamedChild(i)
				if strings.HasPrefix(c.Type(), "method") {
					continue
				}
				out = append(out, add(name, relExtends, collectBases(c, src)...)...)
			}
		}
		return out
	}
	return nil
}

func extendsClauseBases(clause *sitter.Node, src []byte) []string {
	var out []string
	for i := 0; i < int(clause.NamedChildCount()); i++ {
		c := clause.NamedChild(i)
		if c.Type() == "type_arguments" {
			continue
		}
		if b := baseName(c, src); b != "" {
			out = append(out, b)
		}
	}
	return out
}

// extractRelationsTreeSitter returns inheritance, interface-implementation and
// embedding relations. ok is false when the language is unsupported.
func extractRelationsTreeSitter(content, suffix string) (rels []typeRelation, ok bool) {
	lang := languageFor(suffix)
	if lang == nil {
		return nil, false
	}
	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(lang)
	src := []byte(content)
	tree, err := parseWithTimeout(parser, src)
	if err != nil || tree == nil {
		return nil, false
	}
	defer tree.Close()

	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		rels = append(rels, relationsFromNode(n, src)...)
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(tree.RootNode())
	return rels, true
}
