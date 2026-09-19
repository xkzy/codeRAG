package graph

import (
	"errors"
	"regexp"

	"codergag/internal/models"
)

var kindPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

type Direction string

const (
	DirOut  Direction = "out"
	DirIn   Direction = "in"
	DirBoth Direction = "both"
)

type EdgeNode struct {
	Edge *models.Edge
	Node *models.Node
}

type GraphRepository interface {
	UpsertNode(kind string, identity, properties map[string]any) (*models.Node, error)
	GetNode(nodeID string) (*models.Node, error)
	FindNodes(kind string, filters map[string]any) ([]*models.Node, error)
	Link(kind, fromID, toID string, properties map[string]any) (*models.Edge, error)
	Neighbors(nodeID, edgeKind string, direction Direction) ([]EdgeNode, error)
	RemoveNodes(nodeIDs []string) error
	RemoveEdges(edgeIDs []string) error
	QueryReadonly(query string, params map[string]any) ([]map[string]any, error)
	Close() error
}

var ErrNotFound = errors.New("node not found")

func ValidateKind(kind string) error {
	if !kindPattern.MatchString(kind) {
		return errors.New("invalid graph type: " + kind)
	}
	return nil
}

func Present(node *models.Node) map[string]any {
	if node == nil {
		return nil
	}
	result := map[string]any{"id": node.ID, "kind": node.Kind}
	for k, v := range node.Properties {
		result[k] = v
	}
	return result
}
