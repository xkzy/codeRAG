# Web Visualize Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the separate `/` dashboard and `/graphify` pages with a single unified SPA that uses canvas-based D3 rendering (no SVG DOM) for high performance at 1500+ nodes / 17k+ edges, and update the exported `graph.html` to use the same canvas renderer.

**Architecture:** A single Go HTML string constant (`unifiedHTML`) serves all routes at `/` and `/graphify` (both redirect to `/`). The SPA has a three-panel layout: left sidebar (nav + project selector), centre canvas (force graph), right detail panel (memories / thoughts / node inspector). D3 force simulation drives physics math only; pixels are drawn each tick on a `<canvas>` element using quadtree spatial indexing for hit-testing. The `graph.html` static export uses an identical self-contained canvas renderer with the graph data inlined as JSON.

**Tech Stack:** Go 1.21+, D3 v7 (CDN, simulation + quadtree modules only), vanilla JS, `<canvas>` 2D API.

## Global Constraints

- No new Go dependencies.
- No build step — all HTML/JS/CSS lives inside Go string constants or inline `fmt.Sprintf` output.
- D3 loaded from `https://d3js.org/d3.v7.min.js` (already in use).
- All routes remain backward-compatible: `/api/projects`, `/api/memories`, `/api/thoughts`, `/api/nodes`, `/api/graphify/progress`, `/api/graphify/graph.json` — no API changes.
- `/graphify` route is kept as a redirect to `/` (or serves the same HTML) so existing bookmarks work.
- `TestHTTPServerDashboard` checks for `"codeRAG"` in the body — the new unified page title must include the string `codeRAG`.
- `go test ./...` must pass before committing each task.
- `go build ./...` must pass before committing each task.

---

### Task 1: Unified SPA shell — routing, layout, project selector

**Files:**
- Modify: `internal/services/web.go` (replace `dashboardHTML`, `graphifyPageHTML` constants; update route handlers)

**Interfaces:**
- Produces: `unifiedHTML` Go string constant (one large backtick string); `handleDashboard` serves it at `/`; `handleGraphifyPage` serves a `301 /` redirect.
- Produces: global JS object `codeRAG` with methods `setView(name)`, `setProject(id)`, `views: {graph, memories, thoughts}`.

- [ ] **Step 1: Write the failing test**

  Update `TestHTTPServerDashboard` to check for `"codeRAG"` (the new title) instead of `"Agent View"`:

  ```go
  // in internal/services/web_test.go, update existing test:
  func TestHTTPServerDashboard(t *testing.T) {
      app := ApplicationInMemory()
      srv := NewHTTPServer(app, ":0")
      ts := httptest.NewServer(srv.mux)
      defer ts.Close()

      resp, err := http.Get(ts.URL + "/")
      if err != nil { t.Fatal(err) }
      defer resp.Body.Close()
      if resp.StatusCode != http.StatusOK {
          t.Fatalf("status: %d", resp.StatusCode)
      }
      body, _ := io.ReadAll(resp.Body)
      if !strings.Contains(string(body), "codeRAG") {
          t.Fatal("unified page missing codeRAG title")
      }
  }
  ```

  Add a new test for the `/graphify` redirect:

  ```go
  func TestHTTPServerGraphifyRedirects(t *testing.T) {
      app := ApplicationInMemory()
      srv := NewHTTPServer(app, ":0")
      ts := httptest.NewServer(srv.mux)
      defer ts.Close()

      client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
          return http.ErrUseLastResponse // don't follow
      }}
      resp, err := client.Get(ts.URL + "/graphify")
      if err != nil { t.Fatal(err) }
      resp.Body.Close()
      if resp.StatusCode != http.StatusMovedPermanently {
          t.Fatalf("expected 301, got %d", resp.StatusCode)
      }
      if loc := resp.Header.Get("Location"); loc != "/" {
          t.Fatalf("expected Location: /, got %s", loc)
      }
  }
  ```

- [ ] **Step 2: Run the tests to verify they fail**

  ```bash
  cd /home/khing/Desktop/codeintel/codeRAG
  go test ./internal/services/ -run "TestHTTPServerDashboard|TestHTTPServerGraphifyRedirects" -v
  ```

  Expected: `TestHTTPServerDashboard` FAIL ("Agent View" is present but not "codeRAG" yet), `TestHTTPServerGraphifyRedirects` FAIL (no redirect).

- [ ] **Step 3: Replace handlers in `internal/services/web.go`**

  Replace `handleDashboard` and `handleGraphifyPage`:

  ```go
  func (s *HTTPServer) handleDashboard(w http.ResponseWriter, r *http.Request) {
      if r.URL.Path != "/" {
          http.NotFound(w, r)
          return
      }
      w.Header().Set("Content-Type", "text/html; charset=utf-8")
      fmt.Fprint(w, unifiedHTML)
  }

  func (s *HTTPServer) handleGraphifyPage(w http.ResponseWriter, r *http.Request) {
      http.Redirect(w, r, "/", http.StatusMovedPermanently)
  }
  ```

  Replace the `dashboardHTML` and `graphifyPageHTML` constants with a single `unifiedHTML` constant. The initial version of `unifiedHTML` is the **shell only** — correct structure but placeholder view content:

  ```go
  const unifiedHTML = `<!DOCTYPE html>
  <html lang="en">
  <head>
  <meta charset="utf-8">
  <title>codeRAG</title>
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <style>
  *{box-sizing:border-box;margin:0;padding:0}
  body{font-family:system-ui,sans-serif;background:#0d1117;color:#c9d1d9;display:flex;height:100vh;overflow:hidden}
  /* sidebar */
  #sidebar{width:220px;min-width:220px;background:#161b22;border-right:1px solid #30363d;display:flex;flex-direction:column;padding:1rem;gap:.75rem}
  #sidebar h1{font-size:1rem;color:#58a6ff;letter-spacing:.05em}
  #sidebar select,#sidebar input{width:100%;font:inherit;font-size:.8rem;background:#0d1117;color:#c9d1d9;border:1px solid #30363d;border-radius:4px;padding:.35rem .5rem}
  .nav-btn{display:block;width:100%;text-align:left;font:inherit;font-size:.85rem;padding:.4rem .6rem;border:none;border-radius:4px;cursor:pointer;background:transparent;color:#8b949e}
  .nav-btn:hover{background:#21262d;color:#c9d1d9}
  .nav-btn.active{background:#21262d;color:#58a6ff;font-weight:600}
  /* main */
  #main{flex:1;position:relative;overflow:hidden}
  .view{position:absolute;inset:0;display:none}
  .view.active{display:block}
  /* canvas view */
  #canvas-view canvas{display:block;width:100%;height:100%}
  /* list view */
  #list-view{overflow-y:auto;padding:1.5rem;display:flex;flex-direction:column;gap:1rem}
  .card{background:#161b22;border:1px solid #30363d;border-radius:8px;padding:1rem}
  .card h2{font-size:.95rem;color:#58a6ff;margin-bottom:.5rem}
  pre{font-size:.75rem;white-space:pre-wrap;word-break:break-word;color:#8b949e;max-height:300px;overflow-y:auto}
  /* status bar */
  #statusbar{position:absolute;bottom:.5rem;left:50%;transform:translateX(-50%);background:#161b22cc;border:1px solid #30363d;border-radius:4px;padding:.25rem .75rem;font-size:.75rem;color:#8b949e;pointer-events:none}
  </style>
  </head>
  <body>
  <div id="sidebar">
    <h1>codeRAG</h1>
    <select id="proj-select"><option value="">— project —</option></select>
    <hr style="border-color:#30363d">
    <button class="nav-btn active" data-view="graph">⬡ Graph</button>
    <button class="nav-btn" data-view="memories">◈ Memories</button>
    <button class="nav-btn" data-view="thoughts">◎ Thoughts</button>
  </div>
  <div id="main">
    <div id="canvas-view" class="view active"><canvas id="graph-canvas"></canvas></div>
    <div id="list-view" class="view"><div id="list-content"><p style="color:#8b949e;padding:1rem">Select a project</p></div></div>
    <div id="statusbar">no project selected</div>
  </div>
  <script src="https://d3js.org/d3.v7.min.js"></script>
  <script>
  // ── routing ──────────────────────────────────────────────────────────────
  let currentView = 'graph', currentProject = '';
  function setView(name) {
    currentView = name;
    document.querySelectorAll('.nav-btn').forEach(b => b.classList.toggle('active', b.dataset.view === name));
    document.getElementById('canvas-view').classList.toggle('active', name === 'graph');
    document.getElementById('list-view').classList.toggle('active', name !== 'graph');
    if (name !== 'graph') loadList(name);
  }
  function setProject(id) {
    currentProject = id;
    if (currentView === 'graph') loadGraph();
    else loadList(currentView);
  }
  document.querySelectorAll('.nav-btn').forEach(b => b.addEventListener('click', () => setView(b.dataset.view)));
  document.getElementById('proj-select').addEventListener('change', e => setProject(e.target.value));

  // ── project list ─────────────────────────────────────────────────────────
  fetch('/api/projects').then(r=>r.json()).then(ps=>{
    const sel = document.getElementById('proj-select');
    ps.forEach(p => { const o=document.createElement('option'); o.value=p.id||p.project_id||''; o.textContent=o.value; sel.appendChild(o); });
    if (ps.length===1) { sel.value=ps[0].id||ps[0].project_id||''; setProject(sel.value); }
  }).catch(()=>{});

  // ── list view (memories / thoughts) ──────────────────────────────────────
  function loadList(view) {
    const el = document.getElementById('list-content');
    if (!currentProject) { el.innerHTML='<p style="color:#8b949e;padding:1rem">Select a project</p>'; return; }
    el.innerHTML='<p style="color:#8b949e;padding:1rem">Loading…</p>';
    if (view==='memories') {
      fetch('/api/memories?project_id='+encodeURIComponent(currentProject)+'&limit=100')
        .then(r=>r.json()).then(data=>renderMemories(data)).catch(e=>{ el.innerHTML='<p style="color:#f85149">'+e+'</p>'; });
    } else {
      fetch('/api/thoughts?project_id='+encodeURIComponent(currentProject))
        .then(r=>r.json()).then(data=>renderThoughts(data)).catch(e=>{ el.innerHTML='<p style="color:#f85149">'+e+'</p>'; });
    }
  }
  function renderMemories(items) {
    const el = document.getElementById('list-content');
    if (!items||!items.length){el.innerHTML='<p style="color:#8b949e;padding:1rem">No memories</p>';return;}
    el.innerHTML=items.map(m=>'<div class="card"><h2>'+(m.title||m.id||'Memory')+'</h2><pre>'+JSON.stringify(m,null,2)+'</pre></div>').join('');
  }
  function renderThoughts(data) {
    const el = document.getElementById('list-content');
    const kinds = data.kinds||{};
    const parts = Object.entries(kinds).map(([k,items])=>'<div class="card"><h2>'+k+' ('+items.length+')</h2>'+
      items.map(it=>'<pre>'+JSON.stringify(it,null,2)+'</pre>').join('')+'</div>');
    el.innerHTML = parts.length ? parts.join('') : '<p style="color:#8b949e;padding:1rem">No thoughts</p>';
  }

  // ── canvas graph ──────────────────────────────────────────────────────────
  function loadGraph() { /* implemented in Task 2 */ }
  </script>
  </body>
  </html>`
  ```

- [ ] **Step 4: Run tests to verify they pass**

  ```bash
  cd /home/khing/Desktop/codeintel/codeRAG
  go test ./internal/services/ -run "TestHTTPServerDashboard|TestHTTPServerGraphifyRedirects|TestHTTPServerMemories|TestHTTPServerProjects|TestHTTPServerThoughts|TestHTTPServerStartBackgroundSharedPort" -v
  ```

  Expected: all PASS.

- [ ] **Step 5: Build and commit**

  ```bash
  cd /home/khing/Desktop/codeintel/codeRAG
  go build ./...
  git add internal/services/web.go internal/services/web_test.go
  git commit -m "feat(web): unified SPA shell — sidebar, routing, project selector, list views"
  ```

---

### Task 2: Canvas graph renderer with D3 force simulation

**Files:**
- Modify: `internal/services/web.go` — replace the `// implemented in Task 2` stub inside `unifiedHTML` with the full canvas renderer JS.

**Interfaces:**
- Consumes: `/api/graphify/graph.json?project_id=<pid>&limit=1500` (returns `{nodes:[{id,label,kind}], edges:[{source,target}], total_nodes}`)
- Consumes: `/api/graphify/progress` (SSE stream, messages contain `{phase,files_seen,nodes,edges,communities,done,error}`)
- Produces: `loadGraph()` global function that fetches graph data and starts/restarts the simulation.
- Produces: canvas renders nodes as circles (file=blue `#2563eb`, concept=green `#16a34a`); edges as grey lines; node labels at zoom > 1.5.
- Produces: zoom/pan via D3 zoom; scroll wheel zooms, drag pans.
- Produces: click on a node opens a small info box in the sidebar footer area.
- Produces: `#statusbar` shows node/edge counts and SSE phase.

The canvas renderer must handle the current production data set (1473 nodes, 17322 edges) at ≥30fps. It does this by:
1. Running D3 `forceSimulation` in alpha-decay mode with `alphaDecay(0.02)` and `velocityDecay(0.4)`.
2. Drawing all edges first, then all nodes, then labels only for nodes visible in the viewport and with degree ≥ 5 when zoom < 1.5.
3. Using a D3 quadtree for O(log n) hit detection on click/hover.
4. Calling `requestAnimationFrame` on each simulation `tick`, not `setInterval`.

- [ ] **Step 1: Write the failing test**

  Add to `internal/services/web_test.go`:

  ```go
  func TestUnifiedHTMLContainsCanvasAndD3(t *testing.T) {
      if !strings.Contains(unifiedHTML, "<canvas") {
          t.Error("unifiedHTML must contain a <canvas> element")
      }
      if !strings.Contains(unifiedHTML, "d3.v7.min.js") {
          t.Error("unifiedHTML must load D3")
      }
      if !strings.Contains(unifiedHTML, "forceSimulation") {
          t.Error("unifiedHTML must use d3.forceSimulation")
      }
      if !strings.Contains(unifiedHTML, "requestAnimationFrame") {
          t.Error("unifiedHTML must use requestAnimationFrame for canvas rendering")
      }
  }
  ```

- [ ] **Step 2: Run to verify it fails**

  ```bash
  cd /home/khing/Desktop/codeintel/codeRAG
  go test ./internal/services/ -run TestUnifiedHTMLContainsCanvasAndD3 -v
  ```

  Expected: FAIL (`forceSimulation` and `requestAnimationFrame` not yet in the shell HTML).

- [ ] **Step 3: Implement the canvas renderer in `unifiedHTML`**

  Find the line `function loadGraph() { /* implemented in Task 2 */ }` in the `unifiedHTML` constant and **replace it and all surrounding script content** with the full implementation below. Replace the entire `<script>` block (the one after `<script src="https://d3js.org/d3.v7.min.js"></script>`) with:

  ```html
  <script src="https://d3js.org/d3.v7.min.js"></script>
  <script>
  // ── routing ───────────────────────────────────────────────────────────────
  let currentView = 'graph', currentProject = '';
  function setView(name) {
    currentView = name;
    document.querySelectorAll('.nav-btn').forEach(b => b.classList.toggle('active', b.dataset.view === name));
    document.getElementById('canvas-view').classList.toggle('active', name === 'graph');
    document.getElementById('list-view').classList.toggle('active', name !== 'graph');
    if (name !== 'graph') loadList(name);
  }
  function setProject(id) {
    currentProject = id;
    if (currentView === 'graph') loadGraph();
    else loadList(currentView);
  }
  document.querySelectorAll('.nav-btn').forEach(b => b.addEventListener('click', () => setView(b.dataset.view)));
  document.getElementById('proj-select').addEventListener('change', e => setProject(e.target.value));

  // ── project list ──────────────────────────────────────────────────────────
  fetch('/api/projects').then(r=>r.json()).then(ps=>{
    const sel = document.getElementById('proj-select');
    ps.forEach(p => {
      const id = p.id||p.project_id||'';
      const o = document.createElement('option'); o.value=id; o.textContent=id; sel.appendChild(o);
    });
    if (ps.length===1) { sel.value=ps[0].id||ps[0].project_id||''; setProject(sel.value); }
  }).catch(()=>{});

  // ── list view ─────────────────────────────────────────────────────────────
  function loadList(view) {
    const el = document.getElementById('list-content');
    if (!currentProject) { el.innerHTML='<p style="color:#8b949e;padding:1rem">Select a project</p>'; return; }
    el.innerHTML='<p style="color:#8b949e;padding:1rem">Loading…</p>';
    if (view==='memories') {
      fetch('/api/memories?project_id='+encodeURIComponent(currentProject)+'&limit=100')
        .then(r=>r.json()).then(renderMemories).catch(e=>{ el.innerHTML='<p style="color:#f85149">'+e+'</p>'; });
    } else {
      fetch('/api/thoughts?project_id='+encodeURIComponent(currentProject))
        .then(r=>r.json()).then(renderThoughts).catch(e=>{ el.innerHTML='<p style="color:#f85149">'+e+'</p>'; });
    }
  }
  function renderMemories(items) {
    const el = document.getElementById('list-content');
    if (!items||!items.length){el.innerHTML='<p style="color:#8b949e;padding:1rem">No memories</p>';return;}
    el.innerHTML=items.map(m=>'<div class="card"><h2>'+(m.title||m.id||'Memory')+'</h2><pre>'+escH(JSON.stringify(m,null,2))+'</pre></div>').join('');
  }
  function renderThoughts(data) {
    const el = document.getElementById('list-content');
    const kinds = data.kinds||{};
    const parts = Object.entries(kinds).map(([k,items])=>'<div class="card"><h2>'+k+' ('+items.length+')</h2>'+
      items.map(it=>'<pre>'+escH(JSON.stringify(it,null,2))+'</pre>').join('')+'</div>');
    el.innerHTML = parts.length ? parts.join('') : '<p style="color:#8b949e;padding:1rem">No thoughts</p>';
  }
  function escH(s){return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');}

  // ── canvas graph ──────────────────────────────────────────────────────────
  const canvas = document.getElementById('graph-canvas');
  const ctx = canvas.getContext('2d');
  let simNodes = [], simEdges = [], sim = null, qt = null;
  let transform = d3.zoomIdentity;
  let hoveredNode = null, selectedNode = null;
  let rafPending = false;

  // size canvas to container
  function resizeCanvas() {
    const r = canvas.parentElement.getBoundingClientRect();
    canvas.width = r.width * devicePixelRatio;
    canvas.height = r.height * devicePixelRatio;
    canvas.style.width = r.width + 'px';
    canvas.style.height = r.height + 'px';
  }
  resizeCanvas();
  window.addEventListener('resize', () => { resizeCanvas(); schedDraw(); });

  // zoom/pan
  const zoom = d3.zoom()
    .scaleExtent([0.05, 8])
    .on('zoom', e => { transform = e.transform; schedDraw(); });
  d3.select(canvas).call(zoom);

  // schedule one rAF draw (coalesce multiple tick callbacks)
  function schedDraw() {
    if (!rafPending) { rafPending = true; requestAnimationFrame(draw); }
  }

  function draw() {
    rafPending = false;
    const w = canvas.width, h = canvas.height;
    ctx.save();
    ctx.clearRect(0, 0, w, h);
    ctx.scale(devicePixelRatio, devicePixelRatio);
    const cw = w / devicePixelRatio, ch = h / devicePixelRatio;
    ctx.save();
    ctx.translate(transform.x, transform.y);
    ctx.scale(transform.k, transform.k);

    // edges
    ctx.strokeStyle = '#30363d';
    ctx.lineWidth = 1 / transform.k;
    ctx.globalAlpha = 0.5;
    ctx.beginPath();
    for (const e of simEdges) {
      if (e.source && e.target && e.source.x != null) {
        ctx.moveTo(e.source.x, e.source.y);
        ctx.lineTo(e.target.x, e.target.y);
      }
    }
    ctx.stroke();
    ctx.globalAlpha = 1;

    // nodes
    const r = Math.max(3, 5 / transform.k);
    for (const n of simNodes) {
      ctx.beginPath();
      ctx.arc(n.x, n.y, r, 0, 2*Math.PI);
      ctx.fillStyle = n === selectedNode ? '#f0a500' : (n === hoveredNode ? '#fff' : (n.kind==='file' ? '#2563eb' : '#16a34a'));
      ctx.fill();
    }

    // labels (only when zoomed in or high-degree nodes)
    if (transform.k > 1.5 || simNodes.length < 200) {
      ctx.font = (10 / transform.k) + 'px system-ui';
      ctx.fillStyle = '#c9d1d9';
      const [x0,y0,x1,y1] = [(-transform.x)/transform.k, (-transform.y)/transform.k,
                              (cw-transform.x)/transform.k, (ch-transform.y)/transform.k];
      for (const n of simNodes) {
        if (n.x < x0 || n.x > x1 || n.y < y0 || n.y > y1) continue;
        ctx.fillText(n.label || n.id, n.x + r + 2, n.y + 3);
      }
    } else {
      // high-degree labels only
      ctx.font = (10 / transform.k) + 'px system-ui';
      ctx.fillStyle = '#c9d1d9';
      for (const n of simNodes) {
        if ((n._deg||0) >= 10) ctx.fillText(n.label||n.id, n.x + r + 2, n.y + 3);
      }
    }

    ctx.restore(); // undo translate/scale
    ctx.restore(); // undo devicePixelRatio scale
  }

  // hit testing via quadtree
  function buildQT() {
    qt = d3.quadtree().x(d=>d.x).y(d=>d.y).addAll(simNodes);
  }
  function nodeAt(ex, ey) {
    if (!qt) return null;
    // convert canvas pixel -> simulation coordinate
    const sx = (ex - transform.x) / transform.k;
    const sy = (ey - transform.y) / transform.k;
    const r = Math.max(8, 5 / transform.k);
    return qt.find(sx, sy, r) || null;
  }
  canvas.addEventListener('mousemove', e => {
    const rect = canvas.getBoundingClientRect();
    const n = nodeAt(e.clientX - rect.left, e.clientY - rect.top);
    if (n !== hoveredNode) { hoveredNode = n; schedDraw(); }
    canvas.style.cursor = n ? 'pointer' : 'default';
  });
  canvas.addEventListener('click', e => {
    const rect = canvas.getBoundingClientRect();
    const n = nodeAt(e.clientX - rect.left, e.clientY - rect.top);
    selectedNode = n;
    if (n) showNodeInfo(n);
    schedDraw();
  });

  function showNodeInfo(n) {
    const sb = document.getElementById('statusbar');
    sb.style.pointerEvents = 'auto';
    sb.textContent = (n.kind==='file'?'📄':'◆') + ' ' + (n.label||n.id) + ' — degree ' + (n._deg||0);
  }

  // SSE progress
  let sse = null;
  function startSSE() {
    if (sse) sse.close();
    sse = new EventSource('/api/graphify/progress');
    sse.onmessage = e => {
      const p = JSON.parse(e.data);
      const sb = document.getElementById('statusbar');
      if (!selectedNode) sb.textContent = p.done ? ('✓ ' + p.nodes + ' nodes · ' + p.edges + ' edges · ' + p.communities + ' communities') : (p.phase||'running') + '…';
      if (p.done) { sse.close(); sse=null; if (!simNodes.length) loadGraph(); }
    };
    sse.onerror = () => { sse&&sse.close(); sse=null; };
  }

  function loadGraph() {
    const url = '/api/graphify/graph.json' + (currentProject ? '?project_id='+encodeURIComponent(currentProject) : '');
    document.getElementById('statusbar').textContent = 'Loading graph…';
    fetch(url).then(r=>r.json()).then(g => {
      if (!g||!g.nodes||!g.nodes.length) {
        document.getElementById('statusbar').textContent = 'No graph data — run graphify first';
        return;
      }
      // compute degree
      const deg = {};
      for (const e of (g.edges||[])) { deg[e.source]=(deg[e.source]||0)+1; deg[e.target]=(deg[e.target]||0)+1; }
      simNodes = g.nodes.map(n => ({...n, _deg: deg[n.id]||0}));
      const nodeIdx = Object.fromEntries(simNodes.map(n=>[n.id,n]));
      simEdges = (g.edges||[]).map(e=>({source:nodeIdx[e.source]||e.source, target:nodeIdx[e.target]||e.target}));

      const cw = canvas.clientWidth, ch = canvas.clientHeight;
      if (sim) sim.stop();
      sim = d3.forceSimulation(simNodes)
        .force('link', d3.forceLink(simEdges).id(d=>d.id).distance(60).strength(0.3))
        .force('charge', d3.forceManyBody().strength(-30).distanceMax(200))
        .force('center', d3.forceCenter(cw/2, ch/2))
        .alphaDecay(0.02)
        .velocityDecay(0.4)
        .on('tick', () => { buildQT(); schedDraw(); })
        .on('end', () => { buildQT(); schedDraw(); });

      const total = g.total_nodes || g.nodes.length;
      document.getElementById('statusbar').textContent = g.nodes.length + ' nodes · ' + (g.edges||[]).length + ' edges' + (total > g.nodes.length ? ' (showing top '+g.nodes.length+' of '+total+')' : '');
      startSSE();
    }).catch(err => {
      document.getElementById('statusbar').textContent = 'Error: ' + err;
    });
  }

  // start on load
  startSSE();
  </script>
  ```

  > **Note:** This replaces the two separate `<script>` tags at the end of the shell. The routing, project list, and list view JS from Task 1 is duplicated here intentionally — this is the final, complete version of the script block.

- [ ] **Step 4: Run tests to verify they pass**

  ```bash
  cd /home/khing/Desktop/codeintel/codeRAG
  go test ./internal/services/ -v
  ```

  Expected: all tests PASS including `TestUnifiedHTMLContainsCanvasAndD3`.

- [ ] **Step 5: Build and commit**

  ```bash
  cd /home/khing/Desktop/codeintel/codeRAG
  go build ./...
  git add internal/services/web.go internal/services/web_test.go
  git commit -m "feat(web): canvas-based D3 graph renderer with zoom/pan, hit-test, SSE progress"
  ```

---

### Task 3: Update exported `graph.html` to use canvas renderer

**Files:**
- Modify: `internal/services/graphify.go` — replace `writeGraphHTML` function.

**Interfaces:**
- Consumes: `*Graph` (fields: `Nodes []Node`, `Edges []Edge`) — same struct, no change.
- Produces: a standalone `graph.html` file with nodes/edges inlined as JS constants, same canvas renderer as Task 2 (copy of the draw/simulation code, adapted for standalone use with no `/api/*` fetches).

The standalone file must:
- Load D3 from CDN.
- Inline `const nodes = <JSON>; const edges = <JSON>;` at the top of the script.
- Start the simulation immediately on load (no project selector, no SSE).
- Show node count and edge count in a status bar.
- Support zoom/pan and click-to-inspect.

- [ ] **Step 1: Write the failing test**

  Add to `internal/services/graphify_test.go` (or create it if absent — check first with `cat internal/services/graphify_test.go`):

  ```go
  func TestWriteGraphHTMLUsesCanvas(t *testing.T) {
      g := &Graph{
          Nodes: []Node{{ID: "a", Label: "Alpha", Kind: "file"}, {ID: "b", Label: "Beta", Kind: "concept"}},
          Edges: []Edge{{Source: "a", Target: "b", Relation: "defines", Confidence: "EXTRACTED", ConfidenceScore: 1.0}},
      }
      dir := t.TempDir()
      path := filepath.Join(dir, "graph.html")
      if err := writeGraphHTML(g, path); err != nil {
          t.Fatal(err)
      }
      data, err := os.ReadFile(path)
      if err != nil { t.Fatal(err) }
      body := string(data)
      if !strings.Contains(body, "<canvas") {
          t.Error("graph.html must use <canvas> not SVG")
      }
      if strings.Contains(body, "<svg") {
          t.Error("graph.html must not use SVG")
      }
      if !strings.Contains(body, "forceSimulation") {
          t.Error("graph.html must use d3.forceSimulation")
      }
      if !strings.Contains(body, `"Alpha"`) {
          t.Error("graph.html must inline node labels")
      }
  }
  ```

  Make sure `graphify_test.go` imports `"os"`, `"path/filepath"`, `"strings"` (add if missing).

- [ ] **Step 2: Run to verify the test fails**

  ```bash
  cd /home/khing/Desktop/codeintel/codeRAG
  go test ./internal/services/ -run TestWriteGraphHTMLUsesCanvas -v
  ```

  Expected: FAIL (current `writeGraphHTML` generates SVG-based D3, not canvas).

- [ ] **Step 3: Replace `writeGraphHTML` in `internal/services/graphify.go`**

  Find and replace the entire `writeGraphHTML` function (lines ~461–479 in the current file) with:

  ```go
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
  canvas{display:block}
  #status{position:fixed;bottom:.5rem;left:50%%;transform:translateX(-50%%);background:#161b22cc;border:1px solid #30363d;border-radius:4px;padding:.25rem .75rem;font:system-ui,.75rem,sans-serif;color:#8b949e;pointer-events:none}
  #info{position:fixed;top:1rem;right:1rem;max-width:260px;background:#161b22;border:1px solid #30363d;border-radius:8px;padding:.75rem;font:system-ui,.8rem,sans-serif;color:#c9d1d9;display:none}
  #info h3{color:#58a6ff;font-size:.85rem;margin-bottom:.4rem}
  </style>
  </head>
  <body>
  <canvas id="g"></canvas>
  <div id="status">loading…</div>
  <div id="info"><h3 id="info-title"></h3><p id="info-body" style="font-size:.75rem;color:#8b949e"></p></div>
  <script src="https://d3js.org/d3.v7.min.js"></script>
  <script>
  const rawNodes = %s;
  const rawEdges = %s;

  const canvas = document.getElementById('g');
  const ctx = canvas.getContext('2d');
  let transform = d3.zoomIdentity;
  let simNodes = [], simEdges = [], qt = null, hoveredNode = null, selectedNode = null;
  let rafPending = false;

  function resize() {
    canvas.width = window.innerWidth * devicePixelRatio;
    canvas.height = window.innerHeight * devicePixelRatio;
    canvas.style.width = window.innerWidth + 'px';
    canvas.style.height = window.innerHeight + 'px';
  }
  resize();
  window.addEventListener('resize', () => { resize(); schedDraw(); });

  const zoom = d3.zoom().scaleExtent([0.05, 8]).on('zoom', e => { transform = e.transform; schedDraw(); });
  d3.select(canvas).call(zoom);

  function schedDraw() {
    if (!rafPending) { rafPending = true; requestAnimationFrame(draw); }
  }

  function draw() {
    rafPending = false;
    const w = canvas.width, h = canvas.height;
    ctx.save();
    ctx.clearRect(0, 0, w, h);
    ctx.scale(devicePixelRatio, devicePixelRatio);
    ctx.save();
    ctx.translate(transform.x, transform.y);
    ctx.scale(transform.k, transform.k);
    const cw = w / devicePixelRatio, ch = h / devicePixelRatio;

    ctx.strokeStyle = '#30363d'; ctx.lineWidth = 1/transform.k; ctx.globalAlpha = 0.5;
    ctx.beginPath();
    for (const e of simEdges) {
      if (e.source && e.target && e.source.x != null) {
        ctx.moveTo(e.source.x, e.source.y); ctx.lineTo(e.target.x, e.target.y);
      }
    }
    ctx.stroke(); ctx.globalAlpha = 1;

    const r = Math.max(3, 5/transform.k);
    for (const n of simNodes) {
      ctx.beginPath(); ctx.arc(n.x, n.y, r, 0, 2*Math.PI);
      ctx.fillStyle = n===selectedNode ? '#f0a500' : (n===hoveredNode ? '#fff' : (n.kind==='file' ? '#2563eb' : '#16a34a'));
      ctx.fill();
    }

    if (transform.k > 1.5 || simNodes.length < 200) {
      const [x0,y0,x1,y1] = [(-transform.x)/transform.k, (-transform.y)/transform.k, (cw-transform.x)/transform.k, (ch-transform.y)/transform.k];
      ctx.font = (10/transform.k) + 'px system-ui'; ctx.fillStyle = '#c9d1d9';
      for (const n of simNodes) {
        if (n.x < x0 || n.x > x1 || n.y < y0 || n.y > y1) continue;
        ctx.fillText(n.label||n.id, n.x+r+2, n.y+3);
      }
    } else {
      ctx.font = (10/transform.k) + 'px system-ui'; ctx.fillStyle = '#c9d1d9';
      for (const n of simNodes) { if ((n._deg||0)>=10) ctx.fillText(n.label||n.id, n.x+r+2, n.y+3); }
    }
    ctx.restore(); ctx.restore();
  }

  function buildQT() { qt = d3.quadtree().x(d=>d.x).y(d=>d.y).addAll(simNodes); }
  function nodeAt(ex, ey) {
    if (!qt) return null;
    const sx=(ex-transform.x)/transform.k, sy=(ey-transform.y)/transform.k;
    return qt.find(sx, sy, Math.max(8, 5/transform.k)) || null;
  }
  canvas.addEventListener('mousemove', e => {
    const rect = canvas.getBoundingClientRect();
    const n = nodeAt(e.clientX-rect.left, e.clientY-rect.top);
    if (n !== hoveredNode) { hoveredNode=n; schedDraw(); }
    canvas.style.cursor = n ? 'pointer' : 'default';
  });
  canvas.addEventListener('click', e => {
    const rect = canvas.getBoundingClientRect();
    const n = nodeAt(e.clientX-rect.left, e.clientY-rect.top);
    selectedNode = n;
    const info = document.getElementById('info');
    if (n) {
      document.getElementById('info-title').textContent = n.label||n.id;
      document.getElementById('info-body').textContent = 'kind: '+n.kind+' | degree: '+(n._deg||0)+(n.file ? '\nfile: '+n.file : '');
      info.style.display='block';
    } else { info.style.display='none'; }
    schedDraw();
  });

  // start simulation
  const deg = {};
  for (const e of rawEdges) { deg[e.source]=(deg[e.source]||0)+1; deg[e.target]=(deg[e.target]||0)+1; }
  simNodes = rawNodes.map(n => ({...n, _deg: deg[n.id]||0}));
  const nodeIdx = Object.fromEntries(simNodes.map(n=>[n.id,n]));
  simEdges = rawEdges.map(e => ({source: nodeIdx[e.source]||e.source, target: nodeIdx[e.target]||e.target}));

  const sim = d3.forceSimulation(simNodes)
    .force('link', d3.forceLink(simEdges).id(d=>d.id).distance(60).strength(0.3))
    .force('charge', d3.forceManyBody().strength(-30).distanceMax(200))
    .force('center', d3.forceCenter(window.innerWidth/2, window.innerHeight/2))
    .alphaDecay(0.02).velocityDecay(0.4)
    .on('tick', () => { buildQT(); schedDraw(); })
    .on('end',  () => { buildQT(); schedDraw(); });

  document.getElementById('status').textContent = rawNodes.length + ' nodes · ' + rawEdges.length + ' edges';
  </script>
  </body>
  </html>`, nodesJSON, edgesJSON)
      return os.WriteFile(path, []byte(html), 0o644)
  }
  ```

  > **Note:** `%%` in `fmt.Sprintf` produces a literal `%%` — use `%%` for any literal `%` inside the format string (e.g., CSS `translateX(-50%%)` and `left:50%%`).

- [ ] **Step 4: Run tests to verify they pass**

  ```bash
  cd /home/khing/Desktop/codeintel/codeRAG
  go test ./internal/services/ -v
  ```

  Expected: all tests PASS.

- [ ] **Step 5: Build and commit**

  ```bash
  cd /home/khing/Desktop/codeintel/codeRAG
  go build ./...
  git add internal/services/graphify.go internal/services/graphify_test.go
  git commit -m "feat(graphify): canvas-based standalone graph.html export with zoom/pan/inspect"
  ```

---

### Task 4: Smoke-test the live server manually

This task has no automated tests — it is a human verification step.

- [ ] **Step 1: Build and run the server**

  ```bash
  cd /home/khing/Desktop/codeintel/codeRAG
  go build -o /tmp/codergag ./cmd/codergag
  /tmp/codergag serve --http=:9090 &
  ```

- [ ] **Step 2: Open the browser and verify the unified SPA**

  Navigate to `http://localhost:9090/`. Verify:
  - Dark sidebar with "codeRAG" title, project selector, and three nav buttons (Graph, Memories, Thoughts).
  - Selecting a project and clicking "Graph" shows the canvas graph with nodes animating into place.
  - Hovering over a node changes the cursor to pointer.
  - Clicking a node shows its label and degree in the status bar.
  - Scroll-wheel zooms; drag pans.
  - Labels appear when zoomed in past ~1.5×.
  - Navigating to `http://localhost:9090/graphify` redirects to `/`.
  - The "Memories" view shows memory cards for the selected project.
  - The "Thoughts" view shows Evidence / Hypothesis / Observation groups.

- [ ] **Step 3: Verify the exported graph.html**

  ```bash
  # run graphify to regenerate graph.html
  /tmp/codergag graphify .
  # open in browser
  xdg-open graphify-out/graph.html
  ```

  Verify: canvas renders, zoom/pan work, clicking a node shows an info panel.

- [ ] **Step 4: Kill the server and commit**

  ```bash
  kill %1 2>/dev/null || true
  ```

  No commit needed — this task is verification only.

---

## Self-Review

**Spec coverage:**
- ✅ Unified SPA replacing `/` and `/graphify` — Task 1
- ✅ Canvas rendering (no SVG DOM) — Tasks 2 & 3
- ✅ D3 force simulation (layout math only) — Tasks 2 & 3
- ✅ Zoom/pan — Tasks 2 & 3
- ✅ Click-to-inspect nodes — Tasks 2 & 3
- ✅ Quadtree hit-testing — Tasks 2 & 3
- ✅ Labels only at zoom > 1.5 or high-degree — Tasks 2 & 3
- ✅ SSE progress in graph view — Task 2
- ✅ Memories view — Task 1 (list view)
- ✅ Thoughts view — Task 1 (list view)
- ✅ Project selector shared across views — Tasks 1 & 2
- ✅ Exported graph.html canvas renderer — Task 3
- ✅ Backward-compat: all `/api/*` routes unchanged — Tasks 1–3
- ✅ `/graphify` redirect — Task 1
- ✅ Existing tests updated — Tasks 1 & 3

**Placeholder scan:** No TBDs or TODOs. All code blocks are complete.

**Type consistency:** `simNodes`, `simEdges`, `nodeAt()`, `buildQT()`, `schedDraw()`, `draw()` used consistently in Tasks 2 and 3.
