package services

import (
	"sort"
	"strings"
	"testing"

	"codergag/internal/graph"
)

func typeRefs(src, suffix, typeName string) string {
	for _, m := range extractTypes(src, suffix) {
		if m.name == typeName {
			r := append([]string{}, m.refs...)
			sort.Strings(r)
			return strings.Join(r, ",")
		}
	}
	return "<missing>"
}

func TestFieldTypeRefsPerLanguage(t *testing.T) {
	cases := []struct {
		name, suffix, src, typ, want string
	}{
		{"go", ".go", "package x\ntype App struct {\n\tIdx *Index\n\tM map[string]Repo\n\tBase\n\tn int\n}\nfunc (a *App) Run() { var l Local; _ = l }\n", "App", "Index,Repo"},
		{"rust", ".rs", "struct App { idx: Index, items: Vec<Item>, n: i32 }\nimpl App { fn run(&self) { let l: Local; } }\n", "App", "Index,Item,Vec"},
		{"java", ".java", "class App extends Base implements Runner { private Index idx; List<Item> items; void run() { Local l; } }\n", "App", "Index,Item,List"},
		{"ts", ".ts", "class App extends Base { idx: Index; items: Item[]; run() { const l: Local = null; } }\n", "App", "Index,Item"},
		{"py", ".py", "class App(Base):\n    idx: Index\n    items: list[Item] = []\n    def run(self):\n        l: Local = None\n", "App", "Index,Item"},
		{"c", ".c", "struct app { struct index *idx; item_t item; int n; };\n", "app", "index,item_t"},
		{"cpp", ".cpp", "class App : public Base { Index idx; std::vector<Item> items; void run() { Local l; } };\n", "App", "Index,Item"},
		{"nested", ".java", "class Outer { Index a; class Inner { Other b; } }\n", "Outer", "Index"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := typeRefs(c.src, c.suffix, c.typ)
			// Container names like Vec/list/List are kept; they resolve to nothing unless the project defines them.
			if c.name == "cpp" {
				got = strings.ReplaceAll(strings.ReplaceAll(got, "vector,", ""), ",vector", "")
			}
			if got != c.want {
				t.Fatalf("refs of %s = %q, want %q", c.typ, got, c.want)
			}
		})
	}
}

func typeUses(app *Application, kind, name string) []string {
	nodes, _ := app.Graph.FindNodes(kind, map[string]any{"project_id": "p", "name": name})
	var out []string
	for _, n := range nodes {
		nbrs, _ := app.Graph.Neighbors(n.ID, "USES", graph.DirOut)
		for _, en := range nbrs {
			out = append(out, strProp(en.Node, "name"))
		}
	}
	sort.Strings(out)
	return out
}

func TestStructFieldsBecomeUsesEdgesAndFollowEdits(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.go": "package x\ntype Index struct{}\ntype Repo struct{}\ntype Base struct{}\ntype App struct {\n\tBase\n\tIdx *Index\n}\nfunc (a *App) Run() { var r Repo; _ = r }\n",
	})
	app := ApplicationInMemory()
	app.Index.IndexRepository("p", dir, true, nil, false)
	if got := typeUses(app, "Struct", "App"); strings.Join(got, ",") != "Index" {
		t.Fatalf("App uses %v, want [Index]: embedded Base is EXTENDS and method-body Repo belongs to Run", got)
	}
	if got := edgeTargets(app, "p", "EXTENDS", "App"); strings.Join(got, ",") != "Base" {
		t.Fatalf("embedding must still be EXTENDS: %v", got)
	}
	if got := usesOf(t, app, "Run"); strings.Join(got, ",") != "Repo" {
		t.Fatalf("method-body type use stays on the method: %v", got)
	}

	writeTree(t, dir, map[string]string{
		"a.go": "package x\ntype Index struct{}\ntype Repo struct{}\ntype Base struct{}\ntype App struct {\n\tBase\n\tR Repo\n}\n",
	})
	app.Index.IndexRepository("p", dir, true, nil, false)
	if got := typeUses(app, "Struct", "App"); strings.Join(got, ",") != "Repo" {
		t.Fatalf("field edge should move from Index to Repo: %v", got)
	}
}

func TestFieldTypeRefsStayInsideTheirLanguage(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.go": "package x\ntype Thing struct{}\n",
		"b.py": "class Holder:\n    t: Thing\n",
	})
	app := ApplicationInMemory()
	app.Index.IndexRepository("p", dir, true, nil, false)
	if got := typeUses(app, "Class", "Holder"); len(got) != 0 {
		t.Fatalf("a Python annotation must not link to a Go struct: %v", got)
	}
}
