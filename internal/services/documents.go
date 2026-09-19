package services

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"codergag/internal/graph"
	"codergag/internal/models"
	"codergag/internal/search"
)

type DocumentService struct {
	graph  graph.GraphRepository
	ranker search.Ranker
	known  map[string]bool
}

func NewDocumentService(g graph.GraphRepository) *DocumentService {
	return &DocumentService{graph: g, ranker: search.NewBM25()}
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

	headingRe := regexp.MustCompile(`(?m)^(#{1,6})\s+(.+?)\s*$`)
	headings := headingRe.FindAllStringSubmatchIndex(content, -1)
	var sections []map[string]any
	for i, match := range headings {
		heading := content[match[4]:match[5]]
		lineStart := strings.Count(content[:match[0]], "\n") + 1
		end := len(content)
		if i+1 < len(headings) {
			end = headings[i+1][0]
		}
		sectionContent := strings.TrimSpace(content[match[1]:end])
		if len(sectionContent) > 12000 {
			sectionContent = sectionContent[:12000]
		}
		level := match[3] - match[2]
		sec, err := s.graph.UpsertNode("DocumentSection", map[string]any{
			"project_id":  projectID,
			"document_id": document.ID,
			"heading":     strings.TrimSpace(heading),
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
	nodes, err := s.graph.FindNodes("DocumentSection", map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	return rankNodes(s.ranker, nodes, query, limit, func(n *models.Node) []search.Field {
		return []search.Field{
			{Text: strProp(n, "heading"), Weight: 3},
			{Text: strProp(n, "content"), Weight: 1},
		}
	}), nil
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

// VerifyDesign checks a design document against the graph: identifiers it
// mentions that exist are "verified", and code-like references that do not exist
// are returned as "unverified" (with a rename suggestion when one is close).
func (s *DocumentService) VerifyDesign(projectID, documentID string) (map[string]any, error) {
	doc, err := s.graph.GetNode(documentID)
	if err != nil || doc.Properties["project_id"] != projectID {
		return nil, &ServiceError{Message: "document is absent or belongs to another project"}
	}
	sections, _ := s.graphSections(doc)
	ix := s.symbolIndex(projectID)
	var claims, unverified []map[string]any
	seen := map[string]bool{}
	for _, sec := range sections {
		heading := strProp(sec, "heading")
		strong, weak, _ := docCodeRefs(heading + "\n" + strProp(sec, "content"))
		isStrong := map[string]bool{}
		for _, st := range strong {
			isStrong[st] = true
		}
		for _, id := range append(append([]string{}, strong...), weak...) {
			if seen[id] {
				continue
			}
			seen[id] = true
			if _, ok := ix.names[id]; ok {
				claims = append(claims, map[string]any{"identifier": id, "section": heading, "verified": true})
				continue
			}
			if !isStrong[id] {
				continue
			}
			row := map[string]any{"identifier": id, "section": heading}
			if sug := ix.suggest(id); sug != "" {
				row["did_you_mean"] = sug
			}
			unverified = append(unverified, row)
		}
	}
	return map[string]any{
		"document_id":           documentID,
		"verified":              len(claims),
		"unverified":            len(unverified),
		"claims":                claims,
		"unverified_references": unverified,
	}, nil
}
