package main

import "github.com/charmbracelet/lipgloss"

// Colour palette — material-monokai house theme (see /etc/nixos/STYLE.md):
// dark teal surfaces, a single lime accent.
var (
	colorBg        = lipgloss.Color("#192227") // cBg — window background
	colorBgPanel   = lipgloss.Color("#263238") // cPanel — raised panel / button face
	colorBgInset   = lipgloss.Color("#11181c") // cInset — sunken areas / inputs
	colorBgHover   = lipgloss.Color("#314048") // cHover — hover fill
	colorBorder    = lipgloss.Color("#3a4a52") // cBorder
	colorBorderFoc = lipgloss.Color("#9dff00") // focused border = the accent
	colorAccent    = lipgloss.Color("#9dff00") // cAccent — THE accent (lime)
	colorAccent2   = lipgloss.Color("#8db946") // cAccentDark — pressed / secondary
	colorText      = lipgloss.Color("#ffffff") // cText
	colorMuted     = lipgloss.Color("#8f989d") // cTextDim (≈55% white over cBg)
	colorDanger    = lipgloss.Color("#bf616a") // cCritical
	colorWarning   = lipgloss.Color("#cc7a00") // cWarning
	colorSuccess   = lipgloss.Color("#9dff00") // one accent — success is lime too
	colorInfo      = lipgloss.Color("#00b4d8") // svPalette blue
	colorOnAccent  = lipgloss.Color("#1a1a1a") // label colour on an accent fill
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

	styleModItemDeleted = lipgloss.NewStyle().
				Foreground(colorMuted).
				Strikethrough(true).
				PaddingLeft(1)

	styleDeleteBtn = lipgloss.NewStyle().
			Foreground(colorBg).
			Background(colorDanger).
			Bold(true)

	styleSearchLabel = lipgloss.NewStyle().
				Foreground(colorMuted)

	styleSearchActive = lipgloss.NewStyle().
				Foreground(colorAccent)

	styleAddBtn = lipgloss.NewStyle().
			Foreground(colorBg).
			Background(colorSuccess).
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
			Bold(true)

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

	// ── Home dashboard cards ──
	// Cards sit on the dark terminal background with the lighter cPanel fill.
	// Terminal bg resets inside nested styles, so every text style used inside
	// a card carries the card background explicitly; padCard fills the rest.

	styleCardBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder)

	styleCardBorderFoc = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorAccent)

	styleCardFill = lipgloss.NewStyle().
			Background(colorBgPanel)

	styleCardText = lipgloss.NewStyle().
			Foreground(colorText).
			Background(colorBgPanel)

	styleCardMuted = lipgloss.NewStyle().
			Foreground(colorMuted).
			Background(colorBgPanel)

	styleCardAccent = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true).
			Background(colorBgPanel)

	styleCardTitle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true).
			Background(colorBgPanel)

	styleCardTitleDim = lipgloss.NewStyle().
				Foreground(colorMuted).
				Bold(true).
				Background(colorBgPanel)

	// ── Home dashboard ──

	styleHomeTitle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	stylePaneTitle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	stylePaneTitleDim = lipgloss.NewStyle().
				Foreground(colorMuted).
				Bold(true)

	// Button chips — cPanel face; active = accent fill with a dark label
	// (the house MIX-toggle idiom).
	styleBtn = lipgloss.NewStyle().
			Foreground(colorText).
			Background(colorBgPanel).
			Padding(0, 1)

	styleBtnFocused = lipgloss.NewStyle().
			Foreground(colorOnAccent).
			Background(colorAccent).
			Bold(true).
			Padding(0, 1)

	styleBtnDanger = lipgloss.NewStyle().
			Foreground(colorText).
			Background(colorBgPanel).
			Padding(0, 1)

	// Real bordered buttons for the home action panel — cBorder box, accent
	// border + accent label when focused (the house sign-in-form primary idiom).
	styleHomeBtn = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Foreground(colorText).
			Padding(0, 1)

	styleHomeBtnFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorAccent).
				Foreground(colorAccent).
				Bold(true).
				Padding(0, 1)

	styleAgentUser = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	styleAgentText = lipgloss.NewStyle().
			Foreground(colorText)
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
