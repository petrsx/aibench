// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package inspector

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/state"
	"github.com/petrsx/aibench/internal/store"
	"github.com/petrsx/aibench/internal/tui/render"
)

func TestInspectorSubViews(t *testing.T) {
	st := state.New(store.New(500), nil, make(chan capture.Event, 1), spinner.New())
	turn := st.AppendTurn(provider.RoleUser, "q", store.Complete)
	id := st.BeginRecord(capture.Event{
		Kind: capture.KindRequest, Status: 403, Method: "POST",
		Path: "/p", Query: "api-version=1",
		ReqHeaders:  []capture.KeyValue{{Key: "Accept", Value: "application/json"}},
		RespHeaders: []capture.KeyValue{{Key: "Content-Type", Value: "application/json"}},
		ReqBody:     `{"messages":[]}`,
	})
	st.Link(turn, id)
	st.FinishBody(capture.Event{Kind: capture.KindBody, Body: `{"statusCode":403}`, BodyBytes: 18})
	reply := st.AppendTurn(provider.RoleAssistant, "Hello there", store.Complete) // assembled reply
	st.Link(reply, id)

	m := New(st)
	m.SetSize(3, 120, 36) // size the panes (record frame wraps to them)
	rec, _ := st.LastRecord()

	m.view = viewRequest
	req := strings.Join(m.rawLinesFor(rec), "\n")
	reqHdr := strings.Join(m.headerLinesFor(rec), "\n")
	if !strings.Contains(req, `"messages"`) {
		t.Errorf("request view missing the sent body:\n%s", req)
	}
	if head := m.viewHead(""); !strings.Contains(head, "api-version=1") {
		t.Errorf("head strip missing the URL:\n%s", head)
	}
	if !strings.Contains(reqHdr, "Accept") {
		t.Errorf("request headers panel missing Accept:\n%s", reqHdr)
	}
	for _, leak := range []string{"Content-Type", "Hello there"} {
		if strings.Contains(req+reqHdr, leak) {
			t.Errorf("request view leaks %q", leak)
		}
	}

	m.view = viewResponse
	resp := strings.Join(m.rawLinesFor(rec), "\n")
	respHdr := strings.Join(m.headerLinesFor(rec), "\n")
	for _, want := range []string{"statusCode", "403"} {
		if !strings.Contains(resp, want) {
			t.Errorf("response view missing %q:\n%s", want, resp)
		}
	}
	// Headers panel: received headers only — the assembled reply now lives in
	// the response body, not duplicated here.
	if !strings.Contains(respHdr, "Content-Type") {
		t.Errorf("response headers panel missing Content-Type:\n%s", respHdr)
	}
	for _, gone := range []string{"assembled message", "Hello there"} {
		if strings.Contains(respHdr, gone) {
			t.Errorf("response headers panel still shows %q:\n%s", gone, respHdr)
		}
	}
	for _, leak := range []string{"Accept", `"messages"`} {
		if strings.Contains(resp+respHdr, leak) {
			t.Errorf("response view leaks %q", leak)
		}
	}

	// tab flips the sub-view (wraps back to request from response).
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.view != viewRequest {
		t.Errorf("tab key left view = %d; want request", m.view)
	}

	// Transport failures show the condensed error in both views.
	st.FailRecord("dial tcp: connection refused")
	failed, _ := st.LastRecord()
	for _, view := range []int{viewRequest, viewResponse} {
		m.view = view
		if got := strings.Join(m.rawLinesFor(failed), "\n"); !strings.Contains(got, "connection refused") {
			t.Errorf("view %d missing transport error: %q", view, got)
		}
	}
}

// TestRequestSizeIsItsOwnPane pins where the per-message breakdown lives:
// its own scrollable side pane, not the body dump. It is a summary *about*
// the request, so the dump stays a faithful view of the bytes, and the
// breakdown grows with every turn without pushing the headers out of reach.
// It stays request-only — the response view has no request content at all.
func TestRequestSizeIsItsOwnPane(t *testing.T) {
	st := state.New(store.New(500), nil, make(chan capture.Event, 1), spinner.New())
	reqBody := `{"messages":[` +
		`{"role":"system","content":"be brief"},` +
		`{"role":"user","content":"q"},` +
		`{"role":"tool","content":"` + strings.Repeat("x", 500) + `"}]}`
	st.BeginRecord(capture.Event{
		Kind: capture.KindRequest, Status: 403, Method: "POST", Path: "/p",
		ReqBody: reqBody, ReqBytes: int64(len(reqBody)),
	})
	st.FailCurrent("403 Forbidden: Request failed content safety check.")

	m := New(st)
	m.SetSize(3, 120, 36)
	rec, _ := st.LastRecord()

	// The breakdown: one row per message, so a gateway rejection on size
	// is diagnosable.
	title, rows := requestBreakdown(rec, 28)
	// The header answers "how big, of what" before anything is scrolled.
	for _, want := range []string{"3 msg", "B"} {
		if !strings.Contains(title, want) {
			t.Errorf("pane title = %q; want %q in it", title, want)
		}
	}
	sizes := strings.Join(rows, "\n")
	for _, want := range []string{"envelope", "tool", "B"} {
		if !strings.Contains(sizes, want) {
			t.Errorf("breakdown missing %q:\n%s", want, sizes)
		}
	}

	// The dump carries the body and none of the summary.
	m.view = viewRequest
	req := strings.Join(m.rawLinesFor(rec), "\n")
	if !strings.Contains(req, `"messages"`) {
		t.Errorf("request dump missing the sent body:\n%s", req)
	}
	for _, moved := range []string{"3 msg", "envelope"} {
		if strings.Contains(req, moved) {
			t.Errorf("request dump still carries %q; it belongs to the side pane:\n%s", moved, req)
		}
	}

	// Refresh fills the pane on the request view and empties it on the
	// response view, where request content must not appear at all.
	m.Refresh()
	if len(m.sizesLines) == 0 {
		t.Error("request view: the Request size pane is empty")
	}
	m.view = viewResponse
	m.Refresh()
	if len(m.sizesLines) != 0 {
		t.Errorf("response view: the Request size pane must be empty, got:\n%s", strings.Join(m.sizesLines, "\n"))
	}
	resp := strings.Join(m.rawLinesFor(rec), "\n")
	if !strings.Contains(resp, "content safety check") {
		t.Errorf("response view missing the condensed error:\n%s", resp)
	}
	for _, leak := range []string{`"messages"`, "be brief"} {
		if strings.Contains(resp, leak) {
			t.Errorf("response view leaks request content %q:\n%s", leak, resp)
		}
	}
}

// seedRequest puts one record with a turn list in the store, so the
// Request size pane has a breakdown to show (it folds away without one).
func seedRequest(t *testing.T, st *state.State) {
	t.Helper()
	body := `{"messages":[{"role":"system","content":"be brief"},{"role":"user","content":"q"}]}`
	st.BeginRecord(capture.Event{
		Kind: capture.KindRequest, Method: "POST", Path: "/p",
		ReqBody: body, ReqBytes: int64(len(body)),
	})
}

// TestRequestSizePaneFoldsWhenShort pins the split's escape hatch: on a
// short terminal the column is not carved into two unusable halves — the
// sizes pane folds away and Headers takes the height back.
// TestCopyControls pins what each control hands over. The side panels give
// their own rows; the dump gives the body as it arrived, not the pane as
// drawn — the pane is wrapped and hanging-indented for reading, and
// pasting that into jq would paste the reading aid too.
func TestCopyControls(t *testing.T) {
	st := state.New(store.New(500), nil, make(chan capture.Event, 1), spinner.New())
	seedRequest(t, st)
	st.FinishBody(capture.Event{Kind: capture.KindBody, Body: `{"ok":true}`})
	m := New(st)
	m.SetSize(3, 60, 36) // narrow, so the body certainly wraps in the pane
	m.Refresh()

	// The dump has no control of its own: ctrl+y takes the body, whole
	// and as it arrived, and the help line says so. What it hands over is
	// not the pane as drawn — that is wrapped and hanging-indented for
	// reading, and pasting it into jq would paste the reading aid too.
	rec, _ := st.ShownRecord()
	if got := m.shownBody(); got != rec.Req.ReqBody {
		t.Errorf("ctrl+y would hand over %q; want the body as sent (%q)", got, rec.Req.ReqBody)
	}
	if cmd := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}); cmd == nil {
		t.Error("ctrl+y copied nothing")
	}

	// A side panel: its own rows, from the control in its title corner.
	sideLeft := m.width - m.metricsWidth()
	panelCell := render.PanelControlCells(sideLeft, m.top, m.metricsWidth(),
		render.CopyLabel)[0]
	lines, notice, ok := m.copyAt(panelCell.X, panelCell.Y)
	if !ok {
		t.Fatal("no copy control on the Headers panel")
	}
	if len(lines) != len(m.headersLines) {
		t.Errorf("headers control handed over %d lines; want the panel's %d", len(lines), len(m.headersLines))
	}
	if !strings.Contains(notice, "headers") {
		t.Errorf("notice %q does not name what was copied", notice)
	}

	// Elsewhere on those rows is not a control.
	if _, _, ok := m.copyAt(2, m.top); ok {
		t.Error("the header box's left edge answers as a copy control")
	}
}

// TestFormatToggle pins the pane's one reading choice: ctrl+r flips it,
// the label names what you are looking at,
// and the choice holds while you move between records — it says how you
// want to read, not a fact about one body.
func TestFormatToggle(t *testing.T) {
	st := state.New(store.New(500), nil, make(chan capture.Event, 1), spinner.New())
	body := `{"input":[{"arguments":"{\"latitude\":48.85341}"}]}`
	st.BeginRecord(capture.Event{
		Kind: capture.KindRequest, Method: "POST", Path: "/p", Status: 200,
		ReqBody: body, ReqBytes: int64(len(body)),
	})
	st.FinishBody(capture.Event{Kind: capture.KindBody})
	m := New(st)
	m.SetSize(3, 120, 36)
	m.Refresh()

	dump := func() string { return ansi.Strip(strings.Join(m.dumpLines, "\n")) }
	if !strings.Contains(dump(), `\"latitude\"`) || m.formatLabel() != "raw" {
		t.Fatalf("the pane does not open on the wire form (label %q):\n%s", m.formatLabel(), dump())
	}

	// The key.
	m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if m.formatLabel() != "render" || !strings.Contains(dump(), `"latitude": 48.85341`) {
		t.Errorf("ctrl+r did not switch the body to its decoding (label %q):\n%s", m.formatLabel(), dump())
	}

	// It holds across records: another request does not undo how you read.
	st.BeginRecord(capture.Event{Kind: capture.KindRequest, Method: "POST", Path: "/q", Status: 200, ReqBody: body})
	st.FinishBody(capture.Event{Kind: capture.KindBody})
	m.Refresh()
	if m.formatLabel() != "render" {
		t.Error("moving to another record reset how the pane reads bodies")
	}

	// And back again with the key: the pane has no button for it, the
	// help line and the tips name the chord instead.
	m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if m.formatLabel() != "raw" {
		t.Errorf("ctrl+r did not switch back (label %q)", m.formatLabel())
	}
}

// TestStepBetweenRecords pins the walk: ctrl+← / ctrl+→ move the shown
// record one at a time and stop at the ends, so the Inspector can be
// worked without going to another tab to click a row.
func TestStepBetweenRecords(t *testing.T) {
	st := state.New(store.New(500), nil, make(chan capture.Event, 1), spinner.New())
	for range 3 {
		st.BeginRecord(capture.Event{Kind: capture.KindRequest, Method: "POST", Path: "/p", Status: 200})
		st.FinishBody(capture.Event{Kind: capture.KindBody})
		st.FinishRecord()
	}
	m := New(st)
	m.SetSize(3, 120, 36)
	m.Refresh()

	recs := st.Records()
	shown := func() int {
		r, _ := st.ShownRecord()
		return r.ID
	}
	// Following the latest to begin with.
	if shown() != recs[2].ID {
		t.Fatalf("opens on record %d; want the latest (%d)", shown(), recs[2].ID)
	}
	up := tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift}
	down := tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}
	m.Update(up)
	if shown() != recs[1].ID {
		t.Errorf("shift+↑ showed %d; want the one before the latest (%d)", shown(), recs[1].ID)
	}
	m.Update(up)
	m.Update(up) // past the start
	if shown() != recs[0].ID {
		t.Errorf("stepping past the start showed %d; want it held at the first (%d)", shown(), recs[0].ID)
	}
	m.Update(down)
	if shown() != recs[1].ID {
		t.Errorf("shift+↓ showed %d; want the next (%d)", shown(), recs[1].ID)
	}
	// ctrl+end lets the pin go, so both tabs follow the newest again.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	if st.Pinned() != -1 {
		t.Errorf("pinned after ctrl+end = %d; want -1 (following)", st.Pinned())
	}
}

func TestRequestSizePaneFoldsWhenShort(t *testing.T) {
	st := state.New(store.New(500), nil, make(chan capture.Event, 1), spinner.New())
	seedRequest(t, st)
	m := New(st)
	m.SetSize(3, 120, 36)
	if m.sizes.Height() <= 0 {
		t.Fatal("tall terminal: the Request size pane should be showing")
	}

	m.SetSize(3, 120, 12)
	if m.sizes.Height() != 0 {
		t.Errorf("short terminal: sizes pane height = %d; want 0 (folded)", m.sizes.Height())
	}
	// The column less the panel's chrome: two border rows + the title rows.
	if want := 12 - 2 - render.SidePanelTitleRows; m.headers.Height() != want {
		t.Errorf("short terminal: Headers height = %d; want %d (the whole column)", m.headers.Height(), want)
	}
}

// TestRequestSizePaneIsRequestOnly pins that the pane is not merely empty on
// the response sub-view but absent: an empty box titled for content that
// cannot appear there is worse than no box, and Headers gets the rows back.
func TestRequestSizePaneIsRequestOnly(t *testing.T) {
	st := state.New(store.New(500), nil, make(chan capture.Event, 1), spinner.New())
	seedRequest(t, st)
	m := New(st)
	m.SetSize(3, 120, 36)

	// The tab opens on the request view — what went up is what you came to
	// read — so the pane is showing from the start.
	if m.sizes.Height() == 0 {
		t.Fatal("request view: the Request size pane should be showing")
	}
	split := m.headers.Height()

	m.view = viewResponse
	m.Refresh()
	if m.sizes.Height() != 0 {
		t.Errorf("response view: sizes pane height = %d; want 0 (no box at all)", m.sizes.Height())
	}
	full := m.headers.Height()
	if full <= split {
		t.Errorf("response view: Headers height = %d; want more than the split's %d", full, split)
	}
}

// TestRequestSizePaneRenders pins what the pane actually shows: the count in
// its box title (so a 24-message list reads at a glance without scrolling),
// and a scrollbar thumb once the rows overflow — the pane is scrollable, and
// nothing else says so.
func TestRequestSizePaneRenders(t *testing.T) {
	st := state.New(store.New(500), nil, make(chan capture.Event, 1), spinner.New())
	var turns []string
	for range 40 {
		turns = append(turns, `{"role":"user","content":"hello there"}`)
	}
	body := `{"model":"m","messages":[` + strings.Join(turns, ",") + `]}`
	st.BeginRecord(capture.Event{
		Kind: capture.KindRequest, Method: "POST", Path: "/p",
		ReqBody: body, ReqBytes: int64(len(body)),
	})

	m := New(st)
	m.view = viewRequest
	m.SetSize(3, 120, 36)
	out := m.View()

	if !strings.Contains(out, "Request · 40 msg · ") {
		t.Errorf("pane title missing the message count and total:\n%s", out)
	}
	if !strings.Contains(out, "▌") {
		t.Error("40 rows in a short pane must show a scrollbar thumb")
	}
	// The rows account for the whole request: naming the non-conversation
	// remainder is what stops the numbers looking like they don't add up.
	if !strings.Contains(out, "envelope") {
		t.Errorf("pane missing the envelope row:\n%s", out)
	}
}

// TestScrollBarRidesTheBorder pins the border-painted scrollbar: it costs no
// content column, and it lands on content rows only. firstRow is a per-box
// constant (the dump carries a head strip and a rule above its viewport, the
// side panes a title), and an off-by-one there paints the thumb onto a
// border, a title, or the rule — visible only at some scroll positions,
// which is exactly why it is pinned here.
func TestScrollBarRidesTheBorder(t *testing.T) {
	st := state.New(store.New(500), nil, make(chan capture.Event, 1), spinner.New())
	var turns []string
	for range 30 {
		turns = append(turns, `{"role":"user","content":"hello there friend"}`)
	}
	body := `{"model":"m","messages":[` + strings.Join(turns, ",") + `]}`
	st.BeginRecord(capture.Event{
		Kind: capture.KindRequest, Method: "POST", Path: "/p",
		ReqBody: body, ReqBytes: int64(len(body)),
	})

	m := New(st)
	m.view = viewRequest
	m.SetSize(3, 100, 20)
	lines := strings.Split(m.View(), "\n")

	// The box is exactly as wide as the terminal: the bar took no column.
	if w := lipgloss.Width(m.View()); w != 100 {
		t.Errorf("view width = %d; want 100 — the border-painted bar must cost nothing", w)
	}

	var thumbRows []int
	for i, l := range lines {
		if strings.Contains(l, scrollThumbGlyph) {
			thumbRows = append(thumbRows, i)
		}
	}
	if len(thumbRows) == 0 {
		t.Fatal("30 messages in a 20-row terminal must show a thumb")
	}
	// Never on the box's own top or bottom border rows.
	for _, r := range thumbRows {
		if r == 0 || r == len(lines)-1 {
			t.Errorf("thumb on a border row (%d of %d)", r, len(lines))
		}
	}
	// No thumb on the head line or the rule beneath it.
	if first := thumbRows[0]; first < headRows {
		t.Errorf("first thumb row = %d; want >= %d (past the head strip and rule)", first, headRows)
	}
}

// scrollThumbGlyph mirrors render's thumb rune for the assertions above.
const scrollThumbGlyph = "▌"

// TestBreakdownNamesResponsesItems pins the labels for a responses-api
// request: its input carries tool calls and their outputs as typed items
// with no role at all, and calling those "?" hid the rows most worth
// looking at — a tool result is routinely the largest thing in a
// request, which is exactly what this pane exists to show. The names are
// the wire's own, so they can be found in the dump beside it; a name too
// wide for the pane is truncated, never the number.
func TestBreakdownNamesResponsesItems(t *testing.T) {
	st := state.New(store.New(500), nil, make(chan capture.Event, 1), spinner.New())
	reqBody := `{"model":"gpt-5.4-mini","input":[` +
		`{"role":"user","content":"What's the weather in Paris, France?"},` +
		`{"type":"function_call","call_id":"c1","name":"search_locations","arguments":"{\"text\":\"Paris\"}"},` +
		`{"type":"function_call_output","call_id":"c1","output":"` + strings.Repeat("x", 800) + `"},` +
		`{"type":"reasoning","summary":[]},` +
		`{"role":"assistant","content":"21.9°C"}]}`
	st.BeginRecord(capture.Event{
		Kind: capture.KindRequest, Status: 200, Method: "POST", Path: "/responses",
		// The record says which api made it; the breakdown reads the body
		// by that api's rules.
		API:     config.APIResponses,
		ReqBody: reqBody, ReqBytes: int64(len(reqBody)),
	})
	st.FinishBody(capture.Event{Kind: capture.KindBody})
	st.FinishRecord()
	rec, _ := st.LastRecord()

	_, rows := requestBreakdown(rec, 28)
	sizes := strings.Join(rows, "\n")
	for _, want := range []string{"user", "function_call", "function_call_out", "reasoning", "assistant"} {
		if !strings.Contains(sizes, want) {
			t.Errorf("breakdown missing %q:\n%s", want, sizes)
		}
	}
	if strings.Contains(ansi.Strip(sizes), "?") {
		t.Errorf("an item went unnamed:\n%s", sizes)
	}
	// Narrow the pane until the longest name cannot fit: the name is cut
	// and marked, the number is not — a clipped count is useless, and a
	// clipped name is still a name.
	const narrow = 16
	_, tight := requestBreakdown(rec, narrow)
	var cut bool
	for _, row := range tight {
		plain := ansi.Strip(row)
		if strings.Contains(plain, "…") {
			cut = true
		}
		if !strings.HasSuffix(plain, "B") {
			t.Errorf("the size was clipped off a long name: %q", plain)
		}
		if got := ansi.StringWidth(plain); got > narrow {
			t.Errorf("row width = %d; want at most the pane's %d: %q", got, narrow, plain)
		}
	}
	if !cut {
		t.Errorf("nothing was cut at %d columns; the pane cannot be that wide", narrow)
	}
}

// TestBreakdownColumnsAreSizedToTheirContent pins the table's shape: the
// labels take the widest name, the counts the widest number, and they sit
// a gap apart — not pushed to opposite edges of whatever the pane
// happens to be, which made one fact's two halves drift further apart the
// wider the terminal got.
func TestBreakdownColumnsAreSizedToTheirContent(t *testing.T) {
	st := state.New(store.New(500), nil, make(chan capture.Event, 1), spinner.New())
	reqBody := `{"model":"m","input":[` +
		`{"role":"user","content":"hi"},` +
		`{"type":"function_call_output","call_id":"c1","output":"` + strings.Repeat("x", 900) + `"}]}`
	st.BeginRecord(capture.Event{
		Kind: capture.KindRequest, API: config.APIResponses, Status: 200, Method: "POST", Path: "/responses",
		ReqBody: reqBody, ReqBytes: int64(len(reqBody)),
	})
	st.FinishBody(capture.Event{Kind: capture.KindBody})
	st.FinishRecord()
	rec, _ := st.LastRecord()

	// A pane with room to spare: every row is the same width, and that
	// width is the table's, not the pane's.
	_, rows := requestBreakdown(rec, 60)
	if len(rows) < 2 {
		t.Fatalf("rows = %d; want the parts and the items", len(rows))
	}
	w := ansi.StringWidth(ansi.Strip(rows[0]))
	for _, row := range rows {
		if got := ansi.StringWidth(ansi.Strip(row)); got != w {
			t.Errorf("row %q is %d wide; want every row at %d", ansi.Strip(row), got, w)
		}
	}
	if w >= 60 {
		t.Errorf("the table is %d wide in a 60-column pane; want it sized to its content", w)
	}
	// The counts line up: every row ends with its number, right-aligned.
	for _, row := range rows {
		if !strings.HasSuffix(ansi.Strip(row), "B") {
			t.Errorf("row %q does not end with its count", ansi.Strip(row))
		}
	}
}

// TestBreakdownColumnsHoldAcrossRecords pins the pane's stability: the
// label column is floored at the api's own longest name and the number
// column at a width that covers ordinary sizes, so walking from a record
// of short items to one carrying a tool result does not slide the table
// sideways under the reader.
func TestBreakdownColumnsHoldAcrossRecords(t *testing.T) {
	body := func(items string) string { return `{"model":"m","input":[` + items + `]}` }
	small := body(`{"role":"user","content":"hi"}`)
	large := body(`{"role":"user","content":"hi"},` +
		`{"type":"function_call_output","call_id":"c","output":"` + strings.Repeat("x", 2800) + `"}`)

	width := func(src string) int {
		st := state.New(store.New(500), nil, make(chan capture.Event, 1), spinner.New())
		st.BeginRecord(capture.Event{
			Kind: capture.KindRequest, API: config.APIResponses, Status: 200,
			Method: "POST", Path: "/responses", ReqBody: src, ReqBytes: int64(len(src)),
		})
		st.FinishBody(capture.Event{Kind: capture.KindBody})
		st.FinishRecord()
		rec, _ := st.LastRecord()
		_, rows := requestBreakdown(rec, 28)
		if len(rows) == 0 {
			t.Fatalf("no rows for %q", src)
		}
		return ansi.StringWidth(ansi.Strip(rows[0]))
	}
	if a, b := width(small), width(large); a != b {
		t.Errorf("table widths differ across records: %d vs %d", a, b)
	}
}

// TestHeaderRowsAreOneLineEach pins the Headers pane's shape: a header is
// a sentence and it gets one row, cut and marked when the pane is too
// narrow for it. Wrapping turned every long header into two rows, which
// made a pane you count rather than scan — and the whole value is in the
// dump beside it, and in what the pane's copy hands over.
func TestHeaderRowsAreOneLineEach(t *testing.T) {
	st := state.New(store.New(500), nil, make(chan capture.Event, 1), spinner.New())
	st.BeginRecord(capture.Event{
		Kind: capture.KindRequest, Status: 200, Method: "POST", Path: "/p",
		ReqHeaders: []capture.KeyValue{
			{Key: "content-type", Value: "application/json"},
			{Key: "user-agent", Value: strings.Repeat("long/", 40)},
			{Key: "x-ms-deployment-name", Value: "gpt-5.4-mini-2026-04-01-preview"},
		},
	})
	st.FinishBody(capture.Event{Kind: capture.KindBody})
	st.FinishRecord()

	m := New(st)
	m.SetSize(3, 120, 36)
	m.Refresh()

	if got, want := len(m.headersLines), 3; got != want {
		t.Fatalf("header rows = %d; want %d — one per header:\n%s",
			got, want, strings.Join(m.headersLines, "\n"))
	}
	for _, line := range m.headersLines {
		if got := ansi.StringWidth(ansi.Strip(line)); got > m.headers.Width() {
			t.Errorf("row is %d wide in a %d pane: %q", got, m.headers.Width(), ansi.Strip(line))
		}
	}
	if !strings.Contains(ansi.Strip(m.headersLines[1]), "…") {
		t.Errorf("a header too long for the pane was not marked as cut: %q", ansi.Strip(m.headersLines[1]))
	}
}
