package services

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"codergag/internal/graph"
)

func TestExtractFunctionInfosTreeSitter(t *testing.T) {
	cases := []struct {
		name, suffix, src string
		want              map[string]string // qualified name -> comma joined calls
		params            map[string]int
	}{
		{"python", ".py", "def a(x, y):\n    b(x)\n    self.c()\n\nclass K:\n    def m(self):\n        a(1, 2)\n",
			map[string]string{"a": "b,c", "K.m": "a"}, map[string]int{"a": 2}},
		{"go", ".go", "package x\nfunc A(a, b int, c string) { B(); pkg.C() }\nfunc (r *T) M() { A(1,2,\"\") }\n",
			map[string]string{"A": "B,C", "T.M": "A"}, map[string]int{"A": 3, "T.M": 0}},
		{"rust", ".rs", "fn a(x: i32) { b(); s.c(); m::d(); }\nimpl Foo<T> { fn go(&self) { a(1); } }\n",
			map[string]string{"a": "b,c,d", "Foo.go": "a"}, map[string]int{"a": 1}},
		{"c", ".c", "int add(int a, int b) { return helper(a) + b; }\nstatic char *name(void) { return 0; }\n",
			map[string]string{"add": "helper", "name": ""}, map[string]int{"add": 2, "name": 0}},
		{"cpp", ".cpp", "class C { void m() { n(); } };\nvoid C::n() { x.y(); ns::z(); }\n",
			map[string]string{"C.m": "n", "C.n": "y,z"}, nil},
		{"java", ".java", "class J { void a(int x) { b(x); this.c(); } J() { a(1); } }\n",
			map[string]string{"J.a": "b,c", "J.J": "a"}, map[string]int{"J.a": 1}},
		{"js", ".js", "function a(x) { b(); o.c(); }\nconst f = (p, q) => { a(); };\nclass K { m() { f(); } }\n",
			map[string]string{"a": "b,c", "f": "a", "K.m": "f"}, map[string]int{"f": 2}},
		{"ts", ".ts", "export function a(x: number): void { b(); }\nconst g = async () => { a(1); };\n",
			map[string]string{"a": "b", "g": "a"}, map[string]int{"a": 1, "g": 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := map[string]string{}
			params := map[string]int{}
			for _, f := range mergeFuncInfos(extractFunctionInfos(c.src, c.suffix)) {
				calls := uniqueStrings(f.calls, f.name)
				sort.Strings(calls)
				joined := ""
				for i, cl := range calls {
					if i > 0 {
						joined += ","
					}
					joined += cl
				}
				got[f.qualifiedName()] = joined
				params[f.qualifiedName()] = f.params
			}
			if len(got) != len(c.want) {
				t.Fatalf("functions: got %v want %v", got, c.want)
			}
			for n, calls := range c.want {
				if got[n] != calls {
					t.Fatalf("calls of %s: got %q want %q (all: %v)", n, got[n], calls, got)
				}
			}
			for n, p := range c.params {
				if params[n] != p {
					t.Fatalf("params of %s: got %d want %d", n, params[n], p)
				}
			}
		})
	}
}

func TestExtractFunctionInfosRegexFallback(t *testing.T) {
	fs := extractFunctionInfos("def a():\n    b()\n", ".unknown")
	if len(fs) == 0 {
		t.Fatal("fallback should still find definitions")
	}
}

func callTargets(t *testing.T, app *Application, project, fnName string) []string {
	t.Helper()
	fns, _ := app.Graph.FindNodes("Function", map[string]any{"project_id": project, "name": fnName})
	var out []string
	for _, f := range fns {
		nbrs, _ := app.Graph.Neighbors(f.ID, "CALLS", graph.DirOut)
		for _, en := range nbrs {
			out = append(out, en.Node.Properties["path"].(string)+":"+en.Node.Properties["name"].(string))
		}
	}
	sort.Strings(out)
	return out
}

func TestCallsResolveAcrossFilesRegardlessOfOrder(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// "a.go" sorts before "b.go", so its callee is indexed after the caller.
	write("a.go", "package x\nfunc Caller() { Callee(); local() }\nfunc local() {}\n")
	write("b.go", "package x\nfunc Callee() { local() }\nfunc local() {}\n")
	app := ApplicationInMemory()
	if _, err := app.Index.IndexRepository("p", dir, true, nil); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(dir)
	got := callTargets(t, app, "p", "Caller")
	want := []string{filepath.Join(real, "a.go") + ":local", filepath.Join(real, "b.go") + ":Callee"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Caller targets: got %v want %v (same-file local must win over b.go local)", got, want)
	}

	// Rewriting the callee file replaces its nodes; the caller must re-link on the next index.
	write("b.go", "package x\nfunc Callee() {}\nfunc Extra() {}\n")
	if _, err := app.Index.IndexRepository("p", dir, true, nil); err != nil {
		t.Fatal(err)
	}
	got = callTargets(t, app, "p", "Caller")
	if len(got) != 2 {
		t.Fatalf("caller lost edges after callee reindex: %v", got)
	}
	if n := len(callTargets(t, app, "p", "Callee")); n != 0 {
		t.Fatalf("stale call edge from rewritten Callee: %d", n)
	}
}

func TestMethodsWithSameNameStayDistinct(t *testing.T) {
	dir := t.TempDir()
	src := "class A:\n    def run(self):\n        pass\n\nclass B:\n    def run(self):\n        pass\n"
	if err := os.WriteFile(filepath.Join(dir, "m.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	app := ApplicationInMemory()
	if _, err := app.Index.IndexRepository("p", dir, true, nil); err != nil {
		t.Fatal(err)
	}
	fns, _ := app.Graph.FindNodes("Function", map[string]any{"project_id": "p", "name": "run"})
	if len(fns) != 2 {
		t.Fatalf("expected two distinct run methods, got %d", len(fns))
	}
}
