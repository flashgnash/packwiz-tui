package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ── Loading ───────────────────────────────────────────────────────────────────

func (a *App) viewLoading() string {
	spinner := styleLoader.Render(spinnerFrames[a.spinFrame])
	content := lipgloss.JoinVertical(lipgloss.Center,
		renderLogo(),
		"",
		spinner+" "+styleSubtitle.Render(a.loadingMsg),
	)
	return lipgloss.Place(a.width, a.height-1, lipgloss.Center, lipgloss.Center, content)
}

// ── Repo select ───────────────────────────────────────────────────────────────

func (a *App) viewRepoSelect() string {
	var rows []string
	rows = append(rows, styleTitle.Render("  Recent Repositories"), "")

	for i, repo := range a.repoList {
		if i == a.repoListIdx {
			rows = append(rows,
				styleRepoItemSelected.Render(" ▶  "+repo.Name),
				styleRepoPath.Render("     "+truncate(repo.Path, 56)),
				"",
			)
		} else {
			rows = append(rows,
				styleRepoItem.Render("    "+repo.Name),
				styleRepoPath.Render("     "+truncate(repo.Path, 56)),
				"",
			)
		}
	}

	// Clone new entry
	if a.repoListIdx == len(a.repoList) {
		rows = append(rows, styleRepoItemSelected.Render(" ▶  + Clone new repository"))
	} else {
		rows = append(rows, styleRepoItem.Render("    ")+styleAddBtn.Render("+ Clone new repository"))
	}

	panelW := clamp(70, 40, a.width-4)
	panel := stylePanelFocused.Width(panelW).Render(strings.Join(rows, "\n"))

	content := lipgloss.JoinVertical(lipgloss.Center,
		renderLogo(),
		styleLogoSub.Render("  Minecraft Modpack Manager"),
		"",
		panel,
	)
	return lipgloss.Place(a.width, a.height-1, lipgloss.Center, lipgloss.Center, content)
}

// ── Clone repo ────────────────────────────────────────────────────────────────

func (a *App) viewCloneRepo() string {
	errLine := ""
	if a.cloneError != "" {
		errLine = "\n" + styleOutputError.Render("  ✗ "+a.cloneError)
	}
	panelW := clamp(64, 40, a.width-4)
	panel := stylePanelFocused.Width(panelW).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			styleTitle.Render("  Clone Repository"),
			styleSubtitle.Render("  Enter a git URL to clone"),
			"",
			styleSearchLabel.Render("  URL: ")+a.cloneInput.View(),
			errLine,
			"",
			styleSubtitle.Render("  enter to clone  ·  esc to go back"),
		),
	)
	return lipgloss.Place(a.width, a.height-1, lipgloss.Center, lipgloss.Center, panel)
}

// ── Main menu ─────────────────────────────────────────────────────────────────

func (a *App) viewMainMenu() string {
	a.clickZones = nil

	// Calculate vertical centre offset so we can register click zones.
	// Logo(6) + subtitle(1) + packname(1) + path(1) + blank(1) = 10 lines above panel.
	// Panel has 1 top border + 1 blank row before items.
	logoAndHeaderH := 10
	panelW := clamp(52, 36, a.width-4)
	panelX := (a.width - panelW) / 2
	contentH := logoAndHeaderH + 2 + len(mainMenuItems)*2 + 2 // rough
	topY := (a.height - 1 - contentH) / 2
	if topY < 0 {
		topY = 0
	}
	// First item row = topY + logoAndHeaderH + 2 (border + blank)
	firstItemY := topY + logoAndHeaderH + 2

	var rows []string
	rows = append(rows, "")
	for i, item := range mainMenuItems {
		num := styleStatusSep.Render(fmt.Sprintf("[%d]", i+1))
		var line string
		if i == a.menuIdx {
			line = styleMenuItemSelected.Render(" " + item.icon + "  " + item.label + " ")
		} else {
			line = styleMenuItem.Render(" " + item.icon + "  " + item.label + " ")
		}
		rows = append(rows, num+" "+line, "")

		// Each item occupies 2 rows (item + blank), register click on item row.
		itemY := firstItemY + i*2
		a.clickZones = append(a.clickZones, clickZone{
			x: panelX, y: itemY, w: panelW, h: 1,
			action: fmt.Sprintf("menu:%d", i),
		})
	}

	panel := stylePanelFocused.Width(panelW).Render(strings.Join(rows, "\n"))
	content := lipgloss.JoinVertical(lipgloss.Center,
		renderLogo(),
		"  "+styleBadge.Render(" "+a.packName+" "),
		styleSubtitle.Render("  "+a.packDir),
		"",
		panel,
	)
	return lipgloss.Place(a.width, a.height-1, lipgloss.Center, lipgloss.Center, content)
}

// ── Manage mods ───────────────────────────────────────────────────────────────

func (a *App) viewManageMods() string {
	a.clickZones = nil // reset each render

	panelW := clamp(64, 40, a.width-4)
	panelX := (a.width - panelW) / 2 // left edge of panel when centred

	// Search row at top — "/ <input>  [+]"
	const addStr = " + "
	addBtnStr := styleAddBtn.Render(addStr)
	searchPrefixStr := " / "
	searchPrefix := styleSearchLabel.Render(searchPrefixStr)
	if a.searchFocus {
		searchPrefix = styleSearchActive.Render(searchPrefixStr)
	}
	// innerW of panel = panelW - 4 (borders + padding)
	innerW := panelW - 4
	a.searchInput.Width = innerW - len(searchPrefixStr) - len(addStr)
	gap := strings.Repeat(" ", innerW-len(searchPrefixStr)-a.searchInput.Width-len(addStr))
	searchRow := searchPrefix + a.searchInput.View() + gap + addBtnStr

	addBtnX := panelX + panelW - len(addStr) - 2

	// Count subtitle with git stats: "X mods  +Y -Z"
	modCount := fmt.Sprintf("%d mods", len(a.modsFiltered))
	additions := len(a.modsAdded)
	deletions := len(a.modsDeleted)
	gitStats := ""
	if additions > 0 {
		gitStats += lipgloss.NewStyle().Foreground(colorSuccess).Render(fmt.Sprintf("+%d", additions))
	}
	if deletions > 0 {
		if gitStats != "" {
			gitStats += " "
		}
		gitStats += lipgloss.NewStyle().Foreground(colorDanger).Render(fmt.Sprintf("-%d", deletions))
	}
	subtitle := styleSubtitle.Render("  "+modCount) + "  " + gitStats

	const reservedRows = 11
	listH := a.height - reservedRows
	if listH < 4 {
		listH = 4
	}

	// Compute y positions relative to top of screen.
	// Place uses lipgloss.Top so content starts at y=0 with no top padding from Place.
	searchY := 0 // search at very top
	subtitleY := searchY + lipgloss.Height(searchRow)
	listY := subtitleY + lipgloss.Height(subtitle) + 1 // subtitle + blank + top border

	a.clickZones = append(a.clickZones, clickZone{
		x: addBtnX, y: searchY, w: len(addStr), h: 1,
		action: "add_mod",
	})

	const delStr = " − "
	delW := len(delStr) // 3 chars, no ANSI

	var rows []string
	if len(a.modsFiltered) == 0 {
		rows = append(rows, styleSubtitle.Render("  no mods found"))
	}
	// panelW includes borders(2) and padding(1 each side) = 4
	// Reserve 2 chars for status indicators "M " or "D "
	const statusIndicatorW = 2
	nameW := innerW - delW - statusIndicatorW - 1 // 1 space between name and button

	for i, mod := range a.modsFiltered {
		isDeleted := a.modsDeleted[mod.Path]
		isModified := a.modsModified[mod.Path]
		isAdded := a.modsAdded[mod.Path]

		// Status indicators (A for added, M for modified, D for deleted) - rendered separately
		var statusIndicator string
		if isDeleted {
			statusIndicator = styleDeleteBtn.Render("D") + " "
		} else if isAdded {
			statusIndicator = lipgloss.NewStyle().Foreground(colorSuccess).Render("A") + " "
		} else if isModified {
			statusIndicator = styleHighlight.Render("M") + " "
		} else {
			statusIndicator = "  "
		}

		name := truncate(mod.Name, nameW)
		pad := strings.Repeat(" ", nameW-lipgloss.Width(name))

		var del string
		if isDeleted {
			del = styleAddBtn.Render(" + ")
		} else {
			del = styleDeleteBtn.Render(delStr)
		}

		// Apply styling to name only, keep indicator separate
		var nameLine string
		if i == a.modsIdx {
			// Selected item - use selected style even if deleted
			nameLine = styleModItemSelected.Render(name + pad)
		} else if isDeleted {
			// Not selected but deleted - use deleted style
			nameLine = styleModItemDeleted.Render(name + pad)
		} else {
			// Normal item
			nameLine = styleModItem.Render(name + pad)
		}

		// Combine: indicator (unstylied) + styled name + button
		rows = append(rows, statusIndicator+nameLine+" "+del)
	}

	start, end := visibleWindow(a.modsIdx, len(rows), listH)
	visible := rows[start:end]

	// Register click zones only for visible items
	delX := panelX + innerW - delW + 2
	for row := 0; row < len(visible); row++ {
		absoluteIdx := start + row
		if absoluteIdx < len(a.modsFiltered) {
			a.clickZones = append(a.clickZones, clickZone{
				x: delX, y: listY + row, w: delW, h: 1,
				action: fmt.Sprintf("del:%d", absoluteIdx),
			})
		}
	}

	// Set explicit height so the panel never grows beyond the terminal.
	panel := stylePanelFocused.Width(panelW).Height(listH).Render(strings.Join(visible, "\n"))

	content := lipgloss.JoinVertical(lipgloss.Left,
		searchRow, subtitle, "", panel,
	)

	if a.addModModal {
		return a.renderWithModal(
			lipgloss.Place(a.width, a.height-1, lipgloss.Center, lipgloss.Top, content),
			a.viewAddModModal(),
		)
	}

	return lipgloss.Place(a.width, a.height-1, lipgloss.Center, lipgloss.Top, content)
}

func (a *App) viewAddModModal() string {
	title := "◈ Add Mod from Modrinth"
	subtitle := "Enter a mod slug or name"
	hint := "enter to add  ·  esc to cancel"

	// Use muted style without padding for modal text
	modalText := lipgloss.NewStyle().Foreground(colorMuted)

	content := lipgloss.JoinVertical(lipgloss.Left,
		styleModalTitle.Render(title),
		"",
		modalText.Render(subtitle),
		"",
		a.addModInput.View(),
		"",
		modalText.Render(hint),
	)
	return styleModal.Render(content)
}

// renderWithModal overlays a modal centred on top of bg.
func (a *App) renderWithModal(bg, modal string) string {
	bgLines := strings.Split(bg, "\n")
	modalLines := strings.Split(modal, "\n")

	bgH := len(bgLines)
	modalW := lipgloss.Width(modalLines[0]) // use first line width
	modalH := len(modalLines)

	x := (a.width - modalW) / 2
	y := (bgH - modalH) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	// Create output lines by overlaying modal on background
	result := make([]string, bgH)
	for i := 0; i < bgH; i++ {
		modalLineIdx := i - y
		if modalLineIdx >= 0 && modalLineIdx < modalH {
			// This line has modal content
			bgLine := ""
			if i < len(bgLines) {
				bgLine = bgLines[i]
			}

			// Get background content before and after modal
			before := strings.Repeat(" ", x)
			modalLine := modalLines[modalLineIdx]
			after := ""

			// Preserve background content after the modal
			bgWidth := lipgloss.Width(bgLine)
			modalEnd := x + lipgloss.Width(modalLine)
			if bgWidth > modalEnd {
				// Extract the part of the background after the modal
				// This is tricky with ANSI codes, so we'll just pad with spaces
				after = strings.Repeat(" ", bgWidth-modalEnd)
			}

			result[i] = before + modalLine + after
		} else {
			// No modal content, use background as-is
			if i < len(bgLines) {
				result[i] = bgLines[i]
			} else {
				result[i] = ""
			}
		}
	}
	return strings.Join(result, "\n")
}

// ── Manage loader ─────────────────────────────────────────────────────────────

func (a *App) viewManageLoader() string {
	rows := []string{
		styleSubtitle.Render("  Loader management via packwiz CLI:"),
		"",
		styleMenuItem.Render(styleMuted("◈") + " packwiz fabric install"),
		styleMenuItem.Render(styleMuted("◈") + " packwiz forge install"),
		styleMenuItem.Render(styleMuted("◈") + " packwiz neoforge install"),
		styleMenuItem.Render(styleMuted("◈") + " packwiz quilt install"),
		"",
		styleSubtitle.Render("  (Run these manually in your pack directory)"),
		"",
		styleSubtitle.Render("  esc to go back"),
	}
	panelW := clamp(56, 36, a.width-4)
	panel := stylePanelFocused.Width(panelW).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			styleTitle.Render("  Manage Loader"), "",
			strings.Join(rows, "\n"),
		),
	)
	return lipgloss.Place(a.width, a.height-1, lipgloss.Center, lipgloss.Center, panel)
}

// ── Output ────────────────────────────────────────────────────────────────────

func (a *App) viewOutput() string {
	var lines []string
	if !a.outputDone {
		spinner := styleLoader.Render(spinnerFrames[a.spinFrame])
		lines = append(lines, spinner+" "+styleSubtitle.Render("Running…"))
	} else {
		for _, l := range a.outputLines {
			if l == "" {
				continue
			}
			if a.outputErr {
				lines = append(lines, styleOutputError.Render(l))
			} else {
				lines = append(lines, styleOutputSuccess.Render(l))
			}
		}
		lines = append(lines, "")
		if a.outputErr {
			lines = append(lines, styleOutputError.Render("✗ Command failed"))
		} else {
			lines = append(lines, styleOutputSuccess.Render("✓ Done"))
		}
		lines = append(lines, "", styleSubtitle.Render("  press enter or q to continue"))
	}

	panelW := clamp(72, 40, a.width-4)
	panelH := clamp(a.height-6, 4, 30)
	panel := stylePanelFocused.Width(panelW).Height(panelH).Render(strings.Join(lines, "\n"))

	return lipgloss.Place(a.width, a.height-1, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Left, styleTitle.Render("  Output"), "", panel),
	)
}

// ── Interactive ───────────────────────────────────────────────────────────────

func (a *App) viewInteractive() string {
	var rows []string
	rows = append(rows, "")
	rows = append(rows, styleSubtitle.Render("  "+a.interactivePrompt))
	rows = append(rows, "")

	for i, opt := range a.interactiveOptions {
		num := fmt.Sprintf("[%d]", i+1)
		var line string
		if i == a.interactiveSelected {
			line = styleMenuItemSelected.Render(" " + num + " " + opt)
		} else {
			line = styleMenuItem.Render(" " + num + " " + opt)
		}
		rows = append(rows, line)
	}

	panelW := clamp(64, 40, a.width-4)
	panel := stylePanelFocused.Width(panelW).Render(strings.Join(rows, "\n"))

	return lipgloss.Place(a.width, a.height-1, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Left,
			styleTitle.Render("  Multiple Options Found"),
			"",
			panel,
		),
	)
}
