// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package theme holds the TUI's shared lipgloss styles and colors. It is a
// leaf package (no aibench imports) so every tui sub-package can render in
// the same palette without depending on one another. Theme color is 62.
//
// The palette adapts: Apply(isDark) rebuilds every style for a dark or light
// terminal background (the shell calls it once the terminal answers
// tea.RequestBackgroundColor; dark is the default). Components that copy
// styles by value at construction must re-capture them after Apply — the
// shell's applyTheme does.
package theme

import "charm.land/lipgloss/v2"

// The wordmark: block-art AI with "bench" riding the top row (two-row block
// fonts cannot draw a convincing B); the credit line rides the baseline row.
const LogoArt = `▄▀▄ █ bench
█▀█ █`

var (
	FocusedBorder lipgloss.Style
	BlurredBorder lipgloss.Style
	FrameStyle    lipgloss.Style // the blurred-border frame color, for rules that match it
	// Scrollbars: the track sits in the frame color so it reads as part of
	// the border, while the thumb takes the accent — the moving part is the
	// only thing worth the eye.
	ScrollThumbStyle lipgloss.Style
	// PanelTitleStyle names a whole pane — Metrics, Tokens, Headers,
	// Params — the way the header writes a tab that is not the active one:
	// quiet, unfilled. A pane's name is chrome, and chrome that reads as a
	// button invites a click it does not take. TitleStyle heads a group
	// *inside* a pane (Request, Tokens, Pricing) in accent text, so the
	// louder of the two is the one with content under it.
	PanelTitleStyle lipgloss.Style
	TitleStyle      lipgloss.Style
	UserStyle       lipgloss.Style
	// UserRowStyle bands the user's own transcript rows. Authorship there
	// is structural, not a decoration on the first line, so it reads as a
	// block — and unlike a "> " marker it survives wrapping onto
	// continuation lines.
	UserRowStyle lipgloss.Style
	// The transcript's two selection rules, drawn in the same gutter column.
	// SelectionRuleStyle is the heavy accent rule on the request the panels
	// are showing — the composer's own prompt glyph and weight, so a marked
	// block reads the same in both. ExchangeRuleStyle is the light rule on
	// the rest of the exchange that request belongs to: the question, its
	// other rounds, the answer. Weight carries the difference where color
	// cannot, so the two stay apart on a washed-out terminal.
	SelectionRuleStyle   lipgloss.Style
	ExchangeRuleStyle    lipgloss.Style
	AssistantStyle       lipgloss.Style
	SpinnerStyle         lipgloss.Style // streaming spinner: accent, distinct from the green done-dot
	ErrorStyle           lipgloss.Style
	WarningStyle         lipgloss.Style // soft orange: notices, interrupts
	DimStyle             lipgloss.Style
	HelpStyle            lipgloss.Style
	HelpKeyStyle         lipgloss.Style
	HelpDescriptionStyle lipgloss.Style
	SelectionStyle       lipgloss.Style
	NoticeStyle          lipgloss.Style
	// Inspector dump search: every match dim-lit; the focused one uses SelectionStyle.
	SearchMatchStyle lipgloss.Style

	MethodStyle  lipgloss.Style
	MetaKeyStyle lipgloss.Style
	// ControlStyle is a pane's control (copy, raw/render) drawn as a chip.
	ControlStyle lipgloss.Style
	// token total on record lines, slightly emphasized
	UsageTotalStyle lipgloss.Style
	// Tokens graph bar segments: cool blue in vs purple out so the flush
	// boundary reads clearly, staying clear of the amber/red reserved for
	// warning and error statuses.
	TokensInStyle  lipgloss.Style
	TokensOutStyle lipgloss.Style
	// Inspector JSON body tokens
	JSONKeyStyle     lipgloss.Style
	JSONStringStyle  lipgloss.Style
	JSONNumberStyle  lipgloss.Style
	JSONLiteralStyle lipgloss.Style
	OKStatus         lipgloss.Style
	WarningStatus    lipgloss.Style
	ErrorStatus      lipgloss.Style

	// Dark reports whether the last Apply built the dark palette; leaf consumers
	// that render their own colors (e.g. the Prompt tab's glamour markdown) read
	// it to match the background.
	Dark = true
)

func init() { Apply(true) }

// Apply rebuilds the palette for a dark or light terminal background. The
// dark side is the canonical look; the light side swaps the pale grays and
// pastels for darker counterparts that keep contrast on white.
func Apply(isDark bool) {
	Dark = isDark
	ld := lipgloss.LightDark(isDark)
	c := func(light, dark string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(ld(lipgloss.Color(light), lipgloss.Color(dark)))
	}
	accent := lipgloss.Color("62") // holds up on both backgrounds
	frame := ld(lipgloss.Color("248"), lipgloss.Color("240"))

	FocusedBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForegroundBlend(accent, ld(lipgloss.Color("97"), lipgloss.Color("135")))
	BlurredBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(frame)
	FrameStyle = lipgloss.NewStyle().Foreground(frame)
	ScrollThumbStyle = lipgloss.NewStyle().Foreground(accent)
	TitleStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	UserStyle = c("235", "252")
	UserRowStyle = lipgloss.NewStyle().Bold(true).
		Foreground(ld(lipgloss.Color("233"), lipgloss.Color("255")))
	SelectionRuleStyle = lipgloss.NewStyle().Foreground(accent)
	ExchangeRuleStyle = FrameStyle
	AssistantStyle = lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("29"), lipgloss.Color("42")))
	SpinnerStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	ErrorStyle = c("160", "196")
	WarningStyle = c("166", "215")
	DimStyle = c("242", "246")
	PanelTitleStyle = DimStyle // the header's own unselected-tab treatment
	HelpStyle = c("242", "246").Padding(0, 1)
	HelpKeyStyle = c("238", "249")
	HelpDescriptionStyle = c("245", "241")
	SelectionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(accent)
	// A pane's controls (copy, raw/render): a chip, so the eye finds a
	// thing to press rather than a word among words. The fill is the
	// frame's own colour — the control belongs to the chrome, not to the
	// content — and only the label carries ink, which keeps it quieter
	// than the accent pill the active tab wears.
	ControlStyle = lipgloss.NewStyle().
		Foreground(ld(lipgloss.Color("236"), lipgloss.Color("252"))).
		Background(ld(lipgloss.Color("253"), lipgloss.Color("238")))
	NoticeStyle = lipgloss.NewStyle().Foreground(accent)
	SearchMatchStyle = lipgloss.NewStyle().
		Foreground(ld(lipgloss.Color("235"), lipgloss.Color("230"))).
		Background(ld(lipgloss.Color("250"), lipgloss.Color("240")))

	MethodStyle = c("32", "75").Bold(true)
	MetaKeyStyle = c("61", "104")
	UsageTotalStyle = c("238", "250").Bold(true)
	TokensInStyle = c("32", "75")
	TokensOutStyle = c("98", "141")
	JSONKeyStyle = c("30", "80")
	JSONStringStyle = c("235", "252")
	JSONNumberStyle = c("32", "75")
	JSONLiteralStyle = c("172", "214")
	OKStatus = lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("29"), lipgloss.Color("42")))
	WarningStatus = c("172", "214").Bold(true)
	ErrorStatus = c("160", "196").Bold(true)
}
