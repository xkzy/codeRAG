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

// Hard limits keep a single Graphify pass bounded no matter how large or
// pathological the tree is (huge/binary files, files with thousands of
// concepts, or a mis-rooted project).
const (
	graphifyMaxFiles           = 20000
	graphifyMaxFileBytes       = 1 << 20
	graphifyMaxConceptsPerFile = 64
	graphifyMaxNodes           = 200000
	graphifyMaxEdges           = 400000
)

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
		if !info.Mode().IsRegular() || info.Size() > graphifyMaxFileBytes {
			return nil
		}
		if globalProgress.Snapshot().FilesSeen >= graphifyMaxFiles || len(graph.Nodes) >= graphifyMaxNodes || len(graph.Edges) >= graphifyMaxEdges {
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(g.root, p)
		globalProgress.Update(func() {
			globalProgress.FilesSeen++
			globalProgress.Phase = "walking"
		})
		addNode(Node{ID: "file:" + rel, Label: rel, Kind: "file", File: rel})
		concepts := extractConcepts(p)
		if len(concepts) > graphifyMaxConceptsPerFile {
			concepts = concepts[:graphifyMaxConceptsPerFile]
		}
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
<html lang="en">
<head>
<meta charset="utf-8">
<title>codeRAG Graph</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{background:#0d1117;overflow:hidden}
canvas{display:block;cursor:grab}
#status{position:fixed;bottom:.5rem;left:50%%;transform:translateX(-50%%);background:#161b22cc;border:1px solid #30363d;border-radius:4px;padding:.25rem .75rem;font:system-ui,.75rem,sans-serif;font-size:.75rem;color:#8b949e;pointer-events:none;white-space:nowrap}
#info{position:fixed;top:1rem;right:1rem;width:260px;background:#161b22;border:1px solid #30363d;border-radius:8px;padding:.75rem;font-family:system-ui;font-size:.8rem;color:#c9d1d9;display:none}
#info h3{color:#58a6ff;font-size:.85rem;margin-bottom:.4rem;word-break:break-all}
#info .kv{display:flex;gap:.4rem;margin:.2rem 0;font-size:.75rem}
#info .kv .k{color:#c9d1d9;min-width:50px}
#info .kv .v{color:#8b949e;word-break:break-all}
#info button{margin-top:.5rem;font:inherit;font-size:.75rem;background:transparent;border:1px solid #30363d;color:#8b949e;border-radius:4px;padding:.2rem .5rem;cursor:pointer}
</style>
</head>
<body>
<canvas id="g"></canvas>
<div id="status">loading…</div>
<div id="info">
  <h3 id="info-title"></h3>
  <div id="info-rows"></div>
  <button onclick="document.getElementById('info').style.display='none'">✕ close</button>
</div>
<script src="https://d3js.org/d3.v7.min.js"></script>
<script>
'use strict';
const rawNodes = %s;
const rawEdges = %s;

const canvas = document.getElementById('g');
const ctx = canvas.getContext('2d');
let transform = d3.zoomIdentity;
let simNodes = [], simEdges = [], qt = null;
let hoveredNode = null, selectedNode = null, rafPending = false;

function resize() {
  canvas.width  = window.innerWidth  * devicePixelRatio;
  canvas.height = window.innerHeight * devicePixelRatio;
  canvas.style.width  = window.innerWidth  + 'px';
  canvas.style.height = window.innerHeight + 'px';
}
resize();
window.addEventListener('resize', () => { resize(); schedDraw(); });

const zoom = d3.zoom().scaleExtent([0.03, 12]).on('zoom', e => { transform = e.transform; schedDraw(); });
d3.select(canvas).call(zoom);

function schedDraw() {
  if (!rafPending) { rafPending = true; requestAnimationFrame(draw); }
}

function draw() {
  rafPending = false;
  const dpr = devicePixelRatio;
  const w = canvas.width, h = canvas.height;
  const cw = w / dpr, ch = h / dpr;
  ctx.save();
  ctx.clearRect(0, 0, w, h);
  ctx.scale(dpr, dpr);
  ctx.save();
  ctx.translate(transform.x, transform.y);
  ctx.scale(transform.k, transform.k);

  const vx0 = -transform.x / transform.k, vy0 = -transform.y / transform.k;
  const vx1 = (cw - transform.x) / transform.k, vy1 = (ch - transform.y) / transform.k;

  // edges
  ctx.strokeStyle = '#30363d';
  ctx.lineWidth = 1 / transform.k;
  ctx.globalAlpha = 0.45;
  ctx.beginPath();
  for (const e of simEdges) {
    const s = e.source, t = e.target;
    if (!s || !t || s.x == null) continue;
    if (s.x < vx0 - 100 && t.x < vx0 - 100) continue;
    if (s.x > vx1 + 100 && t.x > vx1 + 100) continue;
    ctx.moveTo(s.x, s.y); ctx.lineTo(t.x, t.y);
  }
  ctx.stroke();
  ctx.globalAlpha = 1;

  // nodes
  const nr = Math.max(2.5, 5 / transform.k);
  for (const n of simNodes) {
    if (n.x < vx0 - 20 || n.x > vx1 + 20 || n.y < vy0 - 20 || n.y > vy1 + 20) continue;
    ctx.beginPath();
    ctx.arc(n.x, n.y, n === selectedNode ? nr * 1.8 : (n === hoveredNode ? nr * 1.4 : nr), 0, 2 * Math.PI);
    ctx.fillStyle = n === selectedNode ? '#f0a500'
      : n === hoveredNode ? '#ffffff'
      : (n.kind === 'file' ? '#2563eb' : '#16a34a');
    ctx.fill();
  }

  // labels
  const showAll = transform.k > 1.5 || simNodes.length < 200;
  ctx.font = Math.max(8, 10 / transform.k) + 'px system-ui';
  ctx.fillStyle = '#c9d1d9';
  for (const n of simNodes) {
    if (n.x < vx0 - 20 || n.x > vx1 + 20 || n.y < vy0 - 20 || n.y > vy1 + 20) continue;
    if (showAll || (n._deg || 0) >= 10 || n === selectedNode || n === hoveredNode) {
      ctx.fillText(n.label || n.id, n.x + nr + 2, n.y + 3 / transform.k);
    }
  }
  ctx.restore(); ctx.restore();
}

function buildQT() { qt = d3.quadtree().x(d => d.x).y(d => d.y).addAll(simNodes); }

function nodeAt(ex, ey) {
  if (!qt) return null;
  const sx = (ex - transform.x) / transform.k, sy = (ey - transform.y) / transform.k;
  return qt.find(sx, sy, Math.max(10, 5 / transform.k)) || null;
}

canvas.addEventListener('mousemove', e => {
  const rect = canvas.getBoundingClientRect();
  const n = nodeAt(e.clientX - rect.left, e.clientY - rect.top);
  if (n !== hoveredNode) { hoveredNode = n; schedDraw(); }
  canvas.style.cursor = n ? 'pointer' : 'grab';
});

canvas.addEventListener('click', e => {
  const rect = canvas.getBoundingClientRect();
  const n = nodeAt(e.clientX - rect.left, e.clientY - rect.top);
  selectedNode = n;
  if (n) {
    document.getElementById('info-title').textContent = n.label || n.id;
    document.getElementById('info-rows').innerHTML = [
      ['kind', n.kind || '—'],
      ['degree', n._deg || 0],
      ['file', n.file || '—'],
    ].map(([k,v]) => '<div class="kv"><span class="k">'+k+'</span><span class="v">'+String(v)+'</span></div>').join('');
    document.getElementById('info').style.display = 'block';
  } else {
    document.getElementById('info').style.display = 'none';
  }
  schedDraw();
});

// build simulation
const deg = {};
for (const e of rawEdges) { deg[e.source]=(deg[e.source]||0)+1; deg[e.target]=(deg[e.target]||0)+1; }
simNodes = rawNodes.map(n => ({...n, _deg: deg[n.id]||0}));
const nodeIdx = Object.fromEntries(simNodes.map(n => [n.id, n]));
simEdges = rawEdges.map(e => ({source: nodeIdx[e.source]||e.source, target: nodeIdx[e.target]||e.target}));

d3.forceSimulation(simNodes)
  .force('link', d3.forceLink(simEdges).id(d => d.id).distance(60).strength(0.3))
  .force('charge', d3.forceManyBody().strength(-30).distanceMax(200))
  .force('center', d3.forceCenter(window.innerWidth/2, window.innerHeight/2))
  .alphaDecay(0.02).velocityDecay(0.4)
  .on('tick', () => { buildQT(); schedDraw(); })
  .on('end',  () => { buildQT(); schedDraw(); });

document.getElementById('status').textContent =
  rawNodes.length + ' nodes · ' + rawEdges.length + ' edges · scroll to zoom · drag to pan · click to inspect';
</script>
</body>
</html>`, nodesJSON, edgesJSON)
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
