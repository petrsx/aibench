// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package prompt is the Prompt tab: a spec-driven params frame over a
// rendered view of the markdown system prompt, backed by the prompt file
// (loaded on start, hot-reloaded on disk changes). The system prompt itself is
// read-only here — it is edited in the file and rendered with glamour into a
// scrollable, searchable view; only the params are editable (and debounced back
// to the file). It is an independent tea.Model — it reads no session store; the
// shell reads its SystemPrompt/Params when assembling a send.
package prompt

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/petrsx/aibench/internal/bindings"
	"github.com/petrsx/aibench/internal/preset"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/tui/find"
	"github.com/petrsx/aibench/internal/tui/notify"
	"github.com/petrsx/aibench/internal/tui/render"
	"github.com/petrsx/aibench/internal/tui/theme"
)

// ReloadMsg fires when the prompt file changes on disk; SaveMsg is the
// debounced auto-save tick for the params. Both are produced by this model's
// own commands and must be routed back to Update even while another tab is
// active, so they are exported for the shell to forward.
type (
	ReloadMsg struct{}
	SaveMsg   struct{}
)

// Model is the Prompt tab.
type Model struct {
	spec       provider.Spec
	promptFile string
	toolsFiles []string
	header     string // the tab header's content: the shell's profile strip
	keys       keyMap // this tab's keys — matching and help both read them
	// tipIdx is which hint the header is showing; the shell advances it
	// on its timer (tips.go).
	tipIdx int

	sysView   viewport.Model // rendered, read-only view of the markdown prompt
	sysLines  []string       // the rendered lines behind sysView, kept clean of find highlights
	sysSource string         // the raw markdown behind sysView (sent as the system message)
	// rows are the params the prompt file defines (params.go); params are
	// their inputs, one per row.
	rows   []paramRow
	params []textinput.Model
	focus  int // 0 = system prompt view, 1.. = params

	// system-prompt search (ctrl+f): the shared find widget shown as a top-right
	// overlay, the matches over sysLines, and the overlay's screen origin
	// recorded at View time.
	find find.Pane

	// loadErr / toolsErr are why the declared files did not load, kept so
	// the tab can name them rather than render as if nothing was asked for.
	loadErr      error
	toolsErr     error
	findX, findY int

	note     notify.Notification // transient corner notice (e.g. "copied to clipboard")
	presetCh <-chan struct{}
	loaded   preset.Preset // last state read from / written to the file
	dirty    bool          // an unsaved param edit is pending
	dirtyAt  time.Time

	// the active set's tool files, merged: per-api body overlays +
	// tool bindings. Read-only here, reloaded with the prompt.
	request bindings.Set

	top    int // screen row the content region begins at, for cursor math
	width  int
	height int
}

// New builds the Prompt tab for an api's spec, backed by promptFile and
// its watcher signal. A missing or broken file just leaves the fields empty.
func New(spec provider.Spec, promptFile string, toolsFiles []string, header string, presetCh <-chan struct{}) *Model {
	m := &Model{
		spec:       spec,
		promptFile: promptFile,
		toolsFiles: toolsFiles,
		header:     header,
		keys:       newKeyMap(),
		find:       find.NewPane(),
		presetCh:   presetCh,
	}
	m.ApplyTheme() // capture the search-highlight styles into the view
	m.buildParamInputs()
	m.reloadPreset(nil)
	m.loadRequest()
	return m
}

// Init arms the prompt-file watcher.
func (m *Model) Init() tea.Cmd { return m.waitPreset() }

// SetProfile re-points the tab at a new api's spec and prompt file
// (a config profile or prompt-set switch): the inputs rebuild, the file
// reloads, and the tab header picks up the new profile strip.
func (m *Model) SetProfile(spec provider.Spec, promptFile string, toolsFiles []string, header string) {
	m.spec = spec
	m.promptFile = promptFile
	m.toolsFiles = toolsFiles
	m.header = header
	// The rows belong to the prompt file, not the api, so a profile
	// switch re-reads the file if it can and otherwise re-marks what is
	// already loaded: the same param can be this api's own and the next
	// one's stranger.
	// The rows belong to the prompt file, so a switch re-reads it and
	// falls back to what is already loaded when it cannot.
	m.reloadPreset(&m.loaded)
	m.loadRequest()
}

// reloadPreset re-reads the prompt file, remembering why it could not be
// read so the tab can say so instead of showing an empty prompt that
// looks deliberate. fallback is applied when the read fails.
func (m *Model) reloadPreset(fallback *preset.Preset) {
	// No path at all is not a failure: a kind: agent profile owns its
	// instructions server-side, and the tab shows nothing on purpose.
	// Keeping the previous profile's prompt here would be worse than
	// wrong — it would claim the agent is being sent one.
	if m.promptFile == "" {
		m.loadErr = nil
		m.applyPreset(preset.Preset{})
		return
	}
	pre, err := preset.Load(m.promptFile)
	m.loadErr = err
	if err == nil {
		m.applyPreset(pre)
		return
	}
	if fallback != nil {
		m.applyPreset(*fallback)
	}
}

// LoadIssues is what the tab could not read: the declared files that are
// not there, phrased for a notice. Empty when everything loaded.
func (m *Model) LoadIssues() []string {
	var out []string
	if m.loadErr != nil && m.promptFile != "" {
		out = append(out, "prompt file not found: "+m.promptFile)
	}
	if m.toolsErr != nil {
		out = append(out, "tools file: "+m.toolsErr.Error())
	}
	return out
}

// SetSize lays out the view within the content region the shell allots (top is
// the screen row the region begins at, for cursor math; contentHeight already
// excludes the header and help chrome).
func (m *Model) SetSize(top, width, contentHeight int) {
	m.top, m.width, m.height = top, width, contentHeight
	// Main column: the tab header, then the frameless prompt view. -3 is the
	// left pad, the scrollbar's gutter, and the bar (see render.MainColumn);
	// the height gives up the header, the pad row, and the closing rule.
	m.sysView.SetWidth(max(1, m.mainWidth()-3))
	m.sysView.SetHeight(max(1, contentHeight-render.TabHeaderRows-1))
	m.sizeParamInputs()
	m.renderPreview()
}

// withParamsDocsNote pads the rows out and sets a link to the params
// reference on the pane's last line. Dropped when the pane is too short
// to spare a row — the values are what the pane is for.
func (m *Model) withParamsDocsNote(rows []string) []string {
	avail := render.SidePanelRows(m.height)
	if avail < len(rows)+2 {
		return rows
	}
	for len(rows) < avail-1 {
		rows = append(rows, "")
	}
	return append(rows, " "+theme.DimStyle.Render(
		render.DocsLink("params reference ↗", "prompts.md", "")))
}

// sizeParamInputs shares what the panel's width leaves after the labels.
// Called on a resize and whenever the rows change — a param appearing on
// disk can widen the label column.
func (m *Model) sizeParamInputs() {
	valueW := max(4, min(16, m.paramsWidth()-m.paramLabelW()-5))
	for i := range m.params {
		m.params[i].SetWidth(valueW)
	}
}

// mainWidth is the main column's outer width (tab header + prompt view);
// paramsWidth is the Params side panel's. Both come from the shared split,
// so this tab's columns line up with the other two.
func (m *Model) mainWidth() int   { main, _ := render.SplitWidths(m.width); return main }
func (m *Model) paramsWidth() int { _, side := render.SplitWidths(m.width); return side }

// copyAt reports a click on the tab's copy control, on the rule that
// closes the main column. What it hands over is the prompt as the file
// holds it — markdown, not the glamour-rendered view: the file is the
// thing you meant to take, and the rendering is this tab's way of reading
// it.
func (m *Model) copyAt(x, y int) bool {
	cells := render.FooterControlCells(1, m.top+render.TabHeaderRows+m.sysView.Height(),
		max(1, m.sysView.Width()+2), render.CopyLabel)
	return cells[0].Hit(x, y)
}

func (m *Model) copyPrompt() tea.Cmd {
	return m.note.Copy(m.sysSource, theme.NoticeStyle.Render("prompt copied to clipboard"))
}

// paramsCopyAt reports a click on the Params panel's control, in its
// title corner.
func (m *Model) paramsCopyAt(x, y int) bool {
	return render.PanelControlCells(m.mainWidth(), m.top, m.paramsWidth(),
		render.CopyLabel)[0].Hit(x, y)
}

// copyParams hands over the params as frontmatter — "name: value" a line
// at a time, which is the shape they live in and the shape they can be
// pasted back as. Empty fields are left out: an unset param is not a
// param, here as on the wire.
func (m *Model) copyParams() tea.Cmd {
	var lines []string
	for i, row := range m.rows {
		if v := strings.TrimSpace(m.params[i].Value()); v != "" {
			lines = append(lines, row.name+": "+v)
		}
	}
	return m.note.Copy(strings.Join(lines, "\n"),
		theme.NoticeStyle.Render("params copied to clipboard"))
}

// Notify shows a pre-styled notice in this tab's own spot until it
// lapses. The shell uses it for app-level messages, handing them to
// whichever tab is in front: the placement is the tab's business, and
// each one already knows where a notice reads best in its layout.
func (m *Model) Notify(text string) tea.Cmd { return m.note.Show(text) }

// Focus gives focus to the field the tab points at (the system-prompt view
// takes focus for scroll/search; it has no cursor of its own).
func (m *Model) Focus() tea.Cmd {
	for i := range m.params {
		m.params[i].Blur()
	}
	if m.focus == 0 {
		return nil
	}
	return m.params[m.focus-1].Focus()
}

// Blur parks every field (terminal focus lost / tab switched away).
func (m *Model) Blur() {
	for i := range m.params {
		m.params[i].Blur()
	}
}

// Update cycles field focus with tab, opens search with ctrl+f, and routes
// everything else to the focused field: the system-prompt view scrolls, a param
// input edits (arming the debounced auto-save). Reload/save messages keep the
// tab in sync with the file.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case notify.ExpiredMsg:
		return nil // the notice lapsed; this render drops it
	case ReloadMsg:
		// The prompt file changed on disk: the file wins. Skip when the content
		// matches what the tab already shows — that's the echo of our own
		// param auto-save (or a no-op touch).
		if pre, err := preset.Load(m.promptFile); err == nil && !presetEqual(pre, m.currentPreset()) {
			m.applyPreset(pre)
			m.dirty = false
		}
		// The request file rides the same watch signal (nothing here saves
		// back to it, so a plain re-read is always safe).
		m.loadRequest()
		return m.waitPreset()

	case SaveMsg:
		// A promptless (kind: agent) profile has no file to save back to;
		// param edits still ride the send, they just don't persist.
		if !m.dirty || m.promptFile == "" {
			m.dirty = false
			return nil
		}
		if rest := time.Second - time.Since(m.dirtyAt); rest > 0 {
			return tea.Tick(rest, func(time.Time) tea.Msg { return SaveMsg{} })
		}
		m.dirty = false
		cur := m.currentPreset()
		if !presetEqual(cur, m.loaded) {
			m.loaded = cur
			_ = preset.Save(m.promptFile, cur) // best effort; the fields keep the state
		}
		return nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyPressMsg:
		// While the find overlay is open it claims the keys.
		if m.find.Active() {
			return m.find.Route(msg, m)
		}
		switch {
		case key.Matches(msg, m.keys.NextField):
			m.focus = (m.focus + 1) % (len(m.params) + 1)
			return m.Focus()
		case key.Matches(msg, m.keys.Copy):
			return m.copyPrompt()
		case key.Matches(msg, m.keys.Find):
			return m.openFind()
		}
	}

	// The system-prompt view has focus: scroll it (arrows/pgup/pgdn).
	if m.focus == 0 {
		var cmd tea.Cmd
		m.sysView, cmd = m.sysView.Update(msg)
		return cmd
	}

	var cmd tea.Cmd
	m.params[m.focus-1], cmd = m.params[m.focus-1].Update(msg)
	// Keys and pastes edit the params (v2 delivers pastes as their own message,
	// not key runes), so both arm the debounced auto-save.
	dirtying := false
	switch msg.(type) {
	case tea.KeyMsg, tea.PasteMsg:
		dirtying = true
	}
	if dirtying {
		m.dirtyAt = time.Now()
		if !m.dirty {
			m.dirty = true
			return tea.Batch(cmd, tea.Tick(time.Second, func(time.Time) tea.Msg { return SaveMsg{} }))
		}
	}
	return cmd
}

// Cursor is the real terminal cursor inside the focused field. The system-prompt
// view is read-only (no cursor) except when the find overlay owns it; a focused
// param input places the cursor inside the params frame.
func (m *Model) Cursor() *tea.Cursor {
	if m.find.Active() {
		return m.find.Cursor(m.findX, m.findY)
	}
	if m.focus == 0 {
		return nil
	}
	i := m.focus - 1
	c := m.params[i].Cursor()
	if c == nil {
		return nil
	}
	// Past the main column, the panel border and leading space, then over
	// the padded label to where the value box begins.
	c.X += m.mainWidth() + 2 + m.paramLabelW() + 1
	c.Y += m.top + 1 + render.SidePanelTitleRows + i // panel border + title rows
	return c
}

// View lays out the main column — the shared tab header (the shell's
// profile strip) over the rendered system prompt — beside the Params side
// panel; the find overlay docks over the content's top-right.
func (m *Model) View() string {
	// The prompt view is frameless, like the transcript and the dump: the
	// header box above and the side panel beside it are the chrome. Focus
	// shows on the panel's border — blurred while the prompt view has it.
	paramsBorder := theme.BlurredBorder
	if m.focus > 0 {
		paramsBorder = theme.FocusedBorder
	}

	// One row per param the file defines, on the panels' shared table
	// (render.KeyValues): the name column is sized to the names, the
	// inputs line up in theirs, and a name too long for the pane is the
	// half that gives.
	//
	// The name carries the row's state, so it is styled here and the
	// table is handed the finished string: focused, or foreign to this
	// api — the file says it, the api does not take it, and it is not
	// sent, which is said out loud because a param quietly ignored is how
	// a typo survives a whole session.
	kv := make([]render.KeyValue, 0, len(m.rows))
	for i, row := range m.rows {
		style := theme.MetaKeyStyle
		switch {
		case m.focus == i+1:
			style = theme.TitleStyle
		case !row.known:
			style = theme.WarningStyle
		}
		kv = append(kv, render.KeyValue{Key: row.name, KeyStyle: style, Value: m.params[i].View()})
	}
	rows := render.KeyValues(kv, m.paramsWidth()-2, render.KeyValueOpts{Indent: 1})
	if len(rows) == 0 {
		rows = append(rows, " "+theme.DimStyle.Render("none set"), "",
			" "+theme.DimStyle.Render("add them to the prompt"),
			" "+theme.DimStyle.Render("file's frontmatter"))
	}
	// A footnote on the pane's last row: what this api takes is a
	// reference question, and the reference is one click away where the
	// terminal supports links.
	rows = m.withParamsDocsNote(rows)
	// The panel's own control, in its title corner like every other side
	// panel's: the params as the file holds them, which is what you would
	// paste into another prompt file or a bug report.
	params := render.SidePanelStyled(paramsBorder, " Params", render.CopyLabel,
		rows, m.paramsWidth(), m.height)

	view := m.sysView.View()
	if m.find.Active() {
		w := m.sysView.Width()
		// The content is flush left inside the column's one-column pad, and
		// starts below the header box and its pad row.
		m.findX, m.findY = 1+w-m.find.Width(), m.top+render.TabHeaderRows
		view = render.WithFind(view, m.find.View(), w, m.sysView.Height())
	}
	// The bar rides beside the text on the column's last cell, as on the
	// other two tabs; the rule gives the column a floor rather than letting
	// it trail off into the help line.
	view = render.ScrollBarFor(view, m.sysView)
	// The notice goes on last, over the finished column: it ends beside
	// the scrollbar rather than where the viewport stops, and never on
	// the rail itself.
	if notice := m.note.Text(); notice != "" {
		view = render.WithNoticeIn(view, notice, lipgloss.Width(view), lipgloss.Height(view),
			lipgloss.Width(view)-render.ScrollBarCols)
	}
	// The rule just closes the column; ctrl+y takes the prompt, and the
	// help line names it.
	rule := render.FooterControls(max(1, m.sysView.Width()+2))

	left := lipgloss.JoinVertical(lipgloss.Left,
		render.TabHeader(m.header, "", m.mainWidth()),
		render.MainColumn(
			lipgloss.JoinVertical(lipgloss.Left, view, rule),
			m.sysView.Width()),
	)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, params)
}

// --- preset round-tripping ------------------------------------------------
