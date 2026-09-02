package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ModDetail is the state of the mod configuration pane (left of the agent).
type ModDetail struct {
	TomlPath   string
	Slug       string
	Name       string
	Filename   string
	Side       string
	Source     string // "modrinth", "curseforge", "local"
	Optional   bool
	Default    bool // optional mods only: enabled by default in installers
	ProjectID  string
	VersionID  string
	CurVersion string // resolved human version number

	Versions    []modVersion
	VersionsErr string
	Sha1        string
	Converting  bool   // background conversion in flight
	ConvertErr  string // last conversion failure, shown inline
	verIdx      int
	ctlIdx      int
	dropOpen    bool
}

// verCacheKey identifies a version list by source+project, so a converted
// mod naturally misses the stale entry.
func (d *ModDetail) verCacheKey() string { return d.Source + ":" + d.ProjectID }

// cachedVersions is a cached version-list lookup (success or failure).
type cachedVersions struct {
	vers []modVersion
	err  string
	at   time.Time
}

const (
	verCacheTTL    = 15 * time.Minute
	verCacheErrTTL = 2 * time.Minute
)

func (c cachedVersions) fresh() bool {
	if c.err != "" {
		return time.Since(c.at) < verCacheErrTTL
	}
	return time.Since(c.at) < verCacheTTL
}

type modVersion struct {
	ID     string
	Number string
	Name   string
}

type msgModVersions struct {
	slug string
	key  string // cache key (source:projectID)
	vers []modVersion
	err  error
}
type msgModCurVer struct {
	slug   string
	sha1   string
	number string
}
type msgModDetailDone struct{ err error }
type msgModConvertDone struct {
	slug   string
	report string
	err    error
}
type msgModDetailFetch struct{ slug string } // debounced scroll-sync fetch

// Control rows in the detail pane: fields first, then the button stack.
// mdCtlDefault only exists while the mod is optional — navigation skips it
// otherwise.
const (
	mdCtlSide = iota
	mdCtlOptional
	mdCtlDefault
	mdCtlVersion
	mdCtlConvert
	mdCtlProject
	mdCtlEdit
	mdCtlDelete
	mdCtlCount
)

var sideOrder = []string{"client", "server", "both"}

// convertFailLine pulls the per-mod ✗ line out of a conversion report.
func convertFailLine(report string) string {
	for _, l := range strings.Split(report, "\n") {
		if strings.Contains(l, "✗") {
			return strings.TrimSpace(l)
		}
	}
	return "conversion failed"
}

// openModDetail loads a mod's metafile into the detail pane, focuses and
// pins it, and starts the async version lookups.
func (a *App) openModDetail(mod ModFile) tea.Cmd {
	a.setModDetail(mod)
	a.detailPinned = true
	a.setHomeFocus(5)
	return a.modDetailFetches()
}

// syncModDetail swaps the open pane to another mod without stealing focus
// (used while scrolling the list); the network lookups are debounced so
// holding a scroll key doesn't hammer the APIs.
func (a *App) syncModDetail(mod ModFile) tea.Cmd {
	prevCtl := 0
	if a.modDetail != nil {
		prevCtl = a.modDetail.ctlIdx
	}
	a.setModDetail(mod)
	a.modDetail.ctlIdx = prevCtl
	slug := a.modDetail.Slug
	return tea.Tick(350*time.Millisecond, func(time.Time) tea.Msg {
		return msgModDetailFetch{slug: slug}
	})
}

func (a *App) setModDetail(mod ModFile) {
	d := &ModDetail{TomlPath: mod.Path}
	d.Slug = strings.TrimSuffix(strings.TrimSuffix(filepath.Base(mod.Path), ".toml"), ".pw")
	loadModDetailFields(d)
	// Hydrate from the session caches so revisiting a mod is free.
	if c, ok := a.modVerCache[d.verCacheKey()]; ok && c.fresh() {
		d.Versions = c.vers
		d.VersionsErr = c.err
		for _, v := range c.vers {
			if v.ID == d.VersionID {
				d.CurVersion = v.Number
			}
		}
	}
	if d.CurVersion == "" && d.Sha1 != "" {
		if n, ok := a.modHashVer[d.Sha1]; ok {
			d.CurVersion = n
		}
	}
	a.modDetail = d
}

// modDetailFetches builds the async lookups for the current detail mod,
// skipping anything the cache already answered.
func (a *App) modDetailFetches() tea.Cmd {
	d := a.modDetail
	if d == nil {
		return nil
	}
	var cmds []tea.Cmd
	if len(d.Versions) == 0 && d.VersionsErr == "" {
		switch {
		case d.Source == "modrinth" && d.ProjectID != "":
			cmds = append(cmds, fetchModVersions(d.Slug, d.verCacheKey(), d.ProjectID, a.packMeta))
		case d.Source == "curseforge" && d.ProjectID != "":
			// CurseForge's official API is key-gated; the cfwidget proxy
			// lists project files without one.
			cmds = append(cmds, fetchCurseforgeVersions(d.Slug, d.verCacheKey(), d.ProjectID, a.packMeta))
		}
	}
	// Resolve the installed version number from the file hash — works for
	// curseforge-sourced mods too (same jar is usually on modrinth).
	if d.CurVersion == "" && d.Sha1 != "" {
		if _, cached := a.modHashVer[d.Sha1]; !cached {
			cmds = append(cmds, fetchCurVersionByHash(d.Slug, d.Sha1))
		}
	}
	return tea.Batch(cmds...)
}

func loadModDetailFields(d *ModDetail) {
	data, err := os.ReadFile(d.TomlPath)
	if err != nil {
		return
	}
	s := string(data)
	d.Name, _ = readTomlField(d.TomlPath, "name")
	if d.Name == "" {
		d.Name = d.Slug
	}
	d.Filename, _ = readTomlField(d.TomlPath, "filename")
	d.Side, _ = readTomlField(d.TomlPath, "side")
	if d.Side == "" {
		d.Side = "both"
	}
	d.Optional = strings.Contains(s, "optional = true")
	d.Default = strings.Contains(s, "default = true")
	if hf, _ := readTomlField(d.TomlPath, "hash-format"); hf == "sha1" {
		d.Sha1, _ = readTomlField(d.TomlPath, "hash")
	}
	switch {
	case strings.Contains(s, "[update.modrinth]"):
		d.Source = "modrinth"
		d.ProjectID, _ = readTomlField(d.TomlPath, "mod-id")
		d.VersionID, _ = readTomlField(d.TomlPath, "version")
	case strings.Contains(s, "[update.curseforge]"):
		d.Source = "curseforge"
		d.ProjectID, _ = readTomlField(d.TomlPath, "project-id")
		d.VersionID, _ = readTomlField(d.TomlPath, "file-id")
	default:
		d.Source = "local"
	}
}

// fetchModVersions lists a modrinth project's versions matching the pack's
// mc version + loader.
func fetchModVersions(slug, cacheKey, projID string, meta PackMeta) tea.Cmd {
	return func() tea.Msg {
		q := url.Values{}
		if meta.Minecraft != "" {
			q.Set("game_versions", `["`+meta.Minecraft+`"]`)
		}
		if meta.Loader != "" {
			q.Set("loaders", `["`+meta.Loader+`"]`)
		}
		u := "https://api.modrinth.com/v2/project/" + projID + "/version"
		if enc := q.Encode(); enc != "" {
			u += "?" + enc
		}
		data, err := cachedGET(u, nil)
		if err != nil {
			return msgModVersions{slug: slug, key: cacheKey, err: err}
		}
		var raw []struct {
			ID     string `json:"id"`
			Number string `json:"version_number"`
			Name   string `json:"name"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return msgModVersions{slug: slug, key: cacheKey, err: err}
		}
		if len(raw) > 20 {
			raw = raw[:20]
		}
		vers := make([]modVersion, len(raw))
		for i, r := range raw {
			vers[i] = modVersion{ID: r.ID, Number: r.Number, Name: r.Name}
		}
		return msgModVersions{slug: slug, key: cacheKey, vers: vers}
	}
}

// fetchCurseforgeVersions lists a curseforge project's files via the
// cfwidget.com proxy (the official API needs a key), filtered to the pack's
// mc version and loader.
func fetchCurseforgeVersions(slug, cacheKey, projID string, meta PackMeta) tea.Cmd {
	return func() tea.Msg {
		data, err := cachedGET("https://api.cfwidget.com/"+projID, nil)
		if err != nil {
			return msgModVersions{slug: slug, key: cacheKey, err: err}
		}
		var raw struct {
			Files []struct {
				ID       int      `json:"id"`
				Display  string   `json:"display"`
				Versions []string `json:"versions"`
			} `json:"files"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return msgModVersions{slug: slug, key: cacheKey, err: err}
		}
		matches := func(tags []string, want string) bool {
			for _, t := range tags {
				if strings.EqualFold(t, want) {
					return true
				}
			}
			return false
		}
		var vers []modVersion
		// cfwidget lists oldest-first — walk backwards for newest-first.
		for i := len(raw.Files) - 1; i >= 0 && len(vers) < 20; i-- {
			f := raw.Files[i]
			if meta.Minecraft != "" && !matches(f.Versions, meta.Minecraft) {
				continue
			}
			if meta.Loader != "" && !matches(f.Versions, meta.Loader) {
				continue
			}
			vers = append(vers, modVersion{ID: fmt.Sprint(f.ID), Number: f.Display})
		}
		if len(vers) == 0 {
			return msgModVersions{slug: slug, key: cacheKey, err: fmt.Errorf("no matching files on curseforge")}
		}
		return msgModVersions{slug: slug, key: cacheKey, vers: vers}
	}
}

// fetchCurVersionByHash resolves the installed file's version number via the
// modrinth hash endpoint.
func fetchCurVersionByHash(slug, sha1v string) tea.Cmd {
	return func() tea.Msg {
		data, err := cachedGET("https://api.modrinth.com/v2/version_file/"+sha1v+"?algorithm=sha1", nil)
		if err != nil {
			return msgModCurVer{slug: slug, sha1: sha1v}
		}
		var v struct {
			Number string `json:"version_number"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return msgModCurVer{slug: slug, sha1: sha1v}
		}
		return msgModCurVer{slug: slug, sha1: sha1v, number: v.Number}
	}
}

// updateModDetail handles keys while the detail pane is focused.
func (a *App) updateModDetail(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := a.modDetail
	if d == nil {
		a.setHomeFocus(0)
		return a, nil
	}
	if d.dropOpen {
		switch m.String() {
		case "esc":
			d.dropOpen = false
		case "up", "k":
			if d.verIdx > 0 {
				d.verIdx--
			}
		case "down", "j":
			if d.verIdx < len(d.Versions)-1 {
				d.verIdx++
			}
		case "enter", " ":
			d.dropOpen = false
			if d.verIdx < len(d.Versions) {
				return a, a.installModVersion(d.Versions[d.verIdx])
			}
		}
		return a, nil
	}
	switch m.String() {
	case "esc":
		// Unpin and hand focus back to the list — the pane keeps following
		// the selection and closes for good once focus leaves the list.
		a.detailPinned = false
		return a, a.focusHome(0)
	case "up", "k":
		if d.ctlIdx > 0 {
			d.ctlIdx--
			if d.ctlIdx == mdCtlDefault && !d.Optional {
				d.ctlIdx--
			}
		}
	case "down", "j":
		if d.ctlIdx < mdCtlCount-1 {
			d.ctlIdx++
			if d.ctlIdx == mdCtlDefault && !d.Optional {
				d.ctlIdx++
			}
		}
	case "right", "l":
		// Move from the field column into the button stack.
		if d.ctlIdx <= mdCtlVersion {
			d.ctlIdx = mdCtlConvert
		}
	case "left", "h":
		// From the buttons back to the fields; from the fields out to the list.
		if d.ctlIdx >= mdCtlConvert {
			d.ctlIdx = mdCtlSide
		} else {
			return a, a.focusHome(0)
		}
	case "enter", " ":
		return a.activateModDetailCtl(d.ctlIdx)
	case "s":
		return a.applyModSide(stepSide(d.Side, 1))
	case "o":
		return a.applyModOptional(!d.Optional)
	case "v":
		d.ctlIdx = mdCtlVersion
		return a.activateModDetailCtl(mdCtlVersion)
	}
	return a, nil
}

func stepSide(side string, dir int) string {
	for i, s := range sideOrder {
		if s == side {
			return sideOrder[(i+dir+len(sideOrder))%len(sideOrder)]
		}
	}
	return "both"
}

// applyModSide writes the side tag and refreshes.
func (a *App) applyModSide(side string) (tea.Model, tea.Cmd) {
	d := a.modDetail
	if d == nil {
		return a, nil
	}
	if err := writeTomlSide(d.TomlPath, side); err != nil {
		a.statusMsg = "side change failed: " + err.Error()
		a.statusIsErr = true
		a.statusExpire = time.Now().Add(4 * time.Second)
		return a, a.expireStatus()
	}
	d.Side = side
	packDir := a.packDir
	go func() { RunPackwiz(packDir, "refresh") }()
	return a, a.loadMods()
}

// applyModOptional writes the optional flag and refreshes.
func (a *App) applyModOptional(optional bool) (tea.Model, tea.Cmd) {
	d := a.modDetail
	if d == nil || d.Optional == optional {
		return a, nil
	}
	if err := writeTomlOptional(d.TomlPath, optional); err != nil {
		a.statusMsg = "optional toggle failed: " + err.Error()
		a.statusIsErr = true
		a.statusExpire = time.Now().Add(4 * time.Second)
		return a, a.expireStatus()
	}
	d.Optional = optional
	if !optional && d.ctlIdx == mdCtlDefault {
		d.ctlIdx = mdCtlOptional // the default row just disappeared
	}
	packDir := a.packDir
	go func() { RunPackwiz(packDir, "refresh") }()
	return a, a.loadMods()
}

// applyModDefault writes the enabled-by-default flag and refreshes.
func (a *App) applyModDefault(def bool) (tea.Model, tea.Cmd) {
	d := a.modDetail
	if d == nil || !d.Optional || d.Default == def {
		return a, nil
	}
	if err := writeTomlDefault(d.TomlPath, def); err != nil {
		a.statusMsg = "default toggle failed: " + err.Error()
		a.statusIsErr = true
		a.statusExpire = time.Now().Add(4 * time.Second)
		return a, a.expireStatus()
	}
	d.Default = def
	packDir := a.packDir
	go func() { RunPackwiz(packDir, "refresh") }()
	return a, a.loadMods()
}

// activateModDetailCtl runs the selected control's action.
func (a *App) activateModDetailCtl(i int) (tea.Model, tea.Cmd) {
	d := a.modDetail
	if d == nil {
		return a, nil
	}
	flash := func(msg string) {
		a.statusMsg = msg
		a.statusIsErr = false
		a.statusExpire = time.Now().Add(3 * time.Second)
	}
	switch i {
	case mdCtlSide:
		return a.applyModSide(stepSide(d.Side, 1))
	case mdCtlOptional:
		return a.applyModOptional(!d.Optional)
	case mdCtlDefault:
		return a.applyModDefault(!d.Default)
	case mdCtlVersion:
		switch {
		case d.Source == "local":
			flash("local mods have no source to pick versions from")
			return a, a.expireStatus()
		case d.VersionsErr != "":
			flash("versions unavailable: " + d.VersionsErr)
			return a, a.expireStatus()
		case len(d.Versions) == 0:
			flash("still loading versions…")
			return a, a.expireStatus()
		default:
			d.dropOpen = true
			for vi, v := range d.Versions {
				if v.ID == d.VersionID {
					d.verIdx = vi
				}
			}
		}
	case mdCtlConvert:
		if d.Converting {
			return a, nil
		}
		target := ""
		switch d.Source {
		case "modrinth":
			target = "curseforge"
		case "curseforge":
			target = "modrinth"
		default:
			flash("local mods have no source to convert from")
			return a, a.expireStatus()
		}
		d.Converting = true
		d.ConvertErr = ""
		packDir, slug := a.packDir, d.Slug
		return a, func() tea.Msg {
			var buf strings.Builder
			err := ConvertSources(packDir, target, slug, &buf)
			return msgModConvertDone{slug: slug, report: buf.String(), err: err}
		}
	case mdCtlProject:
		var pageURL string
		switch d.Source {
		case "modrinth":
			pageURL = "https://modrinth.com/mod/" + d.Slug
		case "curseforge":
			pageURL = "https://www.curseforge.com/minecraft/mc-mods/" + d.Slug
		default:
			flash("local mods have no project page")
			return a, a.expireStatus()
		}
		go exec.Command("xdg-open", pageURL).Start()
		flash("Opening " + pageURL)
		return a, a.expireStatus()
	case mdCtlEdit:
		return a, a.openInEditor(d.TomlPath)
	case mdCtlDelete:
		path := d.TomlPath
		a.modDetail = nil
		a.detailPinned = false
		a.setHomeFocus(0)
		for mi, m := range a.modsFiltered {
			if m.Path == path {
				a.modsIdx = mi
			}
		}
		return a.deleteMod()
	}
	return a, nil
}

// installModVersion swaps the mod to a specific version on its source.
func (a *App) installModVersion(v modVersion) tea.Cmd {
	d := a.modDetail
	if d == nil {
		return nil
	}
	packDir, slug, projID, side, jar, source := a.packDir, d.Slug, d.ProjectID, d.Side, d.Filename, d.Source
	verID := v.ID
	a.statusMsg = "Installing " + v.Number + "…"
	a.statusIsErr = false
	a.statusExpire = time.Now().Add(30 * time.Second)
	return func() tea.Msg {
		if out, err := RunPackwiz(packDir, "remove", slug); err != nil {
			return msgModDetailDone{err: fmt.Errorf("remove failed: %s", tail(out, 3))}
		}
		var out string
		var err error
		if source == "curseforge" {
			out, err = RunPackwiz(packDir, "curseforge", "add", "--addon-id", projID, "--file-id", verID, "-y")
		} else {
			out, err = RunPackwiz(packDir, "modrinth", "add", "--project-id", projID, "--version-id", verID, "-y")
		}
		if err != nil {
			return msgModDetailDone{err: fmt.Errorf("add failed: %s", tail(out, 3))}
		}
		restoreSide(packDir, slug, jar, side)
		RunPackwiz(packDir, "refresh")
		return msgModDetailDone{}
	}
}

// modDetailClick routes the pane's mouse actions.
func (a *App) modDetailClick(action string) (tea.Model, tea.Cmd) {
	d := a.modDetail
	if d == nil {
		return a, nil
	}
	act := strings.TrimPrefix(action, "home:mdetail:")
	switch {
	case act == "close":
		a.modDetail = nil
		a.detailPinned = false
		a.setHomeFocus(0)
	case act == "focus":
		return a, a.focusHome(5)
	case strings.HasPrefix(act, "ctl:"):
		var i int
		fmt.Sscanf(act, "ctl:%d", &i)
		a.focusHome(5)
		d.ctlIdx = i
		return a.activateModDetailCtl(i)
	case strings.HasPrefix(act, "side:"):
		a.focusHome(5)
		d.ctlIdx = mdCtlSide
		return a.applyModSide(strings.TrimPrefix(act, "side:"))
	case act == "opt:on" || act == "opt:off":
		a.focusHome(5)
		d.ctlIdx = mdCtlOptional
		return a.applyModOptional(act == "opt:on")
	case act == "def:on" || act == "def:off":
		a.focusHome(5)
		d.ctlIdx = mdCtlDefault
		return a.applyModDefault(act == "def:on")
	case strings.HasPrefix(act, "ver:"):
		var i int
		fmt.Sscanf(act, "ver:%d", &i)
		if i < len(d.Versions) {
			d.dropOpen = false
			return a, a.installModVersion(d.Versions[i])
		}
	}
	return a, nil
}

// renderModDetailPane renders the mod configuration pane: fields on the left,
// a right-aligned stack of bordered action buttons.
func (a *App) renderModDetailPane(totalW, h, paneX, paneTop int) string {
	d := a.modDetail
	aw := totalW - 4
	focused := a.homeFocus == 5

	var lines []string
	zone := func(x, wdt, ht int, action string) {
		if len(lines) >= h-2 {
			return // row will be cropped — no phantom click target
		}
		a.clickZones = append(a.clickZones, clickZone{
			x: paneX + 2 + x, y: paneTop + 1 + len(lines), w: wdt, h: ht, action: action,
		})
	}
	sel := func(i int) bool { return focused && !d.dropOpen && d.ctlIdx == i }
	chip := func(text string, active bool) string {
		if active {
			return styleBtnFocused.Render(" " + text + " ")
		}
		return styleBtn.Render(" " + text + " ")
	}

	// ── Header ──
	titleStyle := styleCardTitleDim
	if focused {
		titleStyle = styleCardTitle
	}
	closeBtn := styleCardAccent.Render("✕")
	titleTxt := titleStyle.Render(truncate("◈ "+d.Name, maxInt(4, aw-3)))
	pad := maxInt(1, aw-lipgloss.Width(titleTxt)-1)
	zone(aw-2, 3, 1, "home:mdetail:close")
	lines = append(lines, titleTxt+styleCardFill.Render(strings.Repeat(" ", pad))+closeBtn)
	// Filename left, source badge right-aligned on the same line.
	srcStyle := styleCardMuted
	switch d.Source {
	case "modrinth":
		srcStyle = styleCardAccent
	case "curseforge":
		srcStyle = lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Background(colorBgPanel)
	}
	srcPart := styleCardMuted.Render("source: ") + srcStyle.Render(d.Source)
	fn := styleCardMuted.Render(truncate(d.Filename, maxInt(4, aw-lipgloss.Width(srcPart)-2)))
	fnPad := maxInt(1, aw-lipgloss.Width(fn)-lipgloss.Width(srcPart))
	lines = append(lines, fn+styleCardFill.Render(strings.Repeat(" ", fnPad))+srcPart)
	lines = append(lines, "")

	// ── Right button stack ──
	type btnDef struct {
		ctl     int
		text    string
		enabled bool
	}
	convertLabel := "⇄ Convert to Modrinth"
	if d.Source == "modrinth" {
		convertLabel = "⇄ Convert to CurseForge"
	}
	btns := []btnDef{
		{mdCtlConvert, convertLabel, d.Source != "local"},
		{mdCtlProject, "↗ Project page", d.Source != "local"},
		{mdCtlEdit, "✎ Edit metafile", true},
		{mdCtlDelete, "− Delete", true},
	}
	btnW := 0
	for _, b := range btns {
		btnW = maxInt(btnW, lipgloss.Width(b.text))
	}
	btnW += 4 // border + padding
	if d.Converting {
		// Swap the label after the width calc so the column doesn't jitter.
		btns[0].text = spinnerFrames[a.spinFrame] + " Converting…"
	}
	renderBtn := func(b btnDef) []string {
		if b.ctl == mdCtlDelete {
			// Danger styling: red border + red label, inverted when selected.
			border := styleCardBorder.Copy().BorderForeground(colorDanger)
			labelSt := lipgloss.NewStyle().Foreground(colorDanger).Background(colorBgPanel)
			if sel(b.ctl) {
				labelSt = lipgloss.NewStyle().Foreground(colorOnAccent).Background(colorDanger).Bold(true)
			}
			return strings.Split(cardStyled([]string{labelSt.Render(b.text)}, btnW-4, border), "\n")
		}
		st := styleCardText
		if !b.enabled {
			st = styleCardMuted
		} else if sel(b.ctl) {
			st = styleCardAccent
		}
		return strings.Split(card([]string{st.Render(b.text)}, btnW-4, sel(b.ctl)), "\n")
	}
	var btnLines []string
	for _, b := range btns {
		btnLines = append(btnLines, renderBtn(b)...)
	}

	twoCol := aw-btnW-2 >= 26
	lw := aw
	if twoCol {
		lw = aw - btnW - 2
	}

	// ── Field rows (left column) ──
	fieldTop := len(lines)
	var fields []string
	fieldZones := []func(rowIdx int){}
	addField := func(row string, register func(rowIdx int)) {
		fields = append(fields, row)
		fieldZones = append(fieldZones, register)
	}

	// Field groups are two lines each — emphasized bold label, then the
	// value/chips — with a blank line between groups.
	groupLabel := func(ctl int, text string) string {
		if sel(ctl) {
			return styleCardTitle.Render("▸ " + text)
		}
		return styleCardTitleDim.Render(text)
	}
	lineZone := func(action string) func(int) {
		return func(row int) {
			a.clickZones = append(a.clickZones, clickZone{
				x: paneX + 2, y: paneTop + 1 + fieldTop + row, w: lw, h: 1, action: action,
			})
		}
	}

	// Side.
	addField(groupLabel(mdCtlSide, "Side"), lineZone("home:mdetail:ctl:0"))
	sideRow := ""
	sx := 0
	sideXs := make([]int, 0, len(sideOrder))
	for _, s := range sideOrder {
		sideXs = append(sideXs, sx)
		sideRow += chip(s, s == d.Side) + styleCardFill.Render(" ")
		sx += len(s) + 3
	}
	addField(sideRow, func(row int) {
		for si, s := range sideOrder {
			a.clickZones = append(a.clickZones, clickZone{
				x: paneX + 2 + sideXs[si], y: paneTop + 1 + fieldTop + row,
				w: len(sideOrder[si]) + 2, h: 1, action: "home:mdetail:side:" + s,
			})
		}
	})
	addField("", nil)

	// Optional — same chip formatting as Side.
	addField(groupLabel(mdCtlOptional, "Optional"), lineZone("home:mdetail:ctl:1"))
	addField(chip("on", d.Optional)+styleCardFill.Render(" ")+chip("off", !d.Optional),
		func(row int) {
			a.clickZones = append(a.clickZones,
				clickZone{x: paneX + 2, y: paneTop + 1 + fieldTop + row, w: 4, h: 1, action: "home:mdetail:opt:on"},
				clickZone{x: paneX + 2 + 5, y: paneTop + 1 + fieldTop + row, w: 5, h: 1, action: "home:mdetail:opt:off"})
		})
	addField("", nil)

	// Enabled by default — only meaningful for optional mods.
	if d.Optional {
		addField(groupLabel(mdCtlDefault, "Enabled by default"), lineZone("home:mdetail:ctl:2"))
		addField(chip("yes", d.Default)+styleCardFill.Render(" ")+chip("no", !d.Default),
			func(row int) {
				a.clickZones = append(a.clickZones,
					clickZone{x: paneX + 2, y: paneTop + 1 + fieldTop + row, w: 5, h: 1, action: "home:mdetail:def:on"},
					clickZone{x: paneX + 2 + 6, y: paneTop + 1 + fieldTop + row, w: 4, h: 1, action: "home:mdetail:def:off"})
			})
		addField("", nil)
	}

	// Version.
	curVer := d.CurVersion
	if curVer == "" {
		if d.Source != "local" && d.VersionsErr == "" {
			curVer = "(loading…)"
		} else {
			curVer = "(unknown)"
		}
	}
	addField(groupLabel(mdCtlVersion, "Version"), lineZone("home:mdetail:ctl:3"))
	addField(styleCardText.Render(truncate(curVer, maxInt(4, lw-3)))+styleCardAccent.Render(" ▾"),
		lineZone("home:mdetail:ctl:3"))

	if d.dropOpen {
		dropMax := maxInt(1, h-2-fieldTop-len(fields)-1)
		start, end := visibleWindow(d.verIdx, len(d.Versions), dropMax)
		for vi := start; vi < end; vi++ {
			v := d.Versions[vi]
			verLabel := truncate("  "+v.Number, maxInt(4, lw-1))
			vidx := vi
			var row string
			if vi == d.verIdx {
				row = styleModItemSelected.Render(verLabel)
			} else {
				row = styleCardText.Render(verLabel)
			}
			addField(row, func(rowIdx int) {
				a.clickZones = append(a.clickZones, clickZone{
					x: paneX + 2, y: paneTop + 1 + fieldTop + rowIdx, w: lw, h: 1,
					action: fmt.Sprintf("home:mdetail:ver:%d", vidx),
				})
			})
		}
	}
	if d.VersionsErr != "" {
		addField(styleCardMuted.Render(truncate("versions: "+d.VersionsErr, lw)), nil)
	}

	// ── Merge columns ──
	if twoCol {
		rows := maxInt(len(fields), len(btnLines))
		for i := 0; i < rows; i++ {
			leftPart := ""
			if i < len(fields) {
				leftPart = fields[i]
				if fieldZones[i] != nil {
					fieldZones[i](i)
				}
			}
			if gap := lw - lipgloss.Width(leftPart); gap > 0 {
				leftPart += styleCardFill.Render(strings.Repeat(" ", gap))
			}
			rightPart := ""
			if i < len(btnLines) {
				rightPart = btnLines[i]
			}
			lines = append(lines, leftPart+styleCardFill.Render("  ")+rightPart)
		}
		// Button click zones (3 rows per bordered button).
		for k, b := range btns {
			if fieldTop+3*k+3 <= h-2 {
				a.clickZones = append(a.clickZones, clickZone{
					x: paneX + 2 + lw + 2, y: paneTop + 1 + fieldTop + 3*k, w: btnW, h: 3,
					action: fmt.Sprintf("home:mdetail:ctl:%d", b.ctl),
				})
			}
		}
	} else {
		for i, f := range fields {
			if fieldZones[i] != nil {
				fieldZones[i](i)
			}
			lines = append(lines, f)
		}
		lines = append(lines, "")
		base := len(lines)
		for k, b := range btns {
			if base+3*k+3 <= h-2 {
				a.clickZones = append(a.clickZones, clickZone{
					x: paneX + 2, y: paneTop + 1 + base + 3*k, w: btnW, h: 3,
					action: fmt.Sprintf("home:mdetail:ctl:%d", b.ctl),
				})
			}
		}
		for _, b := range btns {
			btnRows := renderBtn(b)
			lines = append(lines, btnRows...)
		}
	}

	// Conversion errors show in a centered danger box spanning the pane,
	// beneath both columns.
	if d.ConvertErr != "" {
		boxW := clamp(aw-8, 20, aw-4)
		dangerBorder := styleCardBorder.Copy().BorderForeground(colorDanger)
		dangerText := lipgloss.NewStyle().Foreground(colorDanger).Background(colorBgPanel)
		var errLines []string
		for _, wl := range wrapText(d.ConvertErr, boxW) {
			errLines = append(errLines, dangerText.Render(wl))
		}
		box := cardStyled(errLines, boxW, dangerBorder)
		lines = append(lines, "")
		for _, bl := range strings.Split(box, "\n") {
			off := maxInt(0, (aw-lipgloss.Width(bl))/2)
			lines = append(lines, styleCardFill.Render(strings.Repeat(" ", off))+bl)
		}
	}

	for len(lines) < h-2 {
		lines = append(lines, "")
	}
	// Pane-wide focus zone last so the specific zones above win.
	a.clickZones = append(a.clickZones, clickZone{
		x: paneX, y: paneTop, w: totalW, h: h, action: "home:mdetail:focus",
	})
	return card(lines[:minInt(len(lines), h-2)], aw, focused)
}
