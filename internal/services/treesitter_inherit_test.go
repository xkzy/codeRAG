package services

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"codergag/internal/graph"
)

func relSet(src, suffix string) []string {
	rels, _ := extractRelationsTreeSitter(src, suffix)
	var out []string
	for _, r := range rels {
		out = append(out, encodeRelation(r))
	}
	sort.Strings(out)
	return out
}

func TestExtractRelationsTreeSitter(t *testing.T) {
	cases := []struct {
		name, suffix, src string
		want              []string
	}{
		{"python", ".py", "class A(Base, mixins.Mix, metaclass=Meta):\n    pass\nclass G(Generic[T]): pass\nclass Plain: pass\n",
			[]string{"A|EXTENDS|Base", "A|EXTENDS|Mix", "G|EXTENDS|Generic"}},
		{"java", ".java", "class C extends B implements I, J<String> {}\ninterface K extends I, L {}\nenum E implements I { X }\n",
			[]string{"C|EXTENDS|B", "C|IMPLEMENTS|I", "C|IMPLEMENTS|J", "E|IMPLEMENTS|I", "K|EXTENDS|I", "K|EXTENDS|L"}},
		{"ts", ".ts", "class C extends B<T> implements I, J {}\ninterface K extends I {}\nabstract class D extends B {}\n",
			[]string{"C|EXTENDS|B", "C|IMPLEMENTS|I", "C|IMPLEMENTS|J", "D|EXTENDS|B", "K|EXTENDS|I"}},
		{"js", ".js", "class C extends B {}\nclass D extends ns.Base {}\nclass E {}\n",
			[]string{"C|EXTENDS|B", "D|EXTENDS|Base"}},
		{"cpp", ".cpp", "class D : public B, private ns::M {};\nstruct S : B<int> { int a; };\nclass Alone {};\n",
			[]string{"D|EXTENDS|B", "D|EXTENDS|M", "S|EXTENDS|B"}},
		{"rust", ".rs", "trait Sub: Base + Other {}\nimpl Shape for Circle {}\nimpl<T> fmt::Display for Wrapper<T> {}\nimpl Inherent {}\n",
			[]string{"Circle|IMPLEMENTS|Shape", "Sub|EXTENDS|Base", "Sub|EXTENDS|Other", "Wrapper|IMPLEMENTS|Display"}},
		{"go", ".go", "package x\ntype A struct { B; *C; io.Reader; name string }\ntype I interface { J; K; M() }\n",
			[]string{"A|EXTENDS|B", "A|EXTENDS|C", "A|EXTENDS|Reader", "I|EXTENDS|J", "I|EXTENDS|K"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := relSet(c.src, c.suffix)
			if len(got) != len(c.want) {
				t.Fatalf("got %v\nwant %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("got %v\nwant %v", got, c.want)
				}
			}
		})
	}
}

func edgeTargets(app *Application, project, kind, from string) []string {
	nodes, _ := app.Graph.FindNodes("Class", map[string]any{"project_id": project, "name": from})
	more, _ := app.Graph.FindNodes("Struct", map[string]any{"project_id": project, "name": from})
	var out []string
	for _, n := range append(nodes, more...) {
		nbrs, _ := app.Graph.Neighbors(n.ID, kind, graph.DirOut)
		for _, en := range nbrs {
			out = append(out, en.Node.Properties["name"].(string))
		}
	}
	sort.Strings(out)
	return out
}

func TestInheritanceEdgesAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// a.py sorts before b.py, so the base class is indexed after its subclass.
	write("a.py", "class Child(Base, Missing):\n    pass\n")
	write("b.py", "class Base:\n    pass\n")
	write("s.rs", "struct Circle {}\ntrait Shape {}\nimpl Shape for Circle {}\n")
	app := ApplicationInMemory()
	if _, err := app.Index.IndexRepository("p", dir, true, nil); err != nil {
		t.Fatal(err)
	}
	if got := edgeTargets(app, "p", "EXTENDS", "Child"); len(got) != 1 || got[0] != "Base" {
		t.Fatalf("Child EXTENDS: %v (unresolved external base Missing must not create an edge)", got)
	}
	if got := edgeTargets(app, "p", "IMPLEMENTS", "Circle"); len(got) != 1 || got[0] != "Shape" {
		t.Fatalf("Circle IMPLEMENTS: %v", got)
	}

	// Re-indexing must not duplicate edges.
	write("b.py", "class Base:\n    x = 1\n")
	if _, err := app.Index.IndexRepository("p", dir, true, nil); err != nil {
		t.Fatal(err)
	}
	if got := edgeTargets(app, "p", "EXTENDS", "Child"); len(got) != 1 {
		t.Fatalf("edges after base file rewrite: %v", got)
	}
}
