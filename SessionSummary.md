# Session Summary - codeRAG Autonomous Second Brain

Date: 2026-09-19
Working directory: `/home/khing/Desktop/codeintel/codeRAG`

## Objective

Continue transforming codeRAG into an autonomous local second brain:

- Start indexing automatically without manual triggers.
- Observe project files and index changes incrementally.
- Preserve graph IDs, relationships, cache state, and provenance.
- Provide live operational status and efficiency metrics.
- Keep the existing MCP interface available as an explicit control/debug interface.

## Completed in This Session

### Automatic Daemon and Project Detection

Added the background daemon implementation in `internal/services/daemon.go`:

- `Daemon` discovers projects from configured roots.
- `ProjectDetector` identifies Git roots and common project manifests.
- One `ProjectWorker` is created per detected project.
- Workers have bounded concurrency, maintenance scheduling, active/idle tracking, and last-error reporting.
- `ApplicationFromConfig` constructs and starts the daemon when `watch.enabled` is true.
- `serve` shuts down the daemon and event engine cleanly.

### Filesystem Observation

Added `internal/services/observer.go`:

- Uses `fsnotify` for recursive directory watching.
- Debounces events and coalesces repeated operations per path.
- Emits `FileCreated`, `FileModified`, `FileDeleted`, and `FileRenamed` events through `EventEngine`.
- Tracks watched directories and removes watches when directories disappear.
- Applies configured ignore patterns and built-in ignored directories.
- Generated files are no longer suppressed by the observer; they are indexed and flagged by the indexing service.

### Incremental Indexing

Updated `internal/services/indexing.go`:

- Added `IndexFiles`, which indexes a specific set of changed files.
- Unchanged files are skipped when their stored hash and parser version match.
- Deleted files are removed from the graph with their defined symbols.
- Added `ProjectWorker.dirty` tracking so filesystem events can drive targeted reindexing.
- Periodic maintenance falls back to a full repository walk when there are no dirty files.
- `Indexing.Incremental` and `Watch.IndexOnChange` are wired from configuration.

### Graphify Integration

`IndexRepository` now runs Graphify after indexing and stores a `GraphifyRun` node containing graph JSON and node/edge/community counts. This keeps graph visualization data available to the existing HTTP dashboard and agents.

### Configuration and HTTP Status

Existing configuration now includes watch settings:

- `watch.enabled`
- `watch.interval`
- `watch.index_interval`
- `watch.debounce`
- `watch.index_on_change`
- `watch.max_workers`
- `watch.queue_size`
- `watch.max_watch_dirs`
- `watch.project_scan_depth`
- `watch.project_roots`

The HTTP server exposes project, memory, thought, node, and Graphify progress views.

## Verification Already Run

Before the latest audit, the following passed:

```text
go build ./...
go test ./...
```

The service test suite also passed, including indexing, generated-file, graphify, document extraction, privacy, context, MCP, and verification tests.

The worktree remains uncommitted and contains many changes from the broader autonomous-second-brain milestone.

## Audit Findings

A read-only audit of the daemon/watch/indexing path identified these issues to fix next:

### Critical

1. `Daemon.Start` launches `detectProjects` twice, causing duplicate startup work and a race.
2. `Daemon.Status` reports `event_queue: 0` instead of the actual event queue length.
3. `Daemon.jobs` is never updated, so background job status is always zero.
4. `ProjectDetectInterval` is configured but not used for periodic project discovery.
5. `WatchConfig.IndexInterval` is ignored; the daemon hardcodes five minutes.
6. The core daemon, observer, event engine, project detector, and worker paths have no direct regression coverage.

### High

1. `Observer.emitForPath` can read the watcher after `Stop` has changed it, creating a shutdown race.
2. `EventEngine.Emit` silently drops events when its queue is full.
3. Observer pending-map overflow evicts one nondeterministic map entry and silently loses events.
4. `Daemon.Stop` marks the daemon stopped before workers have finished stopping.
5. A partial `watch:` configuration can lose the default `index_on_change: true` value.
6. Graphify runs on every `IndexRepository` call, including incremental runs.
7. `ProjectWorker.OnEvent` schedules maintenance for every event instead of batching rapid changes.

### Medium

1. Observer overflow eviction should use deterministic FIFO ordering.
2. `Config.Load` returns a fresh default configuration on unmarshal errors rather than retaining the partial result.
3. EventEngine shutdown can leave queued events undelivered.
4. `PersistentGraphRepository.Save` holds its mutex during disk I/O.
5. `indexFile` does not explicitly fail when the project node/path is unavailable.
6. `Daemon.AddProject` silently ignores `filepath.Abs` errors.
7. No end-to-end test covers Observer -> EventEngine -> ProjectWorker -> IndexFiles.

## Current Implementation Risks

- `IndexFiles` currently discards per-file errors and does not restore dirty paths after a failed indexing attempt.
- `IndexFiles` does not resolve graph edges after a changed batch.
- The observer and event engine can lose events under heavy churn.
- Graphify output is generated synchronously during indexing and can make incremental updates expensive.
- Live terminal status is not yet implemented; only HTTP/status snapshots exist.

## Next Steps

1. Fix daemon lifecycle and status accounting:
   - Remove duplicate project detection.
   - Add periodic detection using `ProjectDetectInterval`.
   - Honor `WatchConfig.IndexInterval`.
   - Report real queue and active job counts.
   - Stop workers before marking the daemon fully stopped.
2. Harden event delivery:
   - Add deterministic observer pending queues.
   - Prevent watcher use-after-stop races.
   - Drain or explicitly account for queued events during shutdown.
   - Preserve dirty paths when incremental indexing fails.
3. Make incremental indexing graph-consistent:
   - Resolve affected graph edges after changed batches.
   - Return per-file errors instead of silently discarding them.
   - Run Graphify only for full or explicitly requested runs.
4. Add regression and integration tests for:
   - Project detection and daemon start/stop.
   - Observer debounce, ignore patterns, rename/delete handling, and shutdown.
   - EventEngine delivery and queue behavior.
   - Dirty-file batching and incremental indexing.
   - End-to-end filesystem change to graph update.
5. Add the requested live terminal status:
   - A `codergag watch`/status mode or stderr dashboard for interactive use.
   - Show daemon state, projects, active jobs, event queue, files changed, throughput, errors, and cache efficiency.
   - Keep MCP stdout JSON-clean; render terminal UI only on stderr.

## Relevant Files

- `internal/services/daemon.go`
- `internal/services/observer.go`
- `internal/services/events.go`
- `internal/services/project.go`
- `internal/services/indexing.go`
- `internal/services/app.go`
- `internal/services/graphify.go`
- `internal/services/web.go`
- `internal/config/config.go`
- `cmd/codergag/main.go`
- `cmd/codergag/report.go`
