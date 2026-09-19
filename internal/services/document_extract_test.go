package services

import (
	"archive/zip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
