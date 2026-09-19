package services

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type EventKind string

const (
	AgentStarted         EventKind = "AgentStarted"
	AgentStopped         EventKind = "AgentStopped"
	SessionStarted       EventKind = "SessionStarted"
	SessionEnded         EventKind = "SessionEnded"
	ProjectDetected      EventKind = "ProjectDetected"
	UserPrompt           EventKind = "UserPrompt"
	AgentResponse        EventKind = "AgentResponse"
	ToolCallStarted      EventKind = "ToolCallStarted"
	ToolCallCompleted    EventKind = "ToolCallCompleted"
	ToolCallFailed       EventKind = "ToolCallFailed"
	FileOpened           EventKind = "FileOpened"
	FileCreated          EventKind = "FileCreated"
	FileModified         EventKind = "FileModified"
	FileDeleted          EventKind = "FileDeleted"
	FileRenamed          EventKind = "FileRenamed"
	CommandExecuted      EventKind = "CommandExecuted"
	BuildStarted         EventKind = "BuildStarted"
	BuildCompleted       EventKind = "BuildCompleted"
	TestStarted          EventKind = "TestStarted"
	TestCompleted        EventKind = "TestCompleted"
	GitCommit            EventKind = "GitCommit"
	GitCheckout          EventKind = "GitCheckout"
	GitBranchChanged     EventKind = "GitBranchChanged"
	SearchPerformed      EventKind = "SearchPerformed"
	SymbolReferenced     EventKind = "SymbolReferenced"
	FunctionInvestigated EventKind = "FunctionInvestigated"
	ErrorObserved        EventKind = "ErrorObserved"
	CompilerError        EventKind = "CompilerError"
	RuntimeError         EventKind = "RuntimeError"
	DebuggerObservation  EventKind = "DebuggerObservation"
)

type Event struct {
	ID        string         `json:"id"`
	Kind      EventKind      `json:"kind"`
	ProjectID string         `json:"project_id,omitempty"`
	Agent     string         `json:"agent,omitempty"`
	Session   string         `json:"session,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Payload   map[string]any `json:"payload,omitempty"`
}

type EventWorker interface {
	OnEvent(event Event)
}

type eventJob struct {
	event  Event
	worker EventWorker
}

type EventEngine struct {
	mu          sync.RWMutex
	startMu     sync.Mutex
	workers     []EventWorker
	queue       chan Event
	jobs        chan eventJob
	buffer      []Event
	maxBuf      int
	workerCount int
	stopCh      chan struct{}
	doneCh      chan struct{}
	running     bool
	metrics     map[string]int
	nextID      uint64
}

func NewEventEngine(maxBuf int, workerCounts ...int) *EventEngine {
	if maxBuf <= 0 {
		maxBuf = 1024
	}
	workerCount := 4
	if len(workerCounts) > 0 && workerCounts[0] > 0 {
		workerCount = workerCounts[0]
	}
	return &EventEngine{
		queue:       make(chan Event, maxBuf),
		jobs:        make(chan eventJob, maxBuf),
		maxBuf:      maxBuf,
		workerCount: workerCount,
		metrics:     map[string]int{"worker_count": workerCount},
	}
}

func (e *EventEngine) Start() {
	e.startMu.Lock()
	defer e.startMu.Unlock()
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	e.stopCh = stopCh
	e.doneCh = doneCh
	e.jobs = make(chan eventJob, e.maxBuf)
	e.running = true
	e.mu.Unlock()
	go e.run(stopCh, doneCh)
}

func (e *EventEngine) Stop() {
	e.startMu.Lock()
	defer e.startMu.Unlock()
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	stopCh := e.stopCh
	doneCh := e.doneCh
	e.running = false
	close(stopCh)
	e.mu.Unlock()
	<-doneCh
}

func (e *EventEngine) AddWorker(worker EventWorker) {
	if worker == nil {
		return
	}
	e.Start()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.workers = append(e.workers, worker)
}

func (e *EventEngine) RemoveWorker(worker EventWorker) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, existing := range e.workers {
		if existing == worker {
			e.workers = append(e.workers[:i], e.workers[i+1:]...)
			return
		}
	}
}

func (e *EventEngine) Emit(event Event) {
	e.Start()
	e.mu.Lock()
	event.Timestamp = event.Timestamp.Truncate(time.Second)
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	if event.ID == "" {
		seq := atomic.AddUint64(&e.nextID, 1)
		event.ID = fmt.Sprintf("event-%d-%d", event.Timestamp.UnixNano(), seq)
	}
	e.metrics["events_seen"]++
	if len(e.buffer) >= e.maxBuf {
		e.metrics["events_dropped"]++
		e.buffer = e.buffer[1:]
	}
	e.buffer = append(e.buffer, event)
	e.mu.Unlock()

	select {
	case e.queue <- event:
	default:
		e.mu.Lock()
		e.metrics["events_dropped"]++
		e.mu.Unlock()
	}
}

func (e *EventEngine) run(stopCh, doneCh chan struct{}) {
	defer close(doneCh)
	var workers sync.WaitGroup
	for i := 0; i < e.workerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-stopCh:
					return
				case job, ok := <-e.jobs:
					if !ok {
						return
					}
					func() {
						defer func() {
							if recover() != nil {
								e.mu.Lock()
								e.metrics["worker_errors"]++
								e.mu.Unlock()
							}
						}()
						job.worker.OnEvent(job.event)
					}()
				}
			}
		}()
	}

	for {
		select {
		case <-stopCh:
			workers.Wait()
			return
		case event, ok := <-e.queue:
			if !ok {
				workers.Wait()
				return
			}
			e.mu.RLock()
			workersForEvent := append([]EventWorker(nil), e.workers...)
			e.mu.RUnlock()
			for _, worker := range workersForEvent {
				select {
				case e.jobs <- eventJob{event: event, worker: worker}:
				case <-stopCh:
					workers.Wait()
					return
				}
			}
			e.mu.Lock()
			e.metrics["events_processed"]++
			e.mu.Unlock()
		}
	}
}

// drain delivers events still queued at shutdown so none are silently lost.
func (e *EventEngine) drain() {
	for {
		select {
		case event := <-e.queue:
			e.mu.RLock()
			workers := append([]EventWorker(nil), e.workers...)
			e.mu.RUnlock()
			for _, w := range workers {
				func() {
					defer func() { _ = recover() }()
					w.OnEvent(event)
				}()
			}
			e.mu.Lock()
			e.metrics["events_processed"]++
			e.mu.Unlock()
		default:
			return
		}
	}
}

func (e *EventEngine) Snapshot() []Event {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Event, len(e.buffer))
	copy(out, e.buffer)
	return out
}

func (e *EventEngine) Metrics() map[string]int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	m := make(map[string]int, len(e.metrics))
	for key, value := range e.metrics {
		m[key] = value
	}
	return m
}

func (e *EventEngine) QueueLength() int {
	return len(e.queue)
}
