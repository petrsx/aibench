// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A tool call as these APIs really send one: the arguments are JSON, but
// they travel as a string, so json.Indent leaves them as an escaped wall.
const toolCallBody = `{"input":[{"type":"function_call","name":"get_weather",` +
	`"arguments":"{\"latitude\":48.85341,\"longitude\":2.3488}"},` +
	`{"type":"message","content":"line one\nline two"}]}`

// TestBodyIsWireFormByDefault pins the default: the dump shows what the
// endpoint sent — a string — because whether a request carries structure
// or text is the question this pane exists to answer. Decoding is
// something you ask for.
func TestBodyIsWireFormByDefault(t *testing.T) {
	plain := ansi.Strip(strings.Join(PrettyBody(toolCallBody), "\n"))
	if !strings.Contains(plain, `\"latitude\"`) {
		t.Errorf("the default dump does not show the escaped string the wire carried:\n%s", plain)
	}
	if strings.Contains(plain, `"latitude": 48.85341`) {
		t.Error("the default dump decoded a string it was not asked to")
	}
}

// TestDecodedBodyShowsStructure pins what the pane's toggle does: every
// string that carries JSON or newlines is shown as what it holds, in place
// of escapes nobody can read.
func TestDecodedBodyShowsStructure(t *testing.T) {
	plain := ansi.Strip(strings.Join(PrettyBodyWith(toolCallBody, true), "\n"))
	if !strings.Contains(plain, `"latitude": 48.85341`) {
		t.Errorf("a decodable JSON string was not decoded:\n%s", plain)
	}
	if strings.Contains(plain, `\"latitude\"`) {
		t.Error("the decoded body still shows the escaped form")
	}
	// A string carrying newlines becomes lines, not one escaped run.
	if !strings.Contains(plain, "line one\n") || strings.Contains(plain, `line one\nline two`) {
		t.Errorf("a newline-carrying string was not opened out:\n%s", plain)
	}
}

// TestDecodeOnlyCleanJSON pins the guard: a decoder that guessed at
// half-JSON would cost this pane the only thing it has, which is being
// trusted. Those values read the same either way.
func TestDecodeOnlyCleanJSON(t *testing.T) {
	for _, body := range []string{
		`{"a":"{not json"}`,
		`{"a":"42"}`,
		`{"a":"plain words"}`,
		`{"a":"[1,2,"}`,
	} {
		off := ansi.Strip(strings.Join(PrettyBody(body), "\n"))
		on := ansi.Strip(strings.Join(PrettyBodyWith(body, true), "\n"))
		if off != on {
			t.Errorf("%s: decoding changed a value that is not decodable:\n%s\n%s", body, off, on)
		}
	}
}
