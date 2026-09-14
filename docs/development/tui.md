# The terminal

Two constraints on how aibench draws itself: which characters are safe to
print, and one Bubble Tea API that must stay unused.

## Glyphs

**An app cannot choose the terminal's font — only which characters it
prints**, and coverage varies enormously by character. Pick from blocks
every monospace font carries:

| Safe | Block | Not safe |
| --- | --- | --- |
| `●` `○` | Geometric Shapes | `⏺` (U+23FA, Misc Technical) |
| `└` `│` `┃` | Box Drawing | most of Misc Technical |

`⏺` was the assistant marker until Windows Terminal drew it as a **blue
emoji box**: Cascadia Mono lacks that code point, so Windows substituted an
emoji font for it. `●` renders as the intended dot in the same font,
unchanged.

If you are *seeing* boxes, a Nerd Font variant (`Cascadia Mono NF`) covers
far more — but the app should not need one. Verify a new glyph on Windows
before adopting it; `Write-Host "…"` in PowerShell is enough.

## `View.OnMouse` is deliberately unused

The runtime invokes it *in
addition* to normal Update delivery, and runs whatever command it returns
through `go p.Send(...)` — another goroutine. That makes it safe only as a
pure raw-mouse→semantic-message translator, never as somewhere to touch
state. Our tabs own their geometry and mutate inside Update, so mouse
handling belongs in each tab's `mouse.go`, reached through Update like
every other message.
