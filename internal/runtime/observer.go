package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"codergag/internal/graph"
	"codergag/internal/models"
	"codergag/internal/reverse"
)

type ObserverConfig struct {
	QueueSize        int
	DropPolicy       DropPolicy
	NumWorkers       int
	ProcessInterval  time.Duration
	PrivacySensitive bool
}

type RuntimeObserver struct {
	config  ObserverConfig
	queue   *EventQueue
	parser  *Parser
	store   *LogTemplateStore
	resolver *RelationResolver
	graph   graph.GraphRepository

	mu       sync.RWMutex
	sessions map[string]*Session
	closed   bool

	workerWG   sync.WaitGroup
	outputPipe *os.File
	cmd        *exec.Cmd
	stdout     io.ReadCloser
	stderr     io.ReadCloser
}

type Session struct {
	ID        string
	ProjectID string
	TaskID    string
	ProcessID string
	Active    bool
	StartTime time.Time
}

func NewRuntimeObserver(g graph.GraphRepository, cfg ObserverConfig) *RuntimeObserver {
	if cfg.NumWorkers <= 0 {
		cfg.NumWorkers = runtime.NumCPU()
	}
	if cfg.ProcessInterval <= 0 {
		cfg.ProcessInterval = 100 * time.Millisecond
	}

	return &RuntimeObserver{
		config:   cfg,
		queue:    NewEventQueue(QueueConfig{Capacity: cfg.QueueSize, DropPolicy: cfg.DropPolicy}),
		parser:   NewParser(),
		store:    NewLogTemplateStore(),
		resolver: NewRelationResolver(g, nil),
		graph:    g,
		sessions: make(map[string]*Session),
	}
}

func (o *RuntimeObserver) SetCodeIndex(idx CodeIndex) {
	o.resolver = NewRelationResolver(o.graph, idx)
}

func (o *RuntimeObserver) Start() {
	o.workerWG.Add(o.config.NumWorkers)
	for i := 0; i < o.config.NumWorkers; i++ {
		go o.processWorker(i)
	}
}

func (o *RuntimeObserver) Stop() {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return
	}
	o.closed = true
	o.mu.Unlock()

	o.queue.Close()
	o.workerWG.Wait()
}

func (o *RuntimeObserver) processWorker(id int) {
	defer o.workerWG.Done()

	for {
		events, ok := o.queue.DequeueBlocking(500 * time.Millisecond)
		if !ok {
			o.mu.RLock()
			closed := o.closed
			o.mu.RUnlock()
			if closed {
				return
			}
			continue
		}

		for _, event := range events {
			o.processEvent(event)
		}
	}
}

func (o *RuntimeObserver) processEvent(event ObservationEvent) {
	ctx := context.Background()

	obs := o.parseEvent(event)

	template, isNew := o.store.FindOrCreate(event.ProjectID, obs.NormalizedText)
	if !isNew {
		o.store.Increment(template.ID, extractSampleValue(obs.NormalizedText))
	} else {
		template.Count = 1
		template.SampleVals = []string{extractSampleValue(obs.NormalizedText)}
	}

	obsNode, err := o.resolver.CreateObservationNode(ctx, obs)
	if err != nil {
		log.Printf("failed to create observation node: %v", err)
		return
	}

	resolutions := o.resolver.Resolve(ctx, obs)
	for _, res := range resolutions {
		if res.TargetID != "" {
			if err := o.resolver.LinkObservation(ctx, obsNode.ID, res); err != nil {
				log.Printf("failed to link observation: %v", err)
			}
		}
	}

	if template.ID != "" && len(template.SourceIDs) > 0 {
		o.graph.Link("MATCHES_LOG_TEMPLATE", obsNode.ID, template.ID, map[string]any{
			"evidence":   string(reverse.EvidenceStrong),
			"confidence": 0.9,
			"method":     "log_template_match",
			"reason":     "normalized log template match",
		})
	}

	session := o.getSession(event.SessionID)
	if session != nil && session.TaskID != "" {
		o.graph.Link("RELATED_TO", obsNode.ID, session.TaskID, map[string]any{
			"session_id": event.SessionID,
			"timestamp":  event.Timestamp.Format(time.RFC3339),
		})
	}
}

func (o *RuntimeObserver) parseEvent(event ObservationEvent) *reverse.RuntimeObservation {
	obs := o.parser.Parse(event.RawText)
	obs.ID = event.ID
	obs.ProjectID = event.ProjectID
	obs.SessionID = event.SessionID
	obs.ProcessID = event.ProcessID
	obs.Source = reverse.ObservationSource(event.Source)
	obs.Stream = reverse.ObservationStream(event.Stream)
	obs.Timestamp = event.Timestamp.Format(time.RFC3339Nano)
	if v, ok := event.Metadata["working_dir"]; ok {
		obs.WorkingDirectory = v
	}
	if v, ok := event.Metadata["executable"]; ok {
		obs.Executable = v
	}
	if v, ok := event.Metadata["binary_id"]; ok {
		obs.BinaryID = v
	}
	if v, ok := event.Metadata["git_commit"]; ok {
		obs.GitCommit = v
	}
	if v, ok := event.Metadata["git_branch"]; ok {
		obs.GitBranch = v
	}
	if v, ok := event.Metadata["binary_hash"]; ok {
		obs.BinaryHash = v
	}
	return obs
}

func (o *RuntimeObserver) CaptureOutput(projectID, sessionID string, output []byte, stream string) bool {
	event := ObservationEvent{
		ID:        generateEventID(),
		ProjectID: projectID,
		SessionID: sessionID,
		Timestamp: time.Now(),
		RawText:   output,
		Stream:    stream,
		Metadata:  make(map[string]string),
	}
	return o.queue.Enqueue(event)
}

func (o *RuntimeObserver) CaptureProcessOutput(projectID, sessionID, processID string, output []byte, stream string, metadata map[string]string) bool {
	event := ObservationEvent{
		ID:        generateEventID(),
		ProjectID: projectID,
		SessionID: sessionID,
		ProcessID: processID,
		Timestamp: time.Now(),
		RawText:   output,
		Stream:    stream,
		Metadata:  metadata,
	}
	return o.queue.Enqueue(event)
}

func (o *RuntimeObserver) RegisterSession(sessionID, projectID, taskID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sessions[sessionID] = &Session{
		ID:        sessionID,
		ProjectID: projectID,
		TaskID:    taskID,
		Active:    true,
		StartTime: time.Now(),
	}
}

func (o *RuntimeObserver) UnregisterSession(sessionID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if s, ok := o.sessions[sessionID]; ok {
		s.Active = false
	}
}

func (o *RuntimeObserver) getSession(sessionID string) *Session {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.sessions[sessionID]
}

func (o *RuntimeObserver) StartProcess(ctx context.Context, projectID, sessionID string, name string, args ...string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, _ = os.Getwd()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdout.Close()
		return nil, err
	}

	processID := fmt.Sprintf("%d", cmd.Process.Pid)

	go o.streamOutput(projectID, sessionID, processID, stdout, "stdout")
	go o.streamOutput(projectID, sessionID, processID, stderr, "stderr")

	o.mu.Lock()
	o.sessions[sessionID] = &Session{
		ID:        sessionID,
		ProjectID: projectID,
		ProcessID: processID,
		Active:    true,
		StartTime: time.Now(),
	}
	o.mu.Unlock()

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	go func() {
		cmd.Wait()
		o.mu.Lock()
		if s, ok := o.sessions[sessionID]; ok {
			s.ProcessID = ""
		}
		o.mu.Unlock()
	}()

	return cmd, nil
}

func (o *RuntimeObserver) streamOutput(projectID, sessionID, processID string, r io.Reader, stream string) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			o.CaptureProcessOutput(projectID, sessionID, processID, buf[:n], stream, map[string]string{
				"executable": processID,
			})
		}
		if err != nil {
			break
		}
	}
}

func (o *RuntimeObserver) Stats() ObserverStats {
	return ObserverStats{
		QueueStats: o.queue.Stats(),
		NumSessions: func() int {
			o.mu.RLock()
			defer o.mu.RUnlock()
			return len(o.sessions)
		}(),
	}
}

type ObserverStats struct {
	QueueStats   QueueStats
	NumSessions  int
	IsRunning    bool
}

func (o *RuntimeObserver) GetObservation(projectID, obsID string) (*models.Node, error) {
	return o.graph.GetNode(obsID)
}

func (o *RuntimeObserver) GetObservationsByFunction(projectID, functionID string) ([]*models.Node, error) {
	edges, err := o.graph.Neighbors(functionID, "EMITTED_BY", graph.DirIn)
	if err != nil {
		return nil, err
	}
	var observations []*models.Node
	for _, e := range edges {
		if e.Node.Kind == "RuntimeObservation" {
			observations = append(observations, e.Node)
		}
	}
	return observations, nil
}

func (o *RuntimeObserver) GetObservationsByProject(projectID string, limit int) ([]*models.Node, error) {
	nodes, err := o.graph.FindNodes("RuntimeObservation", map[string]any{
		"project_id": projectID,
	})
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(nodes) > limit {
		return nodes[:limit], nil
	}
	return nodes, nil
}

func (o *RuntimeObserver) GetLogTemplate(projectID, templateID string) *reverse.LogTemplate {
	return o.store.Get(templateID)
}

func (o *RuntimeObserver) GetLogTemplatesByProject(projectID string) []*reverse.LogTemplate {
	return o.store.GetByProject(projectID)
}

func generateEventID() string {
	h := sha256.New()
	h.Write([]byte(strconv.FormatInt(time.Now().UnixNano(), 10)))
	return "obs_" + hex.EncodeToString(h.Sum(nil))[:16]
}

func extractSampleValue(text string) string {
	parts := strings.Split(text, " ")
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		if len(last) > 0 && len(last) < 50 {
			return last
		}
	}
	if len(text) > 50 {
		return text[:50] + "..."
	}
	return text
}
