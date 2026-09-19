package services

import (
	"encoding/json"
	"fmt"
	"net"
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
	s.mux.HandleFunc("/api/graphify/graph.json", s.handleGraphifyGraph)
	s.mux.HandleFunc("/graphify", s.handleGraphifyPage)
	return s
}

func (s *HTTPServer) ListenAndServe() error {
	return http.ListenAndServe(s.addr, s.mux)
}

// StartBackground binds the address and serves in a goroutine. It returns the
// bind error (e.g. another codergag instance already owns the port) so callers
// can treat it as non-fatal. The returned func stops the listener.
func (s *HTTPServer) StartBackground() (net.Addr, func(), error) {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return nil, nil, err
	}
	srv := &http.Server{Handler: s.mux}
	go srv.Serve(ln)
	return ln.Addr(), func() { srv.Close() }, nil
}

func (s *HTTPServer) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, unifiedHTML)
}

// handleGraphifyPage redirects to the unified SPA so existing bookmarks work.
func (s *HTTPServer) handleGraphifyPage(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusMovedPermanently)
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

// latestGraphifyRun returns the newest stored GraphifyRun, optionally limited
// to one project.
func (s *HTTPServer) latestGraphifyRun(projectID string) map[string]any {
	filter := map[string]any(nil)
	if projectID != "" {
		filter = map[string]any{"project_id": projectID}
	}
	nodes, err := s.app.Graph.FindNodes("GraphifyRun", filter)
	if err != nil {
		return nil
	}
	var best map[string]any
	for _, n := range nodes {
		p := Present(n)
		if best == nil || fmt.Sprint(p["updated_at"]) > fmt.Sprint(best["updated_at"]) {
			best = p
		}
	}
	return best
}

// handleGraphifyGraph serves the stored graph for the visualization. The node
// count is capped (default 1500, most-connected first) so the browser stays
// responsive on large repositories.
func (s *HTTPServer) handleGraphifyGraph(w http.ResponseWriter, r *http.Request) {
	run := s.latestGraphifyRun(r.URL.Query().Get("project_id"))
	if run == nil {
		writeJSON(w, map[string]any{"nodes": []any{}, "edges": []any{}})
		return
	}
	var g Graph
	if err := json.Unmarshal([]byte(fmt.Sprint(run["graph_json"])), &g); err != nil {
		http.Error(w, "stored graph is unreadable: "+err.Error(), http.StatusInternalServerError)
		return
	}
	limit := 1500
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	total := len(g.Nodes)
	if limit > 0 && total > limit {
		deg := map[string]int{}
		for _, e := range g.Edges {
			deg[e.Source]++
			deg[e.Target]++
		}
		sort.SliceStable(g.Nodes, func(i, j int) bool { return deg[g.Nodes[i].ID] > deg[g.Nodes[j].ID] })
		g.Nodes = g.Nodes[:limit]
		keep := make(map[string]bool, limit)
		for _, n := range g.Nodes {
			keep[n.ID] = true
		}
		edges := g.Edges[:0:0]
		for _, e := range g.Edges {
			if keep[e.Source] && keep[e.Target] {
				edges = append(edges, e)
			}
		}
		g.Edges = edges
	}
	writeJSON(w, map[string]any{"nodes": g.Nodes, "edges": g.Edges, "total_nodes": total})
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
			if p.StartedAt.IsZero() {
				// This process has not indexed anything; report the last stored run.
				if run := s.latestGraphifyRun(""); run != nil {
					p.Phase, p.Done = "last run", true
					p.Nodes, p.Edges, p.Communities = toInt(run["nodes"]), toInt(run["edges"]), toInt(run["communities"])
				}
			}
			data, _ := json.Marshal(p)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			if p.Done {
				return
			}
		}
	}
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// unifiedHTML is the single-page application that replaces both the old
// dashboard (/) and the old graphify page (/graphify). It renders a canvas-
// based force-directed graph using D3 simulation for layout (no SVG DOM nodes),
// with quadtree hit-testing for click/hover, plus list views for memories and
// thoughts. The /graphify route issues a 301 redirect here.
const unifiedHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>codeRAG</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:system-ui,sans-serif;background:#0d1117;color:#c9d1d9;display:flex;height:100vh;overflow:hidden}
/* ── sidebar ── */
#sidebar{width:220px;min-width:220px;background:#161b22;border-right:1px solid #30363d;display:flex;flex-direction:column;padding:1rem;gap:.75rem;overflow-y:auto}
#sidebar h1{font-size:1rem;color:#58a6ff;letter-spacing:.05em;user-select:none}
#sidebar select,#sidebar input{width:100%;font:inherit;font-size:.8rem;background:#0d1117;color:#c9d1d9;border:1px solid #30363d;border-radius:4px;padding:.35rem .5rem}
#sidebar select:focus,#sidebar input:focus{outline:none;border-color:#58a6ff}
.nav-btn{display:block;width:100%;text-align:left;font:inherit;font-size:.85rem;padding:.4rem .6rem;border:none;border-radius:4px;cursor:pointer;background:transparent;color:#8b949e;transition:background .1s,color .1s}
.nav-btn:hover{background:#21262d;color:#c9d1d9}
.nav-btn.active{background:#21262d;color:#58a6ff;font-weight:600}
#sidebar hr{border:none;border-top:1px solid #30363d}
#node-detail{flex:1;overflow-y:auto;font-size:.78rem;color:#8b949e;display:none;word-break:break-all}
#node-detail h3{color:#58a6ff;font-size:.82rem;margin-bottom:.35rem}
#node-detail .kv{display:flex;gap:.4rem;margin:.2rem 0}
#node-detail .kv .k{color:#c9d1d9;min-width:50px}
/* ── main ── */
#main{flex:1;position:relative;overflow:hidden}
.view{position:absolute;inset:0;display:none}
.view.active{display:block}
/* canvas */
#canvas-view canvas{display:block;width:100%;height:100%}
/* list view */
#list-view{overflow-y:auto;padding:1.5rem;display:flex;flex-direction:column;gap:1rem}
.card{background:#161b22;border:1px solid #30363d;border-radius:8px;padding:1rem}
.card h2{font-size:.9rem;color:#58a6ff;margin-bottom:.5rem}
.card pre{font-size:.72rem;white-space:pre-wrap;word-break:break-word;color:#8b949e;max-height:260px;overflow-y:auto;background:#0d1117;border:1px solid #30363d;border-radius:4px;padding:.5rem;margin-top:.4rem}
/* status bar */
#statusbar{position:absolute;bottom:.5rem;left:50%;transform:translateX(-50%);background:#161b22cc;border:1px solid #30363d;border-radius:4px;padding:.25rem .75rem;font-size:.75rem;color:#8b949e;pointer-events:none;white-space:nowrap;max-width:80vw;overflow:hidden;text-overflow:ellipsis}
/* search */
#search-wrap{position:absolute;top:.75rem;left:50%;transform:translateX(-50%);z-index:10;display:none}
#search-wrap input{font:inherit;font-size:.82rem;background:#161b22;color:#c9d1d9;border:1px solid #30363d;border-radius:4px;padding:.35rem .7rem;width:220px}
#search-wrap input:focus{outline:none;border-color:#58a6ff}
</style>
</head>
<body>
<div id="sidebar">
  <h1>⬡ codeRAG</h1>
  <select id="proj-select"><option value="">— project —</option></select>
  <hr>
  <button class="nav-btn active" data-view="graph">⬡ Graph</button>
  <button class="nav-btn" data-view="memories">◈ Memories</button>
  <button class="nav-btn" data-view="thoughts">◎ Thoughts</button>
  <hr>
  <div id="node-detail"></div>
</div>
<div id="main">
  <div id="canvas-view" class="view active">
    <canvas id="graph-canvas"></canvas>
    <div id="search-wrap"><input id="search" placeholder="search nodes… (Ctrl+F)" autocomplete="off"></div>
  </div>
  <div id="list-view" class="view">
    <div id="list-content"><p style="color:#8b949e;padding:1rem">Select a project</p></div>
  </div>
  <div id="statusbar">no project selected</div>
</div>
<script src="https://d3js.org/d3.v7.min.js"></script>
<script>
'use strict';
// ── routing ───────────────────────────────────────────────────────────────────
let currentView = 'graph', currentProject = '';

function setView(name) {
  currentView = name;
  document.querySelectorAll('.nav-btn').forEach(b =>
    b.classList.toggle('active', b.dataset.view === name));
  document.getElementById('canvas-view').classList.toggle('active', name === 'graph');
  document.getElementById('list-view').classList.toggle('active', name !== 'graph');
  document.getElementById('search-wrap').style.display = name === 'graph' ? '' : 'none';
  if (name !== 'graph') loadList(name);
}

function setProject(id) {
  currentProject = id;
  clearNodeDetail();
  if (currentView === 'graph') loadGraph();
  else loadList(currentView);
}

document.querySelectorAll('.nav-btn').forEach(b =>
  b.addEventListener('click', () => setView(b.dataset.view)));
document.getElementById('proj-select').addEventListener('change',
  e => setProject(e.target.value));

// ── projects ──────────────────────────────────────────────────────────────────
fetch('/api/projects').then(r => r.json()).then(ps => {
  const sel = document.getElementById('proj-select');
  ps.forEach(p => {
    const id = p.id || p.project_id || '';
    const o = document.createElement('option');
    o.value = id; o.textContent = id;
    sel.appendChild(o);
  });
  if (ps.length === 1) {
    sel.value = ps[0].id || ps[0].project_id || '';
    setProject(sel.value);
  }
}).catch(() => {});

// ── list views (memories / thoughts) ─────────────────────────────────────────
function loadList(view) {
  const el = document.getElementById('list-content');
  if (!currentProject) {
    el.innerHTML = '<p style="color:#8b949e;padding:1rem">Select a project</p>';
    return;
  }
  el.innerHTML = '<p style="color:#8b949e;padding:1rem">Loading…</p>';
  if (view === 'memories') {
    fetch('/api/memories?project_id=' + encodeURIComponent(currentProject) + '&limit=100')
      .then(r => r.json()).then(renderMemories)
      .catch(e => { el.innerHTML = '<p style="color:#f85149">Error: ' + escH(String(e)) + '</p>'; });
  } else {
    fetch('/api/thoughts?project_id=' + encodeURIComponent(currentProject))
      .then(r => r.json()).then(renderThoughts)
      .catch(e => { el.innerHTML = '<p style="color:#f85149">Error: ' + escH(String(e)) + '</p>'; });
  }
}

function renderMemories(items) {
  const el = document.getElementById('list-content');
  if (!items || !items.length) {
    el.innerHTML = '<p style="color:#8b949e;padding:1rem">No memories</p>';
    return;
  }
  el.innerHTML = items.map(m =>
    '<div class="card"><h2>' + escH(m.title || m.id || 'Memory') + '</h2>' +
    '<pre>' + escH(JSON.stringify(m, null, 2)) + '</pre></div>'
  ).join('');
}

function renderThoughts(data) {
  const el = document.getElementById('list-content');
  const kinds = data.kinds || {};
  const parts = Object.entries(kinds).map(([k, items]) =>
    '<div class="card"><h2>' + escH(k) + ' (' + items.length + ')</h2>' +
    items.map(it => '<pre>' + escH(JSON.stringify(it, null, 2)) + '</pre>').join('') +
    '</div>'
  );
  el.innerHTML = parts.length
    ? parts.join('')
    : '<p style="color:#8b949e;padding:1rem">No thoughts</p>';
}

function escH(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

// ── node detail panel ─────────────────────────────────────────────────────────
function showNodeDetail(n) {
  const el = document.getElementById('node-detail');
  el.style.display = 'block';
  const rows = [
    ['kind', n.kind || '—'],
    ['degree', n._deg || 0],
    ['file', n.file || '—'],
  ];
  el.innerHTML = '<h3>' + escH(n.label || n.id) + '</h3>' +
    rows.map(([k, v]) =>
      '<div class="kv"><span class="k">' + k + '</span><span>' + escH(String(v)) + '</span></div>'
    ).join('');
}

function clearNodeDetail() {
  const el = document.getElementById('node-detail');
  el.style.display = 'none';
  el.innerHTML = '';
}

// ── canvas graph renderer ─────────────────────────────────────────────────────
const canvas = document.getElementById('graph-canvas');
const ctx = canvas.getContext('2d');
let simNodes = [], simEdges = [], sim = null, qt = null;
let transform = d3.zoomIdentity;
let hoveredNode = null, selectedNode = null;
let rafPending = false;
let searchTerm = '';

function resizeCanvas() {
  const r = canvas.parentElement.getBoundingClientRect();
  canvas.width = r.width * devicePixelRatio;
  canvas.height = r.height * devicePixelRatio;
  canvas.style.width = r.width + 'px';
  canvas.style.height = r.height + 'px';
}
resizeCanvas();
window.addEventListener('resize', () => { resizeCanvas(); schedDraw(); });

// zoom / pan
const zoom = d3.zoom()
  .scaleExtent([0.03, 12])
  .on('zoom', e => { transform = e.transform; schedDraw(); });
d3.select(canvas).call(zoom);

function schedDraw() {
  if (!rafPending) { rafPending = true; requestAnimationFrame(draw); }
}

function draw() {
  rafPending = false;
  const w = canvas.width, h = canvas.height;
  const dpr = devicePixelRatio;
  const cw = w / dpr, ch = h / dpr;

  ctx.save();
  ctx.clearRect(0, 0, w, h);
  ctx.scale(dpr, dpr);

  // apply pan/zoom
  ctx.save();
  ctx.translate(transform.x, transform.y);
  ctx.scale(transform.k, transform.k);

  // viewport bounds in sim coordinates
  const vx0 = -transform.x / transform.k;
  const vy0 = -transform.y / transform.k;
  const vx1 = (cw - transform.x) / transform.k;
  const vy1 = (ch - transform.y) / transform.k;

  // edges
  ctx.strokeStyle = '#30363d';
  ctx.lineWidth = 1 / transform.k;
  ctx.globalAlpha = 0.45;
  ctx.beginPath();
  for (const e of simEdges) {
    const s = e.source, t = e.target;
    if (!s || !t || s.x == null) continue;
    // rough viewport cull: skip if both endpoints far outside
    if (s.x < vx0 - 100 && t.x < vx0 - 100) continue;
    if (s.x > vx1 + 100 && t.x > vx1 + 100) continue;
    ctx.moveTo(s.x, s.y);
    ctx.lineTo(t.x, t.y);
  }
  ctx.stroke();
  ctx.globalAlpha = 1;

  // nodes
  const nr = Math.max(2.5, 5 / transform.k);
  for (const n of simNodes) {
    if (n.x < vx0 - 20 || n.x > vx1 + 20 || n.y < vy0 - 20 || n.y > vy1 + 20) continue;
    const isSelected = n === selectedNode;
    const isHovered = n === hoveredNode;
    const isSearch = searchTerm && (n.label || n.id || '').toLowerCase().includes(searchTerm);

    ctx.beginPath();
    ctx.arc(n.x, n.y, isSelected ? nr * 1.8 : (isHovered ? nr * 1.4 : nr), 0, 2 * Math.PI);
    ctx.fillStyle = isSelected ? '#f0a500'
      : isSearch ? '#e879f9'
      : isHovered ? '#ffffff'
      : (n.kind === 'file' ? '#2563eb' : '#16a34a');
    ctx.fill();
    if (isSelected || isSearch) {
      ctx.strokeStyle = isSelected ? '#f0a500' : '#e879f9';
      ctx.lineWidth = 2 / transform.k;
      ctx.globalAlpha = 0.35;
      ctx.beginPath();
      ctx.arc(n.x, n.y, nr * 2.8, 0, 2 * Math.PI);
      ctx.stroke();
      ctx.globalAlpha = 1;
    }
  }

  // labels
  const showAllLabels = transform.k > 1.5 || simNodes.length < 200;
  ctx.font = Math.max(8, 10 / transform.k) + 'px system-ui';
  ctx.fillStyle = '#c9d1d9';
  for (const n of simNodes) {
    if (n.x < vx0 - 20 || n.x > vx1 + 20 || n.y < vy0 - 20 || n.y > vy1 + 20) continue;
    const label = n.label || n.id;
    const isSearch = searchTerm && label.toLowerCase().includes(searchTerm);
    if (showAllLabels || (n._deg || 0) >= 10 || isSearch || n === selectedNode || n === hoveredNode) {
      ctx.fillText(label, n.x + nr + 2, n.y + 3 / transform.k);
    }
  }

  ctx.restore(); // undo translate/scale
  ctx.restore(); // undo dpr scale
}

// quadtree hit testing
function buildQT() {
  qt = d3.quadtree().x(d => d.x).y(d => d.y).addAll(simNodes);
}

function nodeAt(ex, ey) {
  if (!qt) return null;
  const sx = (ex - transform.x) / transform.k;
  const sy = (ey - transform.y) / transform.k;
  const r = Math.max(10, 5 / transform.k);
  return qt.find(sx, sy, r) || null;
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
    showNodeDetail(n);
    setStatus((n.kind === 'file' ? '📄' : '◆') + ' ' + (n.label || n.id) + ' · degree ' + (n._deg || 0));
  } else {
    clearNodeDetail();
  }
  schedDraw();
});

// search
const searchInput = document.getElementById('search');
searchInput.addEventListener('input', e => {
  searchTerm = e.target.value.trim().toLowerCase();
  schedDraw();
});
document.addEventListener('keydown', e => {
  if ((e.ctrlKey || e.metaKey) && e.key === 'f' && currentView === 'graph') {
    e.preventDefault();
    searchInput.focus();
  }
  if (e.key === 'Escape') { searchInput.value = ''; searchTerm = ''; schedDraw(); }
});

// status bar
function setStatus(msg) {
  document.getElementById('statusbar').textContent = msg;
}

// SSE progress
let sse = null;
function startSSE() {
  if (sse) { sse.close(); sse = null; }
  sse = new EventSource('/api/graphify/progress');
  sse.onmessage = e => {
    const p = JSON.parse(e.data);
    if (!selectedNode) {
      setStatus(p.done
        ? '✓ ' + p.nodes + ' nodes · ' + p.edges + ' edges · ' + p.communities + ' communities'
        : (p.phase || 'running') + '…');
    }
    if (p.done) { if (sse) { sse.close(); sse = null; } if (!simNodes.length) loadGraph(); }
  };
  sse.onerror = () => { if (sse) { sse.close(); sse = null; } };
}

function loadGraph() {
  let url = '/api/graphify/graph.json';
  if (currentProject) url += '?project_id=' + encodeURIComponent(currentProject);
  setStatus('Loading graph…');
  fetch(url).then(r => r.json()).then(g => {
    if (!g || !g.nodes || !g.nodes.length) {
      setStatus('No graph data — run graphify first');
      return;
    }
    // compute degree for label and hit-test priority
    const deg = {};
    for (const e of (g.edges || [])) {
      deg[e.source] = (deg[e.source] || 0) + 1;
      deg[e.target] = (deg[e.target] || 0) + 1;
    }
    simNodes = g.nodes.map(n => ({ ...n, _deg: deg[n.id] || 0 }));
    const nodeIdx = Object.fromEntries(simNodes.map(n => [n.id, n]));
    simEdges = (g.edges || []).map(e => ({
      source: nodeIdx[e.source] || e.source,
      target: nodeIdx[e.target] || e.target,
    }));

    const cw = canvas.clientWidth, ch = canvas.clientHeight;
    if (sim) sim.stop();
    sim = d3.forceSimulation(simNodes)
      .force('link', d3.forceLink(simEdges).id(d => d.id).distance(60).strength(0.3))
      .force('charge', d3.forceManyBody().strength(-30).distanceMax(200))
      .force('center', d3.forceCenter(cw / 2, ch / 2))
      .alphaDecay(0.02)
      .velocityDecay(0.4)
      .on('tick', () => { buildQT(); schedDraw(); })
      .on('end',  () => { buildQT(); schedDraw(); });

    const total = g.total_nodes || g.nodes.length;
    const cap = total > g.nodes.length ? ' (top ' + g.nodes.length + ' of ' + total + ')' : '';
    setStatus(g.nodes.length + ' nodes · ' + (g.edges || []).length + ' edges' + cap);
    startSSE();
  }).catch(err => setStatus('Error: ' + err));
}

// initialise search bar visibility and kick off SSE
document.getElementById('search-wrap').style.display = '';
startSSE();
</script>
</body>
</html>`
