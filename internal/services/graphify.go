package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// GraphifyProgress tracks the real-time progress of a graphify run.
type GraphifyProgress struct {
	mu          sync.RWMutex
	Phase       string    `json:"phase"`
	FilesSeen   int       `json:"files_seen"`
	Nodes       int       `json:"nodes"`
	Edges       int       `json:"edges"`
	Communities int       `json:"communities"`
	StartedAt   time.Time `json:"started_at"`
	EndedAt     time.Time `json:"ended_at"`
	Error       string    `json:"error,omitempty"`
	Done        bool      `json:"done"`
}

var globalProgress = &GraphifyProgress{}

func (p *GraphifyProgress) Update(fn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fn()
}

func (p *GraphifyProgress) Snapshot() *GraphifyProgress {
	p.mu.RLock()
	defer p.mu.RUnlock()
	cp := &GraphifyProgress{
		Phase:       p.Phase,
		FilesSeen:   p.FilesSeen,
		Nodes:       p.Nodes,
		Edges:       p.Edges,
		Communities: p.Communities,
		StartedAt:   p.StartedAt,
		EndedAt:     p.EndedAt,
		Error:       p.Error,
		Done:        p.Done,
	}
	return cp
}

func (p *GraphifyProgress) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Phase = "walking"
	p.FilesSeen = 0
	p.Nodes = 0
	p.Edges = 0
	p.Communities = 0
	p.StartedAt = time.Now()
	p.EndedAt = time.Time{}
	p.Error = ""
	p.Done = false
}

// Graphify builds a navigable knowledge graph from a folder of files,
// detects communities, and writes HTML + JSON + a plain-language report.
type Graphify struct {
	root     string
	deep     bool
	directed bool
}

func NewGraphify(root string, deep, directed bool) *Graphify {
	return &Graphify{root: root, deep: deep, directed: directed}
}

// Run walks the folder, extracts a graph, detects communities, and writes
// outputs under graphify-out/. It returns the in-memory graph.
func (g *Graphify) Run() (*Graph, error) {
	globalProgress.Reset()
	defer func() {
		globalProgress.mu.Lock()
		globalProgress.Done = true
		globalProgress.EndedAt = time.Now()
		globalProgress.mu.Unlock()
	}()
	graph, err := g.extract()
	if err != nil {
		globalProgress.mu.Lock()
		globalProgress.Error = err.Error()
		globalProgress.mu.Unlock()
		return nil, err
	}
	globalProgress.Update(func() {
		globalProgress.Phase = "communities"
	})
	if len(graph.Nodes) == 0 {
		return nil, fmt.Errorf("graph is empty - no supported files found in %s", g.root)
	}
	communities := labelPropagate(graph, g.directed)
	graph.Communities = communities
	globalProgress.Update(func() {
		globalProgress.Phase = "writing"
		globalProgress.Nodes = len(graph.Nodes)
		globalProgress.Edges = len(graph.Edges)
		globalProgress.Communities = len(communities)
	})
	if err := os.MkdirAll(filepath.Join(g.root, "graphify-out"), 0o755); err != nil {
		return nil, err
	}
	if err := writeGraphJSON(graph, filepath.Join(g.root, "graphify-out", "graph.json")); err != nil {
		return nil, err
	}
	if err := writeGraphHTML(graph, filepath.Join(g.root, "graphify-out", "graph.html")); err != nil {
		return nil, err
	}
	if err := writeGraphReport(graph, g.root, filepath.Join(g.root, "graphify-out", "GRAPH_REPORT.md")); err != nil {
		return nil, err
	}
	return graph, nil
}

func (g *Graphify) extract() (*Graph, error) {
	graph := &Graph{Nodes: []Node{}, Edges: []Edge{}}
	nodeIDs := map[string]bool{}
	edgeSet := map[string]bool{}
	addNode := func(n Node) {
		if nodeIDs[n.ID] {
			return
		}
		nodeIDs[n.ID] = true
		graph.Nodes = append(graph.Nodes, n)
	}
	addEdge := func(e Edge) {
		key := e.Source + "|" + e.Target + "|" + e.Relation
		if edgeSet[key] {
			return
		}
		edgeSet[key] = true
		graph.Edges = append(graph.Edges, e)
	}

	err := filepath.Walk(g.root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if isIgnoreDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(g.root, p)
		globalProgress.Update(func() {
			globalProgress.FilesSeen++
			globalProgress.Phase = "walking"
		})
		addNode(Node{ID: "file:" + rel, Label: rel, Kind: "file", File: rel})
		concepts := extractConcepts(p)
		for _, c := range concepts {
			c.File = rel
			addNode(c)
			addEdge(Edge{Source: "file:" + rel, Target: c.ID, Relation: "defines", Confidence: "EXTRACTED", ConfidenceScore: 1.0, SourceFile: rel})
		}
		// Edges between concepts mentioned together in the same file.
		for i, a := range concepts {
			for _, b := range concepts[i+1:] {
				if a.ID == b.ID {
					continue
				}
				rel := "references"
				if g.deep {
					rel = "semantically_related_to"
				}
				addEdge(Edge{Source: a.ID, Target: b.ID, Relation: rel, Confidence: "INFERRED", ConfidenceScore: 0.7, SourceFile: rel})
			}
		}
		// Import edges: import path -> concept if the imported name exists as a concept.
		imports := extractImportsFromSource(p)
		for _, imp := range imports {
			targetID := "concept:" + imp
			if nodeIDs[targetID] {
				addEdge(Edge{Source: "file:" + rel, Target: targetID, Relation: "imports", Confidence: "EXTRACTED", ConfidenceScore: 1.0, SourceFile: rel})
			}
		}
		return nil
	})
	return graph, err
}

func isIgnoreDir(name string) bool {
	for _, d := range []string{".git", "node_modules", "build", "dist", "target", "vendor", ".codegraph"} {
		if name == d {
			return true
		}
	}
	return false
}

func extractConcepts(path string) []Node {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".txt", ".yaml", ".yml", ".json", ".toml", ".xml":
		return extractDocConcepts(string(data))
	default:
		if isCodeExt(ext) {
			return extractCodeConcepts(string(data), ext)
		}
	}
	return nil
}

func isCodeExt(ext string) bool {
	for _, e := range []string{".py", ".c", ".h", ".cc", ".cpp", ".hpp", ".rs", ".js", ".ts", ".tsx", ".java", ".go"} {
		if e == ext {
			return true
		}
	}
	return false
}

func extractDocConcepts(content string) []Node {
	re := regexp.MustCompile(`(?m)^(#{1,6})\s+(.+?)\s*$`)
	matches := re.FindAllStringSubmatch(content, -1)
	var nodes []Node
	for _, m := range matches {
		heading := strings.TrimSpace(m[2])
		if heading == "" {
			continue
		}
		nodes = append(nodes, Node{ID: conceptID(heading), Label: heading, Kind: "concept"})
	}
	if len(nodes) == 0 {
		// Fall back to paragraphs.
		for _, p := range strings.FieldsFunc(content, func(r rune) bool { return r == '\n' }) {
			p = strings.TrimSpace(p)
			if len(p) > 20 {
				nodes = append(nodes, Node{ID: conceptID(firstLineOf(p)), Label: firstLineOf(p), Kind: "concept"})
			}
		}
	}
	return nodes
}

func extractCodeConcepts(content, ext string) []Node {
	var nodes []Node
	funcs := extractFunctionNames(content, ext)
	for _, f := range funcs {
		nodes = append(nodes, Node{ID: conceptID(f), Label: f, Kind: "concept"})
	}
	types := extractTypeNames(content, ext)
	for _, t := range types {
		nodes = append(nodes, Node{ID: conceptID(t), Label: t, Kind: "concept"})
	}
	return nodes
}

func extractImportsFromSource(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return extractGoImports(string(data))
	case ".py":
		return extractPyImports(string(data))
	case ".js", ".ts", ".tsx":
		return extractJSImports(string(data))
	default:
		return nil
	}
}

func extractGoImports(content string) []string {
	re := regexp.MustCompile(`(?m)^\s*import\s+(?:\([^)]*\)|"([^"]+)")`)
	matches := re.FindAllStringSubmatch(content, -1)
	var out []string
	for _, m := range matches {
		if len(m) > 1 && m[1] != "" {
			out = append(out, m[1])
		}
	}
	return out
}

func extractPyImports(content string) []string {
	re := regexp.MustCompile(`(?m)^\s*(?:from\s+([\w.]+)\s+import|import\s+([\w.]+))`)
	matches := re.FindAllStringSubmatch(content, -1)
	var out []string
	for _, m := range matches {
		for _, v := range m[1:] {
			if v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

func extractJSImports(content string) []string {
	re := regexp.MustCompile(`(?m)^\s*(?:import|export)\s+.*\s+from\s+["']([^"']+)["']`)
	matches := re.FindAllStringSubmatch(content, -1)
	var out []string
	for _, m := range matches {
		if len(m) > 1 {
			out = append(out, m[1])
		}
	}
	return out
}

func extractFunctionNames(content, ext string) []string {
	var re *regexp.Regexp
	switch ext {
	case ".py":
		re = regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+([A-Za-z_]\w*)\s*\(`)
	case ".go":
		re = regexp.MustCompile(`(?m)^\s*func\s+(?:\([^)]*\)\s+)?([A-Za-z_]\w*)\s*\(`)
	case ".js", ".ts", ".tsx":
		re = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:async\s+)?(?:function\s+|(?:const|let|var)\s+)([A-Za-z_$][\w$]*)\s*[=:]?\s*(?:async\s+)?\(`)
	case ".java":
		re = regexp.MustCompile(`(?m)^\s*(?:public|private|protected|static|final|abstract|native|\s)+[\w<>\[\], ?]+\s+([A-Za-z_]\w*)\s*\(`)
	case ".c", ".h":
		re = regexp.MustCompile(`(?m)^\s*(?:static\s+)?[\w\s\*]+\s+([A-Za-z_]\w*)\s*\(`)
	case ".cpp", ".cc", ".hpp":
		re = regexp.MustCompile(`(?m)^\s*(?:static\s+)?[\w\s<>]*\s+([A-Za-z_]\w*)\s*::\s*([A-Za-z_]\w*)\s*\(`)
	default:
		return nil
	}
	matches := re.FindAllStringSubmatch(content, -1)
	var out []string
	for _, m := range matches {
		name := ""
		if len(m) == 2 {
			name = m[1]
		} else if len(m) == 3 && m[2] != "" {
			name = m[1] + "_" + m[2]
		}
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func extractTypeNames(content, ext string) []string {
	var re *regexp.Regexp
	switch ext {
	case ".go":
		re = regexp.MustCompile(`(?m)^\s*(?:type\s+)([A-Za-z_]\w*)\s+(?:struct|interface|enum)`)
	case ".py":
		re = regexp.MustCompile(`(?m)^\s*class\s+([A-Za-z_]\w*)`)
	case ".java":
		re = regexp.MustCompile(`(?m)^\s*(?:public|private|protected|abstract|final|\s)+(?:class|interface|enum)\s+([A-Za-z_]\w*)`)
	case ".rs":
		re = regexp.MustCompile(`(?m)^\s*(?:pub\s+)*(?:struct|enum|trait|type)\s+([A-Za-z_]\w*)`)
	case ".cpp", ".cc", ".hpp":
		re = regexp.MustCompile(`(?m)^\s*(?:class|struct|enum)\s+([A-Za-z_]\w*)`)
	default:
		return nil
	}
	matches := re.FindAllStringSubmatch(content, -1)
	var out []string
	for _, m := range matches {
		if len(m) > 1 && m[1] != "" {
			out = append(out, m[1])
		}
	}
	return out
}

func conceptID(label string) string {
	lower := strings.ToLower(label)
	var b strings.Builder
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	id := b.String()
	id = strings.Trim(id, "_")
	if id == "" {
		id = "concept"
	}
	return "concept:" + id
}

// labelPropagate runs label propagation community detection.
func labelPropagate(g *Graph, directed bool) map[string][]string {
	if len(g.Nodes) == 0 {
		return nil
	}
	labels := map[string]string{}
	for _, n := range g.Nodes {
		labels[n.ID] = n.ID
	}
	neighbors := map[string][]string{}
	for _, e := range g.Edges {
		neighbors[e.Source] = append(neighbors[e.Source], e.Target)
		if !directed {
			neighbors[e.Target] = append(neighbors[e.Target], e.Source)
		}
	}
	for iter := 0; iter < 20; iter++ {
		changed := false
		for _, n := range g.Nodes {
			nbrs := neighbors[n.ID]
			if len(nbrs) == 0 {
				continue
			}
			counts := map[string]int{}
			for _, id := range nbrs {
				counts[labels[id]]++
			}
			best := labels[n.ID]
			max := 0
			for label, c := range counts {
				if c > max || (c == max && label < best) {
					best = label
					max = c
				}
			}
			if best != labels[n.ID] {
				labels[n.ID] = best
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	communities := map[string][]string{}
	for id, label := range labels {
		communities[label] = append(communities[label], id)
	}
	for k := range communities {
		sort.Strings(communities[k])
	}
	return communities
}

func writeGraphJSON(g *Graph, path string) error {
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeGraphHTML(g *Graph, path string) error {
	nodesJSON, _ := json.Marshal(g.Nodes)
	edgesJSON, _ := json.Marshal(g.Edges)
	html := fmt.Sprintf(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>codeRAG Graph</title>
<script src="https://d3js.org/d3.v7.min.js"></script>
<style>body{font-family:system-ui;background:#f7f7f9;margin:0}svg{background:#fff}</style></head>
<body><h1>Knowledge Graph</h1><svg width="100%%" height="100%%"></svg>
<script>
const nodes=%s; const edges=%s;
const svg=d3.select("svg"); const width=window.innerWidth; const height=window.innerHeight;
const sim=d3.forceSimulation(nodes).force("link",d3.forceLink(edges).id(d=>d.id).distance(80)).force("charge",d3.forceManyBody().strength(-50)).force("center",d3.forceCenter(width/2,height/2));
const link=svg.selectAll("line").data(edges).enter().append("line").attr("stroke","#999").attr("stroke-width",1);
const node=svg.selectAll("circle").data(nodes).enter().append("circle").attr("r",5).attr("fill",d=>d.kind==="file"?"#2563eb":"#16a34a");
const text=svg.selectAll("text").data(nodes).enter().append("text").text(d=>d.label).attr("font-size",10).attr("x",6).attr("y",3);
sim.on("tick",()=>{link.attr("x1",d=>d.source.x).attr("y1",d=>d.source.y).attr("x2",d=>d.target.x).attr("y2",d=>d.target.y);node.attr("cx",d=>d.x).attr("cy",d=>d.y);text.attr("x",d=>d.x).attr("y",d=>d.y);});
</script></body></html>`, nodesJSON, edgesJSON)
	return os.WriteFile(path, []byte(html), 0o644)
}

func writeGraphReport(g *Graph, root, path string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Graph Report\n\nRoot: %s\n\n", root)
	fmt.Fprintf(&b, "Nodes: %d\nEdges: %d\nCommunities: %d\n\n", len(g.Nodes), len(g.Edges), len(g.Communities))
	fmt.Fprintln(&b, "## God Nodes")
	god := godNodes(g)
	for _, n := range god {
		fmt.Fprintf(&b, "- %s (%s)\n", n.Label, n.Kind)
	}
	fmt.Fprintln(&b, "\n## Surprising Connections")
	surp := surprisingConnections(g)
	for _, e := range surp {
		fmt.Fprintf(&b, "- %s -> %s (%s)\n", e.Source, e.Target, e.Relation)
	}
	fmt.Fprintln(&b, "\n## Suggested Questions")
	qs := suggestQuestions(g)
	for _, q := range qs {
		fmt.Fprintf(&b, "- %s\n", q)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func godNodes(g *Graph) []Node {
	degree := map[string]int{}
	for _, e := range g.Edges {
		degree[e.Source]++
		degree[e.Target]++
	}
	type kv struct {
		node Node
		deg  int
	}
	var list []kv
	for _, n := range g.Nodes {
		list = append(list, kv{n, degree[n.ID]})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].deg > list[j].deg })
	var out []Node
	for i, kv := range list {
		if i >= 10 || kv.deg == 0 {
			break
		}
		out = append(out, kv.node)
	}
	return out
}

func surprisingConnections(g *Graph) []Edge {
	nodeComm := map[string]string{}
	for comm, members := range g.Communities {
		for _, id := range members {
			nodeComm[id] = comm
		}
	}
	var out []Edge
	for _, e := range g.Edges {
		if nodeComm[e.Source] != "" && nodeComm[e.Target] != "" && nodeComm[e.Source] != nodeComm[e.Target] {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	if len(out) > 50 {
		out = out[:50]
	}
	return out
}

func suggestQuestions(g *Graph) []string {
	var qs []string
	if len(g.Communities) > 1 {
		qs = append(qs, "Which concepts bridge the most communities?")
	}
	if len(g.Edges) > 0 {
		qs = append(qs, "What is the most connected node?")
	}
	if len(g.Nodes) > 10 {
		qs = append(qs, "How do file nodes connect to concepts?")
	}
	return qs
}

// Graph is the JSON-serializable knowledge graph output.
type Graph struct {
	Nodes       []Node              `json:"nodes"`
	Edges       []Edge              `json:"edges"`
	Communities map[string][]string `json:"communities,omitempty"`
}

// Node is a graph node.
type Node struct {
	ID       string         `json:"id"`
	Label    string         `json:"label"`
	Kind     string         `json:"kind"`
	File     string         `json:"file,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Edge is a graph edge.
type Edge struct {
	Source          string         `json:"source"`
	Target          string         `json:"target"`
	Relation        string         `json:"relation"`
	Confidence      string         `json:"confidence"`
	ConfidenceScore float64        `json:"confidence_score"`
	SourceFile      string         `json:"source_file,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

func firstLineOf(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
