// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package bindings

// The mcp executor: calls a tool on an MCP server over streamable HTTP.
// One short-lived session per call — stateless, nothing shared across the
// tea.Cmd goroutines that run tools. (stdio/spawned servers are a future
// feature: process lifecycle doesn't belong in a hot-reloading file yet.)

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/petrsx/aibench/internal/provider"
)

func runMCP(ctx context.Context, b Binding, call provider.ToolCall) (string, error) {
	if b.Server == "" {
		return "", fmt.Errorf("tool %q: mcp binding needs a server URL", call.Name)
	}

	args := map[string]any{}
	if strings.TrimSpace(call.Arguments) != "" {
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return "", fmt.Errorf("tool %q arguments: %w", call.Name, err)
		}
	}
	tool := b.Tool
	if tool == "" {
		tool = call.Name
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "aibench"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: b.Server}, nil)
	if err != nil {
		return "", fmt.Errorf("mcp %s: %w", b.Server, err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return "", fmt.Errorf("mcp %s: %w", tool, err)
	}

	// Text content joins into the result; a tool-level error (IsError)
	// carries that text as the failure. Non-text-only results fall back to
	// the marshaled content list so something inspectable returns.
	var texts []string
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			texts = append(texts, t.Text)
		}
	}
	out := strings.Join(texts, "\n")
	if out == "" && len(res.Content) > 0 {
		if raw, err := json.Marshal(res.Content); err == nil {
			out = string(raw)
		}
	}
	if res.IsError {
		if out == "" {
			out = "tool failed"
		}
		return "", errors.New(truncate(out))
	}
	return out, nil // Run's capResult enforces the result budget
}
