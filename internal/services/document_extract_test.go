package services

import (
	"archive/zip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestExtractTextMarkdown(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.md")
	if err := os.WriteFile(p, []byte("# Title\n\nbody text"), 0o644); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractText(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "body text") {
		t.Fatalf("missing body: %q", text)
	}
}

func TestExtractTextDOCX(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.docx")
	if err := writeDOCX(t, p, "Hello DOCX"); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractText(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Hello DOCX") {
		t.Fatalf("missing text: %q", text)
	}
}

func TestExtractTextPDF(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not installed")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "d.pdf")
	if err := writeMinimalPDF(t, p, "Hello PDF"); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractText(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Hello PDF") {
		t.Fatalf("missing text: %q", text)
	}
}

func TestIndexDocumentDOCX(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.docx")
	app := ApplicationInMemory()
	if err := writeDOCX(t, p, "DOCX body for search"); err != nil {
		t.Fatal(err)
	}
	res, err := app.Documents.IndexDocument("p", p)
	if err != nil {
		t.Fatal(err)
	}
	if res["sections"] != 1 {
		t.Fatalf("sections: %v", res["sections"])
	}
	docs, err := app.Documents.Search("p", "search", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("search results: %v", docs)
	}
}

func TestIndexDocumentPDF(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not installed")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "d.pdf")
	if err := writeMinimalPDF(t, p, "PDF body for search"); err != nil {
		t.Fatal(err)
	}
	app := ApplicationInMemory()
	res, err := app.Documents.IndexDocument("p", p)
	if err != nil {
		t.Fatal(err)
	}
	if res["format"] != ".pdf" {
		t.Fatalf("format: %v", res["format"])
	}
	docs, err := app.Documents.Search("p", "search", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("search results: %v", docs)
	}
}

func TestIndexDocumentMarkdown(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.md")
	if err := os.WriteFile(p, []byte("# Title\n\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := ApplicationInMemory()
	res, err := app.Documents.IndexDocument("p", p)
	if err != nil {
		t.Fatal(err)
	}
	if res["sections"] != 1 {
		t.Fatalf("sections: %v", res["sections"])
	}
}

func writeDOCX(t *testing.T, path, text string) error {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()
	doc, err := zw.Create("word/document.xml")
	if err != nil {
		return err
	}
	escaped := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
	_, err = doc.Write([]byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body><w:p><w:r><w:t>` + escaped + `</w:t></w:r></w:p></w:body>
</w:document>`))
	return err
}

func writeMinimalPDF(t *testing.T, path, text string) error {
	t.Helper()
	escaped := strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)").Replace(text)
	body := fmt.Sprintf("BT\n/F1 12 Tf\n100 700 Td\n(%s) Tj\nET", escaped)
	objects := []string{
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj",
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj",
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj",
		fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj", len(body), body),
		"5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj",
	}
	var buf strings.Builder
	offsets := make([]int, len(objects))
	pos := 0
	header := "%PDF-1.4\n"
	buf.WriteString(header)
	pos = len(header)
	for i, obj := range objects {
		offsets[i] = pos
		buf.WriteString(obj)
		pos += len(obj)
	}
	xrefOff := buf.Len()
	var xref strings.Builder
	xref.WriteString(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", len(objects)+1))
	for _, off := range offsets {
		xref.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	buf.WriteString(xref.String())
	buf.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefOff))
	return os.WriteFile(path, []byte(buf.String()), 0o644)
}

func TestDOCXTextPreservesEntities(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.docx")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()
	doc, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = doc.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>caf&amp;é &lt;tag&gt;</w:t></w:r></w:p></w:body></w:document>`))
	if err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractText(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "caf&é") || !strings.Contains(text, "<tag>") {
		t.Fatalf("entity handling failed: %q", text)
	}
}

func TestSplitDocumentSectionsByHeadings(t *testing.T) {
	content := "# A\nbody A\n\n# B\nbody B"
	sections := splitDocumentSections(content)
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}
	if sections[0]["heading"] != "A" || sections[1]["heading"] != "B" {
		t.Fatalf("headings: %v", sections)
	}
}

func TestSplitDocumentSectionsByParagraphs(t *testing.T) {
	content := "First paragraph\n\nSecond paragraph"
	sections := splitDocumentSections(content)
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}
	if !strings.Contains(sections[0]["content"].(string), "First") {
		t.Fatalf("first content: %v", sections[0])
	}
}

// ---- CSV/TSV tests ----

func TestExtractTextCSV(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.csv")
	if err := os.WriteFile(p, []byte("name,age,city\nAlice,30,NYC\nBob,25,LA"), 0o644); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractText(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Alice") || !strings.Contains(text, "Bob") {
		t.Fatalf("missing data: %q", text)
	}
	if !strings.Contains(text, "|") {
		t.Fatalf("expected pipe separator in CSV output: %q", text)
	}
}

func TestExtractTextTSV(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.tsv")
	if err := os.WriteFile(p, []byte("name\tage\tcity\nAlice\t30\tNYC\nBob\t25\tLA"), 0o644); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractText(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Alice") || !strings.Contains(text, "Bob") {
		t.Fatalf("missing data: %q", text)
	}
}

func TestExtractTextCSVWithCommasInField(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.csv")
	if err := os.WriteFile(p, []byte("name,description\nAlice,\"Hello, world\""), 0o644); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractText(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Hello, world") {
		t.Fatalf("missing field with comma: %q", text)
	}
}

func TestSplitTableSectionsCSV(t *testing.T) {
	content := "name,age,city\nAlice,30,NYC\nBob,25,LA"
	sections := splitTableSections(content, ',')
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}
	if sections[0]["heading"] != "Alice" {
		t.Errorf("first heading: %v", sections[0]["heading"])
	}
	if sections[1]["heading"] != "Bob" {
		t.Errorf("second heading: %v", sections[1]["heading"])
	}
	// Verify header is included in content
	if !strings.Contains(sections[0]["content"].(string), "name") {
		t.Errorf("expected header in content: %v", sections[0]["content"])
	}
	if !strings.Contains(sections[0]["content"].(string), "Alice") {
		t.Errorf("expected row data in content: %v", sections[0]["content"])
	}
}

func TestSplitTableSectionsTSV(t *testing.T) {
	content := "name\tage\nAlice\t30\nBob\t25"
	sections := splitTableSections(content, '\t')
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}
}

func TestSplitTableSectionsHeaderOnly(t *testing.T) {
	content := "name,age,city"
	sections := splitTableSections(content, ',')
	if len(sections) != 1 {
		t.Fatalf("expected 1 section (header only), got %d", len(sections))
	}
}

func TestIndexDocumentCSV(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.csv")
	if err := os.WriteFile(p, []byte("name,age\nAlice,30\nBob,25"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := ApplicationInMemory()
	res, err := app.Documents.IndexDocument("p", p)
	if err != nil {
		t.Fatal(err)
	}
	if res["sections"] != 2 {
		t.Fatalf("expected 2 sections, got %v", res["sections"])
	}
	if res["format"] != ".csv" {
		t.Fatalf("format: %v", res["format"])
	}
}

func TestIndexDocumentTSV(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.tsv")
	if err := os.WriteFile(p, []byte("name\tage\nAlice\t30\nBob\t25"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := ApplicationInMemory()
	res, err := app.Documents.IndexDocument("p", p)
	if err != nil {
		t.Fatal(err)
	}
	if res["sections"] != 2 {
		t.Fatalf("expected 2 sections, got %v", res["sections"])
	}
	if res["format"] != ".tsv" {
		t.Fatalf("format: %v", res["format"])
	}
}

// ---- XLSX tests ----

func writeMinimalXLSX(t *testing.T, path string, sheets ...xlsxSheetData) error {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	// Write shared strings
	sharedStrings := []string{}
	for _, s := range sheets {
		for _, row := range s.Rows {
			for _, cell := range row.Cells {
				if cell.IsShared {
					sharedStrings = append(sharedStrings, cell.Value)
				}
			}
		}
	}

	ssXML := buildXlsxSharedStringsXML(sharedStrings)
	ssFile, err := zw.Create("xl/sharedStrings.xml")
	if err != nil {
		return err
	}
	ssFile.Write([]byte(ssXML))

	for i, sheet := range sheets {
		sheetPath := fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)
		wsFile, err := zw.Create(sheetPath)
		if err != nil {
			return err
		}
		wsFile.Write([]byte(buildXlsxSheetXML(sheet, len(sharedStrings))))
	}

	// Write minimum required metadata files
	typesFile, err := zw.Create("[Content_Types].xml")
	if err != nil {
		return err
	}
	typesFile.Write([]byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
</Types>`))

	relsFile, err := zw.Create("_rels/.rels")
	if err != nil {
		return err
	}
	relsFile.Write([]byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`))

	wbFile, err := zw.Create("xl/workbook.xml")
	if err != nil {
		return err
	}
	wbFile.Write([]byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
		buildXlsxWorkbookSheets(len(sheets)) +
		`</workbook>`))

	wbRelsFile, err := zw.Create("xl/_rels/workbook.xml.rels")
	if err != nil {
		return err
	}
	wbRelsFile.Write([]byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		buildXlsxWorkbookRels(len(sheets)) +
		`</Relationships>`))

	return nil
}

type xlsxSheetData struct {
	Name string
	Rows []xlsxRowData
}

type xlsxRowData struct {
	Cells []xlsxCellData
}

type xlsxCellData struct {
	Value     string
	IsShared  bool
}

func buildXlsxSharedStringsXML(strs []string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	sb.WriteString(`<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="`)
	sb.WriteString(strconv.Itoa(len(strs)))
	sb.WriteString(`" uniqueCount="`)
	sb.WriteString(strconv.Itoa(len(strs)))
	sb.WriteString(`">`)
	for _, s := range strs {
		escaped := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
		sb.WriteString(`<si><t>`)
		sb.WriteString(escaped)
		sb.WriteString(`</t></si>`)
	}
	sb.WriteString(`</sst>`)
	return sb.String()
}

func buildXlsxSheetXML(sheet xlsxSheetData, ssCount int) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	sb.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	sb.WriteString(`<sheetData>`)

	// Track shared string values to assign indices
	ssMap := make(map[string]int)
	ssIdx := 0
	for _, row := range sheet.Rows {
		sb.WriteString(`<row r="1">`)
		for colIdx, cell := range row.Cells {
			colName := string(rune('A'+colIdx)) + "1"
			if cell.IsShared {
				if _, exists := ssMap[cell.Value]; !exists {
					ssMap[cell.Value] = ssIdx
					ssIdx++
				}
				sb.WriteString(`<c r="`)
				sb.WriteString(colName)
				sb.WriteString(`" t="s"><v>`)
				sb.WriteString(strconv.Itoa(ssMap[cell.Value]))
				sb.WriteString(`</v></c>`)
			} else {
				sb.WriteString(`<c r="`)
				sb.WriteString(colName)
				sb.WriteString(`"><v>`)
				sb.WriteString(strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(cell.Value))
				sb.WriteString(`</v></c>`)
			}
		}
		sb.WriteString(`</row>`)
	}
	sb.WriteString(`</sheetData></worksheet>`)
	return sb.String()
}

func buildXlsxWorkbookSheets(count int) string {
	var sb strings.Builder
	for i := 0; i < count; i++ {
		sb.WriteString(`<sheet name="Sheet`)
		sb.WriteString(strconv.Itoa(i+1))
		sb.WriteString(`" sheetId="`)
		sb.WriteString(strconv.Itoa(i+1))
		sb.WriteString(`" r:id="rId1"/>`)
	}
	return sb.String()
}

func buildXlsxWorkbookRels(sheetCount int) string {
	var sb strings.Builder
	for i := 0; i < sheetCount; i++ {
		sb.WriteString(`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet`)
		sb.WriteString(strconv.Itoa(i+1))
		sb.WriteString(`.xml"/>`)
	}
	return sb.String()
}

func TestExtractTextXLSX(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.xlsx")
	if err := writeMinimalXLSX(t, p, xlsxSheetData{
		Name: "Sheet1",
		Rows: []xlsxRowData{
			{Cells: []xlsxCellData{
				{Value: "Name", IsShared: true},
				{Value: "Age", IsShared: true},
			}},
			{Cells: []xlsxCellData{
				{Value: "Alice", IsShared: true},
				{Value: "30", IsShared: false},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractText(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Alice") {
		t.Fatalf("missing Alice: %q", text)
	}
	if !strings.Contains(text, "sheet1") {
		t.Fatalf("missing sheet identifier: %q", text)
	}
}
