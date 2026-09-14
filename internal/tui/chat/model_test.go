// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/preset"
	"github.com/petrsx/aibench/internal/pricing"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/state"
	"github.com/petrsx/aibench/internal/store"
	"github.com/petrsx/aibench/internal/tui/render"
	"github.com/petrsx/aibench/internal/tui/selection"
)

func newChatModel() *Model {
	st := state.New(store.New(500), nil, make(chan capture.Event, 1), spinner.New())
	m := New(st, provider.SpecFor("openai"), config.Config{Model: "test-model"}, nil, "")
	m.SetSize(3, 120, 36) // top=3, width=120, content=36
	return m
}

// addRecord appends a user turn and a linked request/response record, mirroring
// what state's Capture/Receive do, so the Chat tab has record lines.
func addRecord(s *state.State, status int, usage *provider.Usage) {
	turn := s.AppendTurn(provider.RoleUser, "q", store.Complete)
	id := s.BeginRecord(capture.Event{Kind: capture.KindRequest, Status: status, Method: "POST", Path: "/p"})
	s.Link(turn, id)
	s.FinishBody(capture.Event{Kind: capture.KindBody})
	if usage != nil {
		s.SetUsage(usage)
	}
	s.FinishRecord()
}

// addExchange is addRecord plus the reply it produced — the shape a real
// send leaves behind. The two turns are written as separate blocks, so
// anything about a record's extent has to hold across them.
func addExchange(s *state.State, status int) {
	addRecord(s, status, nil)
	rec, _ := s.LastRecord()
	reply := s.AppendTurn(provider.RoleAssistant, "an answer", store.Complete)
	s.Link(reply, rec.ID)
}

func TestInputHistory(t *testing.T) {
	m := newChatModel()
	m.sent = []string{"first", "second"}
	m.histIdx = len(m.sent)
	m.input.SetValue("my draft")

	if !m.histPrev() || m.input.Value() != "second" {
		t.Fatalf("after 1x prev input = %q; want %q", m.input.Value(), "second")
	}
	if !m.histPrev() || m.input.Value() != "first" {
		t.Fatalf("after 2x prev input = %q; want %q", m.input.Value(), "first")
	}
	if m.histPrev() {
		t.Error("histPrev() at oldest entry = true; want false")
	}
	if !m.histNext() || m.input.Value() != "second" {
		t.Fatalf("after next input = %q; want %q", m.input.Value(), "second")
	}
	if !m.histNext() || m.input.Value() != "my draft" {
		t.Fatalf("after next past newest input = %q; want draft restored", m.input.Value())
	}
	if m.histNext() {
		t.Error("histNext() at draft = true; want false")
	}
}

func TestInputHistoryGatedByCursorLine(t *testing.T) {
	m := newChatModel()
	m.sent = []string{"old"}
	m.histIdx = 1
	m.input.SetValue("line1\nline2") // cursor ends on the last line

	if m.histPrev() {
		t.Error("histPrev() with cursor below first line = true; want false")
	}
}

func TestPosIn(t *testing.T) {
	m := newChatModel()
	m.chatLines = make([]string, 30)
	m.chatView.SetHeight(10)
	// v2 clamps SetYOffset against real content, so the viewport needs the 30
	// lines loaded before the offset sticks.
	m.chatView.SetContent(strings.TrimSuffix(strings.Repeat("x\n", 30), "\n"))
	m.chatView.SetYOffset(5)

	top := m.chatTop()
	if pos, ok := m.SelPosAt(paneChat, 7, top, false); !ok || pos.Line != 5 || pos.Col != 6 {
		t.Errorf("posIn(chat, top) = %v,%v; want line 5 col 6", pos, ok)
	}
	if _, ok := m.SelPosAt(paneChat, 0, top-2, false); ok {
		t.Error("posIn above chat without clamp = ok; want false")
	}
	if pos, ok := m.SelPosAt(paneChat, 0, top-2, true); !ok || pos.Line != 5 {
		t.Errorf("posIn above chat with clamp = %v,%v; want line 5", pos, ok)
	}
}

// TestMetricsRowsMapToTheRowClicked pins the panel's mouse mapping: the
// row under the pointer is the row you select. It was off by one — you had
// to aim a row below the one you wanted — because the right column's
// baseline counted a row the column does not have.
func TestMetricsRowsMapToTheRowClicked(t *testing.T) {
	m := newChatModel()
	addRecord(m.state, 200, nil)
	m.renderMetrics()
	if len(m.metricsLines) == 0 {
		t.Fatal("no metrics lines to select")
	}

	// Screen rows of the Metrics box: the Tokens graph above it, then the
	// box's top border, its title, the blank under it — and the content.
	first := m.top + m.tokensBlock() + 1 + render.SidePanelTitleRows
	left := m.chatColWidth() + 1
	for _, row := range []int{0, 1, 2} {
		pos, ok := m.SelPosAt(paneMetrics, left, first+row, false)
		if !ok {
			t.Fatalf("click on metrics row %d landed outside the pane", row)
		}
		if pos.Line != row {
			t.Errorf("click on screen row %d selected line %d; want %d (%q)",
				first+row, pos.Line, row, ansi.Strip(m.metricsLines[row]))
		}
	}
}

// TestTokensGraphIsNotText pins that the graph is a control, not content:
// a click on a bar picks its record, and a click anywhere else in the
// graph starts no text selection — there is nothing there worth copying,
// and a drag that began on a chart would only smear over the panel below.
func TestTokensGraphIsNotText(t *testing.T) {
	m := newChatModel()
	addRecord(m.state, 200, &provider.Usage{Prompt: 100, Completion: 10, Total: 110})
	m.viewTokens() // caches the bar→record mapping and the origin

	graphRow := m.rightTop() + 3 // first chart row
	// The axis-label column, left of the first bar: inside the graph, on no
	// bar at all.
	m.handleMouse(tea.MouseClickMsg{Button: tea.MouseLeft, X: m.chatColWidth() + 1, Y: graphRow})
	if m.sel.Pane != paneNone || m.sel.Drag {
		t.Errorf("a click in the graph started a selection (pane=%d drag=%v); the graph is not text",
			m.sel.Pane, m.sel.Drag)
	}
	// A bar still selects its record.
	m.state.Pin(-1)
	m.handleMouse(tea.MouseClickMsg{Button: tea.MouseLeft, X: m.tokenBarX0, Y: graphRow})
	if m.state.Pinned() < 0 {
		t.Error("a click on a bar did not pick its record")
	}
	if m.sel.Pane != paneNone {
		t.Errorf("a click on a bar started a selection (pane=%d); it is a control", m.sel.Pane)
	}
}

// TestMetricsCopyLabel pins the panel's one control: the icon drawn in the
// title row is the cell the click test accepts — the drawing and the hit
// box come from render.PanelCopyCell, and this is what catches them
// drifting apart — and clicking it copies the panel without starting a
// selection or moving which record is shown.
func TestMetricsCopyLabel(t *testing.T) {
	m := newChatModel()
	addRecord(m.state, 200, &provider.Usage{Prompt: 100, Completion: 10, Total: 110})
	m.renderMetrics()

	// Where the glyph actually lands in the rendered panel, in screen cells.
	panelX, panelY := m.chatColWidth(), m.rightTop()+m.tokensBlock()
	drawnX, drawnY := -1, -1
	for row, line := range strings.Split(m.viewMetrics(), "\n") {
		plain := ansi.Strip(line)
		// Byte offset to cell column: the box drawing around it is
		// multi-byte, so the two are not the same number.
		if at := strings.Index(plain, render.CopyLabel); at >= 0 {
			drawnX, drawnY = panelX+ansi.StringWidth(plain[:at]), panelY+row
			break
		}
	}
	if drawnX < 0 {
		t.Fatal("the panel draws no copy icon")
	}
	if !m.metricsCopyAt(drawnX, drawnY) {
		t.Errorf("the icon is drawn at (%d,%d) but the click test does not accept it", drawnX, drawnY)
	}
	// It keeps a cell of daylight from the border, and its neighbours are
	// not part of the control.
	if inner := panelX + m.metricsWidth() - 2; drawnX >= inner {
		t.Errorf("icon at column %d; want at least %d cells in from the border (%d)",
			drawnX, render.ControlPad, inner)
	}
	// The chip's fill answers a click too — a button whose edge does
	// nothing is a button you have to aim at — but the cell before the
	// chip, and the row below it, are not the control.
	if !m.metricsCopyAt(drawnX-1, drawnY) {
		t.Error("the chip's left pad does not answer a click")
	}
	if m.metricsCopyAt(drawnX-2, drawnY) || m.metricsCopyAt(drawnX, drawnY+1) {
		t.Error("the copy hit box reaches beyond the chip")
	}

	pinned := m.state.Pinned()
	if cmd := m.handleMouse(tea.MouseClickMsg{Button: tea.MouseLeft, X: drawnX, Y: drawnY}); cmd == nil {
		t.Error("clicking the copy icon produced no command; want the clipboard write")
	}
	if m.sel.Pane != paneNone || m.sel.Drag {
		t.Errorf("clicking the copy icon started a selection (pane=%d)", m.sel.Pane)
	}
	if m.state.Pinned() != pinned {
		t.Error("clicking the copy icon changed which record is shown")
	}

	// What lands on the clipboard is the panel as shown, without styling.
	text := m.copiedMetricsText()
	if !strings.Contains(text, "Request") || !strings.Contains(text, "in") {
		t.Errorf("copied text = %q; want the panel's rows", text)
	}
	if strings.Contains(text, "\x1b") {
		t.Error("copied text carries styling; want plain text")
	}
}

// TestHideDiagnostics pins ctrl+d: the record lines under the turns go and
// the conversation is left. The column keeps its width and the panels keep
// theirs — a key that reflowed the text you are reading would cost more
// than the rows it saves.
func TestHideDiagnostics(t *testing.T) {
	m := newChatModel()
	addRecord(m.state, 200, &provider.Usage{Prompt: 100, Completion: 10, Total: 110})
	m.state.AppendTurn(provider.RoleAssistant, "an answer", store.Complete)
	m.refresh()

	full := m.chatColWidth()
	if !strings.Contains(ansi.Strip(strings.Join(m.chatLines, "\n")), "POST") {
		t.Fatal("no record line to hide")
	}

	m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	body := ansi.Strip(strings.Join(m.chatLines, "\n"))
	if strings.Contains(body, "POST") {
		t.Errorf("the record lines survived ctrl+d:\n%s", body)
	}
	if !strings.Contains(body, "an answer") {
		t.Error("ctrl+d took the conversation with the diagnostics")
	}
	if m.chatColWidth() != full {
		t.Errorf("chat column = %d; want it unchanged at %d", m.chatColWidth(), full)
	}
	if m.metricsWidth() == 0 || m.tokensBlock() == 0 {
		t.Error("ctrl+d took the panels' space; they are a column of their own")
	}
	if !strings.Contains(ansi.Strip(m.View()), "Metrics") {
		t.Error("the Metrics panel went with the record lines")
	}

	// Back again: the record lines return.
	m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if !strings.Contains(ansi.Strip(strings.Join(m.chatLines, "\n")), "POST") {
		t.Error("ctrl+d did not bring the diagnostics back")
	}
}

// TestRecordKeysWalkThePin pins ctrl+←/→/↓ in the chat: the same walk over
// the same store pin the Inspector has, so the two tabs always show one
// record between them.
func TestRecordKeysWalkThePin(t *testing.T) {
	m := newChatModel()
	for range 3 {
		addRecord(m.state, 200, &provider.Usage{Prompt: 10, Completion: 1, Total: 11})
		m.state.AppendTurn(provider.RoleAssistant, "an answer", store.Complete)
	}
	m.refresh()
	recs := m.state.Records()
	if len(recs) != 3 {
		t.Fatalf("records = %d; want 3", len(recs))
	}
	if m.state.Pinned() != -1 {
		t.Fatalf("pinned at rest = %d; want -1 (following the latest)", m.state.Pinned())
	}

	prev := tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift}
	next := tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}
	latest := tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl}

	m.Update(prev) // off the newest, onto the one before it
	if got := m.state.Pinned(); got != recs[1].ID {
		t.Fatalf("pinned after shift+↑ = %d; want %d", got, recs[1].ID)
	}
	m.Update(prev)
	m.Update(prev) // already at the oldest: stops, never wraps
	if got := m.state.Pinned(); got != recs[0].ID {
		t.Fatalf("pinned at the oldest = %d; want %d", got, recs[0].ID)
	}
	m.Update(next)
	if got := m.state.Pinned(); got != recs[1].ID {
		t.Fatalf("pinned after ctrl+end = %d; want %d", got, recs[1].ID)
	}
	// Walking onto the newest lets the pin go: the views follow again.
	m.Update(next)
	if got := m.state.Pinned(); got != -1 {
		t.Fatalf("pinned on the newest = %d; want -1 (following)", got)
	}
	m.Update(prev)
	m.Update(latest)
	if got := m.state.Pinned(); got != -1 {
		t.Errorf("pinned after ctrl+end = %d; want -1 (following the latest again)", got)
	}
	// shift+end is the same reach on a keyboard without ctrl+end.
	m.Update(prev)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModShift})
	if got := m.state.Pinned(); got != -1 {
		t.Errorf("pinned after shift+end = %d; want -1", got)
	}
}

// TestHelpLineNamesWhatYouReachFor pins what the short help says: a key
// you cannot see is a key nobody presses — and a line naming everything
// names nothing, so the rest lives in the tips and the key sheet.
func TestHelpLineNamesWhatYouReachFor(t *testing.T) {
	m := newChatModel()
	var line string
	for _, b := range m.ShortHelp() {
		line += b.Help().Key + " " + b.Help().Desc + " · "
	}
	for _, want := range []string{"ctrl+f", "ctrl+y"} {
		if !strings.Contains(line, want) {
			t.Errorf("short help is missing %q: %s", want, line)
		}
	}
	// ctrl+d, ctrl+end and the record walk are named by the rotating
	// tips and the key sheet; the one line always on screen keeps the
	// controls you reach for, and room for the credit lockup.
	for _, absent := range []string{"ctrl+d", "ctrl+end", "shift+↑/↓"} {
		if strings.Contains(line, absent) {
			t.Errorf("short help carries %s: %s", absent, line)
		}
	}
	var sheet string
	for _, col := range m.FullHelp() {
		for _, b := range col {
			sheet += b.Help().Key + " "
		}
	}
	for _, want := range []string{"ctrl+d", "ctrl+end", "shift+↑/↓", "ctrl+y"} {
		if !strings.Contains(sheet, want) {
			t.Errorf("%s left the key sheet too: %s", want, sheet)
		}
	}
}

func TestExitQuits(t *testing.T) {
	m := newChatModel()
	m.input.SetValue("exit")
	// The exit word emits a QuitRequestMsg for the shell (which flushes
	// the session before quitting) rather than quitting directly.
	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on exact \"exit\" returned no command")
	}
	if _, ok := cmd().(QuitRequestMsg); !ok {
		t.Error("enter on exact \"exit\" did not request quit")
	}
}

func TestRecordLineTotalElapsed(t *testing.T) {
	m := newChatModel()
	// A finished 200 record whose own request took 800ms.
	id := m.state.BeginRecord(capture.Event{
		Kind: capture.KindRequest, Method: "POST", Path: "/p", Status: 200,
		Duration: 800 * time.Millisecond,
	})
	m.state.FinishBody(capture.Event{Kind: capture.KindBody, Duration: 800 * time.Millisecond})
	m.state.FinishRecord()
	rec, ok := m.state.RecordByID(id)
	if !ok {
		t.Fatal("record not found")
	}

	// No retries: total ≈ the request's own time ⇒ no "total" suffix.
	if got := ansi.Strip(m.recordLineFor(rec, 850*time.Millisecond)); strings.Contains(got, "total") {
		t.Errorf("recordLineFor with total≈request showed a total suffix: %q", got)
	}
	// Retried after a 429: total dwarfs the winning request ⇒ suffix shows,
	// in the shared elapsed format (render.Elapsed, not Duration.String).
	if got := ansi.Strip(m.recordLineFor(rec, 9*time.Second)); !strings.Contains(got, "total 9.00 s") {
		t.Errorf("recordLineFor after a retry = %q; want a \"total 9.00s\" suffix", got)
	}
}

// TestMetricsSkeleton pins the panel's shape: Request, Tokens and Pricing
// are always there, always the same rows, with a placeholder where a value
// is missing — so nothing you are reading moves when you click another
// record, and an absent number is visible as absent.
func TestMetricsSkeleton(t *testing.T) {
	m := newChatModel()
	m.cfg.Model = "m"

	rows := func(title string) map[string]string {
		for _, g := range m.metricsGroups() {
			if strings.HasPrefix(g.title, title) {
				out := map[string]string{}
				for _, kv := range g.rows {
					out[kv.Key] = ansi.Strip(kv.Value)
				}
				return out
			}
		}
		t.Fatalf("no %s group", title)
		return nil
	}
	titles := func() []string {
		var out []string
		for _, g := range m.metricsGroups() {
			out = append(out, g.title)
		}
		return out
	}

	// Empty session: the skeleton stands, filled with placeholders.
	if got := titles(); len(got) != 3 || got[0] != "Request" || got[1] != "Tokens" || got[2] != "Pricing" {
		t.Fatalf("groups on an empty session = %v; want the three fixed ones", got)
	}
	for _, g := range []string{"Request", "Tokens", "Pricing"} {
		for k, v := range rows(g) {
			if v != "–" {
				t.Errorf("%s[%q] = %q on an empty session; want the placeholder", g, k, v)
			}
		}
	}

	// A record with usage but no pricing for its model: counts land, the
	// money rows stay placeholders — the row is there either way.
	addRecord(m.state, 200, &provider.Usage{Prompt: 100_000, Completion: 50_000, Total: 150_000})
	tok, price := rows("Tokens"), rows("Pricing")
	if tok["in"] != "100000" || tok["total"] != "150000" {
		t.Errorf("Tokens = %v; want the exact counts", tok)
	}
	if tok["context"] != "–" || price["cost"] != "–" || price["session"] != "–" {
		t.Errorf("unpriced model: context/cost/session = %q/%q/%q; want placeholders",
			tok["context"], price["cost"], price["session"])
	}

	// With rates: 100k@$3/1M + 50k@$15/1M = 0.3 + 0.75 = $1.05; ctx 50%.
	m.SetPricing(pricing.Table{"m": {Input: 3, Output: 15, Context: 200_000}})
	tok, price = rows("Tokens"), rows("Pricing")
	if tok["context"] != "100k / 200k · 50%" {
		t.Errorf("context = %q; want the ratio", tok["context"])
	}
	if price["cost"] != "$1.05" || price["session"] != "$1.05" {
		t.Errorf("Pricing = %v; want cost and session at $1.05", price)
	}

	// The conditional groups come after the skeleton, never inside it.
	if got := titles(); len(got) < 3 || got[0] != "Request" || got[1] != "Tokens" || got[2] != "Pricing" {
		t.Errorf("groups = %v; want the fixed three first", got)
	}
}

// TestNoTotalSuffixOnMultiRoundSends pins that the record line stops
// repeating the exchange's wall-clock. Turn.Total spans every round, so on
// a tool loop the "total" suffix fired for a reason it was never about —
// and the Exchange group says it better, with the request count.
func TestNoTotalSuffixOnMultiRoundSends(t *testing.T) {
	m := newChatModel()
	addRecord(m.state, 200, nil)
	first, _ := m.state.LastRecord()
	call := m.state.AppendTurn(provider.RoleAssistant, "", store.Complete)
	m.state.Link(call, first.ID)
	round := m.state.BeginRecord(capture.Event{Kind: capture.KindRequest, Status: 200, Method: "POST", Path: "/p"})
	m.state.FinishBody(capture.Event{Kind: capture.KindBody})
	m.state.FinishRecord()
	tool := m.state.AppendToolResult("call_x", "get_weather", "17.3C")
	m.state.Link(tool, round)
	// The send took far longer than any one request — the loop, not backoff.
	m.state.SetTotal(0, 5*time.Second)

	m.renderChat(true)
	if got := ansi.Strip(strings.Join(m.chatLines, "\n")); strings.Contains(got, "total ") {
		t.Errorf("a multi-round send still carries a total suffix; the Exchange group owns that:\n%s", got)
	}
}

// TestExchangeSpansItsRounds pins what an exchange is for now that its
// totals are gone from the panel: the position on the Request title, and
// the rule covering every round of the question while the heavy weight
// stays on the one request the panel measures.
func TestExchangeSpansItsRounds(t *testing.T) {
	m := newChatModel()
	m.cfg.Model = "m"
	m.SetPricing(pricing.Table{"m": {Input: 3, Output: 15, Context: 200_000}})

	// One question, two rounds: the call, then the answer.
	addRecord(m.state, 200, &provider.Usage{Prompt: 100_000, Completion: 10_000, Total: 110_000})
	first, _ := m.state.LastRecord()
	call := m.state.AppendTurn(provider.RoleAssistant, "", store.Complete)
	m.state.Link(call, first.ID)

	round := m.state.BeginRecord(capture.Event{Kind: capture.KindRequest, Status: 200, Method: "POST", Path: "/p"})
	m.state.FinishBody(capture.Event{Kind: capture.KindBody})
	m.state.SetUsage(&provider.Usage{Prompt: 150_000, Completion: 20_000, Total: 170_000})
	m.state.FinishRecord()
	tool := m.state.AppendToolResult("call_x", "get_weather", "17.3C")
	m.state.Link(tool, round)

	titles := func() []string {
		var out []string
		for _, g := range m.metricsGroups() {
			out = append(out, g.title)
		}
		return out
	}
	// The panel counts the question on the Request title and nowhere else:
	// no Exchange group of its own.
	var reqTitle string
	for _, ti := range titles() {
		if strings.HasPrefix(ti, "Exchange") {
			t.Errorf("panel still carries an %q group", ti)
		}
		if strings.HasPrefix(ti, "Request") {
			reqTitle = ti
		}
	}
	if reqTitle != "Request (exchange 2 of 2)" {
		t.Errorf("Request title = %q; want the position within the question", reqTitle)
	}

	// The rule still covers the whole exchange, with the heavy weight on
	// the request the panel is measuring.
	m.renderChat(true)
	shown, _ := m.state.ShownRecord()
	marked, heavy := map[int]bool{}, map[int]bool{}
	for i, r := range m.lineRec {
		plain := ansi.Strip(m.chatLines[i])
		switch {
		case strings.HasPrefix(plain, gutterRule):
			heavy[r] = true
			marked[r] = true
		case strings.HasPrefix(plain, exchangeRule):
			marked[r] = true
		}
	}
	if !marked[first.ID] || !marked[round] {
		t.Errorf("marked records = %v; want both rounds of the exchange (%d, %d)", marked, first.ID, round)
	}
	for id := range heavy {
		if id != shown.ID {
			t.Errorf("record %d wears the heavy rule; only the shown request (%d) may", id, shown.ID)
		}
	}
}

// TestGutterHasNoStripes pins that the two weights read as blocks: a
// separator takes the weight of the row above it. Blanks are owned by the
// turn that wrote them — the exchange's first request — so weighing them
// by ownership drew heavy rules between the light rows of a tool round,
// striping one exchange like several.
func TestGutterHasNoStripes(t *testing.T) {
	m := newChatModel()
	// A question, its reply, a tool round of its own, then the answer.
	addRecord(m.state, 200, nil)
	first, _ := m.state.LastRecord()
	call := m.state.AppendTurn(provider.RoleAssistant, "", store.Complete)
	m.state.SetToolCalls(call, []provider.ToolCall{{ID: "c1", Name: "get_weather", Arguments: "{}"}})
	m.state.Link(call, first.ID)
	round := m.state.BeginRecord(capture.Event{Kind: capture.KindRequest, Status: 200, Method: "POST", Path: "/p"})
	m.state.FinishBody(capture.Event{Kind: capture.KindBody})
	m.state.FinishRecord()
	tool := m.state.AppendToolResult("c1", "get_weather", "17.3C")
	m.state.Link(tool, round)
	m.state.Link(m.state.AppendTurn(provider.RoleAssistant, "23.2°C", store.Complete), round)
	m.state.Pin(first.ID)
	m.renderChat(false)

	weight := func(i int) string {
		switch p := ansi.Strip(m.chatLines[i]); {
		case strings.HasPrefix(p, gutterRule):
			return "heavy"
		case strings.HasPrefix(p, exchangeRule):
			return "light"
		}
		return ""
	}
	// Interior separators only: the blank that closes the block is
	// deliberately unpainted, so the rule does not hang below it.
	for i, l := range m.chatLines {
		if i == 0 || i+1 >= len(m.chatLines) || strings.TrimSpace(ansi.Strip(l)) != "" {
			continue
		}
		above, below := weight(i-1), weight(i+1)
		if above == "" || below == "" {
			continue
		}
		if w := weight(i); w != above {
			t.Errorf("row %d is a separator weighed %q between %q rows; a block does not change weight at a blank",
				i, w, above)
		}
	}
	// And the striping this was written for: with a round pinned to another
	// record, the heavy rows must be one run, not scattered.
	var runs int
	prev := ""
	for i := range m.chatLines {
		if w := weight(i); w == "heavy" && prev != "heavy" {
			runs++
		} else if w != "" {
			prev = w
			continue
		}
		prev = weight(i)
	}
	if runs > 1 {
		t.Errorf("the heavy rule appears in %d separate runs; want one block", runs)
	}
}

// TestSelectedRecordGroupIsRuled pins how the Metrics panel's current
// record is shown: a rule down the left of its whole group — the question,
// the reply, the summary row — not a mark on the summary row alone. A
// record is an exchange, so the mark has to have the shape of one.
func TestSelectedRecordGroupIsRuled(t *testing.T) {
	m := newChatModel()
	// Whole exchanges, question and reply: a record is written as more than
	// one block, and the rule has to cross the separator between them.
	addExchange(m.state, 200)
	addExchange(m.state, 200)
	m.renderChat(true)

	// Every line of text in the group wears the rule. The group's trailing
	// separator is owned (it is a click target) but not painted — a rule
	// hanging below the block would look like it marks what comes next.
	body := func(i int) bool { return strings.TrimSpace(ansi.Strip(m.chatLines[i])) != "" }
	ruled := func(recID int) bool {
		found, all := false, true
		for i, r := range m.lineRec {
			if r != recID || !body(i) {
				continue
			}
			found = true
			if !strings.HasPrefix(ansi.Strip(m.chatLines[i]), gutterRule) {
				all = false
			}
		}
		if !found {
			t.Fatalf("no chat line owned by record %d", recID)
		}
		return all
	}
	// The rule is unbroken: the blank rows a turn leaves inside the group
	// carry it too, so one exchange reads as one block.
	unbroken := func() bool {
		first, last := -1, -1
		for i := range m.chatLines {
			if strings.HasPrefix(ansi.Strip(m.chatLines[i]), gutterRule) {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		for i := first; i >= 0 && i <= last; i++ {
			if !strings.HasPrefix(ansi.Strip(m.chatLines[i]), gutterRule) {
				return false
			}
		}
		return first >= 0
	}
	// The chip is gone: no record line advertises a label.
	if strings.Contains(ansi.Strip(strings.Join(m.chatLines, "\n")), "metrics") {
		t.Error("record lines still carry a metrics chip; the rule replaced it")
	}
	// Lines of another record keep the blank gutter, so the mark names one
	// exchange and nothing shifts as it moves.
	for i, r := range m.lineRec {
		if r >= 0 && r != 1 && strings.HasPrefix(ansi.Strip(m.chatLines[i]), gutterRule) {
			t.Errorf("line %d belongs to record %d but wears the shown record's rule", i, r)
		}
	}
	if !unbroken() {
		t.Error("the group's rule is broken by a separator row; one exchange must read as one block")
	}
	// The rule ends with the group's text, not on the blank row after it.
	for i := len(m.chatLines) - 1; i >= 0; i-- {
		if strings.HasPrefix(ansi.Strip(m.chatLines[i]), gutterRule) {
			if !body(i) {
				t.Errorf("the rule's last row (%d) is blank; it must end with the group's text", i)
			}
			break
		}
	}
	// The exchange is the click target, not just the summary row: the user's
	// question is owned by the record it triggered.
	owned := 0
	for _, r := range m.lineRec {
		if r == 1 {
			owned++
		}
	}
	if owned < 2 {
		t.Errorf("record 1 owns %d lines; want its question and summary at least", owned)
	}

	// Following the latest: the newest record's group wears the rule.
	if !ruled(1) || ruled(0) {
		t.Fatal("while following, the latest record's group should be ruled")
	}

	// Pin record 0: the band moves to it.
	line0 := -1
	for i, r := range m.lineRec {
		if r == 0 {
			line0 = i
			break
		}
	}
	m.pinFromClick(line0)
	m.renderChat(false)
	if !ruled(0) || ruled(1) {
		t.Error("after pinning record 0 the rule should move to its group")
	}
}

// TestClickPinning pins what a transcript click does: a line owned by no
// record leaves the panel where it was (a click at nothing is not a
// command), a separator selects the exchange it trails, and clicking the
// pinned group again releases it back to following the latest.
func TestClickPinning(t *testing.T) {
	m := newChatModel()
	addRecord(m.state, 200, nil)
	addRecord(m.state, 200, nil)
	m.renderChat(true)

	// A separator inside record 0's block: the blank row it leaves behind.
	sep, lastOf0 := -1, -1
	for i, r := range m.lineRec {
		if r == 0 {
			lastOf0 = i
			if strings.TrimSpace(ansi.Strip(m.chatLines[i])) == "" {
				sep = i
			}
		}
	}
	if sep < 0 {
		t.Fatalf("record 0 owns no separator row (last line %d)", lastOf0)
	}
	m.pinFromClick(sep)
	if got := m.state.Pinned(); got != 0 {
		t.Errorf("click on record 0's separator pinned %d; want 0 — the exchange it trails", got)
	}

	// A line owned by nothing: the pin stays put.
	for i, r := range m.lineRec {
		if r == -1 {
			m.pinFromClick(i)
			if got := m.state.Pinned(); got != 0 {
				t.Errorf("click on unowned line %d moved the pin to %d; want it left at 0", i, got)
			}
			break
		}
	}

	// Clicking the group that is already pinned keeps it: releasing here
	// would jump the panel to the newest record, which is not what clicking
	// the block you are reading should do.
	m.pinFromClick(sep)
	if got := m.state.Pinned(); got != 0 {
		t.Errorf("second click on the pinned group left pin = %d; want it still 0", got)
	}
}

func TestTokensGraphClick(t *testing.T) {
	m := newChatModel()
	addRecord(m.state, 200, &provider.Usage{Prompt: 100, Completion: 10, Total: 110})
	addRecord(m.state, 200, &provider.Usage{Prompt: 200, Completion: 10, Total: 210})
	m.viewTokens() // caches bar→record mapping + geometry

	// Pair 0 is the newest record (id 1); its out column (one cell right)
	// belongs to the same record.
	m.handleMouse(tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      m.tokenBarX0, Y: m.chatTop() + 3,
	})
	if m.state.Pinned() != 1 {
		t.Fatalf("pinned after pair-0 click = %d; want 1 (newest)", m.state.Pinned())
	}
	m.state.Pin(-1)
	m.handleMouse(tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      m.tokenBarX0 + tokenBarWidth, Y: m.chatTop() + 3,
	})
	if m.state.Pinned() != 1 {
		t.Fatalf("pinned after out-column click = %d; want 1 (same pair)", m.state.Pinned())
	}

	// Pair 1 (one pitch right) is the older record.
	pitch := tokenPairWidth + tokenBarGap
	m.handleMouse(tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      m.tokenBarX0 + pitch, Y: m.chatTop() + 3,
	})
	if m.state.Pinned() != 0 {
		t.Fatalf("pinned after bar-1 click = %d; want 0", m.state.Pinned())
	}

	// The gap column between pairs misses.
	if _, ok := m.tokenBarAt(m.tokenBarX0+tokenPairWidth, m.chatTop()+3); ok {
		t.Error("gap column resolved to a pair; want miss")
	}
}

func TestJSONReplyRendering(t *testing.T) {
	m := newChatModel()
	m.state.AppendTurn(provider.RoleAssistant, `{"a":1,"b":2}`, store.Complete)
	m.renderChat(true)

	plain := ansi.Strip(strings.Join(m.chatLines, "\n"))
	if !strings.Contains(plain, `"a": 1`) || !strings.Contains(plain, `"b": 2`) {
		t.Errorf("JSON reply not pretty-printed across lines:\n%s", plain)
	}

	// Long JSON string values wrap to the chat column instead of overflowing.
	long := `{"message":"` + strings.Repeat("<p>How can I help with the weather today?</p> ", 8) + `"}`
	m.state.AppendTurn(provider.RoleAssistant, long, store.Complete)
	m.renderChat(true)
	for i, l := range m.chatLines {
		if w := lipgloss.Width(l); w > m.chatView.Width() {
			t.Fatalf("chatLines[%d] width %d exceeds chat width %d", i, w, m.chatView.Width())
		}
	}

	// A prose reply stays a single wrapped block (no JSON indentation).
	m2 := newChatModel()
	m2.state.AppendTurn(provider.RoleAssistant, "just some prose", store.Complete)
	m2.renderChat(true)
	if strings.Contains(ansi.Strip(strings.Join(m2.chatLines, "\n")), `"`) {
		t.Error("prose reply unexpectedly JSON-formatted")
	}
}

func TestPinnedRecordSelectsMetrics(t *testing.T) {
	m := newChatModel()
	addRecord(m.state, 200, nil)
	addRecord(m.state, 403, nil)
	m.renderChat(true)

	if rec, ok := m.state.ShownRecord(); !ok || rec.ID != 1 {
		t.Fatalf("ShownRecord unpinned = %+v,%v; want the newest (id 1)", rec, ok)
	}

	find := func(want int) int {
		for i, r := range m.lineRec {
			if r == want {
				return i
			}
		}
		return -1
	}
	line0 := find(0)
	if line0 < 0 {
		t.Fatal("no chat line owned by record 0")
	}
	m.pinFromClick(line0)
	if m.state.Pinned() != 0 {
		t.Fatalf("pinned after clicking record 0 = %d; want 0", m.state.Pinned())
	}
	if rec, _ := m.state.ShownRecord(); rec.ID != 0 {
		t.Errorf("ShownRecord pinned = id %d; want 0", rec.ID)
	}

	// Re-clicking the pinned group is a no-op, and so is a click on nothing
	// (see TestClickPinning): neither may drag the panel to another record.
	m.pinFromClick(line0)
	if m.state.Pinned() != 0 {
		t.Errorf("pinned after re-clicking the pinned group = %d; want it still 0", m.state.Pinned())
	}
}

func TestTokensPane(t *testing.T) {
	m := newChatModel()
	m.SetSize(3, 160, 36) // room for the full legend on the title row
	addRecord(m.state, 200, &provider.Usage{Prompt: 25, Completion: 23, Total: 48})

	if m.tokensBlock() == 0 {
		t.Fatal("tokensBlock() = 0 on a 120x40 model; want visible")
	}
	// Legend names the bar segments (no values); the scale shows as its rounded
	// top value and half (total 48 → 50/25).
	pane := m.viewTokens()
	for _, want := range []string{"Tokens", "in", "out", "50", "25"} {
		if !strings.Contains(pane, want) {
			t.Errorf("viewTokens() missing %q", want)
		}
	}

	// Too short a column hides the pane.
	m.SetSize(3, 160, 8)
	if m.tokensBlock() != 0 {
		t.Errorf("tokensBlock() = %d on a tiny terminal; want 0", m.tokensBlock())
	}
}

func TestPendingTurnReservesRecordRow(t *testing.T) {
	m := newChatModel()
	turn := m.state.AppendTurn(provider.RoleUser, "q", store.Complete)
	// The tab pulls progress from state each render; here we set the snapshot the
	// pending row derives from and re-render the transcript directly.
	m.prog = state.Progress{Streaming: true, StreamTurn: -1, PendingTurn: turn}
	m.renderChat(true)

	found := false
	for _, l := range m.chatLines {
		if strings.Contains(l, "⎿") {
			found = true
			break
		}
	}
	if !found {
		t.Error("renderChat with a pending turn and no record reserved no record row")
	}
}

func TestCompactLimits(t *testing.T) {
	in := []capture.KeyValue{
		{Key: "requests left", Value: "2499"},
		{Key: "requests max", Value: "2500"},
		{Key: "tokens left", Value: "10000"},
		{Key: "tokens max", Value: "2500000"},
		{Key: "consumed", Value: "16"},
	}
	got := compactLimits(in)
	want := []capture.KeyValue{
		{Key: "requests", Value: "2499 / 2500"},
		{Key: "tokens", Value: "10000 / 2500000"},
		{Key: "consumed", Value: "16"},
	}
	if len(got) != len(want) {
		t.Fatalf("compactLimits() = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("compactLimits()[%d] = %v; want %v", i, got[i], want[i])
		}
	}
}

func TestClickInputPositionsCursor(t *testing.T) {
	m := newChatModel()
	m.input.SetValue("first\nsecond\nthird") // cursor lands at the end of "third"

	// The transcript's last row is chatTop()+Height()-1, so chatTop()+Height()
	// is the input box's top border and the next row is its first line. (This
	// read +2 while the transcript still carried a scroll-percent footer; the
	// footer became a scrollbar column, and both the code and this test kept
	// the stale offset until the cursor was found sitting on the box border.)
	top := m.chatTop() + m.chatView.Height() + 1
	if !m.clickInput(1+2+3, top+1) { // row 1 col 3: "sec|ond"
		t.Fatal("clickInput inside the input box = false; want handled")
	}
	if m.input.Line() != 1 {
		t.Errorf("line after click = %d; want 1", m.input.Line())
	}
	if got := m.input.Column(); got != 3 {
		t.Errorf("column after click = %d; want 3", got)
	}

	if m.clickInput(1, top-2) {
		t.Error("clickInput above the box = true; want false")
	}
}

// TestToolCallAlwaysShownPayloadFolds pins the tool group's shape: the
// call the model made and the request it cost always show — they are what
// happened — while a long payload stays behind its own ⎿ header until
// asked for.
func TestToolCallAlwaysShownPayloadFolds(t *testing.T) {
	m := newChatModel()
	call := m.state.AppendTurn(provider.RoleAssistant, "", store.Complete)
	m.state.SetToolCalls(call, []provider.ToolCall{
		{ID: "call_1", Name: "search_locations", Arguments: `{"text":"Paris"}`},
	})
	long := `[{"id":"PAR","name":"` + strings.Repeat("x", 200) + `"}]`
	id := m.state.AppendToolResult("call_1", "search_locations", long)
	m.renderChat(false)

	joined := ansi.Strip(strings.Join(m.chatLines, "\n"))
	// The call line names the call and nothing else: no fold marker, no
	// instruction to click.
	if !strings.Contains(joined, `search_locations({"text":"Paris"})`) {
		t.Errorf("transcript = %q; want the call line", joined)
	}
	// The instruction lives on the payload header — the one thing that
	// folds — and nowhere else: the call line does not fold, so an
	// invitation to click it would be a lie.
	for _, l := range m.chatLines {
		plain := ansi.Strip(l)
		if strings.Contains(plain, "search_locations({") && strings.Contains(plain, "click to") {
			t.Errorf("the call line invites a click it cannot honour: %q", plain)
		}
	}
	if !strings.Contains(joined, "click to expand") {
		t.Error("the folded payload gives no hint that it can be opened")
	}
	// The payload is folded behind its ⎿ header, which carries the summary.
	if !strings.Contains(joined, "▸ 1 items") {
		t.Errorf("transcript = %q; want the folded payload's summary", joined)
	}
	if strings.Contains(joined, "PAR") {
		t.Error("transcript shows the payload unasked; want it behind its header")
	}
	found := false
	for _, tid := range m.lineResult {
		if tid == id {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no lineResult entry for the ⎿ payload header; its click has no target")
	}

	m.resultOpen[id] = true
	m.renderChat(false)
	joined = ansi.Strip(strings.Join(m.chatLines, "\n"))
	if !strings.Contains(joined, "PAR") {
		t.Error("transcript hides the payload with the result opened; want it shown")
	}
	if !strings.Contains(joined, "▾") {
		t.Error("opened payload keeps the folded marker; want ▾")
	}
}

// TestPayloadControlIsItsWords pins where the fold lives: on the words
// that say it, not on the row they sit in. The rest of that row is text —
// the tool's name, the size — and text is for reading and selecting.
func TestPayloadControlIsItsWords(t *testing.T) {
	m := newChatModel()
	call := m.state.AppendTurn(provider.RoleAssistant, "", store.Complete)
	m.state.SetToolCalls(call, []provider.ToolCall{
		{ID: "call_1", Name: "get_weather", Arguments: `{"latitude":48.85}`},
	})
	long := `[{"id":"PAR","name":"` + strings.Repeat("x", 200) + `"}]`
	id := m.state.AppendToolResult("call_1", "get_weather", long)
	m.renderChat(false)

	var row, wordsCol int
	found := false
	for i, l := range m.chatLines {
		if at := strings.Index(ansi.Strip(l), hintExpand); at >= 0 {
			row, wordsCol, found = i, ansi.StringWidth(ansi.Strip(l)[:at]), true
			break
		}
	}
	if !found {
		t.Fatal("no payload control in the transcript")
	}

	// On the words: the control.
	if got := m.resultAt(selection.Pos{Line: row, Col: wordsCol}); got != id {
		t.Errorf("a click on the words resolved to %d; want the result %d", got, id)
	}
	// Left of them — the tool's name, its size — is not.
	if got := m.resultAt(selection.Pos{Line: row, Col: max(0, wordsCol-4)}); got >= 0 {
		t.Error("the row left of the words still answers as the control")
	}
	// Nor is the row's far end.
	if got := m.resultAt(selection.Pos{Line: row, Col: wordsCol + 500}); got >= 0 {
		t.Error("the row past the words still answers as the control")
	}
}

func TestShortToolResultInlineOnCallLine(t *testing.T) {
	m := newChatModel()
	call := m.state.AppendTurn(provider.RoleAssistant, "", store.Complete)
	m.state.SetToolCalls(call, []provider.ToolCall{
		{ID: "call_2", Name: "get_time", Arguments: ""},
	})
	m.state.AppendToolResult("call_2", "get_time", `{"now":"21:05"}`)
	m.renderChat(false)

	// A result short enough to read at a glance sits on its own ⎿ line with
	// nothing to fold.
	joined := ansi.Strip(strings.Join(m.chatLines, "\n"))
	if !strings.Contains(joined, `get_time → {"now":"21:05"}`) {
		t.Errorf("transcript = %q; want the short result shown outright", joined)
	}
	for _, tid := range m.lineResult {
		if tid >= 0 {
			t.Error("a short result registered a fold target; there is nothing to fold")
			break
		}
	}
}

func TestUnansweredToolCallRendersPlain(t *testing.T) {
	m := newChatModel()
	call := m.state.AppendTurn(provider.RoleAssistant, "", store.Complete)
	m.state.SetToolCalls(call, []provider.ToolCall{
		{ID: "call_3", Name: "inspect_only", Arguments: `{"a":1}`},
	})
	m.renderChat(false)

	joined := ansi.Strip(strings.Join(m.chatLines, "\n"))
	if !strings.Contains(joined, `inspect_only({"a":1})`) {
		t.Errorf("transcript = %q; want the bare call line", joined)
	}
	for _, tid := range m.lineResult {
		if tid >= 0 {
			t.Error("resultless call registered a fold target; want none")
			break
		}
	}
}

// TestStartersSeedHistory pins the starter-prompts seeding: starters
// sit before the session's sent messages in the up/down walk, stay
// browsable after sends, and a re-seed (profile switch / file reload)
// resets the browse position without touching sent history.
func TestStartersSeedHistory(t *testing.T) {
	m := newChatModel()
	m.SetStarters([]preset.Starter{{Title: "one", Text: "preset one"}, {Title: "two", Text: "preset two"}})

	if !m.histPrev() || m.input.Value() != "preset two" {
		t.Fatalf("first prev = %q; want the newest preset", m.input.Value())
	}
	if !m.histPrev() || m.input.Value() != "preset one" {
		t.Fatalf("second prev = %q; want the oldest preset", m.input.Value())
	}
	if m.histPrev() {
		t.Error("histPrev() past oldest preset = true; want false")
	}

	// Send something: the walk now covers preset + sent, newest first.
	m.input.SetValue("typed message")
	m.histIdx = m.histLen()
	if cmd := m.send(); cmd == nil {
		t.Fatal("send() = nil cmd")
	}
	if !m.histPrev() || m.input.Value() != "typed message" {
		t.Fatalf("prev after send = %q; want the sent message", m.input.Value())
	}
	if !m.histPrev() || m.input.Value() != "preset two" {
		t.Fatalf("second prev after send = %q; want presets still reachable", m.input.Value())
	}

	// Re-seed (hot reload): position resets, sent history survives.
	m.SetStarters([]preset.Starter{{Title: "fresh", Text: "fresh preset"}})
	if m.histIdx != m.histLen() {
		t.Errorf("histIdx = %d after re-seed; want %d (draft position)", m.histIdx, m.histLen())
	}
	if !m.histPrev() || m.input.Value() != "typed message" {
		t.Fatalf("prev after re-seed = %q; want sent history kept", m.input.Value())
	}
	if !m.histPrev() || m.input.Value() != "fresh preset" {
		t.Fatalf("second prev after re-seed = %q; want the new preset", m.input.Value())
	}
}

// TestComposerHeightSyncs pins the column re-carve: the input's height is
// dynamic, and every path that replaces the composer's contents wholesale
// — sending, history recall, a starter pick — must re-carve the column.
// Update's generic edit path syncs afterwards, but these return before
// reaching it, leaving the transcript sized for a box that shrank.
func TestComposerHeightSyncs(t *testing.T) {
	m := newChatModel()
	m.SetSize(0, 100, 30)
	oneLine := m.chatView.Height()

	// A multi-line draft steals rows from the transcript.
	m.input.SetValue("one\ntwo\nthree")
	m.syncInputHeight(1)
	grown := m.chatView.Height()
	if grown >= oneLine {
		t.Fatalf("transcript height = %d with a 3-line draft; want less than %d", grown, oneLine)
	}

	// Sending collapses the box: the rows must come back.
	m.send()
	if got := m.chatView.Height(); got != oneLine {
		t.Errorf("after send transcript height = %d; want %d (the rows the shrunken box gave back)", got, oneLine)
	}

	// Recalling that multi-line entry grows it again, and stepping past it
	// back to the empty draft shrinks it back.
	if !m.histPrev() {
		t.Fatal("histPrev() = false; want the just-sent entry recalled")
	}
	if got := m.chatView.Height(); got != grown {
		t.Errorf("after history recall transcript height = %d; want %d", got, grown)
	}
	if !m.histNext() {
		t.Fatal("histNext() = false; want the draft restored")
	}
	if got := m.chatView.Height(); got != oneLine {
		t.Errorf("after leaving history transcript height = %d; want %d", got, oneLine)
	}
}

// TestNoticeLeavesTheScrollbarAlone pins where a notice stops: beside the
// rail, never on it. The bar says where the reader is in what they are
// reading, and covering that to say "copied" trades a fact for a
// confirmation that lapses in four seconds.
func TestNoticeLeavesTheScrollbarAlone(t *testing.T) {
	m := newChatModel()
	for i := range 60 { // more than fits, so the bar is drawn
		m.state.AppendTurn(provider.RoleUser, fmt.Sprintf("message %d", i), store.Complete)
	}
	m.refresh()
	m.note.Show("copied to clipboard")

	var row string
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(ansi.Strip(line), "copied to clipboard") {
			row = ansi.Strip(line)
			break
		}
	}
	if row == "" {
		t.Fatal("the notice is not on screen")
	}
	// The chat column's last cell is the rail; the notice ends before it.
	col := []rune(row)[m.chatColWidth()-1]
	if col == ' ' {
		t.Errorf("the notice covered the scrollbar rail: %q", row)
	}
}
