---
name: charmbracelet-bubbletea-v2
description: Bubble Tea v2 (charm.land/bubbletea/v2) API reference and v1→v2 migration guide. Use when migrating this project to Bubble Tea v2 or writing new v2 code. IMPORTANT - this repo migrated to v2 on 2026-07-12: charm.land v2 APIs are the norm here; do NOT reintroduce v1 (github.com/charmbracelet/*) imports or APIs.
---

# Bubble Tea v2

Verified against `charm.land/bubbletea/v2` docs and the official
[UPGRADE_GUIDE_V2](https://github.com/charmbracelet/bubbletea/blob/v2.0.8/UPGRADE_GUIDE_V2.md)
(fetched 2026-07-10). When in doubt, re-check those sources — do not guess API
names from v1 memory.

## Import paths

```go
import tea "charm.land/bubbletea/v2"   // was github.com/charmbracelet/bubbletea
import "charm.land/lipgloss/v2"        // was github.com/charmbracelet/lipgloss
```

Bubbles components also move to `charm.land/bubbles/v2/...`.

## Core interface (the big change: View returns a struct)

```go
type Model interface {
    Init() tea.Cmd
    Update(msg tea.Msg) (tea.Model, tea.Cmd)
    View() tea.View        // v1 returned string
}

func (m model) View() tea.View {
    v := tea.NewView(content)   // wrap the rendered string
    v.AltScreen = true          // terminal features are declared here now
    v.MouseMode = tea.MouseModeCellMotion
    return v
}
```

## Declarative view fields replace program options AND commands

The paradigm shift: terminal features are no longer imperative
(`NewProgram` options or `tea.EnterAltScreen`-style commands); they are
fields on the returned `tea.View`, re-declared every frame.

| v1 (option or command)                      | v2 (view field)                          |
|---------------------------------------------|------------------------------------------|
| `tea.WithAltScreen()` / `tea.EnterAltScreen` | `v.AltScreen = true`                    |
| `tea.WithMouseCellMotion()` / `EnableMouseCellMotion` | `v.MouseMode = tea.MouseModeCellMotion` |
| `tea.WithMouseAllMotion()`                   | `v.MouseMode = tea.MouseModeAllMotion`  |
| `tea.DisableMouse`                           | `v.MouseMode = tea.MouseModeNone`       |
| `tea.WithReportFocus()`                      | `v.ReportFocus = true`                  |
| `tea.WithoutBracketedPaste()`                | `v.DisableBracketedPasteMode = true`    |
| `tea.HideCursor` / `tea.ShowCursor`          | `v.Cursor = nil` / `&tea.Cursor{...}`   |
| `tea.SetWindowTitle("…")`                    | `v.WindowTitle = "…"`                   |

Removed with no replacement needed: `WithInputTTY()`, `WithANSICompressor()`.
New options: `tea.WithColorProfile(p)`, `tea.WithWindowSize(w, h)`.

## Key handling

- `tea.KeyMsg` is now an **interface** (with a `Key()` method) covering both
  presses and releases. Match on `tea.KeyPressMsg` (and `tea.KeyReleaseMsg`
  when keyboard enhancements are enabled).
- Field renames on the key struct:
  - `msg.Type` → `msg.Code` (a `rune`)
  - `msg.Runes` → `msg.Text` (a `string`)
  - `msg.Alt` → `msg.Mod.Contains(tea.ModAlt)`
- `tea.KeyCtrlC`-style constants are gone: check `msg.Code` + `msg.Mod`,
  or keep using `msg.String()` matching (`"ctrl+c"`, `"up"`, …).
- Space bar `String()` is now `"space"`, not `" "`.
- New fields: `ShiftedCode`, `BaseCode`, `IsRepeat`; also `Keystroke()`.
- Paste is no longer delivered as key messages: handle `tea.PasteMsg`
  (`msg.Content`), `tea.PasteStartMsg`, `tea.PasteEndMsg`.
- Opt-in Kitty-protocol features via `v.KeyboardEnhancements` on the View
  (`ReportEventTypes`, `ReportAlternateKeys`, …); the terminal answers with
  `tea.KeyboardEnhancementsMsg`.

## Mouse handling

- `tea.MouseMsg` is now an **interface**; coordinates via `msg.Mouse().X/.Y`.
- Match on the concrete types: `tea.MouseClickMsg`, `tea.MouseReleaseMsg`,
  `tea.MouseWheelMsg`, `tea.MouseMotionMsg` (no more `msg.Action` checks).
- Button constants renamed: `MouseButtonLeft` → `MouseLeft`,
  `MouseButtonWheelUp` → `MouseWheelUp`, etc.

```go
// v1
case tea.MouseMsg:
    if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft { … }
// v2
case tea.MouseClickMsg:
    if msg.Button == tea.MouseLeft { … }
```

## Other renames / removals

- `p.Start()` / `p.StartReturningModel()` → `p.Run()`
- `tea.Sequentially(...)` → `tea.Sequence(...)`
- `tea.WindowSize()` (Cmd) → `tea.RequestWindowSize` (a Msg)
- New request messages: `RequestCursorPosition`, `RequestBackgroundColor`,
  `RequestTerminalVersion`, … (terminal replies arrive as messages, e.g.
  `tea.BackgroundColorMsg`).
- Clipboard: `tea.SetClipboard(s)` Cmd, `tea.ReadClipboard` Msg.
- Cursor: `tea.NewCursor(x, y)` returns `*tea.Cursor`
  (`Position`, `Color`, `Shape`, `Blink`) assigned to `v.Cursor`.

## How this repo uses v2 (migrated 2026-07-12)

- The shell (`internal/tui`) is the only runtime `tea.Model`: `view.go`'s
  `View() tea.View` declares `AltScreen` / `MouseModeCellMotion` /
  `ReportFocus` in `frame()` and surfaces the active tab's cursor;
  `internal/cli/cli.go` calls plain `tea.NewProgram(m)` with no options.
- Keys: everything matches `tea.KeyPressMsg` with `msg.String()` cases
  (`"ctrl+d"`, `"shift+tab"`, …); synthetic keys are built as
  `tea.KeyPressMsg{Code: tea.KeyDown}` (chat wheel-to-textarea).
- Mouse: `internal/tui/{chat,inspector}/mouse.go` type-switch over
  `tea.MouseWheelMsg` / `MouseClickMsg` / `MouseMotionMsg` /
  `MouseReleaseMsg`; the shell's tab-bar click checks `tea.MouseClickMsg`.
- The real terminal cursor is plumbed through `tabModel.Cursor()
  *tea.Cursor` (`internal/tui/tabs.go`): chat input and prompt fields
  offset their component cursor to screen coordinates; inspector returns nil.
