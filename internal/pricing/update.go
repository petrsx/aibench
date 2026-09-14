// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package pricing

import (
	"context"
	"fmt"
)

// Update refreshes the local file from catwalk: what the registry knows
// is added or refreshed, rows only the file has are kept. One routine
// behind both doors — `aibench pricing update` and /pricing-update — so
// they cannot drift. Empty path and url take the defaults (FilePath,
// catwalk.charm.land).
func Update(ctx context.Context, path, url string) (Summary, error) {
	if path == "" {
		path = FilePath()
	}
	existing, err := Load(path)
	if err != nil {
		return Summary{}, err
	}
	fetched, err := FetchCatwalk(ctx, url)
	if err != nil {
		return Summary{}, err
	}
	s := Summary{Path: path}
	for id, r := range fetched {
		if old, ok := existing[id]; !ok {
			s.Added++
		} else if old != r {
			s.Updated++
		}
	}
	for id := range existing {
		if _, ok := fetched[id]; !ok {
			s.Kept++
		}
	}
	merged := Merge(existing, fetched)
	s.Models = len(merged)
	if err := Save(path, merged); err != nil {
		return Summary{}, err
	}
	return s, nil
}

// Summary is what an Update did, for the line that reports it.
type Summary struct {
	Path    string
	Models  int // rows in the file now
	Added   int // new from the registry
	Updated int // registry rows whose rates changed
	Kept    int // rows only the file had
}

// String is the report both doors print: "212 models (3 added, 1 updated, 2 kept)".
func (s Summary) String() string {
	return fmt.Sprintf("%d models (%d added, %d updated, %d kept)", s.Models, s.Added, s.Updated, s.Kept)
}
