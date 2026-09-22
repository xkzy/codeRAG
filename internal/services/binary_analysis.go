package services

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"codergag/internal/graph"
)

type BinaryAnalysisService struct {
	graph graph.GraphRepository
}

func NewBinaryAnalysisService(g graph.GraphRepository) *BinaryAnalysisService {
	return &BinaryAnalysisService{graph: g}
}

type AnalysisArtifact struct {
	ArtifactID    string
	BinaryID     string
	FunctionAddr string
	ArtifactType string
	Data         map[string]any
	Freshness    string
	CreatedAt    string
	Version      string
}

type CFGResult struct {
	FunctionAddr string
	BasicBlocks  []BasicBlockInfo
	Edges        []CFGEdge
	Confidence   float64
}

type BasicBlockInfo struct {
	Address     string
	StartAddr   string
	EndAddr     string
	Instructions []InstructionInfo
	PredCount   int
	SuccCount   int
}

type InstructionInfo struct {
	Address   string
	Mnemonic  string
	Operands  string
	IsBranch  bool
	IsCall    bool
	TargetAddr string
}

type CFGEdge struct {
	From string
	To   string
	Type string
}

type DataFlowResult struct {
	FunctionAddr   string
	Arguments      []ParameterInfo
	ReturnValues   []ReturnInfo
	GlobalReads    []string
	GlobalWrites   []string
	RegisterUsage  map[string]string
	StackVariables []StackVarInfo
}

type ParameterInfo struct {
	Name       string
	Register   string
	Offset     int
	Size       int
	Direction  string
}

type ReturnInfo struct {
	Register string
	Size     int
}

type StackVarInfo struct {
	Name    string
	Offset  int
	Size    int
	Type    string
}

type CallGraphResult struct {
	BinaryID      string
	Nodes        []CallNode
	Edges        []CallEdge
	IndirectCalls []IndirectCallSite
}

type CallNode struct {
	FunctionAddr string
	Name         string
	CallerCount  int
	CalleeCount  int
}

type CallEdge struct {
	Caller string
	Callee string
	Type   string
}

type IndirectCallSite struct {
	Address   string
	TargetReg string
	TargetMem string
}

const (
	FreshnessValid   = "VALID"
	FreshnessStale   = "STALE"
	FreshnessInvalid = "INVALID"
)

func (s *BinaryAnalysisService) ComputeCFG(projectID, binaryID, functionAddr string) (*CFGResult, error) {
	cacheKey := s.cfgCacheKey(binaryID, functionAddr, "cfg")
	if artifact := s.getArtifact(projectID, cacheKey); artifact != nil && artifact.Freshness == FreshnessValid {
		return s.cfgFromArtifact(artifact)
	}

	cfg := &CFGResult{
		FunctionAddr: functionAddr,
		BasicBlocks:  []BasicBlockInfo{},
		Edges:        []CFGEdge{},
		Confidence:   0.0,
	}

	fnNodes, err := s.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
		"address":    functionAddr,
	})
	if err != nil || len(fnNodes) == 0 {
		return nil, err
	}
	fnID := fnNodes[0].ID

	bbNodes, _ := s.graph.Neighbors(fnID, "CONTAINS", graph.DirOut)
	for _, bbNode := range bbNodes {
		if bbNode.Node.Kind != "BasicBlock" {
			continue
		}
		bbInfo := BasicBlockInfo{
			Address:     bbNode.Node.Properties["address"].(string),
			Instructions: []InstructionInfo{},
		}

		insnNodes, _ := s.graph.Neighbors(bbNode.Node.ID, "CONTAINS", graph.DirOut)
		for _, insnNode := range insnNodes {
			if insnNode.Node.Kind != "Instruction" {
				continue
			}
			mnemonic, _ := insnNode.Node.Properties["mnemonic"].(string)
			operands, _ := insnNode.Node.Properties["operands"].(string)
			addr, _ := insnNode.Node.Properties["address"].(string)

			insn := InstructionInfo{
				Address:  addr,
				Mnemonic: mnemonic,
				Operands: operands,
				IsBranch: mnemonic == "jmp" || mnemonic == "je" || mnemonic == "jne" || strings.HasPrefix(mnemonic, "j"),
				IsCall:   mnemonic == "call",
			}

			codeRefs, _ := s.graph.Neighbors(insnNode.Node.ID, "BRANCHES_TO", graph.DirOut)
			for _, ref := range codeRefs {
				insn.IsBranch = true
				insn.TargetAddr = ref.Node.Properties["address"].(string)
			}

			bbInfo.Instructions = append(bbInfo.Instructions, insn)
		}

		cfg.BasicBlocks = append(cfg.BasicBlocks, bbInfo)
		cfg.Confidence += 0.3
	}

	cfg.Confidence = floatMin(cfg.Confidence, 1.0)
	s.storeArtifact(projectID, cacheKey, map[string]any{
		"function_addr": cfg.FunctionAddr,
		"basic_blocks":  cfg.BasicBlocks,
		"edges":         cfg.Edges,
		"confidence":     cfg.Confidence,
	})

	return cfg, nil
}

func (s *BinaryAnalysisService) GetCFG(projectID, binaryID, functionAddr string) (*CFGResult, error) {
	cacheKey := s.cfgCacheKey(binaryID, functionAddr, "cfg")
	if artifact := s.getArtifact(projectID, cacheKey); artifact != nil {
		return s.cfgFromArtifact(artifact)
	}
	return s.ComputeCFG(projectID, binaryID, functionAddr)
}

func (s *BinaryAnalysisService) ComputeDataFlow(projectID, binaryID, functionAddr string) (*DataFlowResult, error) {
	result := &DataFlowResult{
		FunctionAddr:   functionAddr,
		Arguments:      []ParameterInfo{},
		ReturnValues:   []ReturnInfo{},
		GlobalReads:    []string{},
		GlobalWrites:   []string{},
		RegisterUsage:  map[string]string{},
		StackVariables: []StackVarInfo{},
	}

	fnNodes, err := s.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
		"address":    functionAddr,
	})
	if err != nil || len(fnNodes) == 0 {
		return nil, err
	}
	fnID := fnNodes[0].ID

	dataRefs, _ := s.graph.Neighbors(fnID, "REFERENCES_DATA", graph.DirOut)
	for _, ref := range dataRefs {
		refType, _ := ref.Edge.Properties["type"].(string)
		if refType == "read" {
			result.GlobalReads = append(result.GlobalReads, ref.Node.Properties["address"].(string))
		} else if refType == "write" {
			result.GlobalWrites = append(result.GlobalWrites, ref.Node.Properties["address"].(string))
		}
	}

	s.storeArtifact(projectID, s.dfCacheKey(binaryID, functionAddr), map[string]any{
		"function_addr":   result.FunctionAddr,
		"arguments":       result.Arguments,
		"return_values":   result.ReturnValues,
		"global_reads":    result.GlobalReads,
		"global_writes":   result.GlobalWrites,
		"register_usage":  result.RegisterUsage,
		"stack_variables": result.StackVariables,
	})

	return result, nil
}

func (s *BinaryAnalysisService) GetDataFlow(projectID, binaryID, functionAddr string) (*DataFlowResult, error) {
	artifact := s.getArtifact(projectID, s.dfCacheKey(binaryID, functionAddr))
	if artifact != nil {
		return s.dfFromArtifact(artifact)
	}
	return s.ComputeDataFlow(projectID, binaryID, functionAddr)
}

func (s *BinaryAnalysisService) ComputeCallGraph(projectID, binaryID string) (*CallGraphResult, error) {
	result := &CallGraphResult{
		BinaryID:       binaryID,
		Nodes:          []CallNode{},
		Edges:          []CallEdge{},
		IndirectCalls:  []IndirectCallSite{},
	}

	fnNodes, err := s.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
	})
	if err != nil {
		return nil, err
	}

	nodeMap := map[string]int{}
	for i, fn := range fnNodes {
		nodeMap[fn.ID] = i
		result.Nodes = append(result.Nodes, CallNode{
			FunctionAddr: fn.Properties["address"].(string),
			Name:         fn.Properties["name"].(string),
		})
	}

	for i, fn := range fnNodes {
		calls, _ := s.graph.Neighbors(fn.ID, "CALLS", graph.DirOut)
		for _, call := range calls {
			if idx, ok := nodeMap[call.Node.ID]; ok {
				result.Edges = append(result.Edges, CallEdge{
					Caller: fn.Properties["address"].(string),
					Callee: call.Node.Properties["address"].(string),
					Type:   "direct",
				})
				result.Nodes[i].CalleeCount++
				result.Nodes[idx].CallerCount++
			}
		}

		branches, _ := s.graph.Neighbors(fn.ID, "BRANCHES_TO", graph.DirOut)
		for _, br := range branches {
			if _, ok := nodeMap[br.Node.ID]; ok {
				result.Edges = append(result.Edges, CallEdge{
					Caller: fn.Properties["address"].(string),
					Callee: br.Node.Properties["address"].(string),
					Type:   "control_flow",
				})
			}
		}
	}

	return result, nil
}

func (s *BinaryAnalysisService) GetCallGraph(projectID, binaryID string) (*CallGraphResult, error) {
	return s.ComputeCallGraph(projectID, binaryID)
}

func (s *BinaryAnalysisService) GetFunctionFacts(projectID, binaryID, functionAddr string) (map[string]any, error) {
	result := map[string]any{}

	fnNodes, err := s.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
		"address":    functionAddr,
	})
	if err != nil || len(fnNodes) == 0 {
		return nil, err
	}
	fn := fnNodes[0]

	result["address"] = fn.Properties["address"]
	result["name"] = fn.Properties["name"]
	result["size"] = fn.Properties["size"]
	result["stable_id"] = fn.Properties["stable_id"]

	hypotheses, _ := s.graph.Neighbors(fn.ID, "SUSPECTED_AS", graph.DirOut)
	hypList := []map[string]any{}
	for _, h := range hypotheses {
		hypList = append(hypList, map[string]any{
			"id":        h.Node.ID,
			"claim":     h.Node.Properties["claim"],
			"confidence": h.Edge.Properties["confidence"],
			"status":    h.Edge.Properties["state"],
		})
	}
	result["hypotheses"] = hypList

	callers, _ := s.graph.Neighbors(fn.ID, "CALLS", graph.DirIn)
	result["caller_count"] = len(callers)

	callees, _ := s.graph.Neighbors(fn.ID, "CALLS", graph.DirOut)
	result["callee_count"] = len(callees)

	return result, nil
}

func (s *BinaryAnalysisService) InvalidateFunctionAnalysis(projectID, binaryID, functionAddr string) error {
	patterns := []string{"cfg", "dataflow", "callgraph"}
	for _, p := range patterns {
		cacheKey := s.cfgCacheKey(binaryID, functionAddr, p)
		s.invalidateArtifact(projectID, cacheKey)
	}
	return nil
}

func (s *BinaryAnalysisService) InvalidateBinaryAnalysis(projectID, binaryID string) error {
	patterns := []string{"cfg", "dataflow", "callgraph"}
	fnNodes, _ := s.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
	})
	for _, fn := range fnNodes {
		addr := fn.Properties["address"].(string)
		for _, p := range patterns {
			cacheKey := s.cfgCacheKey(binaryID, addr, p)
			s.invalidateArtifact(projectID, cacheKey)
		}
	}
	return nil
}

func (s *BinaryAnalysisService) cfgCacheKey(binaryID, functionAddr, suffix string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s", binaryID, functionAddr, suffix)))
	return fmt.Sprintf("%x", h[:])
}

func (s *BinaryAnalysisService) dfCacheKey(binaryID, functionAddr string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|df", binaryID, functionAddr)))
	return fmt.Sprintf("%x", h[:])
}

func (s *BinaryAnalysisService) getArtifact(projectID, cacheKey string) *AnalysisArtifact {
	artifactNodes, err := s.graph.FindNodes("BinaryAnalysisArtifact", map[string]any{
		"project_id": projectID,
		"cache_key":  cacheKey,
	})
	if err != nil || len(artifactNodes) == 0 {
		return nil
	}
	artifact := artifactNodes[0]
	freshness, _ := artifact.Properties["freshness"].(string)
	return &AnalysisArtifact{
		ArtifactID:    artifact.ID,
		BinaryID:      artifact.Properties["binary_id"].(string),
		FunctionAddr: artifact.Properties["function_addr"].(string),
		ArtifactType: artifact.Properties["artifact_type"].(string),
		Data:         artifact.Properties["data"].(map[string]any),
		Freshness:    freshness,
		CreatedAt:    artifact.Properties["created_at"].(string),
		Version:      artifact.Properties["version"].(string),
	}
}

func (s *BinaryAnalysisService) storeArtifact(projectID string, cacheKey string, data map[string]any) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	binaryID, _ := data["binary_id"].(string)
	if binaryID == "" {
		binaryID, _ = data["binary_id"].(string)
	}
	functionAddr, _ := data["function_addr"].(string)
	artifactType := "analysis"

	_, err := s.graph.UpsertNode("BinaryAnalysisArtifact", map[string]any{
		"project_id":    projectID,
		"cache_key":     cacheKey,
	}, map[string]any{
		"binary_id":     binaryID,
		"function_addr": functionAddr,
		"artifact_type": artifactType,
		"data":          data,
		"freshness":     FreshnessValid,
		"created_at":    now,
		"version":       "1.0",
	})
	if err != nil {
		return
	}
}

func (s *BinaryAnalysisService) invalidateArtifact(projectID, cacheKey string) {
	artifactNodes, _ := s.graph.FindNodes("BinaryAnalysisArtifact", map[string]any{
		"project_id": projectID,
		"cache_key":  cacheKey,
	})
	for _, n := range artifactNodes {
		s.graph.UpsertNode("BinaryAnalysisArtifact", map[string]any{"id": n.ID}, map[string]any{
			"freshness": FreshnessStale,
		})
	}
}

func (s *BinaryAnalysisService) cfgFromArtifact(artifact *AnalysisArtifact) (*CFGResult, error) {
	data := artifact.Data
	blocks, ok := data["basic_blocks"].([]BasicBlockInfo)
	if !ok {
		blocks = []BasicBlockInfo{}
		if blocksRaw, ok := data["basic_blocks"].([]map[string]any); ok {
			for _, b := range blocksRaw {
				bb := BasicBlockInfo{
					Address:     "",
					Instructions: []InstructionInfo{},
				}
				if addr, ok := b["address"].(string); ok {
					bb.Address = addr
				}
				if insns, ok := b["instructions"].([]InstructionInfo); ok {
					bb.Instructions = insns
				}
				blocks = append(blocks, bb)
			}
		}
	}

	confidence := 0.0
	if conf, ok := data["confidence"].(float64); ok {
		confidence = conf
	}

	return &CFGResult{
		FunctionAddr: data["function_addr"].(string),
		BasicBlocks: blocks,
		Edges:        []CFGEdge{},
		Confidence:   confidence,
	}, nil
}

func (s *BinaryAnalysisService) dfFromArtifact(artifact *AnalysisArtifact) (*DataFlowResult, error) {
	data := artifact.Data
	return &DataFlowResult{
		FunctionAddr:   data["function_addr"].(string),
		Arguments:      []ParameterInfo{},
		ReturnValues:   []ReturnInfo{},
		GlobalReads:    toStringSlice(data["global_reads"]),
		GlobalWrites:   toStringSlice(data["global_writes"]),
		RegisterUsage:  toStringStringMap(data["register_usage"]),
		StackVariables: []StackVarInfo{},
	}, nil
}

func toStringSlice(v any) []string {
	if s, ok := v.([]string); ok {
		return s
	}
	return []string{}
}

func toStringStringMap(v any) map[string]string {
	if m, ok := v.(map[string]string); ok {
		return m
	}
	return map[string]string{}
}

func floatMin(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

type ReverseEngineeringContextRequest struct {
	ProjectID    string
	Question     string
	Target       string
	TokenBudget  int
	IncludeFacts bool
	IncludeHypotheses bool
	IncludeCFG   bool
	IncludeDataFlow bool
}

type ReverseEngineeringContext struct {
	FunctionInfo    map[string]any
	Facts          []ContextItem
	Hypotheses     []ContextItem
	CFG            *CFGResult
	DataFlow       *DataFlowResult
	Callers        []map[string]any
	Callees        []map[string]any
	GlobalReadSize int
	GlobalWriteSize int
	Confidence     float64
}

func (s *BinaryAnalysisService) PrepareReverseEngineeringContext(req ReverseEngineeringContextRequest) (*ReverseEngineeringContext, error) {
	ctx := &ReverseEngineeringContext{
		Facts:          []ContextItem{},
		Hypotheses:     []ContextItem{},
	}

	parts := strings.Split(req.Target, "|")
	binaryID := parts[0]
	functionAddr := parts[1]
	if len(parts) > 2 {
		functionAddr = parts[1]
	}

	facts, err := s.GetFunctionFacts(req.ProjectID, binaryID, functionAddr)
	if err == nil && facts != nil {
		ctx.FunctionInfo = facts
		ctx.Confidence = 0.7
	}

	if req.IncludeCFG {
		if cfg, err := s.GetCFG(req.ProjectID, binaryID, functionAddr); err == nil {
			ctx.CFG = cfg
			ctx.Confidence += 0.1
		}
	}

	if req.IncludeDataFlow {
		if df, err := s.GetDataFlow(req.ProjectID, binaryID, functionAddr); err == nil {
			ctx.DataFlow = df
			ctx.Confidence += 0.1
		}
	}

	fnNodes, _ := s.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": req.ProjectID,
		"binary_id":  binaryID,
		"address":    functionAddr,
	})
	if len(fnNodes) > 0 {
		fnID := fnNodes[0].ID

		if req.IncludeFacts {
			strs, _ := s.graph.Neighbors(fnID, "REFERENCES", graph.DirOut)
			for _, str := range strs {
				if str.Node.Kind == "String" {
					ctx.Facts = append(ctx.Facts, ContextItem{
						Kind:   "string",
						Text:   str.Node.Properties["value"].(string),
						Source: "static_analysis",
					})
				}
			}
		}

		if req.IncludeHypotheses {
			hyps, _ := s.graph.Neighbors(fnID, "SUSPECTED_AS", graph.DirOut)
			for _, h := range hyps {
				claim := ""
				if c, ok := h.Node.Properties["claim"].(string); ok {
					claim = c
				}
				conf := 0.0
				if c, ok := h.Edge.Properties["confidence"].(float64); ok {
					conf = c
				}
				ctx.Hypotheses = append(ctx.Hypotheses, ContextItem{
					Kind:       "hypothesis",
					Text:       claim,
					Confidence: conf,
					Source:     "reverse_engineering",
				})
			}
		}

	 callers, _ := s.graph.Neighbors(fnID, "CALLS", graph.DirIn)
	 for _, c := range callers {
		 ctx.Callers = append(ctx.Callers, map[string]any{
			 "address": c.Node.Properties["address"],
			 "name":    c.Node.Properties["name"],
		 })
	 }

	 callees, _ := s.graph.Neighbors(fnID, "CALLS", graph.DirOut)
	 for _, c := range callees {
		 ctx.Callees = append(ctx.Callees, map[string]any{
			 "address": c.Node.Properties["address"],
			 "name":    c.Node.Properties["name"],
		 })
	 }
	}

	return ctx, nil
}
