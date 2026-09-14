// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package bindings loads the tool files the config's top-level `tools:`
// map names (referenced by prompt sets, merged in order): per-api
// request-body overlays (tools, tool_choice, response_format, …) that
// merge into the request verbatim, plus the optional execution bindings
// that make aibench run a tool call. A binding names its executor via
// `type` — the plugin seam: `http` (default) calls an HTTP endpoint,
// `static` returns a canned result, `mcp` calls a tool on an MCP server.
package bindings

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/petrsx/aibench/internal/provider"
)

// maxResultBytes caps a tool result fed back to the model, mirroring the
// capture transport's body cap. maxReadBytes is the larger window an HTTP
// response is read into before pick/truncate — a body truncated mid-JSON
// would break a pick.
const (
	maxResultBytes = 8 << 10
	maxReadBytes   = 512 << 10
)

// runTimeout bounds one tool call; local stubs answer fast, and a hung
// tool must not wedge the send loop's context forever.
const runTimeout = 30 * time.Second

// Binding says how aibench fulfills one tool's calls. Type picks the
// executor — "http" (default when empty), "static", or "mcp" — and the
// rest of the fields belong to their executor (a flat union, like
// config.Profile). A tool without a binding is inspect-only — its calls
// are shown, never executed.
type Binding struct {
	Type string `json:"type"`

	// http: the request to build from the call's arguments. {arg}
	// placeholders in URL, query, and body values fill from the call's
	// JSON argument object; header values expand ${VAR} from the
	// environment so credentials stay in .env, never in the JSON. Pick is
	// an optional gjson path applied to a JSON response, reducing a
	// verbose envelope (a Responses API payload, say) to the answer the
	// model actually needs.
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Query   map[string]string `json:"query"`
	Headers map[string]string `json:"headers"`
	Body    map[string]string `json:"body"`
	Pick    string            `json:"pick"`

	// static: a canned outcome for deterministic prompt testing. Result is
	// any JSON (a string unwraps bare, anything else compacts); a non-empty
	// Error makes the run fail with it instead; DelayMs simulates latency.
	Result  json.RawMessage `json:"result"`
	Error   string          `json:"error"`
	DelayMs int             `json:"delay_ms"`

	// mcp: the streamable-HTTP server to call. Tool overrides the tool
	// name on the server when it differs from the advertised name.
	Server string `json:"server"`
	Tool   string `json:"tool"`
}

// Set is one prompt's request extensions: the per-api body overlays
// (sent verbatim) and the aibench-only bindings (stripped before sending).
type Set struct {
	Overlay  provider.Overlay
	Bindings map[string]Binding
}

// LoadAll parses the declared tool files and merges them in order into
// one Set. Fail-loud throughout: a declared file that is missing or does
// not parse is an error (declaration is intent), overlay `tools` arrays
// concatenate while any other field two files both set is a named
// conflict, and a tool name bound twice is one too. The "//" key is a
// comment convention and is skipped entirely.
func LoadAll(paths []string) (Set, error) {
	merged := Set{Overlay: provider.Overlay{}, Bindings: map[string]Binding{}}
	// origin tracks, per "section.field" and per binding name, which file
	// set it first — so a conflict error can name both files.
	fieldOrigin := map[string]string{}
	bindingOrigin := map[string]string{}
	for _, path := range paths {
		one, err := load(path)
		if err != nil {
			return Set{}, err
		}
		if err := merge(&merged, one, path, fieldOrigin, bindingOrigin); err != nil {
			return Set{}, err
		}
	}
	return merged, nil
}

// merge folds one file's set into the accumulator under the conflict
// rules above.
func merge(dst *Set, src Set, path string, fieldOrigin, bindingOrigin map[string]string) error {
	for section, raw := range src.Overlay {
		if section == "//" {
			continue // the comment key, never merged or sent
		}
		var srcFields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &srcFields); err != nil {
			return fmt.Errorf("%s: section %q: %w", path, section, err)
		}
		dstFields := map[string]json.RawMessage{}
		if cur, ok := dst.Overlay[section]; ok {
			if err := json.Unmarshal(cur, &dstFields); err != nil {
				return fmt.Errorf("section %q: %w", section, err)
			}
		}
		for field, val := range srcFields {
			key := section + "." + field
			prev, taken := dstFields[field]
			switch {
			case !taken:
				dstFields[field] = val
				fieldOrigin[key] = path
			case field == "tools":
				joined, err := concatArrays(prev, val)
				if err != nil {
					return fmt.Errorf("%s: %s: %w (both %s and %s declare it)", path, key, err, fieldOrigin[key], path)
				}
				dstFields[field] = joined
			default:
				return fmt.Errorf("%s.%s declared in both %s and %s — only tools arrays merge", section, field, fieldOrigin[key], path)
			}
		}
		out, err := json.Marshal(dstFields)
		if err != nil {
			return err
		}
		dst.Overlay[section] = out
	}
	for name, b := range src.Bindings {
		if prev, ok := bindingOrigin[name]; ok {
			return fmt.Errorf("tool %q bound in both %s and %s", name, prev, path)
		}
		bindingOrigin[name] = path
		dst.Bindings[name] = b
	}
	return nil
}

// concatArrays joins two JSON arrays element-wise.
func concatArrays(a, b json.RawMessage) (json.RawMessage, error) {
	var av, bv []json.RawMessage
	if err := json.Unmarshal(a, &av); err != nil {
		return nil, fmt.Errorf("tools is not an array")
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return nil, fmt.Errorf("tools is not an array")
	}
	return json.Marshal(append(av, bv...))
}

// load parses one tool file. Top-level keys split by ownership:
// "bindings" is aibench's execution map; every other key is an api
// section kept raw for the overlay.
func load(path string) (Set, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Set{}, err
	}

	sections := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &sections); err != nil {
		return Set{}, fmt.Errorf("%s: %w", path, err)
	}

	s := Set{Overlay: provider.Overlay{}}
	for key, val := range sections {
		if key == "bindings" {
			if err := json.Unmarshal(val, &s.Bindings); err != nil {
				return Set{}, fmt.Errorf("%s: bindings: %w", path, err)
			}
			continue
		}
		s.Overlay[key] = val
	}
	return s, nil
}

// Bound reports whether a tool has an execution binding.
func (s Set) Bound(name string) bool {
	_, ok := s.Bindings[name]
	return ok
}

// Run executes one bound tool call through its binding's executor and
// returns the result fed back to the model (capped at maxResultBytes).
// Callers run it inside a tea.Cmd — never in the message loop. Errors —
// including an unknown binding type, validated here rather than at Load so
// it surfaces in the transcript as the tool's result — flow back to the
// model as error text.
func (s Set) Run(ctx context.Context, call provider.ToolCall) (string, error) {
	b, ok := s.Bindings[call.Name]
	if !ok {
		return "", fmt.Errorf("tool %q has no binding", call.Name)
	}

	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	var out string
	var err error
	switch b.Type {
	case "", "http":
		out, err = runHTTP(ctx, b, call)
	case "static":
		out, err = runStatic(ctx, b)
	case "mcp":
		out, err = runMCP(ctx, b, call)
	default:
		return "", fmt.Errorf("tool %q: unknown binding type %q (want http, static, or mcp)", call.Name, b.Type)
	}
	if err != nil {
		return "", err
	}
	return capResult(out), nil
}

// capResult enforces the tool-result budget fed back to the model. A JSON
// array over budget is cut at element boundaries with a marker element
// appended, so the model always receives valid JSON and knows it is
// partial — a byte cut mid-value would hand it garbage. Anything else
// falls back to the byte cut.
func capResult(s string) string {
	if len(s) <= maxResultBytes {
		return s
	}

	var items []json.RawMessage
	if err := json.Unmarshal([]byte(s), &items); err != nil {
		return truncate(s)
	}
	total := len(items)
	var b strings.Builder
	b.WriteByte('[')
	kept := 0
	for _, item := range items {
		// Leave room for the closing marker element (~64 bytes).
		if b.Len()+len(item)+64 > maxResultBytes {
			break
		}
		if kept > 0 {
			b.WriteByte(',')
		}
		b.Write(item)
		kept++
	}
	if kept > 0 {
		b.WriteByte(',')
	}
	fmt.Fprintf(&b, "%q]", fmt.Sprintf("[truncated: %d of %d items]", kept, total))
	return b.String()
}

// truncate caps s at the tool-result budget fed back to the model.
func truncate(s string) string {
	if len(s) <= maxResultBytes {
		return s
	}
	return s[:maxResultBytes]
}
