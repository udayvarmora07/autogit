// Package mcp contains AutoGit's deliberately small MCP stdio boundary.
//
// The server is read-only by construction. It implements only the JSON-RPC
// lifecycle and tools needed for local diagnostics; it does not import the
// CLI, call a shell, or treat MCP annotations as an authorization boundary.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

const ProtocolRevision = "2025-06-18"

const (
	jsonRPCVersion    = "2.0"
	maxMessageSize    = 1 << 20
	maxCallsPerMinute = 120
)

type Handler func(context.Context, map[string]any) (any, error)

type Handlers struct {
	Status  Handler
	Plan    Handler
	Explain Handler
	Logs    Handler
	// Verify and Publish are intentionally nil in the default server. A
	// caller may provide them only after a separate, domain-owned consent
	// decision; MCP itself never grants that consent.
	Verify  Handler
	Publish Handler
}

type Server struct {
	handlers Handlers
	mu       sync.Mutex
	window   time.Time
	calls    int
}

func New(handlers Handlers) *Server { return &Server{handlers: handlers} }

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  map[string]any  `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	if ctx == nil {
		return errors.New("MCP context is required")
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), maxMessageSize)
	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			if writeErr := encoder.Encode(response{JSONRPC: jsonRPCVersion, Error: &rpcError{Code: -32700, Message: "parse error"}}); writeErr != nil {
				return writeErr
			}
			continue
		}
		// Notifications have no ID and must not receive a response.
		if len(req.ID) == 0 || string(req.ID) == "null" {
			if req.Method == "notifications/initialized" || req.Method == "notifications/cancelled" {
				continue
			}
			continue
		}
		res := s.dispatch(ctx, req)
		if err := encoder.Encode(res); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("MCP input failed: %w", err)
	}
	return nil
}

func (s *Server) dispatch(ctx context.Context, req request) response {
	res := response{JSONRPC: jsonRPCVersion, ID: req.ID}
	if req.JSONRPC != jsonRPCVersion {
		res.Error = &rpcError{Code: -32600, Message: "invalid JSON-RPC version"}
		return res
	}
	if !s.allowCall() {
		res.Error = &rpcError{Code: -32029, Message: "MCP request rate limit exceeded"}
		return res
	}
	switch req.Method {
	case "initialize":
		if version, _ := req.Params["protocolVersion"].(string); version != "" && version != ProtocolRevision {
			res.Error = &rpcError{Code: -32602, Message: "unsupported MCP protocol revision"}
			return res
		}
		res.Result = map[string]any{
			"protocolVersion": ProtocolRevision,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "autogit", "version": "autogit.mcp/1"},
		}
	case "ping":
		res.Result = map[string]any{}
	case "tools/list":
		res.Result = map[string]any{"tools": s.toolDefinitions()}
	case "tools/call":
		result, rpcErr := s.toolCall(ctx, req.Params)
		if rpcErr != nil {
			res.Error = rpcErr
		} else {
			res.Result = result
		}
	default:
		res.Error = &rpcError{Code: -32601, Message: "method not found"}
	}
	return res
}

func (s *Server) allowCall() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.window.IsZero() || now.Sub(s.window) >= time.Minute {
		s.window, s.calls = now, 0
	}
	if s.calls >= maxCallsPerMinute {
		return false
	}
	s.calls++
	return true
}

func (s *Server) toolDefinitions() []map[string]any {
	tools := make([]map[string]any, 0, 6)
	add := func(name, title, description string, properties map[string]any, required []string) {
		schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
		if len(required) > 0 {
			schema["required"] = required
		}
		tools = append(tools, map[string]any{
			"name": name, "title": title, "description": description,
			"inputSchema": schema,
			"annotations": map[string]any{"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false},
		})
	}
	add("autogit_status", "AutoGit status", "Read the redacted lifecycle and repository status for an explicit repository.", map[string]any{"repo": map[string]any{"type": "string"}}, []string{"repo"})
	add("autogit_plan", "AutoGit plan", "Read the redacted candidate and consent plan without changing files, refs, or state.", map[string]any{"repo": map[string]any{"type": "string"}}, []string{"repo"})
	add("autogit_explain", "AutoGit configuration explain", "Explain a verifier configuration without initializing state or contacting a provider.", map[string]any{"verifiers": map[string]any{"type": "string"}}, nil)
	add("autogit_logs", "AutoGit logs", "Read bounded, redacted lifecycle audit records for an explicit repository.", map[string]any{"repo": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 200}}, []string{"repo"})
	if s.handlers.Verify != nil {
		add("autogit_verify", "AutoGit verify", "Run verification only after the caller has separately consented to this domain action.", map[string]any{"consent": map[string]any{"type": "boolean"}}, []string{"consent"})
	}
	if s.handlers.Publish != nil {
		add("autogit_publish", "AutoGit publish", "Publish only after separate explicit provider and visibility consent.", map[string]any{"consent": map[string]any{"type": "boolean"}}, []string{"consent"})
	}
	return tools
}

func (s *Server) callTool(ctx context.Context, params map[string]any) map[string]any {
	result, rpcErr := s.toolCall(ctx, params)
	if rpcErr != nil {
		return map[string]any{"content": []map[string]string{{"type": "text", "text": rpcErr.Message}}, "isError": true}
	}
	return result
}

func (s *Server) toolCall(ctx context.Context, params map[string]any) (map[string]any, *rpcError) {
	name, _ := params["name"].(string)
	args, _ := params["arguments"].(map[string]any)
	var handler Handler
	switch name {
	case "autogit_status":
		handler = s.handlers.Status
	case "autogit_plan":
		handler = s.handlers.Plan
	case "autogit_explain":
		handler = s.handlers.Explain
	case "autogit_logs":
		handler = s.handlers.Logs
	case "autogit_verify":
		handler = s.handlers.Verify
	case "autogit_publish":
		handler = s.handlers.Publish
	default:
		return nil, &rpcError{Code: -32602, Message: "unknown tool"}
	}
	if handler == nil {
		return nil, &rpcError{Code: -32602, Message: "tool is not enabled by domain policy"}
	}
	if (name == "autogit_verify" || name == "autogit_publish") && args["consent"] != true {
		return map[string]any{"content": []map[string]string{{"type": "text", "text": "separate explicit consent is required"}}, "isError": true}, nil
	}
	value, err := handler(ctx, args)
	if err != nil {
		// Do not expose paths, credentials, provider URLs, or raw diagnostics
		// over an agent-controlled channel.
		return map[string]any{"content": []map[string]string{{"type": "text", "text": "read-only operation failed"}}, "isError": true}, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return map[string]any{"content": []map[string]string{{"type": "text", "text": "tool result could not be encoded"}}, "isError": true}, nil
	}
	return map[string]any{"content": []map[string]string{{"type": "text", "text": string(encoded)}}, "structuredContent": value, "isError": false}, nil
}
