package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestReadOnlyProtocolAndUntrustedAnnotations(t *testing.T) {
	var out bytes.Buffer
	server := New(Handlers{
		Status: func(_ context.Context, args map[string]any) (any, error) {
			return map[string]any{"repo": args["repo"], "safe": true}, nil
		},
	})
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"autogit_status","arguments":{"repo":"sha256:abc"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	}, "\n") + "\n"
	if err := server.Run(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("responses=%q", lines)
	}
	var init response
	if err := json.Unmarshal([]byte(lines[0]), &init); err != nil {
		t.Fatal(err)
	}
	result, ok := init.Result.(map[string]any)
	if !ok || result["protocolVersion"] != ProtocolRevision {
		t.Fatalf("initialize=%v", init.Result)
	}
	if strings.Contains(out.String(), "readOnlyHint\":false") {
		t.Fatal("server advertised a mutable tool")
	}
	if !strings.Contains(out.String(), `"structuredContent"`) {
		t.Fatal("tool result lacked structured content")
	}
}

func TestMutationToolsRequireSeparateConsent(t *testing.T) {
	server := New(Handlers{Verify: func(context.Context, map[string]any) (any, error) { return "ran", nil }})
	result := server.callTool(context.Background(), map[string]any{"name": "autogit_verify", "arguments": map[string]any{"consent": false}})
	if result["isError"] != true {
		t.Fatalf("result=%v", result)
	}
}
