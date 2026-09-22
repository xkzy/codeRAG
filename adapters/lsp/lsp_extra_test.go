package lsp

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
)

type mockLSPClient struct {
	nameVal string
}

func (m *mockLSPClient) Name() string                                          { return m.nameVal }
func (m *mockLSPClient) Initialize(ctx context.Context) error                   { return nil }
func (m *mockLSPClient) Shutdown(ctx context.Context) error                     { return nil }
func (m *mockLSPClient) OpenDocument(ctx context.Context, uri, content string) error { return nil }
func (m *mockLSPClient) CloseDocument(ctx context.Context, uri string) error    { return nil }
func (m *mockLSPClient) GetCompletions(ctx context.Context, uri string, line, ch int) ([]CompletionItem, error) {
	return nil, nil
}
func (m *mockLSPClient) GetHover(ctx context.Context, uri string, line, ch int) (*HoverInfo, error) {
	return nil, nil
}
func (m *mockLSPClient) GetDefinition(ctx context.Context, uri string, line, ch int) (*Location, error) {
	return nil, nil
}
func (m *mockLSPClient) GetReferences(ctx context.Context, uri string, line, ch int) ([]Location, error) {
	return nil, nil
}
func (m *mockLSPClient) GetDocumentSymbols(ctx context.Context, uri string) ([]DocumentSymbol, error) {
	return nil, nil
}
func (m *mockLSPClient) GetWorkspaceSymbols(ctx context.Context, query string) ([]DocumentSymbol, error) {
	return nil, nil
}

func TestNewLSPAdapter(t *testing.T) {
	mock := &mockLSPClient{nameVal: "mock"}
	adapter := NewLSPAdapter(mock)
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
	if adapter.Client() != mock {
		t.Fatal("Client() should return the client passed to NewLSPAdapter")
	}
}

func TestLSPAdapterName(t *testing.T) {
	adapter := LSPAdapter{}
	if adapter.Name() != "lsp" {
		t.Errorf("expected name 'lsp', got %s", adapter.Name())
	}
}

func TestVSCodeClientShutdownNoConn(t *testing.T) {
	c := NewVSCodeClient("tcp://localhost:8080")
	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown with no conn should succeed: %v", err)
	}
}

func TestVSCodeClientSendMessageNoConn(t *testing.T) {
	c := NewVSCodeClient("tcp://localhost:8080")
	err := c.OpenDocument(context.Background(), "file:///test.go", "content")
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected 'not connected' error, got %v", err)
	}
}

func TestVSCodeClientGetCompletionsNoConn(t *testing.T) {
	c := NewVSCodeClient("tcp://localhost:8080")
	_, err := c.GetCompletions(context.Background(), "file:///test.go", 0, 0)
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected 'not connected' error, got %v", err)
	}
}

func TestVSCodeClientGetHoverNoConn(t *testing.T) {
	c := NewVSCodeClient("tcp://localhost:8080")
	_, err := c.GetHover(context.Background(), "file:///test.go", 0, 0)
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected 'not connected' error, got %v", err)
	}
}

func TestVSCodeClientGetDefinitionNoConn(t *testing.T) {
	c := NewVSCodeClient("tcp://localhost:8080")
	_, err := c.GetDefinition(context.Background(), "file:///test.go", 0, 0)
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected 'not connected' error, got %v", err)
	}
}

func TestVSCodeClientGetReferencesNoConn(t *testing.T) {
	c := NewVSCodeClient("tcp://localhost:8080")
	_, err := c.GetReferences(context.Background(), "file:///test.go", 0, 0)
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected 'not connected' error, got %v", err)
	}
}

func TestVSCodeClientGetDocumentSymbolsNoConn(t *testing.T) {
	c := NewVSCodeClient("tcp://localhost:8080")
	_, err := c.GetDocumentSymbols(context.Background(), "file:///test.go")
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected 'not connected' error, got %v", err)
	}
}

func TestVSCodeClientGetWorkspaceSymbolsNoConn(t *testing.T) {
	c := NewVSCodeClient("tcp://localhost:8080")
	_, err := c.GetWorkspaceSymbols(context.Background(), "query")
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected 'not connected' error, got %v", err)
	}
}

func TestVSCodeClientCloseDocumentNoConn(t *testing.T) {
	c := NewVSCodeClient("tcp://localhost:8080")
	err := c.CloseDocument(context.Background(), "file:///test.go")
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected 'not connected' error, got %v", err)
	}
}

func testVSCodeClientWithServer(t *testing.T, response string) *VSCodeClient {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				for {
					buf := make([]byte, 4096)
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					msg := string(buf[:n])
					if strings.Contains(msg, "didOpen") || strings.Contains(msg, "didClose") {
						continue
					}
					if _, err := c.Write([]byte(response)); err != nil {
						return
					}
					return
				}
			}(conn)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	uri := "tcp://" + ln.Addr().String()
	client := NewVSCodeClient(uri)
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	return client
}

func TestVSCodeClientFullFlow(t *testing.T) {
	completions := `[{"label":"fmt","kind":"module","detail":"package","insert_text":"fmt"}]`
	c := testVSCodeClientWithServer(t, completions)
	defer c.Shutdown(context.Background())

	// Test OpenDocument
	if err := c.OpenDocument(context.Background(), "file:///test.go", "package main"); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}

	// Test CloseDocument
	if err := c.CloseDocument(context.Background(), "file:///test.go"); err != nil {
		t.Fatalf("CloseDocument: %v", err)
	}

	// Test GetCompletions
	items, err := c.GetCompletions(context.Background(), "file:///test.go", 1, 0)
	if err != nil {
		t.Fatalf("GetCompletions: %v", err)
	}
	if len(items) != 1 || items[0].Label != "fmt" {
		t.Fatalf("expected 1 completion 'fmt', got %+v", items)
	}
}

func TestVSCodeClientGetHover(t *testing.T) {
	hover := `{"contents":[{"language":"go","value":"func main"}]}`
	c := testVSCodeClientWithServer(t, hover)
	defer c.Shutdown(context.Background())
	result, err := c.GetHover(context.Background(), "file:///test.go", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || len(result.Contents) != 1 {
		t.Fatalf("expected hover with 1 content, got %+v", result)
	}
}

func TestVSCodeClientGetDefinition(t *testing.T) {
	loc := `{"uri":"file:///main.go","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":10}}}`
	c := testVSCodeClientWithServer(t, loc)
	defer c.Shutdown(context.Background())
	result, err := c.GetDefinition(context.Background(), "file:///test.go", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.URI != "file:///main.go" {
		t.Fatalf("expected definition location, got %+v", result)
	}
}

func TestVSCodeClientGetReferences(t *testing.T) {
	refs := `[{"uri":"file:///main.go","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":10}}}]`
	c := testVSCodeClientWithServer(t, refs)
	defer c.Shutdown(context.Background())
	result, err := c.GetReferences(context.Background(), "file:///test.go", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].URI != "file:///main.go" {
		t.Fatalf("expected 1 reference, got %+v", result)
	}
}

func TestVSCodeClientGetDocumentSymbols(t *testing.T) {
	symbols := `[{"name":"main","kind":"function","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":10}},"selection_range":{"start":{"line":0,"character":0},"end":{"line":0,"character":10}}}]`
	c := testVSCodeClientWithServer(t, symbols)
	defer c.Shutdown(context.Background())
	result, err := c.GetDocumentSymbols(context.Background(), "file:///test.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Name != "main" {
		t.Fatalf("expected 1 symbol 'main', got %+v", result)
	}
}

func TestVSCodeClientGetWorkspaceSymbols(t *testing.T) {
	symbols := `[{"name":"main","kind":"function","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":10}},"selection_range":{"start":{"line":0,"character":0},"end":{"line":0,"character":10}}}]`
	c := testVSCodeClientWithServer(t, symbols)
	defer c.Shutdown(context.Background())
	result, err := c.GetWorkspaceSymbols(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Name != "main" {
		t.Fatalf("expected 1 workspace symbol, got %+v", result)
	}
}

func TestJetBrainsClientFullFlow(t *testing.T) {
	resp := `[{"label":"fmt","kind":"module"}]`
	c := testJetBrainsClientWithServer(t, resp)
	defer c.Shutdown(context.Background())

	if err := c.OpenDocument(context.Background(), "file:///test.go", "content"); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}
	items, err := c.GetCompletions(context.Background(), "file:///test.go", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Label != "fmt" {
		t.Fatalf("expected 1 completion, got %+v", items)
	}
}

func testJetBrainsClientWithServer(t *testing.T, response string) *JetBrainsClient {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				for {
					buf := make([]byte, 4096)
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					msg := string(buf[:n])
					if strings.Contains(msg, "didOpen") || strings.Contains(msg, "didClose") {
						continue
					}
					if _, err := c.Write([]byte(response)); err != nil {
						return
					}
					return
				}
			}(conn)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	uri := "tcp://" + ln.Addr().String()
	client := NewJetBrainsClient(uri, "idea")
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	return client
}

func TestJetBrainsClientShutdown(t *testing.T) {
	c := NewJetBrainsClient("tcp://localhost:8080", "idea")
	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown with no conn should succeed: %v", err)
	}
}

func TestJetBrainsClientSendMessageNoConn(t *testing.T) {
	c := NewJetBrainsClient("tcp://localhost:8080", "idea")
	err := c.OpenDocument(context.Background(), "file:///test.go", "content")
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected 'not connected' error, got %v", err)
	}
}

func TestJetBrainsClientGetCompletionsNoConn(t *testing.T) {
	c := NewJetBrainsClient("tcp://localhost:8080", "idea")
	_, err := c.GetCompletions(context.Background(), "file:///test.go", 0, 0)
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected 'not connected' error, got %v", err)
	}
}

func TestJetBrainsClientGetHoverNoConn(t *testing.T) {
	c := NewJetBrainsClient("tcp://localhost:8080", "idea")
	_, err := c.GetHover(context.Background(), "file:///test.go", 0, 0)
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected 'not connected' error, got %v", err)
	}
}

func TestJetBrainsClientGetDefinitionNoConn(t *testing.T) {
	c := NewJetBrainsClient("tcp://localhost:8080", "idea")
	_, err := c.GetDefinition(context.Background(), "file:///test.go", 0, 0)
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected 'not connected' error, got %v", err)
	}
}

func TestJetBrainsClientGetReferencesNoConn(t *testing.T) {
	c := NewJetBrainsClient("tcp://localhost:8080", "idea")
	_, err := c.GetReferences(context.Background(), "file:///test.go", 0, 0)
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected 'not connected' error, got %v", err)
	}
}

func TestJetBrainsClientGetDocumentSymbolsNoConn(t *testing.T) {
	c := NewJetBrainsClient("tcp://localhost:8080", "idea")
	_, err := c.GetDocumentSymbols(context.Background(), "file:///test.go")
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected 'not connected' error, got %v", err)
	}
}

func TestJetBrainsClientGetWorkspaceSymbolsNoConn(t *testing.T) {
	c := NewJetBrainsClient("tcp://localhost:8080", "idea")
	_, err := c.GetWorkspaceSymbols(context.Background(), "query")
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected 'not connected' error, got %v", err)
	}
}

func TestJetBrainsClientCloseDocumentNoConn(t *testing.T) {
	c := NewJetBrainsClient("tcp://localhost:8080", "idea")
	err := c.CloseDocument(context.Background(), "file:///test.go")
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected 'not connected' error, got %v", err)
	}
}

func TestVSCodeClientInitializeInvalidURI(t *testing.T) {
	c := NewVSCodeClient("tcp://invalid:0")
	err := c.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid URI")
	}
}

func TestJetBrainsClientInitializeInvalidURI(t *testing.T) {
	c := NewJetBrainsClient("tcp://invalid:0", "idea")
	err := c.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid URI")
	}
}

func TestVSCodeClientGetCompletionsInvalidJSON(t *testing.T) {
	c := testVSCodeClientWithServer(t, "not json")
	defer c.Shutdown(context.Background())
	_, err := c.GetCompletions(context.Background(), "file:///test.go", 0, 0)
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestVSCodeClientGetHoverInvalidJSON(t *testing.T) {
	c := testVSCodeClientWithServer(t, "not json")
	defer c.Shutdown(context.Background())
	_, err := c.GetHover(context.Background(), "file:///test.go", 0, 0)
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestVSCodeClientGetDefinitionInvalidJSON(t *testing.T) {
	c := testVSCodeClientWithServer(t, "not json")
	defer c.Shutdown(context.Background())
	_, err := c.GetDefinition(context.Background(), "file:///test.go", 0, 0)
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestVSCodeClientGetReferencesInvalidJSON(t *testing.T) {
	c := testVSCodeClientWithServer(t, "not json")
	defer c.Shutdown(context.Background())
	_, err := c.GetReferences(context.Background(), "file:///test.go", 0, 0)
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestVSCodeClientGetDocumentSymbolsInvalidJSON(t *testing.T) {
	c := testVSCodeClientWithServer(t, "not json")
	defer c.Shutdown(context.Background())
	_, err := c.GetDocumentSymbols(context.Background(), "file:///test.go")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestVSCodeClientGetWorkspaceSymbolsInvalidJSON(t *testing.T) {
	c := testVSCodeClientWithServer(t, "not json")
	defer c.Shutdown(context.Background())
	_, err := c.GetWorkspaceSymbols(context.Background(), "query")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestJetBrainsClientGetCompletionsInvalidJSON(t *testing.T) {
	resp := `{"id":1,"result":"not array"}`
	c := testJetBrainsClientWithServer(t, resp)
	defer c.Shutdown(context.Background())
	_, err := c.GetCompletions(context.Background(), "file:///test.go", 0, 0)
	if err == nil {
		t.Fatal("expected error for invalid JSON in completions")
	}
}

func TestJetBrainsClientGetHoverInvalidJSON(t *testing.T) {
	resp := `{"id":1,"result":{"contents":{"broken":}}}`
	c := testJetBrainsClientWithServer(t, resp)
	defer c.Shutdown(context.Background())
	_, err := c.GetHover(context.Background(), "file:///test.go", 0, 0)
	if err == nil {
		t.Fatal("expected error for invalid JSON in hover")
	}
}

func TestJetBrainsClientGetDefinitionInvalidJSON(t *testing.T) {
	resp := `{"id":1,"result":{"uri":{"broken":}}}`
	c := testJetBrainsClientWithServer(t, resp)
	defer c.Shutdown(context.Background())
	_, err := c.GetDefinition(context.Background(), "file:///test.go", 0, 0)
	if err == nil {
		t.Fatal("expected error for invalid JSON in definition")
	}
}

func TestJetBrainsClientGetReferencesInvalidJSON(t *testing.T) {
	resp := `{"id":1,"result":"not array"}`
	c := testJetBrainsClientWithServer(t, resp)
	defer c.Shutdown(context.Background())
	_, err := c.GetReferences(context.Background(), "file:///test.go", 0, 0)
	if err == nil {
		t.Fatal("expected error for invalid JSON in references")
	}
}

func TestJetBrainsClientGetDocumentSymbolsInvalidJSON(t *testing.T) {
	resp := `{"id":1,"result":"not array"}`
	c := testJetBrainsClientWithServer(t, resp)
	defer c.Shutdown(context.Background())
	_, err := c.GetDocumentSymbols(context.Background(), "file:///test.go")
	if err == nil {
		t.Fatal("expected error for invalid JSON in document symbols")
	}
}

func TestJetBrainsClientGetWorkspaceSymbolsInvalidJSON(t *testing.T) {
	resp := `{"id":1,"result":"not array"}`
	c := testJetBrainsClientWithServer(t, resp)
	defer c.Shutdown(context.Background())
	_, err := c.GetWorkspaceSymbols(context.Background(), "query")
	if err == nil {
		t.Fatal("expected error for invalid JSON in workspace symbols")
	}
}

func TestVSCodeClientNameAndAdapter(t *testing.T) {
	c := NewVSCodeClient("tcp://localhost:8080")
	if c.Name() != "vscode" {
		t.Errorf("expected name 'vscode', got %s", c.Name())
	}

	adapter := NewLSPAdapter(c)
	if adapter.Name() != "lsp" {
		t.Errorf("adapter name should be 'lsp', got %s", adapter.Name())
	}
	if adapter.Client() != c {
		t.Error("adapter client should match")
	}
}

func TestJetBrainsClientNameAndAdapter(t *testing.T) {
	c := NewJetBrainsClient("tcp://localhost:8080", "idea")
	expectedName := fmt.Sprintf("jetbrains:%s", "idea")
	if c.Name() != expectedName {
		t.Errorf("expected name %q, got %q", expectedName, c.Name())
	}

	adapter := NewLSPAdapter(c)
	if adapter.Name() != "lsp" {
		t.Errorf("adapter name should be 'lsp', got %s", adapter.Name())
	}
}

func TestJetBrainsClientNameEmptyIDE(t *testing.T) {
	c := NewJetBrainsClient("tcp://localhost:8080", "")
	if c.Name() != "jetbrains" {
		t.Errorf("expected name 'jetbrains', got %s", c.Name())
	}
}

func TestVSCodeClientGetCompletionsWithInvalidResponseFormat(t *testing.T) {
	// Test that invalid response (valid JSON but wrong type) is handled
	resp := `{"result": "not an array"}`
	c := testVSCodeClientWithServer(t, resp)
	defer c.Shutdown(context.Background())
	_, err := c.GetCompletions(context.Background(), "file:///test.go", 0, 0)
	if err != nil && !strings.Contains(err.Error(), "cannot unmarshal") {
		t.Fatalf("expected unmarshal error, got %v", err)
	}
}

func TestJetBrainsClientMarshalErrorInJSON(t *testing.T) {
	resp := `{"id":1,"result":`
	c := testJetBrainsClientWithServer(t, resp)
	defer c.Shutdown(context.Background())
	_, err := c.GetCompletions(context.Background(), "file:///test.go", 0, 0)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestVSCodeClientMarshalErrorInJSON(t *testing.T) {
	resp := `{"id":1,"result":`
	c := testVSCodeClientWithServer(t, resp)
	defer c.Shutdown(context.Background())
	_, err := c.GetCompletions(context.Background(), "file:///test.go", 0, 0)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
