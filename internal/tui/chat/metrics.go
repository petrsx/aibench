// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

import (
	"fmt"
	"strings"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/pricing"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/store"
	"github.com/petrsx/aibench/internal/tui/render"
	"github.com/petrsx/aibench/internal/tui/selection"
	"github.com/petrsx/aibench/internal/tui/theme"
)

// metricsGroup is one section of the metrics panel; adding a group here is all
// it takes to extend the panel.
type metricsGroup struct {
	title string
	rows  []capture.KeyValue
}

// noValue stands in for a measurement this record does not have — a body
// that never arrived, a model the pricing table does not know. The row
// stays: a panel whose rows appear and vanish makes you re-find the one
// you were reading every time you click, and an absent number is itself
// worth seeing.
var noValue = theme.DimStyle.Render("–")

// metricsGroups assembles the shown record (pinned, else latest) into
// display groups.
//
// The first three — Request, Tokens, Pricing — are the panel's skeleton:
// always present, always the same rows in the same order, whether or not
// there is anything to put in them. Everything that only sometimes applies
// (a failure, the exchange totals, the endpoint's rate limits) follows
// after, so nothing above it ever moves. The endpoint identity is not here
// at all; it lives in the tab header.
func (m *Model) metricsGroups() []metricsGroup {
	rec, _ := m.state.ShownRecord() // zero record ⇒ a panel of placeholders
	// Both the token and the pricing rows need the model's rates: the
	// deployment name on an azure host often is not a model the table
	// knows, and every row that depends on one says so the same way.
	rates, priced := m.pricing.Lookup(m.cfg.Model)

	groups := []metricsGroup{
		m.requestMetrics(rec),
		m.tokenMetrics(rec, rates, priced),
		m.pricingMetrics(rec, rates, priced),
	}

	// --- conditional from here down; nothing above may move ---

	if rec.Err != "" {
		// An interrupt is the user's own doing, so it is not filed under
		// Error: the panel names it for what happened, the way the chat
		// shows a soft notice rather than a red bubble.
		errTitle, key := "Error", "error"
		if isInterrupt(rec.Err) {
			errTitle, key = "Interrupted", "reason"
		}
		groups = append(groups, metricsGroup{errTitle, []capture.KeyValue{{Key: key, Value: rec.Err}}})
	}

	if limits := provider.Resolve(m.spec.RateLimits, rec.Req.RespHeaders); len(limits) > 0 {
		groups = append(groups, metricsGroup{"Rate limits", compactLimits(limits)})
	}

	return groups
}

// requestMetrics is the timing group, titled with where in the exchange this
// request falls — the transcript marks the same thing with its two rules.
//
// There is no status row: the record line in the chat carries it (colored),
// and the Inspector's head repeats it beside the URL. A third copy in the
// panel spends a row telling you what two other surfaces already did.
//
// The labels are ones a reader can answer without knowing the plumbing: when
// it went out, how long until the answer started, how long until it
// finished, how much came back. "first byte" is the one term of art kept —
// it is what the measurement is, and "waited" would suggest the request was
// queued rather than streaming.
func (m *Model) requestMetrics(rec store.Record) metricsGroup {
	exchange := m.exchangeOf(rec.ID)
	title := "Request"
	for i, r := range exchange {
		if r.ID == rec.ID && len(exchange) > 1 {
			title = fmt.Sprintf("Request (exchange %d of %d)", i+1, len(exchange))
			break
		}
	}

	firstByte := noValue
	if rec.Req.Status != 0 {
		firstByte = render.Elapsed(rec.Req.Duration)
	}
	at := noValue
	if !rec.Time.IsZero() {
		at = render.Timestamp(rec.Time)
	}
	total, body := noValue, noValue
	if rec.Body.Kind == capture.KindBody {
		total = render.Elapsed(rec.Body.Duration)
		body = render.Bytes(rec.Body.BodyBytes)
	}
	return metricsGroup{title, []capture.KeyValue{
		{Key: "sent at", Value: at},
		{Key: "first byte", Value: firstByte},
		{Key: "answered in", Value: total},
		{Key: "response", Value: body},
	}}
}

// tokenMetrics is the usage group: api-neutral labels, since every api
// splits usage the same way (chat's prompt/completion, responses/messages'
// input/output). context needs the model's window, which only the pricing
// table knows.
//
// in and out wear the colours they already have elsewhere — the graph's
// legend and bars, the record line's ↑/↓ counts. It is the one distinction
// in this panel a colour can carry, and carrying it here too means a reader
// learns the pair once. The totals stay plain: a sum of two coloured things
// is neither.
func (m *Model) tokenMetrics(rec store.Record, rates pricing.Rates, priced bool) metricsGroup {
	in, out, sum, ctx := noValue, noValue, noValue, noValue
	if u := rec.Usage; u != nil {
		in = theme.TokensInStyle.Render(render.Tokens(u.Prompt))
		out = theme.TokensOutStyle.Render(render.Tokens(u.Completion))
		sum = render.Tokens(u.Total)
		if priced && rates.Context > 0 {
			ctx = formatContext(u.Prompt, rates.Context)
		}
	}
	return metricsGroup{"Tokens", []capture.KeyValue{
		{Key: "in", Value: in},
		{Key: "out", Value: out},
		{Key: "total", Value: sum},
		{Key: "context", Value: ctx},
	}}
}

// pricingMetrics is this request against the session it belongs to.
func (m *Model) pricingMetrics(rec store.Record, rates pricing.Rates, priced bool) metricsGroup {
	cost, session := noValue, noValue
	if priced {
		session = render.Cost(m.sessionCost(rates))
		if u := rec.Usage; u != nil {
			cost = render.Cost(rates.Cost(u.Prompt, u.Completion))
		}
	}
	return metricsGroup{"Pricing", []capture.KeyValue{
		{Key: "cost", Value: cost},
		{Key: "session", Value: session},
	}}
}

// isInterrupt reports whether a record's failure text is an interrupt
// rather than an endpoint fault. Both writers of that text — the stream's
// canceled context (state) and the canceled round trip (provider) — say
// "interrupted"; nothing else does.
func isInterrupt(err string) bool { return strings.Contains(err, "interrupted") }

// sessionCost totals the cost of every record's usage at the given rate.
// Records don't store their own model, so a mid-session profile switch is
// costed at the current rate — an approximation the session row accepts.
func (m *Model) sessionCost(r pricing.Rates) float64 {
	var total float64
	for _, rec := range m.state.Records() {
		if rec.Usage != nil {
			total += r.Cost(rec.Usage.Prompt, rec.Usage.Completion)
		}
	}
	return total
}

// formatContext reads as "<prompt> / <window> · <pct>", e.g. 12.3k / 200k · 6%
// — compact counts, because the row is a ratio to glance at rather than
// two numbers to compare.
func formatContext(prompt, window int) string {
	return fmt.Sprintf("%s / %s · %d%%",
		render.TokensCompact(prompt), render.TokensCompact(window), prompt*100/window)
}

// compactLimits folds "<name> left" / "<name> max" pairs into a single
// "<name>  left / max" row so the whole group fits shorter panels.
func compactLimits(rows []capture.KeyValue) []capture.KeyValue {
	byKey := make(map[string]string, len(rows))
	for _, kv := range rows {
		byKey[kv.Key] = kv.Value
	}
	merged := make(map[string]bool)
	var out []capture.KeyValue
	for _, kv := range rows {
		if merged[kv.Key] {
			continue
		}
		if name, ok := strings.CutSuffix(kv.Key, " left"); ok {
			if maxV, has := byKey[name+" max"]; has {
				out = append(out, capture.KeyValue{Key: name, Value: kv.Value + " / " + maxV})
				merged[name+" max"] = true
				continue
			}
		}
		out = append(out, kv)
	}
	return out
}

// renderMetrics rebuilds the metrics panel lines from the shown record;
// they are cached for mouse selection like the other panes.
//
// Rows cascade under their group's title, and the value column is shared
// by every group rather than fitted per group: a panel of numbers is read
// down its values, and three groups each aligning to their own longest key
// puts those values at three different columns.
func (m *Model) renderMetrics() {
	groups := m.metricsGroups()

	// The panels' shared table (render.KeyValues), with one key column
	// across every group: the value column is fixed once and passed back
	// as KeyW, so the groups line up with each other instead of each
	// aligning to its own longest key.
	width := m.metricsWidth() - 2
	opts := render.KeyValueOpts{Indent: 3}
	var all []render.KeyValue
	for _, g := range groups {
		for _, kv := range g.rows {
			all = append(all, render.KeyValue{Key: kv.Key, Value: kv.Value})
		}
	}
	opts.KeyW = render.KeyColumn(all, width, opts)

	var lines []string
	for i, g := range groups {
		if i > 0 {
			lines = append(lines, "")
		}
		if g.title != "" {
			lines = append(lines, " "+theme.TitleStyle.Render(g.title))
		}
		rows := make([]render.KeyValue, 0, len(g.rows))
		for _, kv := range g.rows {
			rows = append(rows, render.KeyValue{Key: kv.Key, Value: kv.Value})
		}
		lines = append(lines, render.KeyValues(rows, width, opts)...)
	}

	// Clip in the cache so mouse selection maps to what is shown.
	m.metricsLines = render.Clip(lines, width)
}

// viewMetrics renders the bordered Metrics panel, sized to match the chat +
// input column.
func (m *Model) viewMetrics() string {
	lines := m.metricsLines
	if m.sel.Pane == paneMetrics {
		lines = selection.Apply(lines, m.sel.Anchor, m.sel.Head)
	}

	rows := m.metricsRows()
	if hidden := len(lines) - rows; hidden > 0 {
		// Never clip silently: the last visible row says what fell off.
		lines = lines[:rows]
		if rows > 0 {
			lines[rows-1] = theme.DimStyle.Render(fmt.Sprintf("   … +%d rows (window too short)", hidden+1))
		}
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}

	return render.SidePanel(" Metrics", render.CopyLabel, lines, m.metricsWidth(), m.metricsHeight())
}
