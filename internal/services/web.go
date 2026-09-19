package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// HTTPServer exposes a small browser dashboard for agent memories and
// thinking (evidence, hypotheses, observations). It is intentionally
// read-only and local-only; it does not expose MCP tools.
type HTTPServer struct {
	app  *Application
	mux  *http.ServeMux
	addr string
}

func NewHTTPServer(app *Application, addr string) *HTTPServer {
	s := &HTTPServer{app: app, addr: addr, mux: http.NewServeMux()}
	s.mux.HandleFunc("/", s.handleDashboard)
	s.mux.HandleFunc("/api/projects", s.handleProjects)
	s.mux.HandleFunc("/api/memories", s.handleMemories)
	s.mux.HandleFunc("/api/thoughts", s.handleThoughts)
	s.mux.HandleFunc("/api/nodes", s.handleNodes)
	s.mux.HandleFunc("/api/graphify/progress", s.handleGraphifyProgress)
	s.mux.HandleFunc("/graphify", s.handleGraphifyPage)
	return s
}

func (s *HTTPServer) ListenAndServe() error {
	return http.ListenAndServe(s.addr, s.mux)
}

func (s *HTTPServer) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, dashboardHTML)
}

func (s *HTTPServer) handleProjects(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.app.Graph.FindNodes("Project", nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, Present(n))
	}
	writeJSON(w, out)
}

func (s *HTTPServer) handleMemories(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		http.Error(w, "project_id is required", http.StatusBadRequest)
		return
	}
	q := r.URL.Query().Get("q")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	res, err := s.app.Memory.SearchAll(projectID, q, limit, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

func (s *HTTPServer) handleThoughts(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		http.Error(w, "project_id is required", http.StatusBadRequest)
		return
	}
	kinds := []string{"Evidence", "Hypothesis", "Observation"}
	out := map[string]any{"project_id": projectID, "kinds": map[string]any{}}
	for _, kind := range kinds {
		nodes, err := s.app.Graph.FindNodes(kind, map[string]any{"project_id": projectID})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		items := make([]map[string]any, 0, len(nodes))
		for _, n := range nodes {
			items = append(items, Present(n))
		}
		sort.Slice(items, func(i, j int) bool {
			a, _ := items[i]["created_at"].(string)
			b, _ := items[j]["created_at"].(string)
			return a < b
		})
		out["kinds"].(map[string]any)[kind] = items
	}
	writeJSON(w, out)
}

func (s *HTTPServer) handleNodes(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		http.Error(w, "project_id is required", http.StatusBadRequest)
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		http.Error(w, "kind is required", http.StatusBadRequest)
		return
	}
	nodes, err := s.app.Graph.FindNodes(kind, map[string]any{"project_id": projectID})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, Present(n))
	}
	writeJSON(w, out)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func (s *HTTPServer) handleGraphifyProgress(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			p := globalProgress.Snapshot()
			data, _ := json.Marshal(p)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			if p.Done {
				return
			}
		}
	}
}

func (s *HTTPServer) handleGraphifyPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, graphifyPageHTML)
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>codeRAG Agent View</title>
<style>
body { font-family: system-ui, sans-serif; margin: 2rem; background: #f7f7f9; color: #222; }
h1 { margin-bottom: .25rem; }
.sub { color: #666; margin-bottom: 1.5rem; }
.card { background: #fff; border: 1px solid #e2e2e6; border-radius: 8px; padding: 1rem; margin-bottom: 1rem; }
input, select { font: inherit; padding: .4rem; }
button { font: inherit; padding: .4rem .8rem; background: #2563eb; color: #fff; border: none; border-radius: 4px; cursor: pointer; }
button:hover { background: #1d4ed8; }
#memories, #thoughts { white-space: pre-wrap; word-break: break-word; background: #1e1e24; color: #d4d4d4; padding: 1rem; border-radius: 6px; min-height: 4rem; }
.meta { color: #666; font-size: .9rem; }
</style>
</head>
<body>
<h1>Agent View</h1>
<p class="sub">Visualize memories and thinking across projects.</p>

<div class="card">
<label>Project ID <input id="project" placeholder="p"></label>
<button onclick="load()">Load</button>
</div>

<div class="card">
<h2>Memories</h2>
<input id="memQ" placeholder="search memories" onkeydown="if(event.key==='Enter')load()">
<div id="memories">select a project</div>
</div>

<div class="card">
<h2>Thinking</h2>
<div id="thoughts">select a project</div>
</div>

<script>
let pid = '';
function load() {
  pid = document.getElementById('project').value.trim();
  if (!pid) return;
  fetch('/api/memories?project_id=' + encodeURIComponent(pid))
    .then(r => r.json()).then(data => {
      document.getElementById('memories').textContent = JSON.stringify(data, null, 2);
    }).catch(e => document.getElementById('memories').textContent = e.message);
  fetch('/api/thoughts?project_id=' + encodeURIComponent(pid))
    .then(r => r.json()).then(data => {
      let summary = {};
      for (let [k, v] of Object.entries(data.kinds)) { summary[k] = v.length; }
      document.getElementById('thoughts').textContent = JSON.stringify(summary, null, 2) + '\\n\\n' + JSON.stringify(data, null, 2);
    }).catch(e => document.getElementById('thoughts').textContent = e.message);
}
</script>
</body>
</html>`

const graphifyPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Graphify - Real-time Knowledge Graph</title>
<script src="https://d3js.org/d3.v7.min.js"></script>
<style>
body { font-family: system-ui, sans-serif; margin: 0; background: #0d1117; color: #c9d1d9; }
#header { padding: 1rem 2rem; background: #161b22; border-bottom: 1px solid #30363d; display: flex; justify-content: space-between; align-items: center; }
h1 { margin: 0; font-size: 1.2rem; color: #58a6ff; }
#status { font-size: 0.85rem; color: #8b949e; }
#viz { width: 100%; height: calc(100vh - 50px); position: relative; }
svg { width: 100%; height: 100%; }
.node { cursor: pointer; }
.node circle { stroke: #30363d; stroke-width: 1.5; }
.node.file circle { fill: #2563eb; }
.node.concept circle { fill: #16a34a; }
.link { stroke: #30363d; stroke-opacity: 0.6; }
#panel { position: absolute; top: 1rem; right: 1rem; width: 280px; background: #161b22; border: 1px solid #30363d; border-radius: 8px; padding: 1rem; font-size: 0.85rem; }
#panel h3 { margin: 0 0 0.5rem; color: #58a6ff; font-size: 0.95rem; }
#panel .metric { display: flex; justify-content: space-between; margin: 0.25rem 0; }
#panel .metric span:last-child { color: #58a6ff; font-weight: 600; }
#log { max-height: 120px; overflow-y: auto; background: #0d1117; border: 1px solid #30363d; border-radius: 4px; padding: 0.5rem; margin-top: 0.5rem; font-family: monospace; font-size: 0.75rem; color: #8b949e; }
</style>
</head>
<body>
<div id="header">
<h1>Graphify</h1>
<div id="status">connecting...</div>
</div>
<div id="viz">
<svg></svg>
<div id="panel">
<h3>Progress</h3>
<div class="metric"><span>Phase</span><span id="phase">-</span></div>
<div class="metric"><span>Files</span><span id="files">0</span></div>
<div class="metric"><span>Nodes</span><span id="nodes">0</span></div>
<div class="metric"><span>Edges</span><span id="edges">0</span></div>
<div class="metric"><span>Communities</span><span id="communities">0</span></div>
<div id="log"></div>
</div>
</div>
<script>
const svg = d3.select("svg");
const width = window.innerWidth;
const height = window.innerHeight - 50;
const sim = d3.forceSimulation().force("link", d3.forceLink().id(d => d.id).distance(60)).force("charge", d3.forceManyBody().strength(-40)).force("center", d3.forceCenter(width/2, height/2));
const link = svg.selectAll("line").data([]).enter().append("line").attr("class", "link").attr("stroke-width", 1);
const node = svg.selectAll("circle").data([]).enter().append("circle").attr("class", "node").attr("r", 5);
const label = svg.selectAll("text").data([]).enter().append("text").attr("font-size", 9).attr("fill", "#c9d1d9").attr("x", 8).attr("y", 3);
sim.on("tick", () => {
  link.attr("x1", d => d.source.x).attr("y1", d => d.source.y).attr("x2", d => d.target.x).attr("y2", d => d.target.y);
  node.attr("cx", d => d.x).attr("cy", d => d.y);
  label.attr("x", d => d.x).attr("y", d => d.y);
});
const es = new EventSource("/api/graphify/progress");
es.onmessage = e => {
  const p = JSON.parse(e.data);
  document.getElementById("phase").textContent = p.phase || "-";
  document.getElementById("files").textContent = p.files_seen || 0;
  document.getElementById("nodes").textContent = p.nodes || 0;
  document.getElementById("edges").textContent = p.edges || 0;
  document.getElementById("communities").textContent = p.communities || 0;
  document.getElementById("status").textContent = p.done ? "done" : (p.phase || "running") + "...";
  if (p.error) {
    const log = document.getElementById("log");
    log.innerHTML += "ERROR: " + p.error + "\\n";
    log.scrollTop = log.scrollHeight;
  }
};
fetch("/api/graphify/graph.json").then(r => r.json()).then(g => {
  if (!g || !g.nodes) return;
  const nodes = g.nodes.map(n => ({...n}));
  const links = (g.edges || []).map(e => ({source: e.source, target: e.target}));
  sim.nodes(nodes);
  sim.force("link").links(links);
  sim.alpha(1).restart();
  link.data(links).enter().append("line").attr("class", "link").attr("stroke-width", 1);
  node.data(nodes).enter().append("circle").attr("class", "node").attr("r", 5).attr("fill", d => d.kind === "file" ? "#2563eb" : "#16a34a");
  label.data(nodes).enter().append("text").text(d => d.label).attr("font-size", 9).attr("fill", "#c9d1d9").attr("x", 8).attr("y", 3);
}).catch(() => {});
window.addEventListener("resize", () => {
  sim.force("center", d3.forceCenter(window.innerWidth/2, window.innerHeight/2));
  sim.alpha(0.3).restart();
});
</script>
</body>
</html>`
