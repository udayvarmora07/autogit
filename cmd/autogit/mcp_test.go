package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestMCPServeIsReadOnlyAndExposesOnlyDiagnostics(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateDir)
	input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2025-06-18\"}}\n" +
		"{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n")
	var out bytes.Buffer
	if err := run([]string{"mcp", "serve"}, input, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "autogit_publish") || strings.Contains(out.String(), "autogit_verify") {
		t.Fatalf("mutation tool leaked into default MCP surface: %s", out.String())
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(strings.Split(strings.TrimSpace(out.String()), "\n")[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first["result"] == nil {
		t.Fatalf("initialize response=%v", first)
	}
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("MCP created state files: %v", entries)
	}
}
