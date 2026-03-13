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

	// Header
	count := styleBadgeInfo.Render(fmt.Sprintf(" %d/%d ", len(a.modsFiltered), len(a.mods)))
	header := lipgloss.JoinHorizontal(lipgloss.Left,
		styleTitle.Render("  Manage Mods"), "  ", count,
	)

	panelW := clamp(64, 40, a.width-4)
	panelX := (a.width - panelW) / 2 // left edge of panel when centred

	// Search row — "/ <input>  [+]"
	// Reserve 5 chars on right for " [+] "
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

	const reservedRows = 11
	listH := a.height - reservedRows
	if listH < 4 {
		listH = 4
	}

	// Compute y positions relative to top of screen.
	// Place uses lipgloss.Top so content starts at y=0 with no top padding from Place.
	// Actual content top = (height-1 - contentH) / 2 for lipgloss.Center vertically,
	// but we use lipgloss.Top so contentY = 0.
	headerH := lipgloss.Height(header)
	searchY := headerH + 1 // header + blank line
	listY := searchY + lipgloss.Height(searchRow) + 1 + 1 // search + blank + top border

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
	nameW := innerW - delW - 1 // 1 space between name and button

	for i, mod := range a.modsFiltered {
		name := truncate(mod.Name, nameW)
		pad := strings.Repeat(" ", nameW-lipgloss.Width(name))
		del := styleDeleteBtn.Render(delStr)

		// Register − click zone
		delX := panelX + innerW - delW + 2
		a.clickZones = append(a.clickZones, clickZone{
			x: delX, y: listY + i, w: delW, h: 1,
			action: fmt.Sprintf("del:%d", i),
		})

		if i == a.modsIdx {
			rows = append(rows, styleModItemSelected.Render(name+pad)+" "+del)
		} else {
			rows = append(rows, styleModItem.Render(name+pad)+" "+del)
		}
	}

	start, end := visibleWindow(a.modsIdx, len(rows), listH)
	visible := rows[start:end]

	// Set explicit height so the panel never grows beyond the terminal.
	panel := stylePanelFocused.Width(panelW).Height(listH).Render(strings.Join(visible, "\n"))

	content := lipgloss.JoinVertical(lipgloss.Left,
		header, "", searchRow, "", panel,
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
	return styleModal.Render(lipgloss.JoinVertical(lipgloss.Left,
		styleModalTitle.Render("  ◈ Add Mod from Modrinth"),
		styleSubtitle.Render("  Enter a mod slug or name"),
		"",
		styleModalInput.Render(a.addModInput.View()),
		"",
		styleSubtitle.Render("  enter to add  ·  esc to cancel"),
	))
}

// renderWithModal overlays a modal centred on top of bg.
func (a *App) renderWithModal(bg, modal string) string {
	bgLines := strings.Split(bg, "\n")
	modalLines := strings.Split(modal, "\n")

	bgW := a.width
	bgH := len(bgLines)
	modalW := lipgloss.Width(modal)
	modalH := len(modalLines)

	x := (bgW - modalW) / 2
	y := (bgH - modalH) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	for i, ml := range modalLines {
		li := y + i
		for len(bgLines) <= li {
			bgLines = append(bgLines, "")
		}
		line := bgLines[li]
		// Pad line to bgW
		lineW := lipgloss.Width(line)
		if lineW < bgW {
			line += strings.Repeat(" ", bgW-lineW)
		}
		// We can't cleanly splice ANSI strings by rune offset, so just
		// build: left-of-x plain spaces + modal line + ignore rest.
		prefix := strings.Repeat(" ", x)
		bgLines[li] = prefix + ml
	}
	return strings.Join(bgLines, "\n")
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
