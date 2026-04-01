package ui

import "github.com/charmbracelet/lipgloss"

var (
	ColorGreen  = lipgloss.Color("#22c55e")
	ColorYellow = lipgloss.Color("#eab308")
	ColorRed    = lipgloss.Color("#ef4444")
	ColorCyan   = lipgloss.Color("#06b6d4")
	ColorDim    = lipgloss.Color("#6b7280")
	ColorWhite  = lipgloss.Color("#f9fafb")

	StyleCheck = lipgloss.NewStyle().Foreground(ColorGreen).SetString("✓")
	StyleCross = lipgloss.NewStyle().Foreground(ColorRed).SetString("✗")
	StyleSpin  = lipgloss.NewStyle().Foreground(ColorYellow)
	StyleLabel = lipgloss.NewStyle().Width(20)
	StyleValue = lipgloss.NewStyle().Foreground(ColorCyan)
	StyleDim   = lipgloss.NewStyle().Foreground(ColorDim)
	StyleBold  = lipgloss.NewStyle().Bold(true).Foreground(ColorWhite)

	StyleBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorDim).
			Padding(1, 2)
)
