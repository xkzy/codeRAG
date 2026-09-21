package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndexDocumentAndQueryTable(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(p, []byte("name,age,city\nAlice,30,NYC\nBob,25,LA\nCharlie,30,SF"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := ApplicationInMemory()
	res, err := app.Documents.IndexDocument("p", p)
	if err != nil {
		t.Fatal(err)
	}

	if res["row_count"] != 3 {
		t.Fatalf("expected 3 rows, got %v", res["row_count"])
	}

	cols := res["table_schema"].([]string)
	if len(cols) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(cols))
	}
	if cols[0] != "name" || cols[1] != "age" || cols[2] != "city" {
		t.Errorf("columns: %v", cols)
	}

	docID := res["document_id"].(string)

	// Query all rows
	schema, err := app.Documents.GetTableSchema("p", docID)
	if err != nil {
		t.Fatal(err)
	}
	schemaCols := schema["columns"].([]string)
	if len(schemaCols) != 3 {
		t.Errorf("expected 3 schema columns, got %d", len(schemaCols))
	}
	if schema["row_count"].(int) != 3 {
		t.Errorf("expected 3 rows in schema, got %v", schema["row_count"])
	}

	// Filter by age = 30
	rows, err := app.Documents.QueryTable("p", docID, "age", "30", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows with age=30, got %d", len(rows))
	}
	if rows[0]["name"] != "Alice" {
		t.Errorf("expected Alice, got %v", rows[0]["name"])
	}

	// Filter by city = LA
	rows, err = app.Documents.QueryTable("p", docID, "city", "LA", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row with city=LA, got %d", len(rows))
	}
	if rows[0]["name"] != "Bob" {
		t.Errorf("expected Bob, got %v", rows[0]["name"])
	}

	// Get all rows without filter
	rows, err = app.Documents.QueryTable("p", docID, "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 all rows, got %d", len(rows))
	}

	// Limit to 1 row
	rows, err = app.Documents.QueryTable("p", docID, "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row with limit, got %d", len(rows))
	}
}

func TestQueryTable_NotFound(t *testing.T) {
	app := ApplicationInMemory()
	_, err := app.Documents.QueryTable("p", "nonexistent_id", "col", "val", 10)
	if err == nil {
		t.Fatal("expected error for nonexistent document")
	}
}

func TestQueryTable_WrongProject(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(p, []byte("a,b\n1,2"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := ApplicationInMemory()
	res, err := app.Documents.IndexDocument("project-a", p)
	if err != nil {
		t.Fatal(err)
	}
	docID := res["document_id"].(string)

	_, err = app.Documents.QueryTable("project-b", docID, "", "", 10)
	if err == nil {
		t.Fatal("expected error for wrong project")
	}
}

func TestGetTableSchema_NotTabular(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.md")
	if err := os.WriteFile(p, []byte("# Hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := ApplicationInMemory()
	res, err := app.Documents.IndexDocument("p", p)
	if err != nil {
		t.Fatal(err)
	}
	docID := res["document_id"].(string)

	_, err = app.Documents.GetTableSchema("p", docID)
	if err == nil {
		t.Fatal("expected error for non-tabular document")
	}
}

func TestGetTableSchema_NotFound(t *testing.T) {
	app := ApplicationInMemory()
	_, err := app.Documents.GetTableSchema("p", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent document")
	}
}

func TestIndexDocumentTSVSchema(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data.tsv")
	if err := os.WriteFile(p, []byte("name\tage\nAlice\t30\nBob\t25"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := ApplicationInMemory()
	res, err := app.Documents.IndexDocument("p", p)
	if err != nil {
		t.Fatal(err)
	}
	if res["format"] != ".tsv" {
		t.Errorf("expected .tsv, got %v", res["format"])
	}
	if res["row_count"] != 2 {
		t.Errorf("expected 2 rows, got %v", res["row_count"])
	}
}

func TestIndexDocumentCSVWithQuotes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data.csv")
	content := `"name","description"
"Alice","Hello, world"
"Bob","Foo, bar"`
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	app := ApplicationInMemory()
	res, err := app.Documents.IndexDocument("p", p)
	if err != nil {
		t.Fatal(err)
	}
	docID := res["document_id"].(string)

	rows, err := app.Documents.QueryTable("p", docID, "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0]["description"] != "Hello, world" {
		t.Errorf("expected 'Hello, world', got %v", rows[0]["description"])
	}
}
