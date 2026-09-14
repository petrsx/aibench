// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package bindings

// The static executor: a canned outcome for deterministic prompt testing —
// A/B a prompt without a flaky backend in the loop, or hand the model an
// exact result (empty list, malformed JSON, an error) and watch how it
// reacts.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"
)

func runStatic(ctx context.Context, b Binding) (string, error) {
	if b.DelayMs > 0 {
		select {
		case <-time.After(time.Duration(b.DelayMs) * time.Millisecond):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if b.Error != "" {
		return "", errors.New(b.Error)
	}

	// A JSON string unwraps to the bare text; any other JSON compacts, so
	// the author writes natural JSON either way. Run's capResult enforces
	// the result budget.
	var s string
	if err := json.Unmarshal(b.Result, &s); err == nil {
		return s, nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, b.Result); err == nil {
		return buf.String(), nil
	}
	return string(b.Result), nil
}
