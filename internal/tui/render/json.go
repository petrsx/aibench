// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package render holds the TUI's pure presentation helpers: JSON/SSE body
// formatting, request-line composition, line shaping for a pane's cache,
// the copy control. The functions derive display strings from
// provider/capture/store values and the shared theme; they hold no
// state, so every tui sub-package can share them. (Mouse text selection,
// which does hold state, is internal/tui/selection.)
package render

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/petrsx/aibench/internal/tui/theme"
)

// IsJSONContent reports whether s is a single JSON value (object or array),
// so a structured reply can render with token colors instead of as prose.
func IsJSONContent(s string) bool {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "{") && !strings.HasPrefix(t, "[") {
		return false
	}
	return json.Valid([]byte(t))
}

// PrettyBody re-indents a JSON body for reading and highlights its keys;
// SSE streams keep their line-per-event stream shape with dimmed prefixes
// and highlighted payload keys; anything else comes back verbatim.
// Whitespace and color only — the bytes stay the same.
func PrettyBody(s string) []string { return PrettyBodyWith(s, false) }

// PrettyBodyWith is PrettyBody with decoding: with decode set, string
// values carrying JSON or newlines are shown as what they hold rather than
// as the escaped text the wire carried (see embedded.go).
func PrettyBodyWith(s string, decode bool) []string {
	trimmed := strings.TrimSpace(s)
	if strings.HasPrefix(trimmed, "data:") || strings.HasPrefix(trimmed, "event:") {
		return prettySSE(s)
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var buf bytes.Buffer
		if err := json.Indent(&buf, []byte(trimmed), "", "  "); err == nil {
			return expandEmbedded(strings.Split(buf.String(), "\n"), decode)
		}
	}
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

// prettySSE renders a text/event-stream body one event at a time: the
// SSE field prefixes go dim and each JSON payload is pretty-printed like
// any other body (the stream's one-line-per-event shape wraps unreadably
// in the viewport anyway).
func prettySSE(s string) []string {
	var out []string
	for l := range strings.SplitSeq(strings.TrimRight(s, "\n"), "\n") {
		payload, ok := strings.CutPrefix(l, "data: ")
		if !ok {
			out = append(out, theme.DimStyle.Render(l)) // event:/id: fields, separators
			continue
		}
		if strings.HasPrefix(payload, "{") {
			var buf bytes.Buffer
			if json.Indent(&buf, []byte(payload), "", "  ") == nil {
				body := strings.Split(colorizeJSON(buf.String()), "\n")
				out = append(out, theme.DimStyle.Render("data: ")+body[0])
				out = append(out, body[1:]...)
				continue
			}
			// The 8KB body cap can cut the final event mid-JSON; show a
			// compact marker instead of dumping the broken line.
			out = append(out, theme.DimStyle.Render("data: ")+
				theme.DimStyle.Render(ansi.Truncate(payload, 48, "…")+"  (truncated)"))
			continue
		}
		out = append(out, theme.DimStyle.Render("data: ")+payload) // e.g. [DONE]
	}
	return out
}

// colorizeJSON walks well-formed JSON and colors its tokens — keys,
// strings, numbers, and literals — in the spirit of tidwall/pretty, but
// as our own scanner on our own palette (no dependency). Structure
// (braces, commas, whitespace) stays unstyled; the bytes are preserved.
func colorizeJSON(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"':
			j := i + 1
			for j < len(s) {
				if s[j] == '\\' {
					j += 2
					continue
				}
				if s[j] == '"' {
					j++
					break
				}
				j++
			}
			k := j // a string followed by ':' is a key
			for k < len(s) && s[k] == ' ' {
				k++
			}
			if k < len(s) && s[k] == ':' {
				b.WriteString(theme.JSONKeyStyle.Render(s[i:j]))
			} else {
				b.WriteString(theme.JSONStringStyle.Render(s[i:j]))
			}
			i = j
		case c == '-' || (c >= '0' && c <= '9'):
			j := i
			for j < len(s) && strings.ContainsRune("-+.eE0123456789", rune(s[j])) {
				j++
			}
			b.WriteString(theme.JSONNumberStyle.Render(s[i:j]))
			i = j
		case strings.HasPrefix(s[i:], "true"):
			b.WriteString(theme.JSONLiteralStyle.Render("true"))
			i += 4
		case strings.HasPrefix(s[i:], "false"):
			b.WriteString(theme.JSONLiteralStyle.Render("false"))
			i += 5
		case strings.HasPrefix(s[i:], "null"):
			b.WriteString(theme.JSONLiteralStyle.Render("null"))
			i += 4
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}
