// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package theme

import "testing"

func TestApplyFlipsPalette(t *testing.T) {
	t.Cleanup(func() { Apply(true) }) // the package default other tests assume

	Apply(true)
	dark := DimStyle.GetForeground()
	Apply(false)
	light := DimStyle.GetForeground()
	if dark == light {
		t.Fatal("Apply(false) left DimStyle unchanged; want a light-background variant")
	}
	// Fixed-accent styles stay put: 62 holds up on both backgrounds.
	Apply(true)
	darkAccent := NoticeStyle.GetForeground()
	Apply(false)
	if NoticeStyle.GetForeground() != darkAccent {
		t.Error("accent color changed between palettes; want it stable")
	}
}
