package services

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"codergag/internal/graph"
)

// hasCommand checks if an external command is available on PATH.
func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

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
	case ".csv":
		return extractCSVText(path, ',')
	case ".tsv":
		return extractCSVText(path, '\t')
	case ".xlsx":
		return extractXLSXText(path)
	default:
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
}

// extractPDFText tries pdftotext; falls back to raw text extraction.
func extractPDFText(path string) (string, error) {
	if hasCommand("pdftotext") {
		out, err := exec.Command("pdftotext", "-layout", path, "-").Output()
		if err == nil {
			return collapseWhitespace(string(out)), nil
		}
	}
	// Fallback: read raw bytes as text (PDFs are mostly text with some binary markers)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("pdftotext not available and raw read failed: %w", err)
	}
	text := extractTextFromPDFBytes(data)
	if text == "" {
		return "", fmt.Errorf("could not extract text from PDF: pdftotext not available and no extractable text found")
	}
	return collapseWhitespace(text), nil
}

// extractTextFromPDFBytes does a best-effort text extraction from raw PDF bytes
// by reading text between BT/ET markers (basic PDF text objects).
func extractTextFromPDFBytes(data []byte) string {
	var sb strings.Builder
	btRe := regexp.MustCompile(`(?s)BT\b(.*?)\bET`)
	for _, block := range btRe.FindAll(data, -1) {
		for _, s := range extractPDFStrings(block) {
			sb.WriteString(s)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// extractPDFStrings extracts literal strings from PDF content, handling nested
// parentheses and escape sequences correctly by parsing character-by-character.
func extractPDFStrings(data []byte) []string {
	var results []string
	i := 0
	for i < len(data) {
		if data[i] == '(' {
			s, end := parsePDFString(data, i)
			if s != "" {
				results = append(results, s)
			}
			i = end
		} else {
			i++
		}
	}
	return results
}

// parsePDFString parses a PDF literal string starting at '(' and returns the
// decoded string and the position after the closing ')'.
func parsePDFString(data []byte, start int) (string, int) {
	var sb strings.Builder
	depth := 0
	i := start
	for i < len(data) {
		c := data[i]
		if c == '\\' {
			// Escape sequence
			if i+1 < len(data) {
				next := data[i+1]
				switch next {
				case 'n':
					sb.WriteByte('\n')
				case 'r':
					sb.WriteByte('\r')
				case 't':
					sb.WriteByte('\t')
				case 'b':
					sb.WriteByte('\b')
				case 'f':
					sb.WriteByte('\f')
				case '(':
					sb.WriteByte('(')
				case ')':
					sb.WriteByte(')')
				case '\\':
					sb.WriteByte('\\')
				case '\n':
					// Skip escaped newline
				case '\r':
					// Skip escaped carriage return
				default:
					// Octal escape \ddd or unknown, pass through the escaped char
					if next >= '0' && next <= '7' {
						// Collect up to 3 octal digits
						j := i + 1
						for j < len(data) && j < i+4 && data[j] >= '0' && data[j] <= '7' {
							j++
						}
						octal := string(data[i+1 : j])
						if val, err := strconv.ParseInt(octal, 8, 64); err == nil {
							sb.WriteByte(byte(val))
						}
						i = j - 1
					} else {
						sb.WriteByte(next)
						i++
					}
				}
				i += 2
				continue
			}
			i++
		} else if c == '(' {
			depth++
			if depth > 1 {
				sb.WriteByte(c)
			}
			i++
		} else if c == ')' {
			if depth == 0 {
				return sb.String(), i + 1
			}
			depth--
			if depth > 0 {
				sb.WriteByte(c)
			}
			i++
		} else {
			sb.WriteByte(c)
			i++
		}
	}
	return sb.String(), i
}

// extractDOCText tries antiword; falls back to raw text read.
func extractDOCText(path string) (string, error) {
	if hasCommand("antiword") {
		out, err := exec.Command("antiword", path).Output()
		if err == nil {
			return collapseWhitespace(string(out)), nil
		}
	}
	// Fallback: try to read as text (some .doc files have readable text)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("antiword not available and raw read failed: %w", err)
	}
	text := extractTextFromDOCBytes(data)
	if text == "" {
		return "", fmt.Errorf("could not extract text from DOC: antiword not available and no extractable text found")
	}
	return collapseWhitespace(text), nil
}

// extractTextFromDOCBytes does a best-effort text extraction from legacy .doc format
// by scanning for printable ASCII sequences of 4+ characters.
func extractTextFromDOCBytes(data []byte) string {
	var sb strings.Builder
	var current strings.Builder
	for _, b := range data {
		if b >= 32 && b <= 126 || b == '\n' || b == '\t' {
			current.WriteByte(b)
		} else {
			if current.Len() >= 4 {
				sb.WriteString(current.String())
				sb.WriteString("\n")
			}
			current.Reset()
		}
	}
	if current.Len() >= 4 {
		sb.WriteString(current.String())
	}
	return sb.String()
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

// extractCSVText reads a CSV or TSV file and formats it as pipe-separated text.
// Rows are newline-separated, cells within a row are pipe-separated.
func extractCSVText(path string, delim rune) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.Comma = delim
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		return "", fmt.Errorf("csv parse failed: %w", err)
	}

	var sb strings.Builder
	for _, rec := range records {
		for i, cell := range rec {
			if i > 0 {
				sb.WriteString("|")
			}
			sb.WriteString(strings.TrimSpace(cell))
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// --- CSV/TSV/XLSX extraction functions ---

// extractCSVText reads a CSV or TSV file and formats it as pipe-separated text.
// Only the numeric/shared-string values are extracted; formatting is stripped.
func extractXLSXText(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer zr.Close()

	sharedStrings := parseXLSXSharedStrings(zr)
	sheetPaths := xlsxSheetPaths(zr)
	if len(sheetPaths) == 0 {
		return "", fmt.Errorf("no worksheets found in %s", path)
	}

	var sb strings.Builder
	for _, sheetPath := range sheetPaths {
		if sheetPath == "" {
			continue
		}
		rc, err := zr.Open(sheetPath)
		if err != nil {
			continue
		}
		rows := parseXLSXRows(rc, sharedStrings)
		rc.Close()

		sb.WriteString("=== Sheet: ")
		sb.WriteString(filepath.Base(sheetPath))
		sb.WriteString(" ===\n")
		for _, row := range rows {
			for i, cell := range row {
				if i > 0 {
					sb.WriteString("|")
				}
				sb.WriteString(cell)
			}
			sb.WriteString("\n")
		}
	}
	return sb.String(), nil
}

// parseXLSXRows uses a token-based XML decoder to extract cell values,
// being namespace-agnostic since XLSX uses the spreadsheetml namespace.
func parseXLSXRows(r io.Reader, sharedStrings map[int]string) [][]string {
	dec := xml.NewDecoder(r)
	var rows [][]string
	var currentRow []string
	var currentCell string
	var currentType string
	var inCell bool
	var cellBuffer strings.Builder

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return rows
		}
		switch se := tok.(type) {
		case xml.StartElement:
			local := se.Name.Local
			switch local {
			case "row":
				currentRow = nil
			case "c":
				inCell = true
				currentCell = ""
				currentType = ""
				cellBuffer.Reset()
				for _, attr := range se.Attr {
					if attr.Name.Local == "t" {
						currentType = attr.Value
					}
				}
			}
		case xml.CharData:
			if inCell {
				cellBuffer.Write(se)
			}
		case xml.EndElement:
			local := se.Name.Local
			switch local {
			case "v":
				currentCell = strings.TrimSpace(cellBuffer.String())
			case "c":
				val := currentCell
				if currentType == "s" {
					if idx, err := strconv.Atoi(val); err == nil {
						val = sharedStrings[idx]
					}
				}
				currentRow = append(currentRow, val)
				inCell = false
			case "row":
				if currentRow != nil {
					rows = append(rows, currentRow)
				}
			}
		}
	}
	return rows
}

// xlsxSheetPaths returns the sorted list of worksheet file paths inside the zip.
func xlsxSheetPaths(zr *zip.ReadCloser) []string {
	var sheets []string
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			sheets = append(sheets, f.Name)
		}
	}
	return xlsxSortSheets(sheets)
}

func xlsxSortSheets(paths []string) []string {
	// Extract sheet number for proper ordering: sheet1.xml = 1, sheet2.xml = 2, etc.
	type named struct {
		path  string
		index int
	}
	var namedList []named
	for _, p := range paths {
		base := filepath.Base(p)
		numStr := strings.TrimSuffix(strings.TrimPrefix(base, "sheet"), ".xml")
		if n, err := strconv.Atoi(numStr); err == nil {
			namedList = append(namedList, named{path: p, index: n})
		} else {
			namedList = append(namedList, named{path: p, index: 999})
		}
	}
	// Simple insertion sort for small lists
	for i := 1; i < len(namedList); i++ {
		for j := i; j > 0 && namedList[j-1].index > namedList[j].index; j-- {
			namedList[j-1], namedList[j] = namedList[j], namedList[j-1]
		}
	}
	var result []string
	for _, n := range namedList {
		result = append(result, n.path)
	}
	return result
}

func parseXLSXSharedStrings(zr *zip.ReadCloser) map[int]string {
	ss := make(map[int]string)
	for _, f := range zr.File {
		if f.Name == "xl/sharedStrings.xml" {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			var doc struct {
				Items []struct {
					Text string `xml:"t"`
				} `xml:"si"`
			}
			dec := xml.NewDecoder(rc)
			if err := dec.Decode(&doc); err == nil {
				for i, item := range doc.Items {
					ss[i] = item.Text
				}
			}
			rc.Close()
		}
	}
	return ss
}

// splitTableSections splits CSV/TSV content into sections.
// The first row is treated as a header row. Each subsequent row becomes a section
// with the heading derived from the first cell (or "row N").
func splitTableSections(content string, delimiter rune) []map[string]any {
	reader := csv.NewReader(strings.NewReader(content))
	reader.Comma = delimiter
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil || len(records) == 0 {
		return nil
	}

	var sections []map[string]any
	header := records[0]
	rows := records[1:]

	if len(rows) == 0 {
		// Just a header row
		sections = append(sections, map[string]any{
			"heading":    firstLine(strings.Join(header, " | ")),
			"content":    strings.Join(header, "\n"),
			"level":      0,
			"line_start": 1,
		})
		return sections
	}

	for i, row := range rows {
		heading := firstLine(row[0])
		if heading == "" {
			heading = fmt.Sprintf("row %d", i+1)
		}
		var sb strings.Builder
		sb.WriteString(strings.Join(header, "\n"))
		sb.WriteString("\n")
		sb.WriteString(strings.Join(row, "\n"))
		sections = append(sections, map[string]any{
			"heading":    heading,
			"content":    sb.String(),
			"level":      0,
			"line_start": i + 2,
		})
	}
	return sections
}

// splitXLSXSections splits XLSX-extracted content into sections.
// Sheet boundaries create separate sections.
func splitXLSXSections(content string) []map[string]any {
	sheets := strings.Split(content, "=== Sheet: ")
	if len(sheets) <= 1 {
		return splitDocumentSections(content)
	}

	var sections []map[string]any
	linePos := 1
	for _, sheet := range sheets {
		if sheet == "" {
			continue
		}
		end := strings.Index(sheet, "===")
		var title, body string
		if end >= 0 {
			title = strings.TrimSpace(sheet[:end])
			body = strings.TrimSpace(sheet[end+3:])
		} else {
			title = "Sheet"
			body = strings.TrimSpace(sheet)
		}
		if body == "" {
			continue
		}
		if len(body) > 12000 {
			body = body[:12000]
		}
		sections = append(sections, map[string]any{
			"heading":    title,
			"content":    body,
			"level":      0,
			"line_start": linePos,
		})
		linePos += strings.Count(body, "\n")
	}
	return sections
}

// indexTableSections indexes CSV/TSV content as structured table nodes with
// column metadata and row data. Each row becomes a TableRow node linked to the
// Document via CONTAINS. Column names are extracted from the header row.
func (s *DocumentService) indexTableSections(projectID, documentID, path string, delimiter rune) ([]string, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.Comma = delimiter
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil || len(records) == 0 {
		return nil, 0, err
	}

	header := records[0]
	rows := records[1:]

	// Store table schema as a separate node
	schema, err := s.graph.UpsertNode("TableSchema", map[string]any{
		"project_id":   projectID,
		"document_id":  documentID,
	}, map[string]any{
		"columns": header,
		"row_count": len(rows),
		"delimiter": string(delimiter),
	})
	if err == nil {
		s.graph.Link("CONTAINS", documentID, schema.ID, nil)
	}

	for i, row := range rows {
		if len(row) < len(header) {
			// Pad short rows
			padded := make([]string, len(header))
			copy(padded, row)
			row = padded
		}
		rowMap := make(map[string]any, len(header))
		for j, col := range header {
			if j < len(row) {
				rowMap[col] = row[j]
			}
		}
		rowNode, err := s.graph.UpsertNode("TableRow", map[string]any{
			"project_id":   projectID,
			"document_id":  documentID,
			"row_index":    i,
			"heading":      firstLine(row[0]),
			"identity_col": map[string]any{"document_id": documentID, "row_index": i},
		}, map[string]any{
			"row_index":  i,
			"data":       rowMap,
			"content":    strings.Join(row, "\n"),
		})
		if err == nil {
			s.graph.Link("CONTAINS", documentID, rowNode.ID, nil)
			s.graph.Link("HAS_SCHEMA", schema.ID, rowNode.ID, nil)
		}
	}

	return header, len(rows), nil
}

// QueryTable retrieves rows from a table document that match the given filter.
// Columns and row indices are returned as structured data.
func (s *DocumentService) QueryTable(projectID, documentID, filterCol string, filterVal string, limit int) ([]map[string]any, error) {
	document, err := s.graph.GetNode(documentID)
	if err != nil {
		return nil, err
	}
	if document.Properties["project_id"] != projectID {
		return nil, &ServiceError{Message: "document belongs to another project"}
	}

	// Find the schema
	schemas, _ := s.graph.FindNodes("TableSchema", map[string]any{
		"project_id":  projectID,
		"document_id": documentID,
	})

	var colNames []string
	if len(schemas) > 0 {
		if cols, ok := schemas[0].Properties["columns"].([]string); ok {
			colNames = cols
		} else if cols, ok := schemas[0].Properties["columns"].([]any); ok {
			for _, c := range cols {
				colNames = append(colNames, c.(string))
			}
		}
	}

	// Find rows
	rows, _ := s.graph.FindNodes("TableRow", map[string]any{
		"project_id":  projectID,
		"document_id": documentID,
	})

	var results []map[string]any
	for _, row := range rows {
		rowMap, ok := row.Properties["data"].(map[string]any)
		if !ok {
			continue
		}

		// Apply filter
		if filterCol != "" {
			if val, exists := rowMap[filterCol]; exists {
				if val != filterVal && fmt.Sprintf("%v", val) != filterVal {
					continue
				}
			} else {
				continue
			}
		}

		result := map[string]any{
			"row_index": row.Properties["row_index"],
		}
		for _, col := range colNames {
			result[col] = rowMap[col]
		}
		results = append(results, result)

		if limit > 0 && len(results) >= limit {
			break
		}
	}

	sortTableRowsByIndex(results)
	return results, nil
}

// sortTableRowsByIndex sorts results by row_index ascending.
func sortTableRowsByIndex(results []map[string]any) {
	sort.Slice(results, func(i, j int) bool {
		ri, _ := results[i]["row_index"].(int)
		rj, _ := results[j]["row_index"].(int)
		return ri < rj
	})
}

// GetTableSchema returns the column names and row count for a table document.
func (s *DocumentService) GetTableSchema(projectID, documentID string) (map[string]any, error) {
	document, err := s.graph.GetNode(documentID)
	if err != nil {
		return nil, err
	}
	if document.Properties["project_id"] != projectID {
		return nil, &ServiceError{Message: "document belongs to another project"}
	}

	formats := []string{".csv", ".tsv", ".xlsx"}
	isTable := false
	format := document.Properties["source_format"].(string)
	for _, f := range formats {
		if format == f {
			isTable = true
			break
		}
	}
	if !isTable {
		return nil, &ServiceError{Message: "not a tabular document"}
	}

	schemas, _ := s.graph.FindNodes("TableSchema", map[string]any{
		"project_id":  projectID,
		"document_id": documentID,
	})

	if len(schemas) == 0 {
		return map[string]any{
			"columns":  []string{},
			"row_count": 0,
			"format":    format,
		}, nil
	}

	schema := schemas[0]
	return map[string]any{
		"columns":         schema.Properties["columns"],
		"row_count":       schema.Properties["row_count"],
		"delimiter":       schema.Properties["delimiter"],
		"format":          format,
		"document_id":     documentID,
		"document_title":  document.Properties["title"],
	}, nil
}

// IndexDocument loads a PDF, DOC, DOCX, CSV, TSV, XLSX, or text file into the graph as a
// Document node with DocumentSection (or TableRow) children, so LLM tools can search and
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
	if format == ".csv" {
		colNames, rowCount, err := s.indexTableSections(projectID, document.ID, file, ',')
		if err == nil {
			return map[string]any{
				"document_id":  document.ID,
				"path":         file,
				"sections":     rowCount,
				"format":       format,
				"table_schema": colNames,
				"row_count":    rowCount,
			}, nil
		}
	} else if format == ".tsv" {
		colNames, rowCount, err := s.indexTableSections(projectID, document.ID, file, '\t')
		if err == nil {
			return map[string]any{
				"document_id":  document.ID,
				"path":         file,
				"sections":     rowCount,
				"format":       format,
				"table_schema": colNames,
				"row_count":    rowCount,
			}, nil
		}
	} else if format == ".xlsx" {
		sections = splitXLSXSections(content)
	}
	for i, sec := range sections {
		heading := mapStrProp(sec, "heading")
		sectionContent := mapStrProp(sec, "content")
		level := mapIntProp(sec, "level")
		lineStart := mapIntProp(sec, "line_start")
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
