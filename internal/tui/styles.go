package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorFg     = lipgloss.Color("#E5E7EB")
	colorMuted  = lipgloss.Color("#9CA3AF")
	colorBorder = lipgloss.Color("#374151")
	colorBlue   = lipgloss.Color("#60A5FA")
	colorGreen  = lipgloss.Color("#34D399")
	colorYellow = lipgloss.Color("#FBBF24")
	colorRed    = lipgloss.Color("#F87171")
	colorBgSoft = lipgloss.Color("#111827")

	appStyle = lipgloss.NewStyle().Foreground(colorFg)

	headerBoxStyle = lipgloss.NewStyle().Padding(0, 1)

	headerTitleStyle = lipgloss.NewStyle().
				Foreground(colorBlue).
				Bold(true)

	headerVersionStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	labelStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Width(10)

	valueStyle = lipgloss.NewStyle().Foreground(colorFg)

	urlStyle = lipgloss.NewStyle().
			Foreground(colorBlue).
			Bold(true)

	statusConnected = lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true)

	statusAnonymous = lipgloss.NewStyle().
			Foreground(colorYellow)

	statusReserved = lipgloss.NewStyle().
			Foreground(colorBlue)

	metricStyle = lipgloss.NewStyle().Foreground(colorFg)

	tableFrameStyle = lipgloss.NewStyle().Padding(0, 1)

	tableHeaderStyle = lipgloss.NewStyle().
				Foreground(colorMuted).
				Bold(true)

	tableCellStyle = lipgloss.NewStyle().
			Foreground(colorFg)

	tableSelectedStyle = lipgloss.NewStyle().
				Foreground(colorFg).
				Background(colorBgSoft).
				Bold(true)

	emptyStateStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(1, 1)

	detailFrameStyle = lipgloss.NewStyle().Padding(0, 1)

	detailHeaderStyle = lipgloss.NewStyle().
				Foreground(colorBlue).
				Bold(true)

	detailSectionStyle = lipgloss.NewStyle().
				Foreground(colorMuted).
				Bold(true)

	detailKeyStyle = lipgloss.NewStyle().Foreground(colorMuted)

	detailValStyle = lipgloss.NewStyle().Foreground(colorFg)

	helpBarStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorFg).
			Bold(true)

	dividerStyle = lipgloss.NewStyle().Foreground(colorBorder)
)

func statusCodeStyle(code int) lipgloss.Style {
	switch {
	case code >= 500:
		return lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	case code >= 400:
		return lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	case code >= 300:
		return lipgloss.NewStyle().Foreground(colorYellow)
	default:
		return lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	}
}

func methodStyle(method string) lipgloss.Style {
	switch method {
	case "GET":
		return lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	case "POST":
		return lipgloss.NewStyle().Foreground(colorBlue).Bold(true)
	case "PUT", "PATCH":
		return lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	case "DELETE":
		return lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(colorFg).Bold(true)
	}
}
