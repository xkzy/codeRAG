package services

import (
	"context"
	"sync"

	"codergag/internal/graph"
)

type BinaryBackgroundWorker struct {
	graph     graph.GraphRepository
	analysis  *BinaryAnalysisService
	pending   chan binaryAnalysisJob
	sem       chan struct{}
	mu        sync.Mutex
	running   bool
	ctx       context.Context
	cancel    context.CancelFunc
}

type binaryAnalysisJob struct {
	ProjectID    string
	BinaryID     string
	FunctionAddr string
	JobType      string
	Priority     int
}

const (
	JobTypeCFG        = "cfg"
	JobTypeDataFlow   = "dataflow"
	JobTypeCallGraph  = "callgraph"
	JobTypeTypeInference = "type_inference"
)

func NewBinaryBackgroundWorker(g graph.GraphRepository) *BinaryBackgroundWorker {
	return &BinaryBackgroundWorker{
		graph:    g,
		analysis: NewBinaryAnalysisService(g),
		pending:  make(chan binaryAnalysisJob, 1000),
		sem:      make(chan struct{}, 4),
	}
}

func (w *BinaryBackgroundWorker) Start(ctx context.Context) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.running = true
	w.mu.Unlock()

	go w.workerLoop()
}

func (w *BinaryBackgroundWorker) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.running = false
	w.mu.Unlock()

	if w.cancel != nil {
		w.cancel()
	}
}

func (w *BinaryBackgroundWorker) Submit(job binaryAnalysisJob) bool {
	select {
	case w.pending <- job:
		return true
	default:
		return false
	}
}

func (w *BinaryBackgroundWorker) SubmitCFG(projectID, binaryID, functionAddr string, priority int) bool {
	return w.Submit(binaryAnalysisJob{
		ProjectID:    projectID,
		BinaryID:     binaryID,
		FunctionAddr: functionAddr,
		JobType:      JobTypeCFG,
		Priority:     priority,
	})
}

func (w *BinaryBackgroundWorker) SubmitDataFlow(projectID, binaryID, functionAddr string, priority int) bool {
	return w.Submit(binaryAnalysisJob{
		ProjectID:    projectID,
		BinaryID:     binaryID,
		FunctionAddr: functionAddr,
		JobType:      JobTypeDataFlow,
		Priority:     priority,
	})
}

func (w *BinaryBackgroundWorker) SubmitCallGraph(projectID, binaryID string, priority int) bool {
	return w.Submit(binaryAnalysisJob{
		ProjectID:    projectID,
		BinaryID:     binaryID,
		FunctionAddr: "",
		JobType:      JobTypeCallGraph,
		Priority:     priority,
	})
}

func (w *BinaryBackgroundWorker) workerLoop() {
	for {
		select {
		case <-w.ctx.Done():
			return
		case job := <-w.pending:
			w.sem <- struct{}{}
			go func(j binaryAnalysisJob) {
				w.processJob(j)
				<-w.sem
			}(job)
		}
	}
}

func (w *BinaryBackgroundWorker) processJob(job binaryAnalysisJob) {
	switch job.JobType {
	case JobTypeCFG:
		w.analysis.GetCFG(job.ProjectID, job.BinaryID, job.FunctionAddr)
	case JobTypeDataFlow:
		w.analysis.GetDataFlow(job.ProjectID, job.BinaryID, job.FunctionAddr)
	case JobTypeCallGraph:
		w.analysis.GetCallGraph(job.ProjectID, job.BinaryID)
	case JobTypeTypeInference:
		w.inferTypes(job.ProjectID, job.BinaryID, job.FunctionAddr)
	}
}

func (w *BinaryBackgroundWorker) inferTypes(projectID, binaryID, functionAddr string) {
	fnNodes, err := w.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
		"address":   functionAddr,
	})
	if err != nil || len(fnNodes) == 0 {
		return
	}

	fnID := fnNodes[0].ID
	bbNodes, _ := w.graph.Neighbors(fnID, "CONTAINS", graph.DirOut)

	typeHints := map[string]string{}
	for _, bb := range bbNodes {
		if bb.Node.Kind != "BasicBlock" {
			continue
		}
		insnNodes, _ := w.graph.Neighbors(bb.Node.ID, "CONTAINS", graph.DirOut)
		for _, insn := range insnNodes {
			if insn.Node.Kind != "Instruction" {
				continue
			}
			mnemonic, _ := insn.Node.Properties["mnemonic"].(string)
			if mnemonic == "mov" || mnemonic == "add" || mnemonic == "sub" {
				typeHints[functionAddr] = "integer"
			}
		}
	}

	if len(typeHints) > 0 {
		w.graph.UpsertNode("BinaryFunction", map[string]any{"id": fnID}, map[string]any{
			"type_hints": typeHints,
		})
	}
}

func (w *BinaryBackgroundWorker) QueueLength() int {
	return len(w.pending)
}

func (w *BinaryBackgroundWorker) Status() map[string]any {
	w.mu.Lock()
	running := w.running
	w.mu.Unlock()

	return map[string]any{
		"running":       running,
		"queue_length":  w.QueueLength(),
		"workers":       len(w.sem),
	}
}

type BinaryAnalysisScheduler struct {
	worker      *BinaryBackgroundWorker
	accessCount map[string]int
	mu          sync.Mutex
}

func NewBinaryAnalysisScheduler(g graph.GraphRepository) *BinaryAnalysisScheduler {
	scheduler := &BinaryAnalysisScheduler{
		worker:      NewBinaryBackgroundWorker(g),
		accessCount: make(map[string]int),
	}
	return scheduler
}

func (s *BinaryAnalysisScheduler) Start(ctx context.Context) {
	s.worker.Start(ctx)
}

func (s *BinaryAnalysisScheduler) Stop() {
	s.worker.Stop()
}

func (s *BinaryAnalysisScheduler) RecordAccess(projectID, binaryID, functionAddr string) {
	key := projectID + "|" + binaryID + "|" + functionAddr
	s.mu.Lock()
	s.accessCount[key]++
	count := s.accessCount[key]
	s.mu.Unlock()

	if count%5 == 0 {
		s.worker.SubmitCFG(projectID, binaryID, functionAddr, count)
		s.worker.SubmitDataFlow(projectID, binaryID, functionAddr, count)
	}
}

func (s *BinaryAnalysisScheduler) GetStatus() map[string]any {
	return s.worker.Status()
}

func (s *BinaryAnalysisScheduler) FlushPending(projectID, binaryID string) {
	fnNodes, err := s.worker.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
	})
	if err != nil {
		return
	}

	for _, fn := range fnNodes {
		addr := fn.Properties["address"].(string)
		s.worker.SubmitCFG(projectID, binaryID, addr, 100)
		s.worker.SubmitDataFlow(projectID, binaryID, addr, 100)
	}

	s.worker.SubmitCallGraph(projectID, binaryID, 100)
}
