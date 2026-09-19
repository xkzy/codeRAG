package services

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"codergag/internal/graph"
)

// ExtractText returns the readable text of a document, using format-specific
// loaders for PDF, DOC, and DOCX and falling back to raw file reads for
// everything else. The result is what gets stored in the graph.
func ExtractText(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return extractPDFText(path)
	case ".doc":
		return extractDOCText(path)
	case ".docx":
		return extractDOCXText(path)
	default:
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
}

func extractPDFText(path string) (string, error) {
	cmd := exec.Command("pdftotext", "-layout", path, "-")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pdftotext failed: %w", err)
	}
	return collapseWhitespace(string(out)), nil
}

func extractDOCText(path string) (string, error) {
	out, err := exec.Command("antiword", path).Output()
	if err != nil {
		return "", fmt.Errorf("antiword failed: %w", err)
	}
	return collapseWhitespace(string(out)), nil
}

func extractDOCXText(path string) (string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer r.Close()

	var xmlData []byte
	for _, f := range r.File {
		if strings.EqualFold(f.Name, "word/document.xml") {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			xmlData, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return "", err
			}
			break
		}
	}
	if xmlData == nil {
		return "", fmt.Errorf("word/document.xml not found in %s", path)
	}
	text, err := docxText(xmlData)
	if err != nil {
		return "", fmt.Errorf("docx parse failed: %w", err)
	}
	return collapseWhitespace(text), nil
}

func docxText(data []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var sb strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if char, ok := tok.(xml.CharData); ok {
			sb.WriteString(string(char))
		}
	}
	return sb.String(), nil
}

// collapseWhitespace collapses runs of whitespace to single spaces.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// splitDocumentSections splits extracted text into sections for the graph.
// Markdown headings are preserved; otherwise paragraphs or fixed chunks are
// used. Each section is bounded to 12000 runes of content.
func splitDocumentSections(content string) []map[string]any {
	headingRe := regexp.MustCompile(`(?m)^(#{1,6})\s+(.+?)\s*$`)
	headings := headingRe.FindAllStringSubmatchIndex(content, -1)
	if len(headings) > 0 {
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
			sections = append(sections, map[string]any{
				"heading":    strings.TrimSpace(heading),
				"content":    sectionContent,
				"level":      level,
				"line_start": lineStart,
			})
		}
		return sections
	}

	paragraphs := strings.Split(content, "\n\n")
	var sections []map[string]any
	pos := 0
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			pos += len(p) + 2
			continue
		}
		if len(p) > 12000 {
			p = p[:12000]
		}
		lineStart := strings.Count(content[:pos], "\n") + 1
		sections = append(sections, map[string]any{
			"heading":    firstLine(p),
			"content":    p,
			"level":      0,
			"line_start": lineStart,
		})
		pos += len(p) + 2
	}
	if len(sections) == 0 && content != "" {
		c := strings.TrimSpace(content)
		if len(c) > 12000 {
			c = c[:12000]
		}
		sections = append(sections, map[string]any{
			"heading":    firstLine(c),
			"content":    c,
			"level":      0,
			"line_start": 1,
		})
	}
	return sections
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// IndexDocument loads a PDF, DOC, DOCX, or text file into the graph as a
// Document node with DocumentSection children, so LLM tools can search and
// reference it.
func (s *DocumentService) IndexDocument(projectID, path string) (map[string]any, error) {
	file, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	content, err := ExtractText(file)
	if err != nil {
		return nil, err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	format := strings.ToLower(filepath.Ext(file))

	document, err := s.graph.UpsertNode("Document", map[string]any{
		"project_id": projectID,
		"path":       file,
	}, map[string]any{
		"hash":          digest,
		"title":         filepath.Base(file),
		"source_format": format,
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

	sections := splitDocumentSections(content)
	for i, sec := range sections {
		heading := sec["heading"].(string)
		sectionContent := sec["content"].(string)
		level, _ := sec["level"].(int)
		lineStart, _ := sec["line_start"].(int)
		secNode, err := s.graph.UpsertNode("DocumentSection", map[string]any{
			"project_id":  projectID,
			"document_id": document.ID,
			"heading":     strings.TrimSpace(heading),
		}, map[string]any{
			"level":      level,
			"content":    sectionContent,
			"line_start": lineStart,
		})
		if err == nil {
			s.graph.Link("CONTAINS", document.ID, secNode.ID, nil)
			sections[i] = Present(secNode)
		}
	}
	return map[string]any{
		"document_id": document.ID,
		"path":        file,
		"sections":    len(sections),
		"format":      format,
	}, nil
}
