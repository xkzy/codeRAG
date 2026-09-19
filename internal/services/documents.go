package services

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"codergag/internal/graph"
)

type DocumentService struct {
	graph graph.GraphRepository
}

func NewDocumentService(g graph.GraphRepository) *DocumentService {
	return &DocumentService{graph: g}
}

func (s *DocumentService) IndexMarkdown(projectID, path string) (map[string]any, error) {
	file, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	text, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	content := string(text)

	digest := fmt.Sprintf("%x", sha256.Sum256(text))
	document, err := s.graph.UpsertNode("Document", map[string]any{
		"project_id": projectID,
		"path":       file,
	}, map[string]any{
		"hash":  digest,
		"title": filepath.Base(file),
	})
	if err != nil {
		return nil, err
	}

	old, _ := s.graph.Neighbors(document.ID, "CONTAINS", graph.DirOut)
	var oldIDs []string
	for _, en := range old {
		oldIDs = append(oldIDs, en.Node.ID)
	}
	if len(oldIDs) > 0 {
		s.graph.RemoveNodes(oldIDs)
	}

	projectIDNode, _ := s.graph.FindNodes("Project", map[string]any{"id": projectID})
	var parentID string
	if len(projectIDNode) > 0 {
		parentID = projectIDNode[0].ID
	} else {
		parentID = document.ID
	}
	s.graph.Link("CONTAINS", parentID, document.ID, nil)

	headingRe := regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
	headings := headingRe.FindAllStringSubmatchIndex(content, -1)
	var sections []map[string]any
	for i, match := range headings {
		heading := content[match[2]:match[3]]
		lineStart := strings.Count(content[:match[0]], "\n") + 1
		end := len(content)
		if i+1 < len(headings) {
			end = headings[i+1][0]
		}
		sectionContent := strings.TrimSpace(content[match[1]:end])
		if len(sectionContent) > 12000 {
			sectionContent = sectionContent[:12000]
		}
		level := len(content[match[0]:match[1]])
		sec, err := s.graph.UpsertNode("DocumentSection", map[string]any{
			"project_id":   projectID,
			"document_id":  document.ID,
			"heading":      strings.TrimSpace(heading),
		}, map[string]any{
			"level":      level,
			"content":    sectionContent,
			"line_start": lineStart,
		})
		if err == nil {
			s.graph.Link("CONTAINS", document.ID, sec.ID, nil)
			sections = append(sections, Present(sec))
		}
	}
	return map[string]any{
		"document_id": document.ID,
		"path":        file,
		"sections":    len(sections),
	}, nil
}

func (s *DocumentService) Search(projectID, query string, limit int) ([]map[string]any, error) {
	terms := []string{}
	for _, t := range regexp.MustCompile(`\w+`).FindAllString(strings.ToLower(query), -1) {
		if t != "" {
			terms = append(terms, t)
		}
	}
	nodes, err := s.graph.FindNodes("DocumentSection", map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	type scored struct {
		score int
		node  *modelsNode
	}
	var results []scored
	for _, n := range nodes {
		head, _ := n.Properties["heading"].(string)
		body, _ := n.Properties["content"].(string)
		haystack := strings.ToLower(head + " " + body)
		score := 0
		for _, term := range terms {
			score += strings.Count(haystack, term)
		}
		if score > 0 {
			results = append(results, scored{score, n})
		}
	}
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].score > results[i].score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
	if limit > len(results) {
		limit = len(results)
	}
	var out []map[string]any
	for i := 0; i < limit; i++ {
		n := results[i].node
		row := map[string]any{"score": results[i].score, "id": n.ID, "kind": n.Kind}
		for k, v := range n.Properties {
			row[k] = v
		}
		out = append(out, row)
	}
	return out, nil
}

type modelsNode = struct {
	ID         string
	Kind       string
	Properties map[string]any
}

func (s *DocumentService) ListSources(projectID string) ([]map[string]any, error) {
	nodes, err := s.graph.FindNodes("Document", map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, n := range nodes {
		row := map[string]any{"id": n.ID}
		for k, v := range n.Properties {
			row[k] = v
		}
		results = append(results, row)
	}
	return results, nil
}

func (s *DocumentService) RemoveSource(projectID, documentID string) (map[string]any, error) {
	document, err := s.graph.GetNode(documentID)
	if err != nil || document.Properties["project_id"] != projectID {
		return nil, &ServiceError{Message: "document is absent or belongs to another project"}
	}
	sections, _ := s.graph.Neighbors(document.ID, "CONTAINS", graph.DirOut)
	var ids []string
	for _, en := range sections {
		ids = append(ids, en.Node.ID)
	}
	s.graph.RemoveNodes(append(ids, document.ID))
	return map[string]any{"removed": true, "sections": len(sections)}, nil
}

func (s *DocumentService) VerifyDesign(projectID, documentID string) (map[string]any, error) {
	sections, _ := s.graph.Neighbors(documentID, "CONTAINS", graph.DirOut)
	fns, err := s.graph.FindNodes("Function", map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	symbols := make(map[string]bool)
	for _, n := range fns {
		if name, ok := n.Properties["name"].(string); ok {
			symbols[name] = true
		}
		if qname, ok := n.Properties["qualified_name"].(string); ok {
			symbols[qname] = true
		}
	}
	type claim struct {
		Identifier string `json:"identifier"`
		Section    string `json:"section"`
		Verified   bool   `json:"verified"`
	}
	var claims []map[string]any
	idRe := regexp.MustCompile(`\b[A-Za-z_]\w{2,}\b`)
	for _, en := range sections {
		content, _ := en.Node.Properties["content"].(string)
		heading, _ := en.Node.Properties["heading"].(string)
		identifiers := make(map[string]bool)
		for _, match := range idRe.FindAllString(content, -1) {
			identifiers[match] = true
		}
		for id := range identifiers {
			if symbols[id] {
				claims = append(claims, map[string]any{
					"identifier": id,
					"section":    heading,
					"verified":   true,
				})
			}
		}
	}
	return map[string]any{
		"document_id": documentID,
		"verified":    len(claims),
		"claims":      claims,
	}, nil
}
