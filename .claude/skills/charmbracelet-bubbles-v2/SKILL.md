---
name: charmbracelet-bubbles-v2
description: Bubbles v2 (charm.land/bubbles/v2) component reference and v1→v2 migration guide, companion to [[charmbracelet-bubbletea-v2]] and [[charmbracelet-lipgloss-v2]]. Use when migrating this project to the charm.land v2 stack or writing new Bubbles v2 code. IMPORTANT - this repo migrated to v2 on 2026-07-12: charm.land v2 APIs are the norm here; do NOT reintroduce v1 (github.com/charmbracelet/*) imports or APIs.
---

# Bubbles v2

Verified against `charm.land/bubbles/v2` docs and the official
[UPGRADE_GUIDE_V2](https://github.com/charmbracelet/bubbles/blob/v2.1.1/UPGRADE_GUIDE_V2.md)
(fetched 2026-07-10). When in doubt, re-check those sources — do not guess
API names from v1 memory. Bubbles v2 requires Bubble Tea v2 + Lipgloss v2;
the three must be upgraded together.

## Import path

```go
import "charm.land/bubbles/v2/viewport"   // was github.com/charmbracelet/bubbles/viewport
```

Components: cursor, filepicker, help, key, list, paginator, progress,
spinner, stopwatch, table, textarea, textinput, timer, viewport.
`runeutil` and `memoization` are now internal — no longer importable.

## Global patterns (apply across components)

- **Exported size fields → methods** on filepicker, help, progress, table,
  textinput, viewport: `m.Width = 40` → `m.SetWidth(40)`; reading is
  `m.Width()` (now a method call, so every read site changes too).
- **`DefaultKeyMap` var → `DefaultKeyMap()` func** (paginator, textarea,
  textinput).
- **`NewModel` aliases removed** — always `New()`.
- **Colors are `image/color.Color`** (via `lipgloss.Color(...)`), not strings.
- **`isDark bool` parameter** on `DefaultStyles(...)` (help, list, textinput,
  …) because Lipgloss v2 dropped `AdaptiveColor`. Get it from
  `tea.BackgroundColorMsg.IsDark()` (see [[charmbracelet-lipgloss-v2]]), then rebuild
  component styles in `Update`.
- `tea.KeyMsg` → `tea.KeyPressMsg` in any custom `Update` key handling
  (see [[charmbracelet-bubbletea-v2]]).

## Viewport (biggest restructuring)

```go
// v1
vp := viewport.New(80, 24)
vp.Width, vp.Height = w, h
vp.YOffset = 5
// v2
vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(24))
vp.SetWidth(w); vp.SetHeight(h)      // read via vp.Width(), vp.Height()
vp.SetYOffset(5)                      // read via vp.YOffset()
```

- `HighPerformanceRendering` removed.
- New (additive): `SoftWrap`, `LeftGutterFunc`, per-line `StyleLineFunc`,
  highlights (`SetHighlights`/`HighlightNext`/`HighlightPrevious`/
  `ClearHighlights`), `SetContentLines([]string)`, `GetContent()`,
  horizontal scrolling.

## Textarea / Textinput

- **Style fields consolidated into a `Styles` struct** with `Focused` /
  `Blurred` states:

```go
// v1
ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
ta.FocusedStyle.Prompt = promptStyle
// v2: ta.Styles.Focused / ta.Styles.Blurred (StyleState)
// textinput: build via textinput.DefaultStyles(isDark), then ti.SetStyles(s)
//   (PromptStyle→Focused.Prompt, TextStyle→Focused.Text,
//    PlaceholderStyle→Focused.Placeholder, CompletionStyle→Focused.Suggestion)
```

- **Cursor is virtual now**: `Model.Cursor` (a `cursor.Model`) →
  `Model.Cursor()` returning `*tea.Cursor`, which you assign to
  `view.Cursor` in `View()` so the terminal's real cursor is used;
  `SetVirtualCursor(bool)` opts back into a drawn cursor. The v1
  `textarea.Blink` / cursor-blink command pattern goes away with it.
- textinput width: `ti.Width = 40` → `ti.SetWidth(40)`.
- textarea: `SetCursor(col)` → `SetCursorColumn(col)`; new `Column()`,
  `ScrollYOffset()`, `ScrollPosition()`, `MoveToBeginning()`, `MoveToEnd()`;
  KeyMap gains `PageUp`/`PageDown`.

## Other components (quick table)

| Component | v1 → v2 |
|-----------|---------|
| spinner | `spinner.Tick()` (pkg func) → `model.Tick()` (method) |
| cursor | `Model.Blink` field → `Model.IsBlinked`; `BlinkCmd()` → `Blink()` |
| timer | `NewWithInterval(d, i)` → `New(d, timer.WithInterval(i))` |
| stopwatch | `NewWithInterval(i)` → `New(stopwatch.WithInterval(i))` |
| progress | `WithGradient(a,b)` → `WithColors(...)`; `WithDefaultGradient()` → `WithDefaultBlend()`; `WithSolidFill(s)` → `WithColors(c)`; `WithScaledGradient` → `WithColors(...)+WithScaled(true)`; `FullColor`/`EmptyColor` now `color.Color`; new `WithColorFunc` |
| list | `Styles.FilterPrompt`+`FilterCursor` → `Styles.Filter` (a `textinput.Styles`); `DefaultStyles(isDark)`, `NewDefaultItemStyles(isDark)` |
| paginator | `UsePgUpPgDownKeys`/`UseLeftRightKeys`/`Use*Keys` removed — edit `KeyMap` directly |
| help | `NewModel()` → `New()`; `DefaultStyles(isDark)` / `DefaultDarkStyles()` / `DefaultLightStyles()` |
| filepicker | `DefaultStylesWithRenderer(r)` → `DefaultStyles()` |
| table | size fields → methods; `Update` returns `Model`, not `tea.Model` |

## How this repo uses v2 (migrated 2026-07-12)

- Viewports (`chat` chatView/band, `inspector` dump/headers) size via
  `SetWidth`/`SetHeight` and read via `Width()`/`Height()`/`YOffset()`.
  Gotcha: `SetYOffset` clamps against real content now — tests must
  `SetContent` before setting an offset (see chat's `TestPosIn`).
- Textareas (`chat/composer.go`, `prompt/model.go`) build styles from
  `textarea.DefaultDarkStyles()` and apply with `SetStyles` (the no-bright
  cursor-line + themed-prompt overrides live there); `textarea.Blink` is
  gone — the real cursor comes from `Model.Cursor()` via the shell.
- Textinputs (`prompt.buildParamInputs`) use `SetWidth`.
- The spinner is unchanged (`spin.Tick` method value is still a `tea.Cmd`);
  help sets width via `SetWidth` and keeps per-field `Styles` overrides.
- ntcharts had no place in v2 — the Tokens bar chart is hand-rolled in
  `internal/tui/chat/chart.go` (`drawTokenBars`).
