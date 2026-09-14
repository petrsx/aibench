// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package render

// JSON inside JSON. These APIs carry structure as text all over the place —
// a tool call's `arguments`, a tool result's `output`, a message's
// `content` — so the body a request actually sends is full of escaped
// walls that no amount of indenting can shape:
//
//	"arguments": "{\"latitude\":48.85341,\"longitude\":2.3488}",
//
// The dump could decode those silently, and then it would be lying: the
// endpoint sent a string, and a reader deciding whether their request is
// well-formed needs to know which. So the wire form stays what you see,
// and decoding is a control you can press — the same bargain the chat
// makes with a tool payload.

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/petrsx/aibench/internal/tui/theme"
)

// The pane decides, not each value: a body is read one way or the other,
// and a control per string turned every interesting line into a line with
// a button in it. What decoding is available is named by the pane's own
// toggle (see the Inspector's head box).

// decodeString reports what a JSON string literal could be shown as: its
// decoded form and the control that offers it, or ok=false when there is
// nothing worth offering. Only a clean object or array counts as json —
// a decoder that guessed at half-JSON would cost the view its
// trustworthiness, which is the only thing it has.
func decodeString(lit string) (decoded string, isJSON, ok bool) {
	var s string
	if json.Unmarshal([]byte(lit), &s) != nil {
		return "", false, false
	}
	switch {
	case IsJSONContent(s):
		var buf bytes.Buffer
		if json.Indent(&buf, []byte(strings.TrimSpace(s)), "", "  ") != nil {
			return "", false, false
		}
		return buf.String(), true, true
	case strings.Contains(s, "\n"):
		return strings.TrimRight(s, "\n"), false, true
	}
	return "", false, false
}

// splitValue takes an indented JSON line and returns its parts: everything
// up to and including the colon (or nothing, for an array element), the
// string literal itself, and whatever trails it (a comma). It returns
// ok=false unless the value is a plain string literal.
func splitValue(line string) (head, lit, tail string, ok bool) {
	body := strings.TrimRight(line, " ")
	if strings.HasSuffix(body, ",") {
		body, tail = body[:len(body)-1], ","
	}
	if at := strings.Index(body, `": `); at >= 0 {
		head, body = body[:at+3], body[at+3:]
	} else {
		trimmed := strings.TrimLeft(body, " ")
		head, body = body[:len(body)-len(trimmed)], trimmed
	}
	if len(body) < 2 || !strings.HasPrefix(body, `"`) || !strings.HasSuffix(body, `"`) {
		return "", "", "", false
	}
	return head, body, tail, ok0(body)
}

// ok0 guards splitValue's literal: an escaped quote at the end means the
// line was cut, not closed.
func ok0(lit string) bool { return !strings.HasSuffix(lit, `\"`) }

// expandEmbedded rewrites the indented JSON lines. With decode off the
// lines are the wire form, colorized and nothing more. With it on, every
// string value that carries JSON or newlines is shown decoded and indented
// under its key, in place of the escaped form nobody can read.
func expandEmbedded(plain []string, decode bool) []string {
	var out []string
	for _, line := range plain {
		head, lit, tail, ok := splitValue(line)
		if !ok || !decode {
			out = append(out, colorizeJSON(line))
			continue
		}
		decoded, isJSON, can := decodeString(lit)
		if !can {
			out = append(out, colorizeJSON(line))
			continue
		}
		indent := strings.Repeat(" ", len(head)-len(strings.TrimLeft(head, " ")))
		out = append(out, colorizeJSON(strings.TrimRight(head, " ")))
		for _, l := range strings.Split(decoded, "\n") {
			if isJSON {
				out = append(out, indent+"  "+colorizeJSON(l))
			} else {
				out = append(out, indent+"  "+theme.JSONStringStyle.Render(l))
			}
		}
		if tail != "" {
			out = append(out, indent+tail)
		}
	}
	return out
}
