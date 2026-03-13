package main

import "github.com/charmbracelet/lipgloss"

// Colour palette — forge dark with molten orange accents.
var (
	colorBg        = lipgloss.Color("#0d0f14")
	colorBgPanel   = lipgloss.Color("#141720")
	colorBgHover   = lipgloss.Color("#1c2030")
	colorBorder    = lipgloss.Color("#2a3045")
	colorBorderFoc = lipgloss.Color("#ff6b2b")
	colorAccent    = lipgloss.Color("#ff6b2b")
	colorAccent2   = lipgloss.Color("#ffab76")
	colorText      = lipgloss.Color("#e8eaf0")
	colorMuted     = lipgloss.Color("#5a6380")
	colorDanger    = lipgloss.Color("#ff4444")
	colorSuccess   = lipgloss.Color("#44cc88")
	colorInfo      = lipgloss.Color("#5599ff")
)

var (
	styleBase = lipgloss.NewStyle().
			Background(colorBg).
			Foreground(colorText)

	styleTitle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true).
			PaddingLeft(1)

	styleSubtitle = lipgloss.NewStyle().
			Foreground(colorMuted).
			PaddingLeft(1)

	stylePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	stylePanelFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBorderFoc).
				Padding(0, 1)

	styleMenuItem = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleMenuItemSelected = lipgloss.NewStyle().
				Foreground(colorAccent).
				Background(colorBgHover).
				Bold(true)

	styleMenuItemIcon = lipgloss.NewStyle().
				Foreground(colorAccent).
				PaddingRight(1)

	styleModItem = lipgloss.NewStyle().
			Foreground(colorText).
			PaddingLeft(1)

	styleModItemSelected = lipgloss.NewStyle().
				Foreground(colorAccent).
				Background(colorBgHover).
				Bold(true).
				PaddingLeft(1)

	styleDeleteBtn = lipgloss.NewStyle().
			Foreground(colorDanger).
			Bold(true)

	styleSearchLabel = lipgloss.NewStyle().
				Foreground(colorMuted)

	styleSearchActive = lipgloss.NewStyle().
				Foreground(colorAccent)

	styleAddBtn = lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	styleStatusBar = lipgloss.NewStyle().
			Background(colorBgPanel).
			Foreground(colorMuted)

	styleStatusKey = lipgloss.NewStyle().
			Foreground(colorAccent2).
			Bold(true)

	styleStatusSep = lipgloss.NewStyle().
			Foreground(colorBorder)

	styleModal = lipgloss.NewStyle().
			Background(colorBgPanel).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorderFoc).
			Padding(1, 2)

	styleModalTitle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true).
			MarginBottom(1)

	styleModalInput = lipgloss.NewStyle().
			Foreground(colorText).
			Background(colorBgHover).
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1).
			Width(40)

	styleLogo = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	styleLogoSub = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleRepoItem = lipgloss.NewStyle().
			Foreground(colorText)

	styleRepoItemSelected = lipgloss.NewStyle().
				Foreground(colorAccent).
				Background(colorBgHover).
				Bold(true)

	styleRepoPath = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleBadge = lipgloss.NewStyle().
			Foreground(colorBg).
			Background(colorAccent).
			Padding(0, 1).
			Bold(true)

	styleBadgeInfo = lipgloss.NewStyle().
			Foreground(colorBg).
			Background(colorInfo).
			Padding(0, 1).
			Bold(true)

	styleOutput = lipgloss.NewStyle().
			Foreground(colorMuted).
			PaddingLeft(1)

	styleOutputSuccess = lipgloss.NewStyle().
				Foreground(colorSuccess).
				PaddingLeft(1)

	styleOutputError = lipgloss.NewStyle().
				Foreground(colorDanger).
				PaddingLeft(1)

	styleLoader = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	styleDivider = lipgloss.NewStyle().
			Foreground(colorBorder)

	styleHighlight = lipgloss.NewStyle().
			Foreground(colorAccent2).
			Bold(true)
)

// spinnerFrames for the loading animation.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// renderLogo returns the ASCII art header.
func renderLogo() string {
	art := "  ██████╗  █████╗  ██████╗██╗  ██╗██╗    ██╗██╗███████╗\n" +
		"  ██╔══██╗██╔══██╗██╔════╝██║ ██╔╝██║    ██║██║╚══███╔╝\n" +
		"  ██████╔╝███████║██║     █████╔╝ ██║ █╗ ██║██║  ███╔╝ \n" +
		"  ██╔═══╝ ██╔══██║██║     ██╔═██╗ ██║███╗██║██║ ███╔╝  \n" +
		"  ██║     ██║  ██║╚██████╗██║  ██╗╚███╔███╔╝██║███████╗\n" +
		"  ╚═╝     ╚═╝  ╚═╝ ╚═════╝╚═╝  ╚═╝ ╚══╝╚══╝ ╚═╝╚══════╝"
	return styleLogo.Render(art)
}
