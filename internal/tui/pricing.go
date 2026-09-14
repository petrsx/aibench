// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

import (
	"context"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/pricing"
)

// Pricing, shell-side: the model-rates table the Metrics cost/context
// rows read. Your local pricing.yaml is the source; `aibench pricing
// update` fills or refreshes it from catwalk when you ask, `pricing
// edit` opens it — and the file hot-reloads like the config and prompt
// files do, so either lands in the running session (the watcher only
// signals; the shell re-reads on pricingReloadMsg).
//
// With no local file at all, launch fetches the published table from the
// repo once so a fresh install still shows costs. Having a file stops
// that: a session that can price itself calls no one.

// pricingReloadMsg says pricing.yaml changed on disk.
type pricingReloadMsg struct{}

// pricingUpdatedMsg is /pricing-update's result.
type pricingUpdatedMsg struct {
	sum pricing.Summary
	err error
}

// updatePricing runs /pricing-update: the same catwalk merge as `aibench
// pricing update`, off the loop, reported as a notice. The saved file
// also trips the watcher, so the reprice needs no special path.
func (m *Model) updatePricing() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		s, err := pricing.Update(ctx, "", "")
		return pricingUpdatedMsg{sum: s, err: err}
	}
}

// pricingUpdated reports the result and reprices at once rather than
// wait on the watcher, so the notice and the rows agree.
func (m *Model) pricingUpdated(msg pricingUpdatedMsg) tea.Cmd {
	if msg.err != nil {
		return m.notifyError("pricing: " + msg.err.Error())
	}
	slog.Info("pricing", "source", "update", "models", msg.sum.Models, "file", msg.sum.Path)
	m.SetPricing(pricing.Runtime())
	return m.notifySay("pricing: " + msg.sum.String())
}

// SetPricing supplies the model-id → rates table that drives the Metrics
// cost/context rows. An empty table simply hides them.
func (m *Model) SetPricing(t pricing.Table) {
	m.pricing = t
	m.chat.SetPricing(t)
}

// WatchPricing takes the channel the pricing.yaml watcher signals on.
func (m *Model) WatchPricing(ch <-chan struct{}) { m.pricingCh = ch }

// waitPricing re-arms the watcher signal; a nil channel blocks forever,
// which is fine for a tea command.
func (m *Model) waitPricing() tea.Cmd {
	return func() tea.Msg { <-m.pricingCh; return pricingReloadMsg{} }
}

// reloadPricing re-reads the local file and reprices the Metrics rows in
// place. Quiet on purpose, like the launch fetch: a notice for every
// save would nag someone editing rows by hand, and the cost row changing
// is the confirmation.
func (m *Model) reloadPricing() {
	t := pricing.Runtime()
	slog.Info("pricing", "source", "reload", "models", len(t))
	m.SetPricing(t)
}

// fetchPublishedPricing grabs the repo's table, quietly: a failure is a
// log line and empty cost rows, never a notice — rates are a nicety, and
// nothing about a request depends on them.
func fetchPublishedPricing() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	t, err := pricing.FetchPublished(ctx, "")
	if err != nil {
		slog.Debug("published pricing not fetched", "err", err)
		return nil
	}
	slog.Info("pricing", "source", "published", "models", len(t))
	return pricingMsg(t)
}
