---
name: charmbracelet-lipgloss-v2
description: Lipgloss v2 (charm.land/lipgloss/v2) API reference and v1→v2 migration guide, companion to [[charmbracelet-bubbletea-v2]]. Use when migrating this project to the charm.land v2 stack or writing new Lipgloss v2 code. IMPORTANT - this repo migrated to v2 on 2026-07-12: charm.land v2 APIs are the norm here; do NOT reintroduce v1 (github.com/charmbracelet/*) imports or APIs.
---

# Lipgloss v2

Verified against `charm.land/lipgloss/v2` docs (fetched 2026-07-10). When in
doubt, re-check pkg.go.dev — do not guess API names from v1 memory.

## Import path

```go
import "charm.land/lipgloss/v2"        // was github.com/charmbracelet/lipgloss
import "charm.land/lipgloss/v2/compat" // v1 migration shims
```

Sub-packages: `…/v2/table`, `…/v2/list`, `…/v2/tree`, `…/v2/compat`.

## The headline change: color handling

v2 removes global color-profile detection and the adaptive color types:

- **`AdaptiveColor` and `CompleteColor` are gone** from the main package
  (kept in `…/v2/compat` as migration shims only).
- **No stdin/stdout sniffing at init.** Terminal queries take explicit
  files: `lipgloss.HasDarkBackground(os.Stdin, os.Stdout) bool`,
  `lipgloss.BackgroundColor(in, out) (color.Color, error)`.
- **Runtime selection replaces adaptive types:**

```go
lightDark := lipgloss.LightDark(hasDarkBG)          // LightDarkFunc
c := lightDark(lipgloss.Color("#D7FFAE"), lipgloss.Color("#D75FEE"))
// per-profile control:
complete := lipgloss.Complete(profile)               // CompleteFunc
```

- `lipgloss.Color(s)` still parses hex/ANSI strings (`"#0000FF"`, `"62"`)
  but now returns a standard `color.Color`. There are also 16 ANSI constants
  (`lipgloss.Black` … `lipgloss.BrightWhite`) and manipulation helpers:
  `Darken`, `Lighten`, `Alpha`, `Complementary`, `Blend1D`, `Blend2D`.

### Inside a Bubble Tea v2 program

Downsampling is handled by Bubble Tea's renderer. For background-aware
styles, don't sniff the terminal yourself — ask via message:

```go
func (m model) Init() tea.Cmd { return tea.RequestBackgroundColor }

case tea.BackgroundColorMsg:
    m.styles = newStyles(msg.IsDark())   // rebuild styles once, here
```

This breaks the "package-level styles defined at init" pattern for any style
that needs light/dark variants: such styles must be built after
`BackgroundColorMsg` arrives (fixed-color styles like theme `62` can stay
package-level).

### Outside Bubble Tea (plain printing)

Use the writer functions, which downsample automatically:
`lipgloss.Print/Printf/Println`, `Fprint*` (explicit `io.Writer`),
`Sprint*`, and the global `lipgloss.Writer`.

## What's largely unchanged

`Style` remains an immutable value type with the same core methods:
`Bold/Italic/Underline/…`, `Foreground/Background`, `Padding/Margin/
Width/Height/Border`, `Align*`, `Render(strs ...string) string`, the
`Get*`/`Unset*` families, and `Inherit`. Layout helpers too:
`JoinHorizontal/JoinVertical`, `Place/PlaceHorizontal/PlaceVertical`,
`Width/Height/Size` measurement.

## New in v2 (nice-to-haves)

- **Canvas/layer compositing**: `NewCanvas(w, h)`, `NewLayer(content, …)`,
  `NewCompositor(layers…)` — real overlays instead of string splicing.
- **Border gradients**: `BorderForegroundBlend(colorA, colorB)`.
- **Underline styles**: `UnderlineStyle()` (single/double/curly/dotted/dashed).
- **Hyperlinks**: `Hyperlink(url, …)`; targeted styling via `StyleRanges`/
  `StyleRunes`; `Wrap(s, width, breakpoints)`; `TabWidth(n)` /
  `NoTabConversion`.
- New borders: `MarkdownBorder()`, `ASCIIBorder()`, `BlockBorder()`,
  half-block borders.

## How this repo uses v2 (migrated 2026-07-12)

- `internal/tui/theme` is fixed-color (ANSI 256 strings via
  `lipgloss.Color`), so it ported as-is; nothing adaptive, no background
  query needed.
- v2 emits colors regardless of TTY (downsampling moved into the Bubble
  Tea renderer), so tests assert escape codes directly and strip styling
  with `charmbracelet/x/ansi.Strip` when comparing text —
  `SetColorProfile`/`ColorProfile` no longer exist (see
  `render/render_test.go`, chat's `TestMetricsChip`).

## Canvas/Layer gotcha (learned the hard way, v2.0.5)

Layers only position/stack when drawn through a **Compositor**:

```go
canvas := lipgloss.NewCanvas(w, h)
canvas.Compose(lipgloss.NewCompositor(     // NOT canvas.Compose(layer) per layer!
    lipgloss.NewLayer(content),
    lipgloss.NewLayer(overlay).X(x).Y(y).Z(1),
))
out := canvas.Render()
```

A bare `Layer.Draw` (what `canvas.Compose(layer)` calls) is just a content
stamp at the area origin — X/Y/Z and child layers are all computed by the
Compositor ("All computation related to layers happens in the Compositor").
Composing raw layers one by one makes the later stamp opaquely overwrite the
canvas. See `chat/layout.go overlayBottomRight` for the working pattern.
