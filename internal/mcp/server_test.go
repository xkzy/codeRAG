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

// MCP's stdio transport (used by Claude Code, Codex, Cursor, ...) is one JSON
// message per line, not Content-Length framed. The server must speak it.
func runLines(t *testing.T, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	srv := NewServer(NewToolRegistry(services.ApplicationInMemory()))
	srv.reader = bufio.NewReader(strings.NewReader(strings.Join(lines, "\n") + "\n"))
	srv.writer = &out
	if err := srv.Run(); err != nil {
		t.Fatal(err)
	}
	var msgs []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "Content-Length") {
			t.Fatalf("newline-framed request got a Content-Length reply: %q", out.String())
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("reply is not one JSON object per line: %q: %v", l, err)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

func TestServerSpeaksNewlineDelimitedMCP(t *testing.T) {
	msgs := runLines(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_languages","arguments":{}}}`,
	)
	if len(msgs) != 3 { // the notification gets no reply
		t.Fatalf("want 3 replies, got %d: %v", len(msgs), msgs)
	}
	if msgs[0]["result"].(map[string]any)["serverInfo"].(map[string]any)["name"] != "codeRAG" {
		t.Errorf("initialize: %v", msgs[0])
	}
	if len(msgs[1]["result"].(map[string]any)["tools"].([]any)) < 50 {
		t.Errorf("tools/list too short")
	}
	if _, ok := msgs[2]["result"].(map[string]any)["content"]; !ok {
		t.Errorf("tools/call: %v", msgs[2])
	}
}

func TestResponsesAreWellFormedJSONRPC(t *testing.T) {
	msgs := runLines(t,
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":2,"method":"no/such/method"}`,
		`{"jsonrpc":"2.0","id":"str-id","method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":0,"method":"ping"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{}}`,
		`not json at all`,
	)
	for _, m := range msgs {
		if _, has := m["method"]; has {
			t.Errorf("a response must not carry \"method\" (looks like a request): %v", m)
		}
		_, r := m["result"]
		_, e := m["error"]
		if r == e {
			t.Errorf("a response needs exactly one of result/error: %v", m)
		}
		if m["jsonrpc"] != "2.0" {
			t.Errorf("missing jsonrpc: %v", m)
		}
	}
	if len(msgs) < 5 {
		t.Fatalf("want replies for ping, unknown, string id, id 0 and parse error; got %v", msgs)
	}
	if msgs[0]["result"] == nil {
		t.Errorf("ping must return an empty result object: %v", msgs[0])
	}
	if code := msgs[1]["error"].(map[string]any)["code"]; code != float64(-32601) {
		t.Errorf("unknown method should be -32601, got %v", code)
	}
	if msgs[2]["id"] != "str-id" {
		t.Errorf("string id not echoed: %v", msgs[2])
	}
	if msgs[3]["id"] != float64(0) {
		t.Errorf("id 0 must be echoed, not dropped: %v", msgs[3])
	}
	if code := msgs[4]["error"].(map[string]any)["code"]; code != float64(-32700) || msgs[4]["id"] != nil {
		t.Errorf("garbage input should give a -32700 parse error with null id: %v", msgs[4])
	}
}

func TestContentLengthRepliesStayWellFormed(t *testing.T) {
	var out bytes.Buffer
	srv := NewServer(NewToolRegistry(services.ApplicationInMemory()))
	srv.reader = bufio.NewReader(strings.NewReader(frame(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"})))
	srv.writer = &out
	srv.Run()
	msgs := readFrames(t, &out)
	if len(msgs) != 1 {
		t.Fatalf("got %v", msgs)
	}
	if _, has := msgs[0]["method"]; has || msgs[0]["result"] == nil {
		t.Errorf("malformed reply: %v", msgs[0])
	}
}
