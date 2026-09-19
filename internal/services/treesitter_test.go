package services

import "testing"

func TestExtractTypesTreeSitter(t *testing.T) {
	cases := []struct {
		name, suffix, src string
		want              map[string]string // name -> kind
	}{
		{"python", ".py", "class A:\n    class Inner: pass\n", map[string]string{"A": "Class", "Inner": "Class"}},
		{"go", ".go", "package x\ntype S struct{ a int }\ntype I interface{ M() }\ntype N int\n", map[string]string{"S": "Struct", "I": "Class"}},
		{"rust", ".rs", "struct P { x: i32 }\nenum E { A }\ntrait T {}\n", map[string]string{"P": "Struct", "E": "Class", "T": "Class"}},
		{"c", ".c", "struct node { int v; };\nstruct node *p;\nstruct fwd;\n", map[string]string{"node": "Struct"}},
		{"cpp", ".cpp", "class C {};\nstruct S { int a; };\n", map[string]string{"C": "Class", "S": "Struct"}},
		{"java", ".java", "public class J { interface K {} enum L { X } }\n", map[string]string{"J": "Class", "K": "Class", "L": "Class"}},
		{"js", ".js", "class B extends A {}\n", map[string]string{"B": "Class"}},
		{"ts", ".ts", "class C {}\ninterface I {}\nabstract class D {}\n", map[string]string{"C": "Class", "I": "Class", "D": "Class"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := map[string]string{}
			for _, m := range extractTypes(c.src, c.suffix) {
				got[m.name] = m.kind
			}
			if len(got) != len(c.want) {
				t.Fatalf("got %v want %v", got, c.want)
			}
			for n, k := range c.want {
				if got[n] != k {
					t.Fatalf("got %v want %v", got, c.want)
				}
			}
		})
	}
}

func TestExtractTypesRegexFallback(t *testing.T) {
	got := extractTypes("struct A {};\nclass B {};\n", ".unknown")
	if len(got) != 2 || got[0].kind != "Struct" || got[1].kind != "Class" {
		t.Fatalf("unexpected fallback result: %+v", got)
	}
}
