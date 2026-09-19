package services

import (
	"codergag/internal/graph"
	"codergag/internal/models"
	"codergag/internal/reverse"
)

type ReverseEngineeringService struct {
	graph graph.GraphRepository
}

func NewReverseEngineeringService(g graph.GraphRepository) *ReverseEngineeringService {
	return &ReverseEngineeringService{graph: g}
}

func (s *ReverseEngineeringService) ImportBinary(projectID string, data reverse.NormalizedBinary) (map[string]any, error) {
	binary, err := s.graph.UpsertNode("Binary", map[string]any{
		"project_id": projectID,
		"binary_id":  data.BinaryID,
	}, map[string]any{
		"path":       data.Path,
		"hash":       data.Sha256,
		"analysis_tool": data.Tool,
	})
	if err != nil {
		return nil, err
	}

	functions := make(map[string]*models.Node)
	for _, item := range data.Functions {
		var sizeAny any
		if item.Size != nil {
			sizeAny = *item.Size
		}
		fn, err := s.graph.UpsertNode("BinaryFunction", map[string]any{
			"project_id": projectID,
			"binary_id":  data.BinaryID,
			"address":    item.Address,
		}, map[string]any{
			"name":          item.Name,
			"size":          sizeAny,
			"analysis_tool": data.Tool,
		})
		if err != nil {
			continue
		}
		functions[item.Address] = fn
		s.graph.Link("CONTAINS", binary.ID, fn.ID, nil)

		if item.DecompilerOutput != "" {
			output, err := s.graph.UpsertNode("DecompilerOutput", map[string]any{
				"project_id": projectID,
				"binary_id":  data.BinaryID,
				"address":    item.Address,
			}, map[string]any{
				"text": item.DecompilerOutput,
				"tool": data.Tool,
			})
			if err == nil {
				s.graph.Link("DECOMPILED_AS", fn.ID, output.ID, nil)
			}
		}

		for _, text := range item.Strings {
			// Use address as part of identity to allow same string in different functions
			strNode, err := s.graph.UpsertNode("String", map[string]any{
				"project_id": projectID,
				"value":      text,
			}, map[string]any{
				"value": text,
			})
			if err == nil {
				s.graph.Link("REFERENCES", fn.ID, strNode.ID, map[string]any{"source": data.Tool})
			}
		}
	}

	for _, item := range data.Functions {
		callerFn, ok := functions[item.Address]
		if !ok {
			continue
		}
		for _, addr := range item.Calls {
			if calleeFn, ok := functions[addr]; ok {
				s.graph.Link("CALLS", callerFn.ID, calleeFn.ID, map[string]any{
					"source": data.Tool, "confidence": 0.9,
				})
			}
		}
	}

	return map[string]any{
		"binary_id":  data.BinaryID,
		"functions":  len(functions),
		"tool":       data.Tool,
	}, nil
}

func (s *ReverseEngineeringService) MapSourceFunction(binaryFunctionID, sourceFunctionID string, confidence float64, method string) (map[string]any, error) {
	if err := s.graph.Link("EQUIVALENT_TO", binaryFunctionID, sourceFunctionID, map[string]any{
		"equivalence_status":  "SUSPECTED",
		"confidence":          confidence,
		"validation_method":   method,
	}); err != nil {
		return nil, err
	}
	return map[string]any{
		"binary_function_id":  binaryFunctionID,
		"source_function_id":  sourceFunctionID,
		"equivalence_status":  "SUSPECTED",
		"confidence":          confidence,
	}, nil
}

func (s *ReverseEngineeringService) RecordPort(binaryFunctionID, implementationID, language string, confidence float64) (map[string]any, error) {
	if err := s.graph.Link("PORTED_TO", binaryFunctionID, implementationID, map[string]any{
		"language":           language,
		"confidence":         confidence,
		"equivalence_status": "SUSPECTED",
	}); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":             "recorded",
		"equivalence_status": "SUSPECTED",
	}, nil
}

func (s *ReverseEngineeringService) RecordValidation(binaryFunctionID, implementationID, testName, status string, confidence float64, method string) (map[string]any, error) {
	bfNode, err := s.graph.GetNode(binaryFunctionID)
	if err != nil {
		return nil, err
	}
	projectID, _ := bfNode.Properties["project_id"].(string)
	test, err := s.graph.UpsertNode("Test", map[string]any{
		"project_id": projectID,
		"name":       testName,
	}, map[string]any{
		"status": status,
		"method": method,
	})
	if err != nil {
		return nil, err
	}
	s.graph.Link("VALIDATED_BY", implementationID, test.ID, map[string]any{
		"confidence": confidence,
		"status":     status,
	})
	equivStatus := "VALIDATED"
	if status != "passed" {
		equivStatus = "CONTRADICTED"
	}
	s.graph.Link("EQUIVALENT_TO", binaryFunctionID, implementationID, map[string]any{
		"equivalence_status":      equivStatus,
		"confidence":              confidence,
		"validation_method":       method,
		"test_reference":          test.ID,
	})
	return map[string]any{
		"test_id":           test.ID,
		"equivalence_status": equivStatus,
	}, nil
}
