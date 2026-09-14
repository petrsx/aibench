// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package bindings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/petrsx/aibench/internal/provider"
)

// newMCPServer runs an in-process MCP server over streamable HTTP with an
// "echo" tool (returns its arguments as text) and a "boom" tool (IsError).
func newMCPServer(t *testing.T) string {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server"}, nil)
	schema := map[string]any{"type": "object"}
	server.AddTool(&mcp.Tool{Name: "echo", InputSchema: schema},
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{
				&mcp.TextContent{Text: "echo: " + string(req.Params.Arguments)},
			}}, nil
		})
	server.AddTool(&mcp.Tool{Name: "boom", InputSchema: schema},
		func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "server exploded"}},
			}, nil
		})

	srv := httptest.NewServer(mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server }, nil))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestMCPCallRoundTrip(t *testing.T) {
	url := newMCPServer(t)
	s := Set{Bindings: map[string]Binding{
		"echo": {Type: "mcp", Server: url}, // tool name defaults to the call's
	}}

	out, err := s.Run(t.Context(), provider.ToolCall{
		Name:      "echo",
		Arguments: `{"station_code":"HWD"}`,
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	// The arguments round-trip through the server (key order may differ).
	var args map[string]any
	if !strings.HasPrefix(out, "echo: ") {
		t.Fatalf("Run() = %q; want the echoed arguments", out)
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(out, "echo: ")), &args); err != nil || args["station_code"] != "HWD" {
		t.Errorf("echoed arguments = %q; want station_code HWD (err %v)", out, err)
	}
}

func TestMCPToolNameOverride(t *testing.T) {
	url := newMCPServer(t)
	s := Set{Bindings: map[string]Binding{
		"lookup_station": {Type: "mcp", Server: url, Tool: "echo"},
	}}
	out, err := s.Run(t.Context(), provider.ToolCall{Name: "lookup_station", Arguments: `{}`})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if !strings.HasPrefix(out, "echo: ") {
		t.Errorf("Run() = %q; want the overridden echo tool's reply", out)
	}
}

func TestMCPIsErrorBecomesError(t *testing.T) {
	url := newMCPServer(t)
	s := Set{Bindings: map[string]Binding{
		"boom": {Type: "mcp", Server: url},
	}}
	_, err := s.Run(t.Context(), provider.ToolCall{Name: "boom", Arguments: `{}`})
	if err == nil || !strings.Contains(err.Error(), "server exploded") {
		t.Errorf("Run(IsError tool) error = %v; want the tool's error text", err)
	}
}

func TestMCPMissingServer(t *testing.T) {
	s := Set{Bindings: map[string]Binding{"t": {Type: "mcp"}}}
	if _, err := s.Run(t.Context(), provider.ToolCall{Name: "t"}); err == nil {
		t.Error("Run(mcp without server) error = nil; want a config error")
	}
}
