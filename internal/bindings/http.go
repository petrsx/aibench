// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package bindings

// The http executor: builds an HTTP request from the binding and the
// call's arguments, and returns the response body (optionally reduced by
// a gjson pick).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/petrsx/aibench/internal/provider"
)

func runHTTP(ctx context.Context, b Binding, call provider.ToolCall) (string, error) {
	args := map[string]any{}
	if strings.TrimSpace(call.Arguments) != "" {
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return "", fmt.Errorf("tool %q arguments: %w", call.Name, err)
		}
	}

	rawURL := fillArgs(b.URL, args, url.PathEscape)
	if len(b.Query) > 0 {
		q := url.Values{}
		for k, v := range b.Query {
			if filled, ok := fillQueryValue(v, args); ok {
				q.Set(k, filled)
			}
		}
		if enc := q.Encode(); enc != "" {
			sep := "?"
			if strings.Contains(rawURL, "?") {
				sep = "&"
			}
			rawURL += sep + enc
		}
	}

	method := strings.ToUpper(strings.TrimSpace(b.Method))
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if len(b.Body) > 0 {
		fields := make(map[string]any, len(b.Body))
		for k, v := range b.Body {
			if filled, ok := fillQueryValue(v, args); ok {
				fields[k] = filled
			}
		}
		enc, err := json.Marshal(fields)
		if err != nil {
			return "", err
		}
		body = strings.NewReader(string(enc))
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return "", err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range b.Headers {
		// ${VAR} expands from the *process* environment, so tokens stay out
		// of the tools file. Note a profile's credentials file is not it:
		// that is read into a lookup map, never exported, so a binding's
		// secret must be exported in the shell.
		req.Header.Set(k, os.ExpandEnv(v))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxReadBytes))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("%s %s: %s: %s", method, rawURL, resp.Status, truncate(strings.TrimSpace(string(data))))
	}

	out := string(data)
	if b.Pick != "" {
		picked := gjson.Get(out, b.Pick)
		if !picked.Exists() {
			return "", fmt.Errorf("tool %q: pick %q matched nothing in the response", call.Name, b.Pick)
		}
		out = picked.String()
	}
	return out, nil // Run's capResult enforces the result budget
}

// fillArgs substitutes {name} placeholders from the call's arguments,
// escaping each value with esc. Unknown placeholders stay literal so the
// error surfaces in the request rather than vanishing silently.
func fillArgs(s string, args map[string]any, esc func(string) string) string {
	return os.Expand(strings.NewReplacer("{", "${", "}", "}").Replace(s), func(name string) string {
		if v, ok := args[name]; ok {
			return esc(argString(v))
		}
		return "{" + name + "}"
	})
}

// fillQueryValue substitutes placeholders in a query/body value; a value
// that is a single placeholder for an absent argument drops the pair
// entirely (so optional args don't send empty params).
func fillQueryValue(v string, args map[string]any) (string, bool) {
	if name, ok := solePlaceholder(v); ok {
		if arg, present := args[name]; present {
			return argString(arg), true
		}
		return "", false
	}
	return fillArgs(v, args, func(s string) string { return s }), true
}

// solePlaceholder reports whether v is exactly one {name} placeholder.
func solePlaceholder(v string) (string, bool) {
	if strings.HasPrefix(v, "{") && strings.HasSuffix(v, "}") {
		inner := v[1 : len(v)-1]
		if inner != "" && !strings.ContainsAny(inner, "{}") {
			return inner, true
		}
	}
	return "", false
}

// argString renders a JSON argument value for URL/query use: strings stay
// bare, everything else re-marshals compactly.
func argString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	enc, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(enc)
}
