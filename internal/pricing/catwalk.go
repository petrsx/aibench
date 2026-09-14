// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// DefaultCatwalkURL is charmbracelet/catwalk's public registry. Overridable
// via the CATWALK_URL env or the `pricing update --url` flag.
const DefaultCatwalkURL = "https://catwalk.charm.land"

// CatwalkURL resolves the base URL: the argument, else CATWALK_URL, else the
// public default.
func CatwalkURL(override string) string {
	if override != "" {
		return override
	}
	if v := os.Getenv("CATWALK_URL"); v != "" {
		return v
	}
	return DefaultCatwalkURL
}

// catwalkResponse decodes only the fields we need from /v2/providers; the
// registry carries far more (we deliberately ignore it).
type catwalkResponse []struct {
	Models []struct {
		ID            string  `json:"id"`
		CostPer1MIn   float64 `json:"cost_per_1m_in"`
		CostPer1MOut  float64 `json:"cost_per_1m_out"`
		ContextWindow int     `json:"context_window"`
	} `json:"models"`
}

// FetchCatwalk pulls the provider/model registry and flattens it to a rate
// table keyed by model id. Rows with no cost are skipped — they'd only
// render a misleading $0.
func FetchCatwalk(ctx context.Context, baseURL string) (map[string]Rates, error) {
	url := CatwalkURL(baseURL) + "/v2/providers"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("catwalk %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catwalk %s: unexpected status %s", url, resp.Status)
	}

	var providers catwalkResponse
	if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
		return nil, fmt.Errorf("catwalk %s: %w", url, err)
	}

	out := map[string]Rates{}
	for _, p := range providers {
		for _, m := range p.Models {
			if m.ID == "" || (m.CostPer1MIn == 0 && m.CostPer1MOut == 0) {
				continue
			}
			out[m.ID] = Rates{
				Input:   m.CostPer1MIn,
				Output:  m.CostPer1MOut,
				Context: m.ContextWindow,
			}
		}
	}
	return out, nil
}
