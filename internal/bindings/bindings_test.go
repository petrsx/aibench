// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package bindings

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/petrsx/aibench/internal/provider"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "prompt.tools.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadSplitsOverlayAndBindings(t *testing.T) {
	path := writeFile(t, `{
		"openai":    { "tools": [{"type": "function"}], "tool_choice": "auto" },
		"anthropic": { "tools": [{"name": "get_stations"}] },
		"bindings":  { "get_stations": { "method": "GET", "url": "http://localhost:8080/stations" } }
	}`)

	s, err := LoadAll([]string{path})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !s.Bound("get_stations") {
		t.Error(`Bound("get_stations") = false; want true`)
	}
	if s.Bound("other") {
		t.Error(`Bound("other") = true; want false`)
	}
	if _, ok := s.Overlay["bindings"]; ok {
		t.Error(`Overlay contains "bindings"; must be stripped before sending`)
	}

	// The api sections survive raw, decodable into their fields.
	fields, err := s.Overlay.Fields("openai")
	if err != nil {
		t.Fatalf("Fields(openai) error: %v", err)
	}
	if string(fields["tool_choice"]) != `"auto"` {
		t.Errorf("openai tool_choice = %s; want %q", fields["tool_choice"], `"auto"`)
	}
	if _, ok := fields["tools"]; !ok {
		t.Error("openai overlay missing tools")
	}
	if _, err := s.Overlay.Fields("anthropic"); err != nil {
		t.Errorf("Fields(anthropic) error: %v", err)
	}
}

func TestLoadAllDeclaredMissingFileErrors(t *testing.T) {
	// A declared file that is missing is an error — declaration is
	// intent, unlike the deleted implicit filename pairing.
	if _, err := LoadAll([]string{filepath.Join(t.TempDir(), "absent.json")}); err == nil {
		t.Error("LoadAll(missing) error = nil; want a named error")
	}
}

func TestLoadAllNoFiles(t *testing.T) {
	s, err := LoadAll(nil)
	if err != nil {
		t.Fatalf("LoadAll(nil) error: %v; want nil", err)
	}
	if len(s.Overlay) != 0 || len(s.Bindings) != 0 {
		t.Errorf("LoadAll(nil) = %+v; want empty set", s)
	}
}

func TestLoadRejectsInvalidJSON(t *testing.T) {
	if _, err := LoadAll([]string{writeFile(t, `{not json`)}); err == nil {
		t.Error("LoadAll([]string{invalid}) error = nil; want parse error")
	}
}

func TestRunFillsPlaceholders(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"trains":[]}`))
	}))
	defer srv.Close()

	s := Set{Bindings: map[string]Binding{
		"get_station_timetable": {
			Method: "GET",
			URL:    srv.URL + "/stations/{station_code}",
			Query:  map[string]string{"num_mins": "{num_mins}"},
		},
	}}

	out, err := s.Run(t.Context(), provider.ToolCall{
		ID:        "call_1",
		Name:      "get_station_timetable",
		Arguments: `{"station_code": "HWD", "num_mins": 90}`,
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if out != `{"trains":[]}` {
		t.Errorf("Run() = %q; want the stub body", out)
	}
	if gotPath != "/stations/HWD" {
		t.Errorf("request path = %q; want %q", gotPath, "/stations/HWD")
	}
	if gotQuery != "num_mins=90" {
		t.Errorf("request query = %q; want %q", gotQuery, "num_mins=90")
	}
}

func TestRunOmitsAbsentOptionalQueryArg(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
	}))
	defer srv.Close()

	s := Set{Bindings: map[string]Binding{
		"get_stations": {URL: srv.URL + "/stations", Query: map[string]string{"num_mins": "{num_mins}"}},
	}}

	if _, err := s.Run(t.Context(), provider.ToolCall{Name: "get_stations", Arguments: `{}`}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if gotQuery != "" {
		t.Errorf("request query = %q; want empty (absent optional arg drops the pair)", gotQuery)
	}
}

func TestRunPostsBodyFields(t *testing.T) {
	var gotBody map[string]any
	var gotMethod, gotType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotType = r.Method, r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
	}))
	defer srv.Close()

	s := Set{Bindings: map[string]Binding{
		"search": {Method: "POST", URL: srv.URL, Body: map[string]string{"q": "{query}"}},
	}}

	if _, err := s.Run(t.Context(), provider.ToolCall{Name: "search", Arguments: `{"query": "belfast"}`}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q; want POST", gotMethod)
	}
	if gotType != "application/json" {
		t.Errorf("content-type = %q; want application/json", gotType)
	}
	if gotBody["q"] != "belfast" {
		t.Errorf(`body q = %v; want "belfast"`, gotBody["q"])
	}
}

func TestRunUnboundTool(t *testing.T) {
	if _, err := (Set{}).Run(t.Context(), provider.ToolCall{Name: "nope"}); err == nil {
		t.Error("Run(unbound) error = nil; want no-binding error")
	}
}

func TestRunHTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := Set{Bindings: map[string]Binding{"t": {URL: srv.URL}}}
	_, err := s.Run(t.Context(), provider.ToolCall{Name: "t"})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("Run(500) error = %v; want error carrying the body", err)
	}
}

func TestRunSendsHeadersWithEnvExpansion(t *testing.T) {
	t.Setenv("AGENT_TOKEN", "tok-123")
	var gotAuth, gotFeature string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotFeature = r.Header.Get("Foundry-Features")
	}))
	defer srv.Close()

	s := Set{Bindings: map[string]Binding{
		"ask_agent": {
			Method: "POST",
			URL:    srv.URL,
			Headers: map[string]string{
				"Authorization":    "Bearer ${AGENT_TOKEN}",
				"Foundry-Features": "HostedAgents=V1Preview",
			},
			Body: map[string]string{"input": "{question}"},
		},
	}}
	if _, err := s.Run(t.Context(), provider.ToolCall{Name: "ask_agent", Arguments: `{"question":"hi"}`}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if gotAuth != "Bearer tok-123" {
		t.Errorf("Authorization = %q; want the env-expanded token", gotAuth)
	}
	if gotFeature != "HostedAgents=V1Preview" {
		t.Errorf("Foundry-Features = %q; want the literal header", gotFeature)
	}
}

func TestRunPickExtractsFromEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"message","content":[{"type":"output_text","text":"The 21:05 runs."}]}]}`))
	}))
	defer srv.Close()

	s := Set{Bindings: map[string]Binding{
		"ask_agent": {URL: srv.URL, Pick: "output.0.content.0.text"},
	}}
	out, err := s.Run(t.Context(), provider.ToolCall{Name: "ask_agent"})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if out != "The 21:05 runs." {
		t.Errorf("Run() = %q; want the picked answer, not the envelope", out)
	}
}

func TestRunPickMissIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"output":[]}`))
	}))
	defer srv.Close()

	s := Set{Bindings: map[string]Binding{
		"t": {URL: srv.URL, Pick: "output.0.text"},
	}}
	_, err := s.Run(t.Context(), provider.ToolCall{Name: "t"})
	if err == nil || !strings.Contains(err.Error(), "output.0.text") {
		t.Errorf("Run(pick miss) error = %v; want it naming the path", err)
	}
}

func TestRunTruncatesOversizedResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 3*maxResultBytes)))
	}))
	defer srv.Close()

	s := Set{Bindings: map[string]Binding{"t": {URL: srv.URL}}}
	out, err := s.Run(t.Context(), provider.ToolCall{Name: "t"})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(out) != maxResultBytes {
		t.Errorf("len(Run()) = %d; want capped at %d", len(out), maxResultBytes)
	}
}

func TestRunTruncatesJSONArrayAtElementBoundaries(t *testing.T) {
	// ~166 stations at ~120 bytes each ≈ 20KB — over the result budget.
	type station struct {
		Code string  `json:"code"`
		Name string  `json:"name"`
		Lat  float64 `json:"latitude"`
	}
	var stations []station
	for i := 0; i < 200; i++ {
		stations = append(stations, station{
			Code: fmt.Sprintf("ST%03d", i), Name: strings.Repeat("x", 80), Lat: 53.5,
		})
	}
	full, _ := json.Marshal(stations)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(full)
	}))
	defer srv.Close()

	s := Set{Bindings: map[string]Binding{"list": {URL: srv.URL}}}
	out, err := s.Run(t.Context(), provider.ToolCall{Name: "list"})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(out) > maxResultBytes {
		t.Errorf("len(Run()) = %d; want within the %d budget", len(out), maxResultBytes)
	}
	// The cut result is still valid JSON, ending in the truncation marker.
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("truncated result is not valid JSON: %v\n%s", err, out[len(out)-80:])
	}
	var marker string
	if err := json.Unmarshal(items[len(items)-1], &marker); err != nil || !strings.Contains(marker, "of 200 items") {
		t.Errorf("last element = %s; want the truncation marker naming 200 total", items[len(items)-1])
	}
	if len(items) < 10 {
		t.Errorf("kept %d items; want a meaningful prefix of the array", len(items)-1)
	}
}

// TestLoadAllMerge pins the multi-file rules: per-wire tools arrays
// concatenate in list order, any other duplicated overlay field is a
// named conflict, and so is a tool bound twice.
func TestLoadAllMerge(t *testing.T) {
	shared := writeFile(t, `{
	  "chat": {"tools": [{"type":"function","function":{"name":"websearch"}}]},
	  "bindings": {"websearch": {"type":"static","result":"hit"}}
	}`)
	specific := writeFile(t, `{
	  "chat": {"tools": [{"type":"function","function":{"name":"get_weather"}}], "tool_choice": "auto"},
	  "bindings": {"get_weather": {"type":"static","result":"sunny"}}
	}`)

	s, err := LoadAll([]string{shared, specific})
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}
	fields, err := s.Overlay.Fields("chat")
	if err != nil {
		t.Fatal(err)
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(fields["tools"], &tools); err != nil || len(tools) != 2 {
		t.Errorf("merged chat.tools = %d entries, %v; want both files' tools", len(tools), err)
	}
	if string(fields["tool_choice"]) != `"auto"` {
		t.Errorf("tool_choice = %s; want the single declaration kept", fields["tool_choice"])
	}
	if !s.Bound("websearch") || !s.Bound("get_weather") {
		t.Error("merged bindings missing a tool from one of the files")
	}

	// The same non-tools field in both files: a named conflict.
	conflicting := writeFile(t, `{"chat": {"tool_choice": "required"}}`)
	if _, err := LoadAll([]string{specific, conflicting}); err == nil || !strings.Contains(err.Error(), "tool_choice") {
		t.Errorf("duplicate tool_choice error = %v; want it named", err)
	}

	// The same binding name in both files: a named conflict.
	rebound := writeFile(t, `{"bindings": {"websearch": {"type":"static","result":"other"}}}`)
	if _, err := LoadAll([]string{shared, rebound}); err == nil || !strings.Contains(err.Error(), "websearch") {
		t.Errorf("duplicate binding error = %v; want it named", err)
	}

	// The "//" comment key never merges or conflicts.
	commented := writeFile(t, `{"//": "docs here", "messages": {"tools": []}}`)
	commented2 := writeFile(t, `{"//": "other docs"}`)
	if _, err := LoadAll([]string{commented, commented2}); err != nil {
		t.Errorf("comment keys conflicted: %v", err)
	}
}
