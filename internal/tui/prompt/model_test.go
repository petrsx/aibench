// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/petrsx/aibench/internal/preset"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/tui/render"
)

// TestCopyControl pins what the Prompt tab hands over: the prompt as the
// file holds it, not the glamour-rendered view — the file is the thing you
// meant to take.
func TestCopyControl(t *testing.T) {
	m := New(provider.SpecFor("chat"), "", nil, "", nil)
	m.SetSize(3, 120, 36)
	m.sysSource = "# Be brief\n\nAnswer in one line."

	cells := render.FooterControlCells(1, m.top+render.TabHeaderRows+m.sysView.Height(),
		max(1, m.sysView.Width()+2), render.CopyLabel)
	cell := cells[0]
	if !m.copyAt(cell.X, cell.Y) {
		t.Fatalf("no copy control on the closing rule (%d,%d)", cell.X, cell.Y)
	}
	if m.copyAt(cell.X, cell.Y+1) || m.copyAt(1, cell.Y) {
		t.Error("the copy hit box reaches beyond the control")
	}
	if cmd := m.copyPrompt(); cmd == nil {
		t.Error("copying the prompt produced no command")
	}
	if m.note.Text() == "" {
		t.Error("copying said nothing; a silent copy looks like a click that missed")
	}
}

func TestParamsParsing(t *testing.T) {
	m := New(provider.SpecFor("openai"), "", nil, "", nil)
	// The rows are the file's, so the file is where they come from.
	m.applyPreset(preset.Preset{Params: map[string]string{
		"temperature": "", "top_p": "", "max_tokens": "", "seed": "", "stop": "",
	}})
	set := func(name, v string) {
		for i, row := range m.rows {
			if row.name == name {
				m.params[i].SetValue(v)
				return
			}
		}
		t.Fatalf("no %q row; the prompt file defines it", name)
	}
	set("temperature", "0.7")
	set("max_tokens", "128")
	set("seed", "42")
	set("stop", " END, STOP, ")
	set("top_p", "nonsense") // unparsable stays unset

	p := m.Params()
	if p.Temperature == nil || *p.Temperature != 0.7 {
		t.Errorf("Temperature = %v; want 0.7", p.Temperature)
	}
	if p.TopP != nil {
		t.Errorf("TopP = %v; want nil for unparsable input", *p.TopP)
	}
	if p.MaxTokens != 128 {
		t.Errorf("MaxTokens = %d; want 128", p.MaxTokens)
	}
	if p.Seed == nil || *p.Seed != 42 {
		t.Errorf("Seed = %v; want 42", p.Seed)
	}
	if len(p.Stop) != 2 || p.Stop[0] != "END" || p.Stop[1] != "STOP" {
		t.Errorf("Stop = %v; want [END STOP]", p.Stop)
	}
}

func TestPresetApplyAndRoundTrip(t *testing.T) {
	m := New(provider.SpecFor("openai"), "", nil, "", nil)
	m.applyPreset(preset.Preset{
		System: "Be terse.",
		Params: map[string]string{"temperature": "0.3", "custom_key": "kept"},
	})

	if m.SystemPrompt() != "Be terse." {
		t.Errorf("SystemPrompt() = %q; want applied system", m.SystemPrompt())
	}
	tempIdx := -1
	for i, row := range m.rows {
		if row.name == "temperature" {
			tempIdx = i
		}
	}
	if tempIdx < 0 || m.params[tempIdx].Value() != "0.3" {
		t.Fatalf("no temperature row carrying 0.3; rows = %+v", m.rows)
	}
	// The foreign key gets a row too — marked, never sent — because a
	// param silently ignored is how a typo survives a whole session.
	var foreign bool
	for _, row := range m.rows {
		if row.name == "custom_key" {
			foreign = !row.known
		}
	}
	if !foreign {
		t.Errorf("custom_key has no row of its own, or is not marked foreign: %+v", m.rows)
	}
	// And nothing the file did not define is on screen.
	if len(m.rows) != 2 {
		t.Errorf("rows = %+v; want only the two the file defines", m.rows)
	}

	// Edit in the TUI, rebuild: foreign keys survive, edits land.
	m.params[tempIdx].SetValue("0.9")
	cur := m.currentPreset()
	if cur.Params["temperature"] != "0.9" {
		t.Errorf("current temperature = %q; want 0.9", cur.Params["temperature"])
	}
	if cur.Params["custom_key"] != "kept" {
		t.Errorf("custom_key = %q; want kept through the round-trip", cur.Params["custom_key"])
	}
	if cur.System != "Be terse." {
		t.Errorf("current system = %q; want unchanged", cur.System)
	}
}

// TestSetProfileRemarksForeignParams pins what a profile switch does to
// the rows: the params stay (they are the file's), but which api knows
// them changes — top_k is Anthropic's, so it is foreign on the chat wire
// and its own on the messages one.
func TestSetProfileRemarksForeignParams(t *testing.T) {
	// A real file, since a profile switch re-reads it: the rows are the
	// file's, and it is the marking that follows the api.
	path := filepath.Join(t.TempDir(), "p.md")
	if err := os.WriteFile(path, []byte("---\ntop_k: 40\n---\n\nBe terse.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(provider.SpecFor("chat"), path, nil, "", nil)
	known := func() bool {
		for _, row := range m.rows {
			if row.name == "top_k" {
				return row.known
			}
		}
		t.Fatal("top_k lost its row")
		return false
	}
	if known() {
		t.Error("top_k reads as a chat param; the chat api does not take it")
	}
	m.SetProfile(provider.SpecFor("messages"), path, nil, "")
	if !known() {
		t.Error("top_k still reads as foreign after switching to the messages api")
	}
}

// TestPromptlessProfile pins the kind: agent behavior: with no prompt file
// the tab renders the agent notice, a switch from a model profile clears
// the stale prompt, and nothing tries to save back to a file.
func TestPromptlessProfile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(file, []byte("Be terse."), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(provider.SpecFor("chat"), file, nil, "", nil)
	if m.SystemPrompt() != "Be terse." {
		t.Fatalf("SystemPrompt() = %q; want the file's content", m.SystemPrompt())
	}

	// Switch to an agent profile: no prompt file, stale content must clear.
	m.SetProfile(provider.SpecFor("responses"), "", nil, "")
	if m.SystemPrompt() != "" {
		t.Errorf("SystemPrompt() = %q after promptless switch; want empty", m.SystemPrompt())
	}
	m.SetSize(0, 80, 24)
	if v := m.sysView.View(); !strings.Contains(v, "agent") {
		t.Errorf("view = %q; want the agent notice", v)
	}

	// A dirty param must not create a file out of thin air.
	m.applyPreset(preset.Preset{Params: map[string]string{"temperature": "0.3"}})
	m.params[0].SetValue("0.5")
	m.dirty = true
	m.dirtyAt = time.Now().Add(-2 * time.Second)
	_ = m.Update(SaveMsg{})
	if m.dirty {
		t.Error("dirty still set; want promptless save to no-op")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("dir has %d entries; want just the original prompt.md", len(entries))
	}
}

// TestParamsCopyControl pins the Params panel's own control: it sits in
// the panel's title corner like every other side panel's, and what it
// hands over is the params as frontmatter — the shape they live in, and
// the shape they paste back as. Empty fields are left out: an unset param
// is not a param, here as on the wire.
func TestParamsCopyControl(t *testing.T) {
	m := New(provider.SpecFor("responses"), "", nil, "", nil)
	m.SetSize(3, 120, 36)
	m.applyPreset(preset.Preset{Params: map[string]string{
		"reasoning.effort": "medium", "text.verbosity": "low",
	}})
	m.View() // draws the panel, so the control is on screen

	cell := render.PanelControlCells(m.mainWidth(), m.top, m.paramsWidth(), render.CopyLabel)[0]
	if !m.paramsCopyAt(cell.X, cell.Y) {
		t.Fatalf("no copy control in the Params panel's corner (%d,%d)", cell.X, cell.Y)
	}
	if m.paramsCopyAt(cell.X, cell.Y+1) || m.paramsCopyAt(1, cell.Y) {
		t.Error("the copy hit box reaches beyond the control")
	}
	if cmd := m.copyParams(); cmd == nil {
		t.Fatal("copying the params produced no command")
	}
	if m.note.Text() == "" {
		t.Error("copying said nothing; a silent copy looks like a click that missed")
	}
}

// TestParamsPaneShowsOnlyTheFilesParams pins what the panel is for: the
// params the prompt file defines, and nothing else. Listing every knob
// the api has filled it with empty boxes for things nobody set — five
// rows to say two — while what the api takes is a reference question the
// docs answer, linked from the pane's last row.
func TestParamsPaneShowsOnlyTheFilesParams(t *testing.T) {
	m := New(provider.SpecFor("responses"), "", nil, "", nil)
	m.SetSize(3, 120, 36)
	m.applyPreset(preset.Preset{Params: map[string]string{
		"reasoning.effort": "medium", "text.verbosity": "low",
	}})

	if len(m.rows) != 2 {
		t.Fatalf("rows = %+v; want the two the file defines", m.rows)
	}
	plain := ansi.Strip(m.View())
	for _, unset := range []string{"temperature", "top_p", "max_output_tokens"} {
		if strings.Contains(plain, unset) {
			t.Errorf("the pane lists %q, which the file never set", unset)
		}
	}
	if !strings.Contains(plain, "params reference") {
		t.Error("no link to the params docs on the pane")
	}
	if !strings.Contains(m.View(), ansi.SetHyperlink(
		"https://"+render.RepoURL+"/blob/main/docs/prompts.md")) {
		t.Error("the note is not a real hyperlink")
	}

	// An empty file says so, and still points at the docs.
	m.applyPreset(preset.Preset{})
	plain = ansi.Strip(m.View())
	if !strings.Contains(plain, "none set") || !strings.Contains(plain, "frontmatter") {
		t.Errorf("an empty params pane does not say what to do:\n%s", plain)
	}
}

// TestMissingPromptFileIsNamed pins the difference between "no prompt
// configured" and "the config named one and it is not there" — the two
// looked identical, so a session could send bare messages while the
// header advertised a prompt set.
func TestMissingPromptFileIsNamed(t *testing.T) {
	m := New(provider.SpecFor("openai"), filepath.Join(t.TempDir(), "gone.md"), nil, "", nil)
	m.SetSize(3, 100, 30)

	issues := m.LoadIssues()
	if len(issues) != 1 || !strings.Contains(issues[0], "gone.md") {
		t.Fatalf("LoadIssues() = %v; want the missing file named", issues)
	}
	// The tab keeps saying it after the notice lapses. (The path itself is
	// clipped to the pane when it is long — LoadIssues above is where the
	// whole of it lives.)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Not found") {
		t.Errorf("the tab does not say the file is missing:\n%s", v)
	}
}

// TestAgentProfileHasNoPromptIssue pins the other silence: an agent owns
// its instructions, so no path is nothing to report.
func TestAgentProfileHasNoPromptIssue(t *testing.T) {
	m := New(provider.SpecFor("openai"), "", nil, "", nil)
	m.SetSize(3, 100, 30)
	if issues := m.LoadIssues(); len(issues) != 0 {
		t.Errorf("LoadIssues() = %v; want none for a promptless profile", issues)
	}
}
