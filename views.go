package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	ansitruncate "github.com/muesli/reflow/truncate"
	"github.com/muesli/termenv"
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
	a.clickZones = nil
	pw := clamp(64, 40, a.width-8) // card interior width

	// Repo list card.
	var rows []string
	rows = append(rows, styleCardTitle.Render("Recent Repositories"), "")
	for i, repo := range a.repoList {
		if i == a.repoListIdx {
			rows = append(rows,
				styleCardAccent.Render("▸ "+truncate(repo.Name, pw-2)),
				styleCardMuted.Render("  "+truncate(repo.Path, pw-2)),
				"")
		} else {
			rows = append(rows,
				styleCardText.Copy().Bold(true).Render("  "+truncate(repo.Name, pw-2)),
				styleCardMuted.Render("  "+truncate(repo.Path, pw-2)),
				"")
		}
	}
	if len(a.repoList) == 0 {
		rows = append(rows, styleCardMuted.Render("no recent repositories"), "")
	}
	panel := card(rows[:len(rows)-1], pw, a.repoListIdx < len(a.repoList))

	// Clone / Create as bordered buttons below the card.
	makeBtn := func(label string, selected bool) string {
		st := styleCardText
		if selected {
			st = styleCardAccent
		}
		return card([]string{st.Render(label)}, lipgloss.Width(label), selected)
	}
	cloneBtn := makeBtn("+ Clone repository", a.repoListIdx == len(a.repoList))
	createBtn := makeBtn("✦ Create repository", a.repoListIdx == len(a.repoList)+1)
	btnRow := lipgloss.JoinHorizontal(lipgloss.Top, cloneBtn, "  ", createBtn)

	content := lipgloss.JoinVertical(lipgloss.Center,
		renderLogo(),
		styleLogoSub.Render("Minecraft Modpack Manager"),
		"",
		panel,
		"",
		btnRow,
	)

	// Register click zones from the same centering math Place uses.
	cw, ch := lipgloss.Width(content), lipgloss.Height(content)
	cx := maxInt(0, (a.width-cw)/2)
	cy := maxInt(0, (a.height-1-ch)/2)
	logoH := lipgloss.Height(renderLogo()) + 2 // logo + subtitle + blank
	panelX := cx + (cw-lipgloss.Width(panel))/2
	for i := range a.repoList {
		a.clickZones = append(a.clickZones, clickZone{
			x: panelX, y: cy + logoH + 1 + 2 + i*3, w: lipgloss.Width(panel), h: 2,
			action: fmt.Sprintf("repo:%d", i),
		})
	}
	btnY := cy + logoH + lipgloss.Height(panel) + 1
	btnX := cx + (cw-lipgloss.Width(btnRow))/2
	a.clickZones = append(a.clickZones,
		clickZone{x: btnX, y: btnY, w: lipgloss.Width(cloneBtn), h: 3, action: "repo:clone"},
		clickZone{x: btnX + lipgloss.Width(cloneBtn) + 2, y: btnY, w: lipgloss.Width(createBtn), h: 3, action: "repo:create"})

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

// ── Create repo ───────────────────────────────────────────────────────────────

func (a *App) viewCreateRepo() string {
	errLine := ""
	if a.createError != "" {
		errLine = "\n" + styleOutputError.Render("  ✗ "+a.createError)
	}

	var privBtn, pubBtn string
	if a.createPrivate {
		privBtn = styleMenuItemSelected.Render("  Private  ")
		pubBtn = styleMenuItem.Render("  Public  ")
	} else {
		privBtn = styleMenuItem.Render("  Private  ")
		pubBtn = styleMenuItemSelected.Render("  Public  ")
	}
	visRow := styleSearchLabel.Render("  Visibility: ") +
		lipgloss.JoinHorizontal(lipgloss.Left, privBtn, "   ", pubBtn)

	panelW := clamp(64, 40, a.width-4)
	panel := stylePanelFocused.Width(panelW).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			styleTitle.Render("  Create Repository"),
			styleSubtitle.Render("  Creates a GitHub repo via gh, then runs packwiz init"),
			"",
			styleSearchLabel.Render("  Name: ")+a.createNameInput.View(),
			"",
			visRow,
			errLine,
			"",
			styleSubtitle.Render("  enter to create  ·  tab to toggle visibility  ·  esc to go back"),
		),
	)
	return lipgloss.Place(a.width, a.height-1, lipgloss.Center, lipgloss.Center, panel)
}

// ── Server address ────────────────────────────────────────────────────────────

func (a *App) viewServerIP() string {
	errLine := ""
	if a.serverIPError != "" {
		errLine = "\n" + styleOutputError.Render("  ✗ "+a.serverIPError)
	}
	panelW := clamp(64, 40, a.width-4)
	panel := stylePanelFocused.Width(panelW).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			styleTitle.Render("  Server Address"),
			styleSubtitle.Render("  Prefilled into the multiplayer list of prism installs"),
			"",
			styleSearchLabel.Render("  Address: ")+a.serverIPInput.View(),
			errLine,
			"",
			styleSubtitle.Render("  enter to save  ·  empty to clear  ·  esc to cancel"),
		),
	)
	return lipgloss.Place(a.width, a.height-1, lipgloss.Center, lipgloss.Center, panel)
}

// ── Main menu ─────────────────────────────────────────────────────────────────

func (a *App) viewMainMenu() string {
	if a.wideHome() {
		return a.viewHome()
	}
	return a.viewMainMenuList()
}

// viewHome renders the full-screen dashboard: header (pack title + server
// address), mods pane on the left, and agent chat with the action buttons
// beneath it on the right.
func (a *App) viewHome() string {
	a.clickZones = nil
	w, bodyH := a.width, a.height-1

	// ── Header: PACK + PATH cards left, IP + LOADER cards right ──
	pencil := styleCardAccent.Render(" ✎")
	// lipgloss v0.10 setters share the underlying rules map — always Copy()
	// before deriving from a package-level style or the original mutates.
	boldText := styleCardText.Copy().Bold(true)

	ipVal := a.serverAddr
	if ipVal == "" {
		ipVal = "(not set)"
	}
	loaderVal := "(unknown)"
	if a.packMeta.Loader != "" {
		loaderVal = strings.TrimSpace(a.packMeta.Loader + " " + a.packMeta.LoaderVer)
		if a.packMeta.Minecraft != "" {
			loaderVal += " minecraft " + a.packMeta.Minecraft
		}
	}

	headerCard := func(label, value string, focused bool) string {
		labelStyle := styleCardTitleDim
		if focused {
			labelStyle = styleCardTitle
		}
		cw := lipgloss.Width(value)
		if lw := lipgloss.Width(label); lw > cw {
			cw = lw
		}
		return card([]string{labelStyle.Render(label), value}, cw, focused)
	}
	packBox := headerCard("PACK", boldText.Render(truncate(a.packName, w/3)), false)

	// Shrink the IP/loader values together until the header row fits.
	var ipBox, loaderBox string
	for cut := 0; ; cut++ {
		iv := truncate(ipVal, maxInt(6, len([]rune(ipVal))-cut))
		lv := truncate(loaderVal, maxInt(6, len([]rune(loaderVal))-cut))
		ipBox = headerCard("IP", boldText.Render(iv)+pencil, a.homeFocus == 3)
		// No pencil on the loader — it can't be edited from here (yet).
		loaderBox = headerCard("LOADER", boldText.Render(lv), a.homeFocus == 4)
		if lipgloss.Width(packBox)+2+lipgloss.Width(ipBox)+lipgloss.Width(loaderBox) <= w ||
			(len([]rune(ipVal))-cut <= 6 && len([]rune(loaderVal))-cut <= 6) {
			break
		}
	}

	// Left group: PACK always; PATH and REPO share whatever width is left.
	leftCards := []string{packBox}
	leftActs := []string{""}
	free := w - lipgloss.Width(packBox) - lipgloss.Width(ipBox) - lipgloss.Width(loaderBox) - 2
	repoDisp := strings.TrimPrefix(repoWebURL(a.repoRemote), "https://")
	linkArrow := styleCardAccent.Render(" ↗")
	switch {
	case repoDisp != "" && free-12 >= 16:
		avail := free - 12 // both cards' chrome + gaps + the link arrow
		pf, rf := len([]rune(a.packDir)), len([]rune(repoDisp))
		if pf+rf > avail {
			half := avail / 2
			switch {
			case pf <= half:
				rf = avail - pf
			case rf <= half:
				pf = avail - rf
			default:
				pf, rf = half, avail-half
			}
		}
		leftCards = append(leftCards,
			headerCard("PATH", boldText.Render(truncate(a.packDir, pf)), false),
			headerCard("REPO", boldText.Render(truncate(repoDisp, rf))+linkArrow, false))
		leftActs = append(leftActs, "", "home:repourl")
	case repoDisp != "" && free-7 >= 8:
		leftCards = append(leftCards,
			headerCard("REPO", boldText.Render(truncate(repoDisp, free-7))+linkArrow, false))
		leftActs = append(leftActs, "home:repourl")
	case free-5 >= 8:
		leftCards = append(leftCards,
			headerCard("PATH", boldText.Render(truncate(a.packDir, free-5)), false))
		leftActs = append(leftActs, "")
	}
	var leftParts []string
	lx := 0
	for i, c := range leftCards {
		if i > 0 {
			leftParts = append(leftParts, " ")
			lx++
		}
		if leftActs[i] != "" {
			a.clickZones = append(a.clickZones, clickZone{
				x: lx, y: 0, w: lipgloss.Width(c), h: 4, action: leftActs[i],
			})
		}
		lx += lipgloss.Width(c)
		leftParts = append(leftParts, c)
	}
	left := lipgloss.JoinHorizontal(lipgloss.Top, leftParts...)
	right := lipgloss.JoinHorizontal(lipgloss.Top, ipBox, " ", loaderBox)
	spacer := w - lipgloss.Width(left) - lipgloss.Width(right)
	if spacer < 1 {
		spacer = 1
	}
	header := lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", spacer), right)
	headerH := lipgloss.Height(header)

	ipX := lipgloss.Width(left) + spacer
	a.clickZones = append(a.clickZones, clickZone{
		x: ipX, y: 0, w: lipgloss.Width(ipBox), h: headerH, action: "home:editaddr",
	})
	a.clickZones = append(a.clickZones, clickZone{
		x: ipX + lipgloss.Width(ipBox) + 1, y: 0, w: lipgloss.Width(loaderBox), h: headerH,
		action: "home:loaderbox",
	})

	paneTop := headerH
	midH := bodyH - paneTop

	// ── Fullscreen agent ──
	if a.agentFull {
		body := lipgloss.Place(w, bodyH, lipgloss.Left, lipgloss.Top,
			lipgloss.JoinVertical(lipgloss.Left, header,
				a.renderAgentPane(w, midH, 0, paneTop)))
		return cropBody(body, bodyH, w)
	}

	modsTotalW := clamp(2*w/5, 40, 64)
	rcW := w - modsTotalW - 1 // right column width
	rcX := modsTotalW + 1

	// ── Action buttons: left group flow-wraps, exit pair right-aligns ──
	type btnPos struct{ idx, x, w int }
	var btnRows [][]btnPos
	{
		x := 0
		var row []btnPos
		for i := 0; i < homeBtnRightStart && i < len(homeButtons); i++ {
			bw := lipgloss.Width(a.homeBtnPlain(i)) + 4 // padding + border
			if x+bw > rcW && len(row) > 0 {
				btnRows = append(btnRows, row)
				row = nil
				x = 0
			}
			row = append(row, btnPos{idx: i, x: x, w: bw})
			x += bw + 1
		}
		// Right-aligned exit group: same row if it fits, else its own row.
		rightW := 0
		var rws []int
		for i := homeBtnRightStart; i < len(homeButtons); i++ {
			bw := lipgloss.Width(a.homeBtnPlain(i)) + 4
			rws = append(rws, bw)
			rightW += bw + 1
		}
		rightW--
		xr := rcW - rightW
		if xr < x {
			if len(row) > 0 {
				btnRows = append(btnRows, row)
				row = nil
			}
			xr = maxInt(0, rcW-rightW)
		}
		for k, i := 0, homeBtnRightStart; i < len(homeButtons); k, i = k+1, i+1 {
			row = append(row, btnPos{idx: i, x: xr, w: rws[k]})
			xr += rws[k] + 1
		}
		btnRows = append(btnRows, row)

		// When wrapping was forced, the left/right split reads ragged —
		// center every row instead.
		if len(btnRows) > 1 {
			for r := range btnRows {
				row := btnRows[r]
				if len(row) == 0 {
					continue
				}
				span := row[len(row)-1].x + row[len(row)-1].w - row[0].x
				shift := (rcW-span)/2 - row[0].x
				for i := range row {
					row[i].x += shift
				}
			}
		}
	}
	btnH := len(btnRows) * 3
	agentH := midH - btnH
	if agentH < 8 {
		agentH = 8
	}

	// ── Panes: [mod detail] [agent] [command output], sharing the column ──
	modsPanel := a.renderHomeModsPane(modsTotalW, midH, paneTop)
	nPanes := 1
	if a.modDetail != nil {
		nPanes++
	}
	if a.cmdTitle != "" {
		nPanes++
	}
	sideW := rcW
	if nPanes > 1 {
		sideW = (rcW - (nPanes - 1)) / nPanes
	}
	agentW := rcW - (nPanes-1)*(sideW+1)
	var rowParts []string
	px := rcX
	if a.modDetail != nil {
		rowParts = append(rowParts, a.renderModDetailPane(sideW, agentH, px, paneTop), " ")
		px += sideW + 1
	}
	rowParts = append(rowParts, a.renderAgentPane(agentW, agentH, px, paneTop))
	px += agentW + 1
	if a.cmdTitle != "" {
		rowParts = append(rowParts, " ", a.renderCmdPane(sideW, agentH, px, paneTop))
	}
	agentRow := lipgloss.JoinHorizontal(lipgloss.Top, rowParts...)

	btnTop := paneTop + agentH
	var btnRowStrs []string
	for r, row := range btnRows {
		var cells []string
		curX := 0
		for _, bp := range row {
			if pad := bp.x - curX; pad > 0 {
				cells = append(cells, strings.Repeat(" ", pad))
			}
			b := homeButtons[bp.idx]
			focusedBtn := a.homeFocus == 2 && bp.idx == a.homeBtnIdx
			labelStyle := styleCardText
			if focusedBtn {
				labelStyle = styleCardAccent
			}
			plain := a.homeBtnPlain(bp.idx)
			base := b.icon + " " + b.label
			content := labelStyle.Render(base)
			// The Push & Exit change count is always accent-coloured.
			if extra := strings.TrimPrefix(plain, base); extra != "" {
				content += styleCardAccent.Render(extra)
			}
			cells = append(cells, card([]string{content}, lipgloss.Width(plain), focusedBtn))
			a.clickZones = append(a.clickZones, clickZone{
				x: rcX + bp.x, y: btnTop + r*3, w: bp.w, h: 3,
				action: fmt.Sprintf("home:btn:%d", bp.idx),
			})
			curX = bp.x + bp.w
		}
		btnRowStrs = append(btnRowStrs, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}
	rightCol := lipgloss.JoinVertical(lipgloss.Left,
		append([]string{agentRow}, btnRowStrs...)...)

	mid := lipgloss.JoinHorizontal(lipgloss.Top, modsPanel, " ", rightCol)

	body := lipgloss.Place(w, bodyH, lipgloss.Left, lipgloss.Top,
		lipgloss.JoinVertical(lipgloss.Left, header, mid))
	body = cropBody(body, bodyH, w)

	if a.addModModal {
		// Underlying zones must not respond while the modal is up.
		a.clickZones = nil
		return a.renderWithModal(body, a.viewAddModModal())
	}
	if a.infoTitle != "" {
		// Click anywhere dismisses the popup; underlying zones are dropped.
		a.clickZones = []clickZone{{x: 0, y: 0, w: w, h: bodyH, action: "home:modaldismiss"}}
		return a.renderWithModal(body, a.viewInfoModal())
	}
	if a.approvePending != nil {
		modal, btnXs, btnWs, btnLine := a.viewApproveModal()
		mW := lipgloss.Width(strings.Split(modal, "\n")[0])
		mH := lipgloss.Height(modal)
		mx := maxInt(0, (w-mW)/2)
		my := maxInt(0, (bodyH-mH)/2)
		acts := []string{"approve:allow", "approve:always", "approve:deny"}
		a.clickZones = nil
		for i, act := range acts {
			a.clickZones = append(a.clickZones, clickZone{
				x: mx + 3 + btnXs[i], y: my + 2 + btnLine, w: btnWs[i], h: 1, action: act,
			})
		}
		return a.renderWithModal(body, modal)
	}
	if a.confirmDiscard {
		danger := lipgloss.NewStyle().Foreground(colorDanger).Bold(true)
		dim := lipgloss.NewStyle().Foreground(colorMuted)
		modal := styleModal.Render(lipgloss.JoinVertical(lipgloss.Left,
			danger.Render("⟲ Discard changes"),
			"",
			lipgloss.NewStyle().Foreground(colorText).Render(
				fmt.Sprintf("Reset %d changed file(s) to the last commit?", a.changedCount)),
			dim.Render("This cannot be undone."),
			"",
			dim.Render("enter/y discard  ·  esc/n cancel"),
		))
		a.clickZones = []clickZone{{x: 0, y: 0, w: w, h: bodyH, action: "home:discardcancel"}}
		return a.renderWithModal(body, modal)
	}
	if a.sourcesModal {
		modal, btnXs, btnWs, btnLine := a.viewSourcesModal()
		mW := lipgloss.Width(strings.Split(modal, "\n")[0])
		mH := lipgloss.Height(modal)
		mx := maxInt(0, (w-mW)/2)
		my := maxInt(0, (bodyH-mH)/2)
		// styleModal: 1 border + Padding(1,2) → content offset (+3, +2).
		a.clickZones = []clickZone{
			{x: mx + 3 + btnXs[0], y: my + 2 + btnLine, w: btnWs[0], h: 1, action: "home:srcconv:cf"},
			{x: mx + 3 + btnXs[1], y: my + 2 + btnLine, w: btnWs[1], h: 1, action: "home:srcconv:mr"},
			{x: 0, y: 0, w: w, h: bodyH, action: "home:srcdismiss"},
		}
		return a.renderWithModal(body, modal)
	}
	return body
}

// viewSourcesModal renders the mod-sources popup and returns the convert
// buttons' x offsets/widths and content line for click zones.
func (a *App) viewSourcesModal() (string, [2]int, [2]int, int) {
	c := a.sourceCounts
	colCF := fmt.Sprintf("CURSEFORGE (%d)", c.Curseforge)
	colLocal := fmt.Sprintf("LOCAL (%d)", c.Local)
	colMR := fmt.Sprintf("MODRINTH (%d)", c.Modrinth)
	const btnTxt = " Convert All "
	gap := 4
	w0 := maxInt(len(colCF), len(btnTxt))
	w1 := len(colLocal)
	w2 := maxInt(len(colMR), len(btnTxt))
	total := w0 + gap + w1 + gap + w2

	center := func(s string, w int) (string, int) {
		off := maxInt(0, (w-lipgloss.Width(s))/2)
		return strings.Repeat(" ", off) + s, off
	}

	title, _ := center(styleModalTitle.Render("◈ Mods"), total)

	dim := lipgloss.NewStyle().Foreground(colorMuted).Bold(true)
	cfHdr, _ := center(dim.Render(colCF), w0)
	localHdr, _ := center(dim.Render(colLocal), w1)
	mrHdr, _ := center(dim.Render(colMR), w2)
	countsRow := cfHdr + strings.Repeat(" ", w0-lipgloss.Width(cfHdr)+gap) + localHdr +
		strings.Repeat(" ", w1-lipgloss.Width(localHdr)+gap) + mrHdr

	cfBtnStr := styleBtn.Render(btnTxt)
	mrBtnStr := styleBtn.Render(btnTxt)
	if a.sourcesSel == 0 {
		cfBtnStr = styleBtnFocused.Render(btnTxt)
	} else {
		mrBtnStr = styleBtnFocused.Render(btnTxt)
	}
	cfOff := (w0 - len(btnTxt)) / 2
	mrOff := w0 + gap + w1 + gap + (w2-len(btnTxt))/2
	btnRow := strings.Repeat(" ", cfOff) + cfBtnStr +
		strings.Repeat(" ", mrOff-cfOff-len(btnTxt)) + mrBtnStr

	hint := lipgloss.NewStyle().Foreground(colorMuted).Render("←→ select  ·  enter convert  ·  esc close")
	hintRow, _ := center(hint, total)

	content := lipgloss.JoinVertical(lipgloss.Left,
		title, "", countsRow, "", btnRow, "", hintRow)
	// Content lines: 0 title, 1 blank, 2 counts, 3 blank, 4 buttons…
	return styleModal.Render(content),
		[2]int{cfOff, mrOff}, [2]int{len(btnTxt), len(btnTxt)}, 4
}

// viewApproveModal renders the agent permission dialog and returns the
// button offsets/widths and content line for click zones.
func (a *App) viewApproveModal() (string, [3]int, [3]int, int) {
	r := a.approvePending
	warn := lipgloss.NewStyle().Foreground(colorWarning).Bold(true)
	dim := lipgloss.NewStyle().Foreground(colorMuted)
	textStyle := lipgloss.NewStyle().Foreground(colorText)

	const boxW = 64
	var lines []string
	lines = append(lines, warn.Render("⚠ Agent permission request"), "")
	lines = append(lines, dim.Render("tool: ")+textStyle.Bold(true).Render(r.ToolName))
	for i, wl := range wrapText(approveDetail(r), boxW) {
		if i >= 6 {
			lines = append(lines, dim.Render("…"))
			break
		}
		lines = append(lines, textStyle.Render(wl))
	}
	lines = append(lines, "")

	allowBtn := styleBtnFocused.Render(" Allow ")
	alwaysBtn := styleBtn.Render(" Always allow ")
	denyBtn := lipgloss.NewStyle().Foreground(colorOnAccent).Background(colorDanger).Bold(true).Padding(0, 1).Render(" Deny ")
	var xs, ws [3]int
	xs[0] = 0
	ws[0] = lipgloss.Width(allowBtn)
	xs[1] = xs[0] + ws[0] + 2
	ws[1] = lipgloss.Width(alwaysBtn)
	xs[2] = xs[1] + ws[1] + 2
	ws[2] = lipgloss.Width(denyBtn)
	btnLine := len(lines)
	lines = append(lines, allowBtn+"  "+alwaysBtn+"  "+denyBtn)
	lines = append(lines, "", dim.Render("y allow · w always · n/esc deny"))

	return styleModal.Render(lipgloss.JoinVertical(lipgloss.Left, lines...)), xs, ws, btnLine
}

// viewInfoModal renders the info/warning popup shown over the home screen.
func (a *App) viewInfoModal() string {
	titleStyle := styleModalTitle
	icon := "✓"
	if a.infoErr {
		titleStyle = lipgloss.NewStyle().Foreground(colorDanger).Bold(true)
		icon = "⚠"
	}
	modalText := lipgloss.NewStyle().Foreground(colorMuted)
	lines := []string{titleStyle.Render(icon + " " + a.infoTitle), ""}
	for _, l := range wrapText(a.infoText, 56) {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorText).Render(l))
	}
	lines = append(lines, "", modalText.Render("enter to dismiss"))
	return styleModal.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// renderCmdPane renders the live command output pane (right of the agent
// chat) and registers its close/kill button zone.
func (a *App) renderCmdPane(totalW, h, paneX, paneTop int) string {
	aw := totalW - 4

	var status string
	switch {
	case a.cmdRunning:
		status = styleCardAccent.Render(spinnerFrames[a.spinFrame] + " ")
	case a.cmdErr != nil:
		status = lipgloss.NewStyle().Foreground(colorDanger).Background(colorBgPanel).Bold(true).Render("✗ ")
	default:
		status = styleCardAccent.Render("✓ ")
	}
	closeBtn := styleCardAccent.Render("✕")
	titleTxt := status + styleCardTitle.Render(truncate(a.cmdTitle, maxInt(4, aw-6)))
	pad := aw - lipgloss.Width(titleTxt) - lipgloss.Width(closeBtn)
	if pad < 1 {
		pad = 1
	}
	title := titleTxt + styleCardFill.Render(strings.Repeat(" ", pad)) + closeBtn
	a.clickZones = append(a.clickZones, clickZone{
		x: paneX + 2 + aw - 2, y: paneTop + 1, w: 3, h: 1, action: "home:cmdclose",
	})

	contentH := h - 3 // borders + title
	if contentH < 1 {
		contentH = 1
	}
	var lines []string
	if a.cmdDone && a.cmdSummary != nil {
		lines = renderTestSummary(a.cmdSummary, aw)
	} else {
		// Tail the raw lines first so wrapping stays cheap, then tail again.
		raw := a.cmdLines
		if len(raw) > contentH {
			raw = raw[len(raw)-contentH:]
		}
		for _, l := range raw {
			for _, wl := range wrapText(l, aw) {
				lines = append(lines, styleCardText.Render(wl))
			}
		}
		if len(lines) > contentH {
			lines = lines[len(lines)-contentH:]
		}
		if a.cmdDone && a.cmdErr != nil {
			errLine := lipgloss.NewStyle().Foreground(colorDanger).Background(colorBgPanel).Render(truncate("✗ "+a.cmdErr.Error(), aw))
			if len(lines) >= contentH {
				lines = lines[1:]
			}
			lines = append(lines, errLine)
		}
		if a.cmdDone && !a.cmdCloseAt.IsZero() {
			secs := int(time.Until(a.cmdCloseAt).Seconds()) + 1
			if secs < 0 {
				secs = 0
			}
			lines = append(lines, "",
				styleCardMuted.Render(fmt.Sprintf("auto-closing in %ds", secs)))
		}
	}
	if len(lines) > contentH {
		lines = lines[:contentH]
	}
	for len(lines) < contentH {
		lines = append(lines, "")
	}

	a.clickZones = append(a.clickZones, clickZone{
		x: paneX, y: paneTop, w: totalW, h: h, action: "home:focus:agent",
	})
	return card(append([]string{title}, lines...), aw, false)
}

// padCard pads (or ANSI-aware crops) a content line to interior width w with
// the card background, adding the one-column side padding on each edge. The
// crop keeps a single over-wide line (e.g. a textinput rendering a phantom
// cursor column) from widening the whole card and staggering the layout.
func padCard(line string, w int) string {
	if lipgloss.Width(line) > w {
		line = ansitruncate.String(line, uint(w))
	}
	gap := w - lipgloss.Width(line)
	if gap < 0 {
		gap = 0
	}
	return styleCardFill.Render(" ") + line + styleCardFill.Render(strings.Repeat(" ", gap+1))
}

// card wraps content lines (interior width w) in a rounded border over the
// light card fill. Total rendered width is w+4.
func card(lines []string, w int, focused bool) string {
	st := styleCardBorder
	if focused {
		st = styleCardBorderFoc
	}
	return cardStyled(lines, w, st)
}

// cardStyled is card() with an arbitrary border style (e.g. danger red).
func cardStyled(lines []string, w int, borderStyle lipgloss.Style) string {
	padded := make([]string, len(lines))
	for i, l := range lines {
		padded[i] = padCard(l, w)
	}
	return borderStyle.Render(strings.Join(padded, "\n"))
}

// cropLines hard-limits s to n lines — a safety net so a mis-measured pane
// can never push the layout off-screen and scroll the terminal.
func cropLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// cropBody bounds the final composed body to h lines × w columns.
func cropBody(s string, h, w int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for i, l := range lines {
		if lipgloss.Width(l) > w {
			lines[i] = ansitruncate.String(l, uint(w))
		}
	}
	return strings.Join(lines, "\n")
}

// renderHomeModsPane renders the embedded mod list pane and registers its
// click zones (pane occupies x ∈ [0, totalW), y ∈ [paneTop, paneTop+midH)).
func (a *App) renderHomeModsPane(totalW, midH, paneTop int) string {
	mw := totalW - 4 // interior width (borders + padding)
	focused := a.homeFocus == 0

	titleStyle := styleCardTitleDim
	if focused {
		titleStyle = styleCardTitle
	}
	gitStats := ""
	if n := len(a.modsAdded); n > 0 {
		gitStats += lipgloss.NewStyle().Foreground(colorSuccess).Background(colorBgPanel).Render(fmt.Sprintf(" +%d", n))
	}
	if n := len(a.modsDeleted); n > 0 {
		gitStats += lipgloss.NewStyle().Foreground(colorDanger).Background(colorBgPanel).Render(fmt.Sprintf(" -%d", n))
	}
	title := titleStyle.Render(fmt.Sprintf("◈ Mods (%d)", len(a.modsFiltered))) + gitStats

	// Search row: "/ <input>  +"
	const addStr = " + "
	searchPrefixStr := "/ "
	searchPrefix := styleCardMuted.Render(searchPrefixStr)
	if a.searchFocus {
		searchPrefix = styleCardAccent.Render(searchPrefixStr)
	}
	a.searchInput.Width = mw - len(searchPrefixStr) - len(addStr) - 3
	inputView := a.searchInput.View()
	// Right-align the add button from the actual rendered width so the row
	// always comes out exactly mw wide.
	gap := mw - lipgloss.Width(searchPrefix) - lipgloss.Width(inputView) - len(addStr)
	if gap < 0 {
		gap = 0
	}
	searchRow := searchPrefix + inputView + styleCardFill.Render(strings.Repeat(" ", gap)) + styleAddBtn.Render(addStr)
	a.clickZones = append(a.clickZones, clickZone{
		x: 2 + mw - len(addStr), y: paneTop + 2, w: len(addStr), h: 1,
		action: "add_mod",
	})

	// Mod rows.
	listH := midH - 2 - 3 // borders, title, search, blank
	if listH < 1 {
		listH = 1
	}
	const delStr = " − "
	delW := lipgloss.Width(delStr) // NOT len() — the − glyph is 3 bytes wide
	const statusIndicatorW = 2
	// The item styles carry PaddingLeft(1), so leave 2 columns beyond the
	// name+button or every row wraps and staggers the whole layout.
	nameW := mw - delW - statusIndicatorW - 2

	cardSpace := styleCardFill.Render(" ")
	itemStyle := styleCardText.Copy().PaddingLeft(1)
	itemSelDim := styleCardAccent.Copy().PaddingLeft(1)
	itemDel := styleCardMuted.Copy().Strikethrough(true).PaddingLeft(1)
	addedStyle := lipgloss.NewStyle().Foreground(colorSuccess).Background(colorBgPanel)
	modifiedStyle := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true).Background(colorBgPanel)

	var rows []string
	if len(a.modsFiltered) == 0 {
		rows = append(rows, styleCardMuted.Render("  no mods found"))
	}
	for i, mod := range a.modsFiltered {
		isDeleted := a.modsDeleted[mod.Path]
		var statusIndicator string
		switch {
		case isDeleted:
			statusIndicator = styleDeleteBtn.Render("D") + cardSpace
		case a.modsAdded[mod.Path]:
			statusIndicator = addedStyle.Render("A") + cardSpace
		case a.modsModified[mod.Path]:
			statusIndicator = modifiedStyle.Render("M") + cardSpace
		default:
			statusIndicator = cardSpace + cardSpace
		}
		name := truncate(mod.Name, nameW)
		pad := strings.Repeat(" ", nameW-lipgloss.Width(name))
		var del string
		if isDeleted {
			del = styleAddBtn.Render(" + ")
		} else {
			del = styleDeleteBtn.Render(delStr)
		}
		var nameLine string
		switch {
		case i == a.modsIdx && focused:
			nameLine = styleModItemSelected.Render(name + pad)
		case i == a.modsIdx:
			nameLine = itemSelDim.Render(name + pad)
		case isDeleted:
			nameLine = itemDel.Render(name + pad)
		default:
			nameLine = itemStyle.Render(name + pad)
		}
		rows = append(rows, statusIndicator+nameLine+cardSpace+del)
	}

	start, end := visibleWindow(a.modsIdx, len(rows), listH)
	visible := rows[start:end]
	delX := 2 + statusIndicatorW + 1 + nameW + 1
	for r := range visible {
		if start+r < len(a.modsFiltered) {
			// The − button first (first zone match wins), then the row itself.
			a.clickZones = append(a.clickZones, clickZone{
				x: delX, y: paneTop + 4 + r, w: delW, h: 1,
				action: fmt.Sprintf("del:%d", start+r),
			})
			a.clickZones = append(a.clickZones, clickZone{
				x: 0, y: paneTop + 4 + r, w: totalW, h: 1,
				action: fmt.Sprintf("home:modrow:%d", start+r),
			})
		}
	}
	// Pane-wide focus zone last, so the specific zones above win.
	a.clickZones = append(a.clickZones, clickZone{
		x: 0, y: paneTop, w: totalW, h: midH, action: "home:focus:mods",
	})

	lines := append([]string{title, searchRow, ""}, visible...)
	for len(lines) < midH-2 {
		lines = append(lines, "")
	}
	return card(lines, mw, focused)
}

// renderAgentPane renders the embedded agent chat pane and registers its
// click zones (pane occupies x ∈ [paneX, paneX+totalW)).
func (a *App) renderAgentPane(totalW, midH, paneX, paneTop int) string {
	aw := totalW - 4 // interior width
	focused := a.homeFocus == 1 || a.agentFull

	titleStyle := styleCardTitleDim
	if focused {
		titleStyle = styleCardTitle
	}
	fullBtn := styleCardAccent.Render("⛶")
	titleTxt := titleStyle.Render("✦ Agent")
	switch a.agentMode {
	case 1:
		titleTxt += styleCardFill.Render(" ") + styleBtnFocused.Render("AUTO")
	case 2:
		titleTxt += styleCardFill.Render(" ") +
			lipgloss.NewStyle().Foreground(colorOnAccent).Background(colorDanger).Bold(true).Padding(0, 1).Render("YOLO")
	}
	pad := aw - lipgloss.Width(titleTxt) - lipgloss.Width(fullBtn)
	if pad < 1 {
		pad = 1
	}
	title := titleTxt + styleCardFill.Render(strings.Repeat(" ", pad)) + fullBtn
	a.clickZones = append(a.clickZones, clickZone{
		x: paneX + 2 + aw - lipgloss.Width(fullBtn), y: paneTop + 1,
		w: lipgloss.Width(fullBtn), h: 1, action: "home:agentfull",
	})

	// Transcript.
	errStyle := lipgloss.NewStyle().Foreground(colorDanger).Background(colorBgPanel)
	var tlines []string
	for _, e := range a.agentEntries {
		switch e.role {
		case "user":
			// Accent marker, white message text.
			for li, l := range wrapText(e.text, aw-2) {
				prefix := styleCardFill.Render("  ")
				if li == 0 {
					prefix = styleCardAccent.Render("❯ ")
				}
				tlines = append(tlines, prefix+styleCardText.Render(l))
			}
		case "error":
			for _, l := range wrapText("✗ "+e.text, aw) {
				tlines = append(tlines, errStyle.Render(l))
			}
		default:
			for _, l := range wrapText(e.text, aw) {
				tlines = append(tlines, styleCardText.Render(l))
			}
		}
		tlines = append(tlines, "")
	}
	if a.agentRunning {
		tlines = append(tlines, styleCardAccent.Render(spinnerFrames[a.spinFrame])+styleCardMuted.Render(" thinking…"))
	}
	if len(tlines) == 0 {
		tlines = []string{styleCardMuted.Render("chat with the agent about this pack — it can run"),
			styleCardMuted.Render("the tests, fix sources, and edit mods itself")}
	}

	transH := midH - 4 // borders, title, input row
	if transH < 1 {
		transH = 1
	}
	if maxScroll := len(tlines) - transH; maxScroll > 0 {
		if a.agentScroll > maxScroll {
			a.agentScroll = maxScroll
		}
		tlines = tlines[len(tlines)-transH-a.agentScroll : len(tlines)-a.agentScroll]
	} else {
		a.agentScroll = 0
	}

	// Input row.
	a.agentInput.Width = aw - 4
	var inputRow string
	if a.agentRunning {
		inputRow = styleCardMuted.Render("❯ …")
	} else {
		promptStyle := styleCardMuted
		if focused {
			promptStyle = styleCardAccent
		}
		inputRow = promptStyle.Render("❯ ") + a.agentInput.View()
	}

	// Pad transcript so the input row sits on the bottom edge.
	for len(tlines) < transH {
		tlines = append(tlines, "")
	}

	a.clickZones = append(a.clickZones, clickZone{
		x: paneX, y: paneTop, w: totalW, h: midH, action: "home:focus:agent",
	})

	return card(append(append([]string{title}, tlines...), inputRow), aw, focused)
}

// renderTestSummary formats a finished test run's stats for the command pane.
func renderTestSummary(s *testSummary, aw int) []string {
	danger := lipgloss.NewStyle().Foreground(colorDanger).Bold(true).Background(colorBgPanel)
	kv := func(key, val string) string {
		return styleCardMuted.Render(fmt.Sprintf("%-12s", key)) +
			styleCardText.Render(truncate(val, maxInt(4, aw-12)))
	}

	var lines []string
	lines = append(lines, "")
	if s.passed {
		lines = append(lines, styleCardAccent.Render("✓ TEST PASSED"))
	} else {
		lines = append(lines, danger.Render("✗ TEST FAILED"))
	}
	lines = append(lines, "")
	if s.elapsed != "" {
		lines = append(lines, kv("elapsed", s.elapsed))
	}
	if len(s.tps) > 0 {
		lines = append(lines, "", styleCardMuted.Render("tps samples"))
		for _, t := range s.tps {
			lines = append(lines, styleCardText.Render(truncate("  "+t, aw)))
		}
	}
	if s.shots != "" {
		lines = append(lines, "")
		for _, wl := range wrapText(s.shots, aw) {
			lines = append(lines, styleCardMuted.Render(wl))
		}
	}
	if s.errText != "" {
		lines = append(lines, "")
		for _, wl := range wrapText("✗ "+s.errText, aw) {
			lines = append(lines, danger.Render(wl))
		}
	}
	lines = append(lines, "", styleCardMuted.Render("✕ to close"))
	return lines
}

// viewMainMenuList is the narrow-terminal fallback: the original vertical
// list menu.
func (a *App) viewMainMenuList() string {
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
	delW := lipgloss.Width(delStr) // visual width — the − glyph is 3 bytes

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

// Source tags in the add-mod popup wear their platform's brand colour.
var (
	styleTagModrinth   = lipgloss.NewStyle().Foreground(lipgloss.Color("#1bd96a")).Bold(true)
	styleTagCurseforge = lipgloss.NewStyle().Foreground(lipgloss.Color("#f16436")).Bold(true)
)

// sourceLogo returns a platform's 2-cell logo image block, or "" when it
// isn't available (not kitty, or still fetching).
func (a *App) sourceLogo(source string) string {
	logoURL := modrinthLogoURL
	if source == "curseforge" {
		logoURL = curseforgeLogoURL
	}
	if b, ok := a.addModImgs[imgKey(logoURL, 2, 1)]; ok && b != "" && b != pendingImg {
		return b
	}
	return ""
}

// hitSourceLogos renders a hit's source badges as the platforms' real logos
// (kitty graphics). Falls back to ok=false until every needed logo is in.
func (a *App) hitSourceLogos(h ModHit) (string, int, bool) {
	logos, width := "", 0
	for _, s := range h.Sources() {
		l := a.sourceLogo(s)
		if l == "" {
			return "", 0, false
		}
		if width > 0 {
			logos += " "
			width++
		}
		logos += l
		width += 2
	}
	return logos, width, true
}

// hitSourceTags renders a hit's source badges — every platform hosting the
// mod, primary first. wide=false gives the two-letter list form ("mr cf"),
// wide=true the full names. Returns the styled string and its cell width.
func hitSourceTags(h ModHit, wide bool) (string, int) {
	var parts []string
	width := 0
	for _, s := range h.Sources() {
		label, st := "mr", styleTagModrinth
		if s == "curseforge" {
			label, st = "cf", styleTagCurseforge
		}
		if wide {
			label = s
		}
		if width > 0 {
			width++ // joining space
		}
		parts = append(parts, st.Render(label))
		width += len(label)
	}
	return strings.Join(parts, " "), width
}

// viewAddModModal renders the near-fullscreen add-mod popup: the query input
// with the live results list beneath it in a slim left column, and a large
// preview of the selected mod on the right — logo, brand-tagged sources,
// downloads, first screenshot, description. Images use kitty graphics where
// the terminal supports them, half-block pixel art (sized to the pane, so a
// bigger pane means more resolution) elsewhere, nothing on no-color
// terminals. Narrow terminals get the list only, no preview.
func (a *App) viewAddModModal() string {
	h := clamp(a.height-4, 14, 40)
	_, fh := styleModal.GetFrameSize()
	ch := h - fh
	lw, rw := a.addModPaneWidths()
	twoCol := rw > 0
	cw := lw

	muted := lipgloss.NewStyle().Foreground(colorMuted)
	danger := lipgloss.NewStyle().Foreground(colorDanger)
	color := terminalDoesColor()
	kitty := color && kittyGraphicsOK()
	a.addModInput.Width = maxInt(10, lw-4)

	// The overlay centres the popup (renderWithModal) — mirror that math so
	// click zones land on the real screen cells.
	fwFull, fhFull := styleModal.GetFrameSize()
	contentX := maxInt(0, (a.width-(cw+fwFull))/2) + fwFull/2
	contentY := maxInt(0, (a.height-1-(ch+fhFull))/2) + fhFull/2
	if twoCol {
		contentX = maxInt(0, (a.width-(lw+3+rw+fwFull))/2) + fwFull/2
	}
	zone := func(x, y, w, h int, action string) {
		a.clickZones = append(a.clickZones, clickZone{
			x: contentX + x, y: contentY + y, w: w, h: h, action: action,
		})
	}

	// Result rows: real-image icon (kitty only — no indent when the terminal
	// can't place images), slug, brand-coloured source tags.
	renderRows := func(width, height, baseRow int) []string {
		iconW := 0
		if kitty {
			iconW = 3
		}
		var rows []string
		start, end := visibleWindow(a.addModIdx, len(a.addModHits), height)
		for i := start; i < end; i++ {
			hit := a.addModHits[i]
			tags, tagW := hitSourceTags(hit, false)
			if kitty {
				if logos, w, ok := a.hitSourceLogos(hit); ok {
					tags, tagW = logos, w
				}
			}
			prefix := ""
			if kitty {
				if block, ok := a.addModImgs[imgKey(hit.IconURL, 2, 1)]; ok && block != "" && block != pendingImg {
					prefix = block + " "
				} else {
					prefix = strings.Repeat(" ", iconW)
				}
			}
			// styleModItem* carry one column of left padding.
			textW := width - iconW - 1
			name := truncate(hit.Slug, maxInt(8, textW-tagW-2))
			pad := maxInt(1, textW-lipgloss.Width(name)-tagW)
			st := styleModItem
			if i == a.addModIdx {
				st = styleModItemSelected
			}
			zone(0, baseRow+len(rows), width, 1, fmt.Sprintf("addmod:row:%d", i))
			rows = append(rows, prefix+st.Render(name)+strings.Repeat(" ", pad-1)+tags)
		}
		return rows
	}

	statusLine := func(width int) string {
		switch {
		case a.addModSearching:
			return muted.Render(spinnerFrames[a.spinFrame] + " searching…")
		case a.addModErr != "":
			return danger.Render(truncate("error: "+a.addModErr, width))
		case a.addModQuery == "":
			return muted.Render("results appear as you type")
		case len(a.addModHits) == 0:
			return muted.Render(truncate("no results for "+a.addModQuery, width))
		default:
			return muted.Render(fmt.Sprintf("%d results", len(a.addModHits)))
		}
	}

	scope := strings.TrimSpace(a.packMeta.Minecraft + " " + a.packMeta.Loader)
	if scope == "" {
		scope = "all versions"
	}
	hint := muted.Render(truncate("↑/↓ select · ←/→ buttons · enter go · esc close", lw))

	// ✕ close button pinned to the popup's top-right corner.
	closeModalBtn := func(lines []string, width int) []string {
		if pad := width - 2 - lipgloss.Width(lines[0]); pad > 0 {
			lines[0] += strings.Repeat(" ", pad)
		}
		lines[0] += " " + styleCardAccent.Render("✕")
		return lines
	}

	if !twoCol {
		var lines []string
		lines = append(lines, styleModalTitle.Render("◈ Add Mod"))
		lines = append(lines, muted.Render(truncate("Modrinth + CurseForge · "+scope, cw)))
		lines = append(lines, a.addModInput.View())
		lines = append(lines, statusLine(cw))
		lines = append(lines, renderRows(cw, maxInt(1, ch-len(lines)-1), len(lines))...)
		for len(lines) < ch-1 {
			lines = append(lines, "")
		}
		lines = append(lines[:ch-1], hint)
		lines = closeModalBtn(lines, cw)
		zone(cw-2, 0, 2, 1, "addmod:close")
		return styleModal.Render(strings.Join(paintModalBg(lines), "\n"))
	}

	// ── Left column: query input with the results list beneath it ──
	var left []string
	left = append(left, styleModalTitle.Render("◈ Add Mod"))
	left = append(left, muted.Render(truncate("Modrinth + CurseForge · "+scope, lw)))
	left = append(left, "")
	left = append(left, a.addModInput.View())
	left = append(left, statusLine(lw))
	left = append(left, "")
	left = append(left, renderRows(lw, maxInt(1, ch-len(left)-1), len(left))...)
	for len(left) < ch-1 {
		left = append(left, "")
	}
	left = append(left[:ch-1], hint)

	// ── Right column: large preview of the selected mod — a scrollable
	// column (wheel / pgup·pgdn) so the description is never cut off. Click
	// zones are collected content-relative and registered after scrolling.
	var prev []string
	var pzones []addModZone
	center := func(l string) string {
		off := maxInt(0, (rw-lipgloss.Width(l))/2)
		return strings.Repeat(" ", off) + l
	}
	if a.addModIdx < len(a.addModHits) {
		hit := a.addModHits[a.addModIdx]
		lc, lr := a.addModLogoSize(rw, len(hit.Gallery) > 0)
		logo := ""
		if color && hit.IconURL != "" && a.logoRenderable(rw) {
			logo = a.addModImgs[imgKey(hit.IconURL, lc, lr)]
		}
		logoOK := logo != "" && logo != pendingImg
		tags, _ := hitSourceTags(hit, true)
		titleLine := styleModalTitle.Render(truncate(hit.Title, rw-4))
		dlLine := tags + muted.Render(" · "+humanCount(hit.Downloads)+" downloads")
		if lipgloss.Width(dlLine) > rw {
			dlLine = tags
		}
		switch {
		case kitty && logoOK:
			// Small full-res logo beside the title block.
			logoLines := strings.Split(logo, "\n")
			logoW := lc
			textW := maxInt(8, rw-logoW-4)
			titleLine = styleModalTitle.Render(truncate(hit.Title, textW))
			if lipgloss.Width(dlLine) > textW {
				dlLine = tags
			}
			text := []string{titleLine, dlLine}
			for i := 0; i < maxInt(len(logoLines), len(text)); i++ {
				lpart := strings.Repeat(" ", logoW)
				if i < len(logoLines) {
					lpart = logoLines[i]
					if pad := logoW - lipgloss.Width(lpart); pad > 0 {
						lpart += strings.Repeat(" ", pad)
					}
				}
				tpart := ""
				if i < len(text) {
					tpart = text[i]
				}
				prev = append(prev, lpart+"  "+tpart)
			}
		case logoOK:
			// Big half-block logo — the pane's width is its resolution.
			for _, l := range strings.Split(logo, "\n") {
				prev = append(prev, center(l))
			}
			prev = append(prev, "")
			prev = append(prev, center(titleLine))
			prev = append(prev, center(dlLine))
		default:
			if color && logo == pendingImg {
				prev = append(prev, center(muted.Render(spinnerFrames[a.spinFrame])))
			}
			prev = append(prev, center(titleLine))
			prev = append(prev, center(dlLine))
		}
		// Gallery page: whole screenshots side by side, paginated with
		// shift+←/→ or the ‹ › arrows. Skipped entirely when the pane can't
		// give half-block art enough pixels to be legible.
		if color && len(hit.Gallery) > 0 && a.shotsRenderable(rw) {
			start, end, shotRows, _ := a.addModShotWindow(hit, rw)
			prev = append(prev, "")
			if len(shotRows) == 0 {
				prev = append(prev, center(muted.Render(spinnerFrames[a.spinFrame]+" loading screenshot…")))
			}
			for ri, rowIdx := range shotRows {
				if ri > 0 {
					prev = append(prev, "")
				}
				var blocks [][]string
				var widths []int
				maxH := 0
				for _, k := range rowIdx {
					b := strings.Split(a.addModImgs[imgKey(hit.Gallery[k], rw-2, 14)], "\n")
					blocks = append(blocks, b)
					widths = append(widths, lipgloss.Width(b[0]))
					maxH = maxInt(maxH, len(b))
				}
				for r := 0; r < maxH; r++ {
					row := ""
					for bi, b := range blocks {
						if bi > 0 {
							row += "  "
						}
						voff := (maxH - len(b)) / 2
						if r >= voff && r-voff < len(b) {
							row += b[r-voff]
						} else {
							row += strings.Repeat(" ", widths[bi])
						}
					}
					prev = append(prev, center(row))
				}
			}
			if len(hit.Gallery) > 1 {
				label := fmt.Sprintf("screenshot %d/%d", start+1, len(hit.Gallery))
				if end-start > 1 {
					label = fmt.Sprintf("screenshots %d-%d/%d", start+1, end, len(hit.Gallery))
				}
				// Clickable ‹ › pagination arrows flank the caption, dimmed
				// at their respective ends of the gallery.
				arrow := func(sym string, active bool) string {
					if active {
						return styleSearchActive.Render(" " + sym + " ")
					}
					return muted.Render(" " + sym + " ")
				}
				line := arrow("‹", start > 0) + muted.Render(label) + arrow("›", end < len(hit.Gallery))
				off := maxInt(0, (rw-lipgloss.Width(line))/2)
				pzones = append(pzones,
					addModZone{x: off, row: len(prev), w: 3, h: 1, action: "addmod:shotprev"},
					addModZone{x: off + lipgloss.Width(line) - 3, row: len(prev), w: 3, h: 1, action: "addmod:shotnext"})
				prev = append(prev, strings.Repeat(" ", off)+line)
			}
		}
		// Full description — never cut off; the pane scrolls instead.
		prev = append(prev, "")
		for _, l := range wrapText(hit.Description, rw) {
			prev = append(prev, l)
		}
		// Buttons: pinned to the pane's bottom when everything fits, else
		// the last section of the scrollable content.
		btnLines, btnZones := a.renderAddModButtons(hit, rw)
		if len(prev)+1+len(btnLines) <= ch {
			for len(prev) < ch-len(btnLines) {
				prev = append(prev, "")
			}
		} else {
			prev = append(prev, "")
		}
		base := len(prev)
		prev = append(prev, btnLines...)
		for _, z := range btnZones {
			pzones = append(pzones, addModZone{x: z.x, row: base + z.row, w: z.w, h: z.h, action: z.action})
		}
	} else {
		for len(prev) < ch/2-1 {
			prev = append(prev, "")
		}
		prev = append(prev, center(muted.Render("mod preview")))
		prev = append(prev, center(muted.Render("appears here as you search")))
	}

	// Scroll the preview, then register the zones still on screen.
	a.addModPrevScroll = clamp(a.addModPrevScroll, 0, maxInt(0, len(prev)-ch))
	sc := a.addModPrevScroll
	prev = prev[sc:minInt(len(prev), sc+ch)]
	for _, z := range pzones {
		if z.row-sc >= 0 && z.row-sc < ch {
			zone(lw+3+z.x, z.row-sc, z.w, z.h, z.action)
		}
	}
	for len(prev) < ch {
		prev = append(prev, "")
	}

	// Fixed geometry: every line is padded to its pane's exact width, so the
	// popup never resizes (and never re-centres) as content changes.
	sep := muted.Render(" │ ")
	merged := make([]string, ch)
	for i := 0; i < ch; i++ {
		l := left[i]
		if pad := lw - lipgloss.Width(l); pad > 0 {
			l += strings.Repeat(" ", pad)
		}
		r := prev[i]
		if pad := rw - lipgloss.Width(r); pad > 0 {
			r += strings.Repeat(" ", pad)
		}
		merged[i] = l + sep + r
	}
	merged = closeModalBtn(merged, lw+3+rw)
	zone(lw+3+rw-2, 0, 2, 1, "addmod:close")
	return styleModal.Render(strings.Join(paintModalBg(merged), "\n"))
}

// paintModalBg re-asserts the popup's panel background after every full SGR
// reset in a line — nested lipgloss renders end with resets that would
// otherwise punch holes in the background from that point on, which reads
// as a patchy, inconsistent popup interior.
func paintModalBg(lines []string) []string {
	bg := "\x1b[" + termenv.ColorProfile().Convert(termenv.RGBColor(string(colorBgPanel))).Sequence(true) + "m"
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = bg + strings.ReplaceAll(l, "\x1b[0m", "\x1b[0m"+bg)
	}
	return out
}

// addModZone is a clickable area relative to the button block's first line.
type addModZone struct {
	x, row, w, h int
	action       string
}

// renderAddModButtons renders the preview's action buttons: for each
// platform hosting the mod, its name (brand-coloured) centred above a row of
// bordered boxes — install and website — so they're comfortable touch
// targets. Platform groups sit side by side when the pane is wide enough,
// stacked otherwise. ←/→ moves focus, enter or a click activates.
func (a *App) renderAddModButtons(hit ModHit, rw int) ([]string, []addModZone) {
	btns := addModButtons(hit)
	focused := clamp(a.addModBtnIdx, 0, len(btns)-1)

	// Build each platform's group: a centred label row over its box row.
	type btnGroup struct {
		lines []string
		width int
		zones []addModZone // relative to the group's own origin
	}
	var groups []btnGroup
	for i := 0; i < len(btns); i += 2 {
		nameStyle := styleTagModrinth
		if btns[i].source == "curseforge" {
			nameStyle = styleTagCurseforge
		}
		var boxes [][]string
		var widths []int
		total := 0
		for j := i; j < minInt(i+2, len(btns)); j++ {
			border := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBorder).Padding(0, 1)
			label := lipgloss.NewStyle().Foreground(colorText)
			if j == focused {
				border = border.BorderForeground(colorAccent)
				label = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
			}
			box := strings.Split(border.Render(label.Render(btns[j].label)), "\n")
			boxes = append(boxes, box)
			widths = append(widths, lipgloss.Width(box[0]))
			total += lipgloss.Width(box[0])
		}
		total += len(boxes) - 1
		g := btnGroup{width: total}
		// Group label: the platform's logo (when the terminal renders real
		// images) to the left of its name.
		label := nameStyle.Render(btns[i].source)
		labelW := len(btns[i].source)
		if logo := a.sourceLogo(btns[i].source); logo != "" {
			label = logo + " " + label
			labelW += 3
		}
		g.lines = append(g.lines, strings.Repeat(" ", maxInt(0, (total-labelW)/2))+label)
		for r := 0; r < len(boxes[0]); r++ {
			row := ""
			for bi, box := range boxes {
				if bi > 0 {
					row += " "
				}
				row += box[r]
			}
			g.lines = append(g.lines, row)
		}
		x := 0
		for bi := range boxes {
			g.zones = append(g.zones, addModZone{
				x: x, row: 1, w: widths[bi], h: len(boxes[bi]),
				action: fmt.Sprintf("addmod:btn:%d", i+bi),
			})
			x += widths[bi] + 1
		}
		groups = append(groups, g)
	}

	pad := func(l string, w int) string {
		if gap := w - lipgloss.Width(l); gap > 0 {
			return l + strings.Repeat(" ", gap)
		}
		return l
	}

	// Side by side when they fit, stacked otherwise.
	const gap = 3
	total := gap * (len(groups) - 1)
	rows := 0
	for _, g := range groups {
		total += g.width
		rows = maxInt(rows, len(g.lines))
	}
	var lines []string
	var zones []addModZone
	if total <= rw && len(groups) > 1 {
		off := maxInt(0, (rw-total)/2)
		for r := 0; r < rows; r++ {
			row := strings.Repeat(" ", off)
			for gi, g := range groups {
				if gi > 0 {
					row += strings.Repeat(" ", gap)
				}
				l := ""
				if r < len(g.lines) {
					l = g.lines[r]
				}
				row += pad(l, g.width)
			}
			lines = append(lines, row)
		}
		x := off
		for _, g := range groups {
			for _, z := range g.zones {
				zones = append(zones, addModZone{x: x + z.x, row: z.row, w: z.w, h: z.h, action: z.action})
			}
			x += g.width + gap
		}
	} else {
		for _, g := range groups {
			off := maxInt(0, (rw-g.width)/2)
			base := len(lines)
			for _, l := range g.lines {
				lines = append(lines, strings.Repeat(" ", off)+l)
			}
			for _, z := range g.zones {
				zones = append(zones, addModZone{x: off + z.x, row: base + z.row, w: z.w, h: z.h, action: z.action})
			}
		}
	}
	return lines, zones
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

	// Overlay the modal, preserving the background on both sides so it
	// reads as a popup rather than blanking the whole row.
	result := make([]string, bgH)
	for i := 0; i < bgH; i++ {
		modalLineIdx := i - y
		if modalLineIdx >= 0 && modalLineIdx < modalH {
			bgLine := ""
			if i < len(bgLines) {
				bgLine = bgLines[i]
			}
			modalLine := modalLines[modalLineIdx]

			before := ansitruncate.String(bgLine, uint(x))
			if pad := x - lipgloss.Width(before); pad > 0 {
				before += strings.Repeat(" ", pad)
			}
			after := ansiTail(bgLine, x+lipgloss.Width(modalLine))

			result[i] = before + "\x1b[0m" + modalLine + "\x1b[0m" + after
		} else {
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
	// Check if this is a yes/no prompt
	isYesNo := len(a.interactiveOptions) == 2
	if isYesNo {
		opt0 := strings.ToLower(a.interactiveOptions[0])
		opt1 := strings.ToLower(a.interactiveOptions[1])
		isYesNo = (opt0 == "yes" || opt0 == "y") && (opt1 == "no" || opt1 == "n")
	}

	if isYesNo {
		// Horizontal layout for yes/no prompts
		promptLines := strings.Split(a.interactivePrompt, "\n")
		panelW := clamp(64, 40, a.width-4)
		panelH := clamp(20, 10, a.height-4)

		var rows []string

		// Show the prompt text centered
		for _, line := range promptLines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				centered := lipgloss.Place(panelW-4, 1, lipgloss.Center, lipgloss.Top, styleSubtitle.Render(trimmed))
				rows = append(rows, centered)
			}
		}

		rows = append(rows, "", "")

		// Show Yes/No options horizontally, centered
		var yesBtn, noBtn string
		if a.interactiveSelected == 0 {
			yesBtn = styleMenuItemSelected.Render("  Yes  ")
			noBtn = styleMenuItem.Render("  No  ")
		} else {
			yesBtn = styleMenuItem.Render("  Yes  ")
			noBtn = styleMenuItemSelected.Render("  No  ")
		}

		buttonRow := lipgloss.JoinHorizontal(lipgloss.Left, yesBtn, "   ", noBtn)
		centeredButtons := lipgloss.Place(panelW-4, 1, lipgloss.Center, lipgloss.Top, buttonRow)
		rows = append(rows, centeredButtons)

		// Center all content vertically within the panel
		panelContent := strings.Join(rows, "\n")
		centeredContent := lipgloss.Place(panelW-4, panelH-2, lipgloss.Center, lipgloss.Center, panelContent)
		panel := stylePanelFocused.Width(panelW).Height(panelH).Render(centeredContent)

		return lipgloss.Place(a.width, a.height-1, lipgloss.Center, lipgloss.Center, panel)
	}

	// Vertical layout for numbered options (original behavior)
	var rows []string
	rows = append(rows, "")
	rows = append(rows, styleSubtitle.Render("  "+a.interactivePrompt))
	rows = append(rows, "")

	for i, opt := range a.interactiveOptions {
		// Check if this is a header
		isHeader := i < len(a.interactiveSources) && a.interactiveSources[i] == "header"

		if isHeader {
			// Render as a section header, not selectable
			headerStyle := lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)
			rows = append(rows, "", headerStyle.Render("  "+opt), "")
		} else {
			num := fmt.Sprintf("[%d]", i)
			var line string
			if i == a.interactiveSelected {
				line = styleMenuItemSelected.Render(" " + num + " " + opt)
			} else {
				line = styleMenuItem.Render(" " + num + " " + opt)
			}
			rows = append(rows, line)
		}
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
