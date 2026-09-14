// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package capture

import "testing"

func TestTimestampFallsBackToNow(t *testing.T) {
	if (Event{}).Timestamp().IsZero() {
		t.Error("Timestamp() on zero event is zero; want now")
	}
}
