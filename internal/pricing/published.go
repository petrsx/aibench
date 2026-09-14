// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package pricing

// The published pricing table: this repo's pricing/pricing.yaml, a
// catwalk harvest served raw off evergreen `main`, is the **fallback**
// for a session with no local pricing.yaml — so cost rows work on a
// fresh install without asking anyone to run a command first.
//
// It is fetched only when there is no local file. Once you have one,
// `aibench pricing update` keeps it current straight from catwalk, and
// launch stops calling out entirely.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultPricingURL is the published pricing.yaml. Overridable via the
// PRICING_URL env or the `cost update --url` flag.
const DefaultPricingURL = "https://raw.githubusercontent.com/petrsx/aibench/main/pricing/pricing.yaml"

// PricingURL resolves the source: the argument, else PRICING_URL, else
// the published default.
func PricingURL(override string) string {
	if override != "" {
		return override
	}
	if v := os.Getenv("PRICING_URL"); v != "" {
		return v
	}
	return DefaultPricingURL
}

// FetchPublished pulls the published pricing.yaml — the same shape the
// local file uses, so the parse is the same.
func FetchPublished(ctx context.Context, url string) (map[string]Rates, error) {
	url = PricingURL(url)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pricing %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pricing %s: unexpected status %s", url, resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("pricing %s: %w", url, err)
	}
	var m map[string]Rates
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("pricing %s: %w", url, err)
	}
	return m, nil
}
