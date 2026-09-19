package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"codergag/internal/services"
)

func frame(v any) string {
	b, _ := json.Marshal(v)
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(b), b)
}

func readFrames(t *testing.T, out *bytes.Buffer) []map[string]any {
	t.Helper()
	r := bufio.NewReader(out)
	var msgs []map[string]any
	for {
		hdr, err := r.ReadString('\n')
		if err == io.EOF {
			return msgs
		}
		var n int
		fmt.Sscanf(strings.TrimSpace(hdr), "Content-Length: %d", &n)
		r.ReadString('\n')
		body := make([]byte, n)
		io.ReadFull(r, body)
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("bad frame %q: %v", body, err)
		}
		msgs = append(msgs, m)
	}
}

// The wire protocol: standard "Content-Length" framing must be understood.
func TestServerSpeaksContentLengthFraming(t *testing.T) {
	in := strings.Join([]string{
		frame(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}}),
		frame(map[string]any{"jsonrpc": "2.0", "method": "initialized"}),
		frame(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}),
		frame(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{
			"name": "list_roles", "arguments": map[string]any{"project_id": "p"}}}),
		frame(map[string]any{"jsonrpc": "2.0", "id": 4, "method": "tools/call", "params": map[string]any{
			"name": "nope", "arguments": map[string]any{"project_id": "p"}}}),
	}, "")
	var out bytes.Buffer
	srv := NewServer(NewToolRegistry(services.ApplicationInMemory()))
	srv.reader = bufio.NewReader(strings.NewReader(in))
	srv.writer = &out
	if err := srv.Run(); err != nil {
		t.Fatal(err)
	}
	msgs := readFrames(t, &out)
	if len(msgs) != 4 { // the "initialized" notification gets no reply
		t.Fatalf("expected 4 responses, got %d: %v", len(msgs), msgs)
	}
	if msgs[0]["result"].(map[string]any)["serverInfo"].(map[string]any)["name"] != "codeRAG" {
		t.Fatalf("initialize: %v", msgs[0])
	}
	tools := msgs[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) < 50 {
		t.Fatalf("tools/list returned %d tools", len(tools))
	}
	content := msgs[2]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if content["type"] != "text" || !strings.Contains(content["text"].(string), "roles") {
		t.Fatalf("tools/call result: %v", msgs[2])
	}
	if msgs[3]["error"] == nil {
		t.Fatalf("unknown tool must return a JSON-RPC error: %v", msgs[3])
	}
}

func TestServerAcceptsLowercaseHeaderToo(t *testing.T) {
	var out bytes.Buffer
	srv := NewServer(NewToolRegistry(services.ApplicationInMemory()))
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	srv.reader = bufio.NewReader(strings.NewReader(fmt.Sprintf("content-length: %d\r\n\r\n%s", len(body), body)))
	srv.writer = &out
	srv.Run()
	if len(readFrames(t, &out)) != 1 {
		t.Fatal("lowercase header should work")
	}
}
