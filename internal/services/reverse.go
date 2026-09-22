package services

import (
	"fmt"

	"codergag/internal/graph"
	"codergag/internal/ids"
	"codergag/internal/models"
	"codergag/internal/reverse"
)

const MaxDecompilerOutputLen = 50000
const MaxInstructionFieldLen = 500

func truncate(s string, maxLen int) string {
	if maxLen > 0 && len(s) > maxLen {
		return s[:maxLen] + "...[truncated]"
	}
	return s
}

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
		"path":          data.Path,
		"hash":          data.Sha256,
		"analysis_tool": data.Tool,
		"stable_id":     ids.BinaryID(data.BinaryID),
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
			"stable_id":     ids.BinFuncID(data.BinaryID, item.Address),
		})
		if err != nil {
			continue
		}
		functions[item.Address] = fn
		s.graph.Link("CONTAINS", binary.ID, fn.ID, nil)

		if item.DecompilerOutput != "" {
			// Truncate overly long decompiler output to prevent token explosion
			// in downstream LLM analysis. The full output can be retrieved from
			// the original source if needed.
			outputText := truncate(item.DecompilerOutput, MaxDecompilerOutputLen)
			outputNode, err := s.graph.UpsertNode("DecompilerOutput", map[string]any{
				"project_id": projectID,
				"binary_id":  data.BinaryID,
				"address":    item.Address,
			}, map[string]any{
				"text":      outputText,
				"tool":      data.Tool,
				"truncated": len(outputText) < len(item.DecompilerOutput),
			})
			if err == nil {
				s.graph.Link("DECOMPILED_AS", fn.ID, outputNode.ID, nil)
			}
		}

		for _, text := range item.Strings {
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
		for _, bb := range item.BasicBlocks {
			bbNode, err := s.graph.UpsertNode("BasicBlock", map[string]any{
				"project_id": projectID,
				"binary_id":  data.BinaryID,
				"address":    bb.Address,
			}, map[string]any{
				"stable_id": ids.BasicBlockID(data.BinaryID, item.Address, bb.Address),
			})
			if err != nil {
				continue
			}
			s.graph.Link("CONTAINS", callerFn.ID, bbNode.ID, nil)
			for _, insn := range bb.Instruction {
				insnNode, err := s.graph.UpsertNode("Instruction", map[string]any{
					"project_id": projectID,
					"binary_id":  data.BinaryID,
					"address":    insn.Address,
				}, map[string]any{
					"mnemonic":  truncate(insn.Mnemonic, MaxInstructionFieldLen),
					"operands":  truncate(insn.Operands, MaxInstructionFieldLen),
					"stable_id": ids.InstructionID(data.BinaryID, item.Address, bb.Address, insn.Address),
				})
				if err != nil {
					continue
				}
				s.graph.Link("CONTAINS", bbNode.ID, insnNode.ID, nil)

				// Data references from instruction
				for _, dref := range insn.DataRefs {
					dataNode, err := s.graph.UpsertNode("BinaryData", map[string]any{
						"project_id": projectID,
						"binary_id":  data.BinaryID,
						"address":    dref.ToAddress,
					}, map[string]any{
						"stable_id": ids.BasicBlockID(data.BinaryID, item.Address, dref.ToAddress),
					})
					if err == nil {
						s.graph.Link("REFERENCES_DATA", insnNode.ID, dataNode.ID, map[string]any{
							"type":   dref.Type,
							"size":   dref.Size,
							"source": data.Tool,
						})
					}
				}

				// Code references from instruction
				for _, cref := range insn.CodeRefs {
					if targetFn, ok := functions[cref.ToAddress]; ok {
						edgeType := "CALLS"
						if cref.Type == "jmp" || cref.Type == "cond_jmp" {
							edgeType = "BRANCHES_TO"
						}
						s.graph.Link(edgeType, insnNode.ID, targetFn.ID, map[string]any{
							"type":   cref.Type,
							"source": data.Tool,
						})
					}
				}
			}
		}

		// Function-level data references
		for _, dref := range item.DataReferences {
			if callerFn, ok := functions[item.Address]; ok {
				dataNode, err := s.graph.UpsertNode("BinaryData", map[string]any{
					"project_id": projectID,
					"binary_id":  data.BinaryID,
					"address":    dref.ToAddress,
				}, map[string]any{
					"stable_id": ids.BasicBlockID(data.BinaryID, item.Address, dref.ToAddress),
				})
				if err == nil {
					s.graph.Link("REFERENCES_DATA", callerFn.ID, dataNode.ID, map[string]any{
						"type":   dref.Type,
						"size":   dref.Size,
						"source": data.Tool,
					})
				}
			}
		}

		// Function-level code references
		for _, cref := range item.CodeReferences {
			if callerFn, ok := functions[item.Address]; ok {
				if targetFn, ok := functions[cref.ToAddress]; ok {
					edgeType := "CALLS"
					if cref.Type == "jmp" || cref.Type == "cond_jmp" {
						edgeType = "BRANCHES_TO"
					}
					s.graph.Link(edgeType, callerFn.ID, targetFn.ID, map[string]any{
						"type":   cref.Type,
						"source": data.Tool,
					})
				}
			}
		}
	}

	return map[string]any{
		"binary_id": data.BinaryID,
		"functions": len(functions),
		"tool":      data.Tool,
	}, nil
}

func (s *ReverseEngineeringService) ImportBinaryMetadata(projectID string, data reverse.NormalizedBinary) (map[string]any, error) {
	binaryNodes, err := s.graph.FindNodes("Binary", map[string]any{
		"project_id": projectID,
		"binary_id": data.BinaryID,
	})
	if err != nil || len(binaryNodes) == 0 {
		return nil, fmt.Errorf("binary %s not found", data.BinaryID)
	}
	binaryID := binaryNodes[0].ID

	stats := map[string]any{
		"modules":  0,
		"sections": 0,
		"globals":  0,
		"imports":  0,
		"exports":  0,
		"symbols":  0,
		"types":    0,
		"registers": 0,
		"constants": 0,
		"extapis":  0,
	}

	for _, m := range data.Modules {
		modNode, err := s.graph.UpsertNode("Module", map[string]any{
			"project_id": projectID,
			"binary_id":  data.BinaryID,
			"name":      m.Name,
		}, map[string]any{
			"name": m.Name,
			"path": m.Path,
		})
		if err == nil {
			s.graph.Link("CONTAINS", binaryID, modNode.ID, nil)
			stats["modules"] = stats["modules"].(int) + 1
		}
	}

	for _, sec := range data.Sections {
		secNode, err := s.graph.UpsertNode("Section", map[string]any{
			"project_id": projectID,
			"binary_id":  data.BinaryID,
			"name":      sec.Name,
		}, map[string]any{
			"name":      sec.Name,
			"address":   sec.Address,
			"size":      sec.Size,
			"alignment": sec.Alignment,
		})
		if err == nil {
			s.graph.Link("CONTAINS", binaryID, secNode.ID, nil)
			stats["sections"] = stats["sections"].(int) + 1
		}
	}

	for _, g := range data.Globals {
		globalNode, err := s.graph.UpsertNode("Global", map[string]any{
			"project_id": projectID,
			"binary_id":  data.BinaryID,
			"address":   g.Address,
		}, map[string]any{
			"address": g.Address,
			"name":    g.Name,
			"size":   g.Size,
		})
		if err == nil {
			s.graph.Link("CONTAINS", binaryID, globalNode.ID, nil)
			stats["globals"] = stats["globals"].(int) + 1
		}
	}

	for _, imp := range data.Imports {
		impNode, err := s.graph.UpsertNode("Import", map[string]any{
			"project_id": projectID,
			"binary_id":  data.BinaryID,
			"name":      imp.Name,
		}, map[string]any{
			"name":    imp.Name,
			"ordinal": imp.Ordinal,
			"hint":   imp.Hint,
		})
		if err == nil {
			s.graph.Link("CONTAINS", binaryID, impNode.ID, nil)
			stats["imports"] = stats["imports"].(int) + 1
		}
	}

	for _, exp := range data.Exports {
		expNode, err := s.graph.UpsertNode("Export", map[string]any{
			"project_id": projectID,
			"binary_id":  data.BinaryID,
			"name":      exp.Name,
		}, map[string]any{
			"name":    exp.Name,
			"address": exp.Address,
			"ordinal": exp.Ordinal,
		})
		if err == nil {
			s.graph.Link("CONTAINS", binaryID, expNode.ID, nil)
			stats["exports"] = stats["exports"].(int) + 1
		}
	}

	for _, sym := range data.Symbols {
		symNode, err := s.graph.UpsertNode("Symbol", map[string]any{
			"project_id": projectID,
			"binary_id":  data.BinaryID,
			"address":   sym.Address,
		}, map[string]any{
			"address": sym.Address,
			"name":    sym.Name,
			"binding": sym.Binding,
			"type":    sym.Type,
		})
		if err == nil {
			s.graph.Link("CONTAINS", binaryID, symNode.ID, nil)
			stats["symbols"] = stats["symbols"].(int) + 1
		}
	}

	for _, t := range data.Types {
		typeNode, err := s.graph.UpsertNode("Type", map[string]any{
			"project_id": projectID,
			"binary_id":  data.BinaryID,
			"name":      t.Name,
		}, map[string]any{
			"name":   t.Name,
			"size":   t.Size,
			"fields": t.Fields,
		})
		if err == nil {
			s.graph.Link("CONTAINS", binaryID, typeNode.ID, nil)
			stats["types"] = stats["types"].(int) + 1
		}
	}

	for _, reg := range data.Registers {
		regNode, err := s.graph.UpsertNode("Register", map[string]any{
			"project_id": projectID,
			"binary_id":  data.BinaryID,
			"name":      reg.Name,
		}, map[string]any{
			"name":      reg.Name,
			"role":     reg.Role,
			"width_bits": reg.WidthBits,
		})
		if err == nil {
			s.graph.Link("CONTAINS", binaryID, regNode.ID, nil)
			stats["registers"] = stats["registers"].(int) + 1
		}
	}

	for _, c := range data.Constants {
		constNode, err := s.graph.UpsertNode("Constant", map[string]any{
			"project_id": projectID,
			"binary_id":  data.BinaryID,
			"value":    c.Value,
		}, map[string]any{
			"value":   c.Value,
			"type":    c.Type,
			"address": c.Address,
		})
		if err == nil {
			s.graph.Link("CONTAINS", binaryID, constNode.ID, nil)
			stats["constants"] = stats["constants"].(int) + 1
		}
	}

	for _, api := range data.ExternalAPIs {
		apiNode, err := s.graph.UpsertNode("ExternalAPI", map[string]any{
			"project_id": projectID,
			"binary_id":  data.BinaryID,
			"name":      api.Name,
		}, map[string]any{
			"name":          api.Name,
			"module":        api.Module,
			"return_type":   api.ReturnType,
			"argument_types": api.ArgumentTypes,
		})
		if err == nil {
			s.graph.Link("CONTAINS", binaryID, apiNode.ID, nil)
			stats["extapis"] = stats["extapis"].(int) + 1
		}
	}

	return map[string]any{
		"binary_id": data.BinaryID,
		"stats":     stats,
	}, nil
}

func (s *ReverseEngineeringService) MapSourceFunction(binaryFunctionID, sourceFunctionID string, confidence float64, method string) (map[string]any, error) {
	if _, err := s.graph.Link("EQUIVALENT_TO", binaryFunctionID, sourceFunctionID, map[string]any{
		"equivalence_status": "SUSPECTED",
		"confidence":         confidence,
		"validation_method":  method,
	}); err != nil {
		return nil, err
	}
	return map[string]any{
		"binary_function_id": binaryFunctionID,
		"source_function_id": sourceFunctionID,
		"equivalence_status": "SUSPECTED",
		"confidence":         confidence,
	}, nil
}

func (s *ReverseEngineeringService) RecordPort(binaryFunctionID, implementationID, language string, confidence float64) (map[string]any, error) {
	if _, err := s.graph.Link("PORTED_TO", binaryFunctionID, implementationID, map[string]any{
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
		"equivalence_status": equivStatus,
		"confidence":         confidence,
		"validation_method":  method,
		"test_reference":     test.ID,
	})
	return map[string]any{
		"test_id":            test.ID,
		"equivalence_status": equivStatus,
	}, nil
}

// RecordRuntimeTrace stores a runtime execution trace.
func (s *ReverseEngineeringService) RecordRuntimeTrace(projectID string, trace reverse.RuntimeTrace) (map[string]any, error) {
	traceNode, err := s.graph.UpsertNode("RuntimeTrace", map[string]any{
		"project_id": projectID,
		"binary_id":  trace.BinaryID,
		"trace_id":   trace.TraceID,
	}, map[string]any{
		"function_addr": trace.FunctionAddr,
		"timestamp":     trace.Timestamp,
	})
	if err != nil {
		return nil, err
	}

	for _, insn := range trace.Instructions {
		insnNode, err := s.graph.UpsertNode("TraceInstruction", map[string]any{
			"project_id": projectID,
			"trace_id":   trace.TraceID,
			"address":    insn.Address,
		}, map[string]any{
			"mnemonic":  insn.Mnemonic,
			"operands":  insn.Operands,
			"registers": insn.Registers,
		})
		if err == nil {
			s.graph.Link("CONTAINS", traceNode.ID, insnNode.ID, nil)
		}
	}

	for _, read := range trace.MemoryReads {
		memNode, err := s.graph.UpsertNode("TraceMemoryRead", map[string]any{
			"project_id": projectID,
			"trace_id":   trace.TraceID,
			"address":    read.Address,
		}, map[string]any{
			"size":  read.Size,
			"value": read.Value,
		})
		if err == nil {
			s.graph.Link("CONTAINS", traceNode.ID, memNode.ID, nil)
		}
	}

	for _, write := range trace.MemoryWrites {
		memNode, err := s.graph.UpsertNode("TraceMemoryWrite", map[string]any{
			"project_id": projectID,
			"trace_id":   trace.TraceID,
			"address":    write.Address,
		}, map[string]any{
			"size":  write.Size,
			"value": write.Value,
		})
		if err == nil {
			s.graph.Link("CONTAINS", traceNode.ID, memNode.ID, nil)
		}
	}

	return map[string]any{"trace_id": trace.TraceID, "status": "recorded"}, nil
}

// RecordHypothesis stores a competing hypothesis for reverse engineering.
func (s *ReverseEngineeringService) RecordHypothesis(projectID string, hypothesis reverse.Hypothesis) (map[string]any, error) {
	hypNode, err := s.graph.UpsertNode("Hypothesis", map[string]any{
		"project_id":    projectID,
		"hypothesis_id": hypothesis.ID,
	}, map[string]any{
		"subject_id": hypothesis.SubjectID,
		"claim":      hypothesis.Claim,
		"confidence": hypothesis.Confidence,
		"status":     hypothesis.Status,
		"analyst":    hypothesis.Analyst,
		"created_at": hypothesis.CreatedAt,
		"updated_at": hypothesis.UpdatedAt,
	})
	if err != nil {
		return nil, err
	}

	// Link from subject to hypothesis for GetHypotheses compatibility
	s.graph.Link("SUSPECTED_AS", hypothesis.SubjectID, hypNode.ID, map[string]any{
		"confidence": hypothesis.Confidence,
		"state":      hypothesis.Status,
	})

	for _, ev := range hypothesis.EvidenceFor {
		evNode, err := s.graph.UpsertNode("Evidence", map[string]any{
			"project_id":  projectID,
			"evidence_id": ev.ID,
		}, map[string]any{
			"description": ev.Description,
			"confidence":  ev.Confidence,
			"kind":        ev.Kind,
		})
		if err == nil {
			s.graph.Link("SUPPORTS", hypNode.ID, evNode.ID, nil)
		}
	}

	for _, ev := range hypothesis.EvidenceAgainst {
		evNode, err := s.graph.UpsertNode("Evidence", map[string]any{
			"project_id":  projectID,
			"evidence_id": ev.ID,
		}, map[string]any{
			"description": ev.Description,
			"confidence":  ev.Confidence,
			"kind":        ev.Kind,
		})
		if err == nil {
			s.graph.Link("CONTRADICTS", hypNode.ID, evNode.ID, nil)
		}
	}

	return map[string]any{"hypothesis_id": hypothesis.ID, "status": "recorded"}, nil
}

// RecordBehavioralEquivalence records a behavioral equivalence comparison result.
func (s *ReverseEngineeringService) RecordBehavioralEquivalence(projectID string, equiv reverse.BehavioralEquivalence) (map[string]any, error) {
	equivNode, err := s.graph.UpsertNode("BehavioralEquivalence", map[string]any{
		"project_id":         projectID,
		"binary_function_id": equiv.BinaryFunctionID,
		"source_function_id": equiv.SourceFunctionID,
	}, map[string]any{
		"equivalence_status": equiv.EquivalenceStatus,
		"confidence":         equiv.Confidence,
		"method":             equiv.Method,
		"analyst":            equiv.Analyst,
		"created_at":         equiv.CreatedAt,
	})
	if err != nil {
		return nil, err
	}

	for _, tc := range equiv.TestCases {
		tcNode, err := s.graph.UpsertNode("EquivalenceTestCase", map[string]any{
			"project_id": projectID,
			"equiv_id":   equiv.BinaryFunctionID + ":" + equiv.SourceFunctionID,
		}, map[string]any{
			"input":      tc.Input,
			"binary_out": tc.BinaryOut,
			"source_out": tc.SourceOut,
			"match":      tc.Match,
		})
		if err == nil {
			s.graph.Link("CONTAINS", equivNode.ID, tcNode.ID, nil)
		}
	}

	for _, diff := range equiv.Differences {
		diffNode, err := s.graph.UpsertNode("EquivalenceDifference", map[string]any{
			"project_id": projectID,
			"equiv_id":   equiv.BinaryFunctionID + ":" + equiv.SourceFunctionID,
		}, map[string]any{
			"type":        diff.Type,
			"description": diff.Description,
			"severity":    diff.Severity,
		})
		if err == nil {
			s.graph.Link("CONTAINS", equivNode.ID, diffNode.ID, nil)
		}
	}

	return map[string]any{"status": "recorded", "equivalence_status": equiv.EquivalenceStatus}, nil
}
