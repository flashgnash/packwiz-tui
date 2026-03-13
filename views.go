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
		icon := "   "
		style := styleRepoItem
		if i == a.repoListIdx {
			icon = " " + styleMenuItemIcon.Render("▶") + " "
			style = styleRepoItemSelected
		}
		rows = append(rows,
			style.Render(icon+repo.Name),
			styleRepoPath.Render("     "+truncate(repo.Path, 56)),
			"",
		)
	}

	// Clone new entry
	cloneStyle := styleRepoItem
	cloneIcon := "   "
	if a.repoListIdx == len(a.repoList) {
		cloneIcon = " " + styleMenuItemIcon.Render("▶") + " "
		cloneStyle = styleRepoItemSelected
	}
	rows = append(rows, cloneStyle.Render(cloneIcon+styleAddBtn.Render("+ Clone new repository")))

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
	var rows []string
	rows = append(rows, "")
	for i, item := range mainMenuItems {
		num := styleStatusSep.Render(fmt.Sprintf("[%d]", i+1))
		var line string
		if i == a.menuIdx {
			line = styleMenuItemSelected.Render(styleMenuItemIcon.Render(item.icon) + " " + item.label)
		} else {
			line = styleMenuItem.Render(styleMuted(item.icon) + " " + item.label)
		}
		rows = append(rows, num+" "+line, "")
	}

	panelW := clamp(52, 36, a.width-4)
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
	// Header
	count := styleBadgeInfo.Render(fmt.Sprintf(" %d/%d ", len(a.modsFiltered), len(a.mods)))
	header := lipgloss.JoinHorizontal(lipgloss.Left,
		styleTitle.Render("  Manage Mods"), "  ", count,
	)

	panelW := clamp(64, 40, a.width-4)

	// Search row — input fills panel width minus the "/ " prefix (3 chars)
	searchPrefixStr := " / "
	searchPrefix := styleSearchLabel.Render(searchPrefixStr)
	if a.searchFocus {
		searchPrefix = styleSearchActive.Render(searchPrefixStr)
	}
	a.searchInput.Width = panelW - len(searchPrefixStr) - 2
	searchRow := searchPrefix + a.searchInput.View()

	const reservedRows = 11
	listH := a.height - reservedRows
	if listH < 4 {
		listH = 4
	}

	var rows []string
	if len(a.modsFiltered) == 0 {
		rows = append(rows, styleSubtitle.Render("  no mods found"))
	}
	rowW := panelW - 2 // subtract panel border padding
	for i, mod := range a.modsFiltered {
		del := styleMuted("[-]")
		delW := lipgloss.Width("[-]")
		nameW := rowW - delW - 2
		name := truncate(mod.Name, nameW)
		namePadded := name + strings.Repeat(" ", nameW-lipgloss.Width(name))
		if i == a.modsIdx {
			del = styleDeleteBtn.Render("[-]")
			rows = append(rows, styleModItemSelected.Render(namePadded)+" "+del)
		} else {
			rows = append(rows, styleModItem.Render(namePadded)+" "+del)
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
