package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorPrimary = lipgloss.Color("#7C7FEB") // yazi-adjacent purple
	colorMuted   = lipgloss.Color("#888888")
	colorStar    = lipgloss.Color("#F5C842")
	colorGood    = lipgloss.Color("#A8CC8C")
	colorBad     = lipgloss.Color("#E88388")
	colorDim     = lipgloss.Color("#555555")

	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	styleCursor = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleMuted = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleStar = lipgloss.NewStyle().
			Foreground(colorStar)

	styleGood = lipgloss.NewStyle().
			Foreground(colorGood)

	styleBad = lipgloss.NewStyle().
			Foreground(colorBad)

	styleDivider = lipgloss.NewStyle().
			Foreground(colorDim)

	styleLog = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleLogTime = lipgloss.NewStyle().
			Foreground(colorDim)
)
