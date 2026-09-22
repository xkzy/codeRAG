package runtime

import (
	"context"
	"strings"

	"codergag/internal/graph"
	"codergag/internal/models"
	"codergag/internal/reverse"
)

type RelationResolver struct {
	graph graph.GraphRepository
	index CodeIndex
}

type CodeIndex interface {
	ResolveSymbol(projectID, name string) string
	ResolveFileLine(projectID, file string, line int) string
	ResolveAddress(projectID, addr string) string
	ResolveBinaryFunction(projectID, binaryID, addr string) string
}

func NewRelationResolver(g graph.GraphRepository, idx CodeIndex) *RelationResolver {
	return &RelationResolver{
		graph: g,
		index: idx,
	}
}

type ResolutionResult struct {
	TargetID   string
	Kind       string
	Evidence   reverse.EvidenceLevel
	Confidence float64
	Method     string
	Reason     string
}

func (r *RelationResolver) Resolve(ctx context.Context, obs *reverse.RuntimeObservation) []ResolutionResult {
	var results []ResolutionResult

	if obs.Extracted.Symbol != "" {
		if res := r.resolveSymbol(ctx, obs); res != nil {
			results = append(results, *res)
		}
	}

	if obs.Extracted.File != "" && obs.Extracted.Line > 0 {
		if res := r.resolveFileLine(ctx, obs); res != nil {
			results = append(results, *res)
		}
	}

	if obs.Extracted.Address != "" {
		if res := r.resolveAddress(ctx, obs); res != nil {
			results = append(results, *res)
		}
	}

	if len(obs.Extracted.Stack) > 0 {
		stackResults := r.resolveStack(ctx, obs)
		results = append(results, stackResults...)
	}

	return results
}

func (r *RelationResolver) resolveSymbol(ctx context.Context, obs *reverse.RuntimeObservation) *ResolutionResult {
	if r.index != nil {
		if targetID := r.index.ResolveSymbol(obs.ProjectID, obs.Extracted.Symbol); targetID != "" {
			return &ResolutionResult{
				TargetID:   targetID,
				Kind:       "EMITTED_BY",
				Evidence:   reverse.EvidenceStrong,
				Confidence: 0.95,
				Method:     "exact_symbol_match",
				Reason:     "exact symbol name match from index",
			}
		}
	}

	nodes, err := r.graph.FindNodes("Function", map[string]any{
		"project_id": obs.ProjectID,
		"name":       obs.Extracted.Symbol,
	})
	if err == nil && len(nodes) > 0 {
		return &ResolutionResult{
			TargetID:   nodes[0].ID,
			Kind:       "EMITTED_BY",
			Evidence:   reverse.EvidenceDirect,
			Confidence: 1.0,
			Method:     "exact_symbol_match",
			Reason:     "exact symbol name match from graph",
		}
	}

	nodes, err = r.graph.FindNodes("Function", map[string]any{
		"project_id":   obs.ProjectID,
		"qualified_name": obs.Extracted.Symbol,
	})
	if err == nil && len(nodes) > 0 {
		return &ResolutionResult{
			TargetID:   nodes[0].ID,
			Kind:       "EMITTED_BY",
			Evidence:   reverse.EvidenceDirect,
			Confidence: 1.0,
			Method:     "exact_qualified_symbol_match",
			Reason:     "exact qualified symbol name match from graph",
		}
	}

	return &ResolutionResult{
		TargetID:   "",
		Kind:       "EMITTED_BY",
		Evidence:   reverse.EvidenceUnresolved,
		Confidence: 0.0,
		Method:     "symbol_lookup",
		Reason:     "symbol not found in graph",
	}
}

func (r *RelationResolver) resolveFileLine(ctx context.Context, obs *reverse.RuntimeObservation) *ResolutionResult {
	if r.index != nil {
		if targetID := r.index.ResolveFileLine(obs.ProjectID, obs.Extracted.File, obs.Extracted.Line); targetID != "" {
			return &ResolutionResult{
				TargetID:   targetID,
				Kind:       "OBSERVED_AT",
				Evidence:   reverse.EvidenceDirect,
				Confidence: 1.0,
				Method:     "exact_file_line_match",
				Reason:     "exact file:line match from index",
			}
		}
	}

	fileNodes, err := r.graph.FindNodes("File", map[string]any{
		"project_id": obs.ProjectID,
		"name":       obs.Extracted.File,
	})
	if err == nil && len(fileNodes) > 0 {
		return &ResolutionResult{
			TargetID:   fileNodes[0].ID,
			Kind:       "OBSERVED_AT",
			Evidence:   reverse.EvidenceStrong,
			Confidence: 0.9,
			Method:     "exact_file_match",
			Reason:     "file match via graph lookup",
		}
	}

	return &ResolutionResult{
		TargetID:   "",
		Kind:       "OBSERVED_AT",
		Evidence:   reverse.EvidenceUnresolved,
		Confidence: 0.0,
		Method:     "file_line_lookup",
		Reason:     "file:line not found in graph",
	}
}

func (r *RelationResolver) resolveAddress(ctx context.Context, obs *reverse.RuntimeObservation) *ResolutionResult {
	if obs.Extracted.Address == "" {
		return nil
	}

	if obs.BinaryID != "" && r.index != nil {
		if targetID := r.index.ResolveBinaryFunction(obs.ProjectID, obs.BinaryID, obs.Extracted.Address); targetID != "" {
			return &ResolutionResult{
				TargetID:   targetID,
				Kind:       "OCCURRED_IN",
				Evidence:   reverse.EvidenceDirect,
				Confidence: 1.0,
				Method:     "address_to_binary_function",
				Reason:     "address resolved to binary function via index",
			}
		}
	}

	funcNodes, err := r.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": obs.ProjectID,
		"address":    obs.Extracted.Address,
	})
	if err == nil && len(funcNodes) > 0 {
		return &ResolutionResult{
			TargetID:   funcNodes[0].ID,
			Kind:       "OCCURRED_IN",
			Evidence:   reverse.EvidenceDirect,
			Confidence: 1.0,
			Method:     "address_to_binary_function",
			Reason:     "address resolved to binary function via graph",
		}
	}

	if obs.Extracted.Symbol != "" {
		funcNodes, err = r.graph.FindNodes("BinaryFunction", map[string]any{
			"project_id": obs.ProjectID,
			"name":       obs.Extracted.Symbol,
		})
		if err == nil && len(funcNodes) > 0 {
			return &ResolutionResult{
				TargetID:   funcNodes[0].ID,
				Kind:       "OCCURRED_IN",
				Evidence:   reverse.EvidenceStrong,
				Confidence: 0.95,
				Method:     "symbol_to_binary_function",
				Reason:     "symbol resolved to binary function via graph",
			}
		}
	}

	return &ResolutionResult{
		TargetID:   "",
		Kind:       "OCCURRED_IN",
		Evidence:   reverse.EvidenceUnresolved,
		Confidence: 0.0,
		Method:     "address_lookup",
		Reason:     "address not found in binary graph",
	}
}

func (r *RelationResolver) resolveStack(ctx context.Context, obs *reverse.RuntimeObservation) []ResolutionResult {
	var results []ResolutionResult

	for i, frameName := range obs.Extracted.Stack {
		var kind string
		if i == 0 {
			kind = "STACK_FRAME"
		} else {
			kind = "CALLER"
		}

		if r.index != nil {
			if targetID := r.index.ResolveSymbol(obs.ProjectID, frameName); targetID != "" {
				results = append(results, ResolutionResult{
					TargetID:   targetID,
					Kind:       kind,
					Evidence:   reverse.EvidenceStrong,
					Confidence: 0.95,
					Method:     "stack_frame_symbol",
					Reason:     "stack frame resolved via index",
				})
				continue
			}
		}

		nodes, err := r.graph.FindNodes("Function", map[string]any{
			"project_id": obs.ProjectID,
			"name":       frameName,
		})
		if err == nil && len(nodes) > 0 {
			results = append(results, ResolutionResult{
				TargetID:   nodes[0].ID,
				Kind:       kind,
				Evidence:   reverse.EvidenceDirect,
				Confidence: 1.0,
				Method:     "stack_frame_symbol",
				Reason:     "stack frame resolved via graph",
			})
			continue
		}

		binNodes, err := r.graph.FindNodes("BinaryFunction", map[string]any{
			"project_id": obs.ProjectID,
			"name":       frameName,
		})
		if err == nil && len(binNodes) > 0 {
			results = append(results, ResolutionResult{
				TargetID:   binNodes[0].ID,
				Kind:       kind,
				Evidence:   reverse.EvidenceStrong,
				Confidence: 0.9,
				Method:     "stack_frame_binary_symbol",
				Reason:     "stack frame resolved to binary function via graph",
			})
			continue
		}
	}

	return results
}

func (r *RelationResolver) CreateObservationNode(ctx context.Context, obs *reverse.RuntimeObservation) (*models.Node, error) {
	node, err := r.graph.UpsertNode("RuntimeObservation", map[string]any{
		"id":         obs.ID,
		"project_id": obs.ProjectID,
	}, map[string]any{
		"session_id":        obs.SessionID,
		"process_id":        obs.ProcessID,
		"source":            string(obs.Source),
		"stream":            string(obs.Stream),
		"raw_hash":          obs.RawHash,
		"normalized_text":   obs.NormalizedText,
		"severity":          string(obs.Severity),
		"event_type":        string(obs.EventType),
		"file":              obs.Extracted.File,
		"line":              obs.Extracted.Line,
		"symbol":            obs.Extracted.Symbol,
		"address":           obs.Extracted.Address,
		"offset":            obs.Extracted.Offset,
		"module":            obs.Extracted.Module,
		"exception":         obs.Extracted.Exception,
		"error_code":        obs.Extracted.ErrorCode,
		"test_name":         obs.Extracted.TestName,
		"working_directory": obs.WorkingDirectory,
		"executable":       obs.Executable,
		"binary_id":         obs.BinaryID,
		"git_commit":        obs.GitCommit,
		"git_branch":       obs.GitBranch,
		"binary_hash":      obs.BinaryHash,
		"source_revision":   obs.SourceRevision,
		"graph_version":     obs.GraphVersion,
		"timestamp":         obs.Timestamp,
	})
	if err != nil {
		return nil, err
	}

	if len(obs.Extracted.Stack) > 0 {
		stackStr := strings.Join(obs.Extracted.Stack, ",")
		node.SetProperty("stack_frames", stackStr)
	}

	return node, nil
}

func (r *RelationResolver) LinkObservation(ctx context.Context, obsNodeID string, result ResolutionResult) error {
	if result.TargetID == "" {
		return nil
	}

	_, err := r.graph.Link(result.Kind, obsNodeID, result.TargetID, map[string]any{
		"evidence":   string(result.Evidence),
		"confidence": result.Confidence,
		"method":     result.Method,
		"reason":     result.Reason,
	})
	return err
}
