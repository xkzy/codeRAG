package models

import (
	"time"
)

type KnowledgeState string

const (
	StateFact        KnowledgeState = "FACT"
	StateObservation KnowledgeState = "OBSERVATION"
	StateInference   KnowledgeState = "INFERENCE"
	StateHypothesis  KnowledgeState = "HYPOTHESIS"
	StateConfirmed   KnowledgeState = "CONFIRMED"
)

type EquivalenceStatus string

const (
	EquivUnknown      EquivalenceStatus = "UNKNOWN"
	EquivSuspected    EquivalenceStatus = "SUSPECTED"
	EquivPartial      EquivalenceStatus = "PARTIAL"
	EquivValidated    EquivalenceStatus = "VALIDATED"
	EquivContradicted EquivalenceStatus = "CONTRADICTED"
)

type Node struct {
	ID         string
	Kind       string
	Properties map[string]any
}

func NewNode(kind string, identity, properties map[string]any) *Node {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	props := make(map[string]any, len(identity)+len(properties)+3)
	for k, v := range identity {
		props[k] = v
	}
	for k, v := range properties {
		props[k] = v
	}
	if _, ok := props["id"]; !ok {
		props["id"] = NewID()
	}
	props["created_at"] = now
	props["updated_at"] = now
	if _, ok := props["project_id"]; !ok {
		props["project_id"] = identity["project_id"]
	}
	return &Node{
		ID:         props["id"].(string),
		Kind:       kind,
		Properties: props,
	}
}

func (n *Node) SetProperty(key string, value any) {
	n.Properties[key] = value
	n.Properties["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
}

type Edge struct {
	ID         string
	Kind       string
	FromID     string
	ToID       string
	Properties map[string]any
}

func NewEdge(kind, fromID, toID string, properties map[string]any) *Edge {
	if properties == nil {
		properties = make(map[string]any)
	}
	return &Edge{
		ID:         NewID(),
		Kind:       kind,
		FromID:     fromID,
		ToID:       toID,
		Properties: properties,
	}
}
