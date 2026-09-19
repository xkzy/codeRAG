package services

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// extractImportsTreeSitter returns the import targets of a file as written in
// source: Go import paths, Python dotted modules, JS/TS module specifiers, Java
// qualified names, C/C++ include paths and Rust `mod` declarations. ok is false
// when the language is unsupported.
func extractImportsTreeSitter(content, suffix string) (targets []string, ok bool) {
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

	add := func(s string) {
		s = strings.Trim(strings.TrimSpace(s), "\"'`<>")
		if s != "" {
			targets = append(targets, s)
		}
	}
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		switch n.Type() {
		case "import_spec": // Go
			if p := n.ChildByFieldName("path"); p != nil {
				add(p.Content(src))
			}
		case "import_statement": // Python, JS, TS
			if s := n.ChildByFieldName("source"); s != nil { // JS/TS
				add(s.Content(src))
			} else { // Python: import a.b, c as d
				for i := 0; i < int(n.NamedChildCount()); i++ {
					c := n.NamedChild(i)
					if c.Type() == "aliased_import" {
						c = c.ChildByFieldName("name")
					}
					if c != nil && c.Type() == "dotted_name" {
						add(c.Content(src))
					}
				}
			}
		case "import_from_statement": // Python: from a.b import c, from . import x
			if m := n.ChildByFieldName("module_name"); m != nil {
				mod := m.Content(src)
				add(mod)
				// `from pkg import sub` may import a submodule: offer pkg.sub too.
				for i := 0; i < int(n.ChildCount()); i++ {
					if n.FieldNameForChild(i) != "name" {
						continue
					}
					c := n.Child(i)
					if c.Type() == "aliased_import" {
						c = c.ChildByFieldName("name")
					}
					if c == nil {
						continue
					}
					if strings.HasSuffix(mod, ".") {
						add(mod + c.Content(src))
					} else {
						add(mod + "." + c.Content(src))
					}
				}
			}
		case "export_statement": // JS/TS: export ... from './x'
			if s := n.ChildByFieldName("source"); s != nil {
				add(s.Content(src))
			}
		case "import_declaration": // Java
			if suffix == ".java" {
				text := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(
					strings.TrimSpace(n.Content(src)), "import"), " static")), ";")
				add(text)
			}
		case "preproc_include": // C, C++
			if p := n.ChildByFieldName("path"); p != nil {
				add(p.Content(src))
			}
		case "mod_item": // Rust: `mod name;` pulls in name.rs or name/mod.rs
			if n.ChildByFieldName("body") == nil {
				if nm := n.ChildByFieldName("name"); nm != nil {
					add(nm.Content(src))
				}
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(tree.RootNode())
	return targets, true
}

// extractImports prefers tree-sitter and falls back to the regex extractor.
func extractImports(content, suffix string) []string {
	if t, ok := extractImportsTreeSitter(content, suffix); ok {
		return t
	}
	if sp := specFor(suffix); sp != nil {
		return specImports(sp, content)
	}
	var out []string
	for _, match := range importRe.FindAllStringSubmatch(content, -1) {
		for _, g := range match[1:] {
			if g != "" {
				out = append(out, g)
				break
			}
		}
	}
	return out
}
