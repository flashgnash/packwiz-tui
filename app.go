package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Screen represents which view is active.
type Screen int

const (
	ScreenLoading Screen = iota
	ScreenRepoSelect
	ScreenCloneRepo
	ScreenMainMenu
	ScreenManageMods
	ScreenManageLoader
	ScreenOutput
)

// ── Messages ─────────────────────────────────────────────────────────────────

type msgReposLoaded struct{ repos []RepoEntry }
type msgPackFound struct {
	packDir  string
	packName string
	repoRoot string
}
type msgPackError struct{ err error }
type msgModsLoaded struct{ mods []ModFile }
type msgCmdDone struct {
	output string
	err    error
}
type msgSpinTick struct{}
type msgStatusExpire struct{}

// ── App ───────────────────────────────────────────────────────────────────────

type App struct {
	screen Screen
	width  int
	height int

	// Spinner
	spinFrame int

	// Repo selection
	repoList    []RepoEntry
	repoListIdx int

	// Clone
	cloneInput textinput.Model
	cloneError string

	// Pack state
	repoRoot string
	packDir  string
	packName string

	// Main menu
	menuIdx int

	// Mods management
	mods         []ModFile
	modsFiltered []ModFile
	modsIdx      int
	searchInput  textinput.Model
	searchFocus  bool
	addModModal  bool
	addModInput  textinput.Model

	// Output screen
	outputLines []string
	outputErr   bool
	outputDone  bool

	// Loading
	loadingMsg string

	// Status bar flash
	statusMsg    string
	statusIsErr  bool
	statusExpire time.Time

	// Mouse click zones
	clickZones []clickZone
}

type clickZone struct {
	x, y, w, h int
	action      string // "add_mod" or "del:N"
}

func NewApp() *App {
	clone := textinput.New()
	clone.Placeholder = "https://github.com/user/modpack"
	clone.CharLimit = 256
	clone.Width = 50

	search := textinput.New()
	search.Placeholder = "search mods…"
	search.CharLimit = 64
	search.Width = 32

	addMod := textinput.New()
	addMod.Placeholder = "e.g. jei"
	addMod.CharLimit = 128
	addMod.Width = 40

	return &App{
		screen:      ScreenLoading,
		loadingMsg:  "Detecting git repository…",
		cloneInput:  clone,
		searchInput: search,
		addModInput: addMod,
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (a *App) Init() tea.Cmd {
	return tea.Batch(a.tickSpinner(), a.detectRepo())
}

// ── Commands ──────────────────────────────────────────────────────────────────

func (a *App) tickSpinner() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return msgSpinTick{} })
}

func (a *App) detectRepo() tea.Cmd {
	return func() tea.Msg {
		root, err := DetectGitRepo()
		if err == nil && root != "" {
			packToml, err2 := FindPackToml(root)
			if err2 == nil {
				packDir := filepath.Dir(packToml)
				name := parsePackName(packToml)
				remote := GetGitRemote(root)
				AddRecentRepo(RepoEntry{
					Name:     GetRepoName(remote, root),
					Path:     root,
					Remote:   remote,
					LastUsed: time.Now().Format(time.RFC3339),
				})
				return msgPackFound{packDir: packDir, packName: name, repoRoot: root}
			}
		}
		return msgReposLoaded{repos: LoadRecentRepos()}
	}
}

func (a *App) loadPackFromRepo(repo RepoEntry) tea.Cmd {
	return func() tea.Msg {
		packToml, err := FindPackToml(repo.Path)
		if err != nil {
			return msgPackError{fmt.Errorf("pack.toml not found in %s", repo.Path)}
		}
		packDir := filepath.Dir(packToml)
		name := parsePackName(packToml)
		remote := GetGitRemote(repo.Path)
		AddRecentRepo(RepoEntry{
			Name:     GetRepoName(remote, repo.Path),
			Path:     repo.Path,
			Remote:   remote,
			LastUsed: time.Now().Format(time.RFC3339),
		})
		return msgPackFound{packDir: packDir, packName: name, repoRoot: repo.Path}
	}
}

func (a *App) cloneRepo(url string) tea.Cmd {
	return func() tea.Msg {
		home, _ := os.UserHomeDir()
		parts := strings.Split(strings.TrimSuffix(url, ".git"), "/")
		folder := "modpack"
		if len(parts) > 0 {
			folder = parts[len(parts)-1]
		}
		target := filepath.Join(home, "modpacks", folder)
		out, err := CloneRepo(url, target)
		if err != nil {
			return msgCmdDone{output: out, err: err}
		}
		packToml, err2 := FindPackToml(target)
		if err2 != nil {
			return msgCmdDone{output: "Cloned but pack.toml not found:\n" + out, err: err2}
		}
		packDir := filepath.Dir(packToml)
		name := parsePackName(packToml)
		remote := GetGitRemote(target)
		AddRecentRepo(RepoEntry{
			Name:     GetRepoName(remote, target),
			Path:     target,
			Remote:   remote,
			LastUsed: time.Now().Format(time.RFC3339),
		})
		return msgPackFound{packDir: packDir, packName: name, repoRoot: target}
	}
}

func (a *App) loadMods() tea.Cmd {
	return func() tea.Msg {
		mods, err := ListModFiles(a.packDir)
		if err != nil {
			return msgCmdDone{output: err.Error(), err: err}
		}
		return msgModsLoaded{mods: mods}
	}
}

func (a *App) runPackwiz(args ...string) tea.Cmd {
	return func() tea.Msg {
		out, err := RunPackwiz(a.packDir, args...)
		return msgCmdDone{output: out, err: err}
	}
}

func (a *App) gitPush() tea.Cmd {
	return func() tea.Msg {
		out, err := GitPushAll(a.repoRoot)
		return msgCmdDone{output: out, err: err}
	}
}

func (a *App) expireStatus() tea.Cmd {
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return msgStatusExpire{} })
}

// ── Update ────────────────────────────────────────────────────────────────────

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {

	case tea.MouseMsg:
		if m.Action == tea.MouseActionRelease && m.Button == tea.MouseButtonLeft {
			for _, z := range a.clickZones {
				if m.X >= z.x && m.X < z.x+z.w && m.Y >= z.y && m.Y < z.y+z.h {
					switch z.action {
					case "add_mod":
						a.addModModal = true
						a.addModInput.Focus()
						return a, textinput.Blink
					default:
						if strings.HasPrefix(z.action, "del:") {
							var idx int
							fmt.Sscanf(z.action, "del:%d", &idx)
							if idx < len(a.modsFiltered) {
								a.modsIdx = idx
								return a.deleteMod()
							}
						} else if strings.HasPrefix(z.action, "menu:") {
							var idx int
							fmt.Sscanf(z.action, "menu:%d", &idx)
							a.menuIdx = idx
							return a.activateMenuItem()
						}
					}
				}
			}
		}
		return a, nil

	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		return a, nil

	case msgSpinTick:
		a.spinFrame = (a.spinFrame + 1) % len(spinnerFrames)
		return a, a.tickSpinner()

	case msgStatusExpire:
		if time.Now().After(a.statusExpire) {
			a.statusMsg = ""
		}
		return a, nil

	case msgReposLoaded:
		a.repoList = m.repos
		a.screen = ScreenRepoSelect
		return a, nil

	case msgPackFound:
		a.packDir = m.packDir
		a.packName = m.packName
		a.repoRoot = m.repoRoot
		a.screen = ScreenMainMenu
		a.menuIdx = 0
		return a, nil

	case msgPackError:
		a.statusMsg = "Error: " + m.err.Error()
		a.statusIsErr = true
		a.statusExpire = time.Now().Add(4 * time.Second)
		a.screen = ScreenRepoSelect
		return a, a.expireStatus()

	case msgModsLoaded:
		a.mods = m.mods
		a.filterMods()
		a.screen = ScreenManageMods
		return a, nil

	case msgCmdDone:
		a.outputLines = strings.Split(strings.TrimSpace(m.output), "\n")
		a.outputErr = m.err != nil
		a.outputDone = true
		return a, nil
	}

	// Delegate to the active screen.
	switch a.screen {
	case ScreenRepoSelect:
		return a.updateRepoSelect(msg)
	case ScreenCloneRepo:
		return a.updateCloneRepo(msg)
	case ScreenMainMenu:
		return a.updateMainMenu(msg)
	case ScreenManageMods:
		return a.updateManageMods(msg)
	case ScreenManageLoader:
		return a.updateManageLoader(msg)
	case ScreenOutput:
		return a.updateOutput(msg)
	}
	return a, nil
}

// ── Screen updaters ───────────────────────────────────────────────────────────

func (a *App) updateRepoSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, ok := msg.(tea.KeyMsg)
	if !ok {
		return a, nil
	}
	total := len(a.repoList) + 1 // +1 for "clone new"
	switch m.String() {
	case "ctrl+c", "q":
		return a, tea.Quit
	case "up", "k":
		a.repoListIdx = (a.repoListIdx - 1 + total) % total
	case "down", "j":
		a.repoListIdx = (a.repoListIdx + 1) % total
	case "enter", " ":
		if a.repoListIdx == len(a.repoList) {
			a.screen = ScreenCloneRepo
			a.cloneInput.Focus()
			return a, textinput.Blink
		}
		repo := a.repoList[a.repoListIdx]
		a.loadingMsg = "Loading pack…"
		a.screen = ScreenLoading
		return a, a.loadPackFromRepo(repo)
	}
	return a, nil
}

func (a *App) updateCloneRepo(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(tea.KeyMsg); ok {
		switch m.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "esc":
			a.screen = ScreenRepoSelect
			a.cloneInput.Blur()
			return a, nil
		case "enter":
			url := strings.TrimSpace(a.cloneInput.Value())
			if url == "" {
				a.cloneError = "Please enter a URL"
				return a, nil
			}
			a.cloneError = ""
			a.loadingMsg = "Cloning repository…"
			a.screen = ScreenLoading
			return a, a.cloneRepo(url)
		}
	}
	var cmd tea.Cmd
	a.cloneInput, cmd = a.cloneInput.Update(msg)
	return a, cmd
}

var mainMenuItems = []struct{ icon, label string }{
	{"◈", "Manage Mods"},
	{"⚙", "Manage Loader"},
	{"↑", "Push & Exit"},
	{"✕", "Exit without Pushing"},
}

func (a *App) updateMainMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, ok := msg.(tea.KeyMsg)
	if !ok {
		return a, nil
	}
	switch m.String() {
	case "ctrl+c", "q":
		return a, tea.Quit
	case "up", "k":
		a.menuIdx = (a.menuIdx - 1 + len(mainMenuItems)) % len(mainMenuItems)
	case "down", "j":
		a.menuIdx = (a.menuIdx + 1) % len(mainMenuItems)
	case "1":
		a.menuIdx = 0
		return a.activateMenuItem()
	case "2":
		a.menuIdx = 1
		return a.activateMenuItem()
	case "3":
		a.menuIdx = 2
		return a.activateMenuItem()
	case "4":
		a.menuIdx = 3
		return a.activateMenuItem()
	case "enter", " ":
		return a.activateMenuItem()
	}
	return a, nil
}

func (a *App) activateMenuItem() (tea.Model, tea.Cmd) {
	switch a.menuIdx {
	case 0:
		a.loadingMsg = "Loading mods…"
		a.screen = ScreenLoading
		return a, a.loadMods()
	case 1:
		a.screen = ScreenManageLoader
	case 2:
		a.startOutput()
		return a, a.gitPush()
	case 3:
		return a, tea.Quit
	}
	return a, nil
}

func (a *App) updateManageLoader(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, ok := msg.(tea.KeyMsg)
	if !ok {
		return a, nil
	}
	switch m.String() {
	case "ctrl+c", "q":
		return a, tea.Quit
	case "esc":
		a.screen = ScreenMainMenu
	}
	return a, nil
}

func (a *App) updateManageMods(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Modal captures all input.
	if a.addModModal {
		if m, ok := msg.(tea.KeyMsg); ok {
			switch m.String() {
			case "ctrl+c":
				return a, tea.Quit
			case "esc":
				a.addModModal = false
				a.addModInput.SetValue("")
				return a, nil
			case "enter":
				name := strings.TrimSpace(a.addModInput.Value())
				if name == "" {
					return a, nil
				}
				a.addModModal = false
				a.addModInput.SetValue("")
				a.startOutput()
				return a, a.runPackwiz("mr", "add", name)
			}
		}
		var cmd tea.Cmd
		a.addModInput, cmd = a.addModInput.Update(msg)
		return a, cmd
	}

	if m, ok := msg.(tea.KeyMsg); ok {
		switch m.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "esc":
			if a.searchFocus {
				if a.searchInput.Value() == "" {
					a.searchFocus = false
					a.searchInput.Blur()
				}
				// else let textinput handle backspace
			} else {
				a.screen = ScreenMainMenu
				return a, nil
			}
		case "/":
			if !a.searchFocus {
				a.searchFocus = true
				a.searchInput.Focus()
				return a, textinput.Blink
			}
		case "n":
			a.addModModal = true
			a.addModInput.Focus()
			return a, textinput.Blink
		case "up", "k":
			if !a.searchFocus && a.modsIdx > 0 {
				a.modsIdx--
			}
		case "down", "j":
			if !a.searchFocus && a.modsIdx < len(a.modsFiltered)-1 {
				a.modsIdx++
			}
		case "enter":
			if a.searchFocus {
				a.searchFocus = false
				a.searchInput.Blur()
			}
		case "d":
			if !a.searchFocus {
				return a.deleteMod()
			}
		}
	}

	if a.searchFocus {
		prev := a.searchInput.Value()
		var cmd tea.Cmd
		a.searchInput, cmd = a.searchInput.Update(msg)
		if a.searchInput.Value() != prev {
			a.filterMods()
			a.modsIdx = 0
		}
		return a, cmd
	}

	return a, nil
}

func (a *App) deleteMod() (tea.Model, tea.Cmd) {
	if len(a.modsFiltered) == 0 || a.modsIdx >= len(a.modsFiltered) {
		return a, nil
	}
	mod := a.modsFiltered[a.modsIdx]
	if err := os.Remove(mod.Path); err != nil {
		a.statusMsg = "Delete failed: " + err.Error()
		a.statusIsErr = true
		a.statusExpire = time.Now().Add(4 * time.Second)
		return a, a.expireStatus()
	}
	a.startOutput()
	return a, a.runPackwiz("refresh")
}

func (a *App) updateOutput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(tea.KeyMsg); ok {
		switch m.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "q", "esc", "enter":
			if a.outputDone {
				if a.outputErr {
					a.screen = ScreenMainMenu
				} else {
					a.loadingMsg = "Refreshing mod list…"
					a.screen = ScreenLoading
					return a, a.loadMods()
				}
			}
		}
	}
	if m, ok := msg.(msgCmdDone); ok {
		a.outputLines = strings.Split(strings.TrimSpace(m.output), "\n")
		a.outputErr = m.err != nil
		a.outputDone = true
	}
	return a, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (a *App) startOutput() {
	a.screen = ScreenOutput
	a.outputLines = nil
	a.outputDone = false
	a.outputErr = false
}

func (a *App) filterMods() {
	q := strings.ToLower(a.searchInput.Value())
	if q == "" {
		a.modsFiltered = make([]ModFile, len(a.mods))
		copy(a.modsFiltered, a.mods)
		return
	}
	a.modsFiltered = nil
	for _, m := range a.mods {
		if strings.Contains(strings.ToLower(m.Name), q) {
			a.modsFiltered = append(a.modsFiltered, m)
		}
	}
	if a.modsIdx >= len(a.modsFiltered) {
		a.modsIdx = 0
	}
}

// ── View ──────────────────────────────────────────────────────────────────────

func (a *App) View() string {
	if a.width == 0 {
		return "Loading…"
	}
	var body string
	switch a.screen {
	case ScreenLoading:
		body = a.viewLoading()
	case ScreenRepoSelect:
		body = a.viewRepoSelect()
	case ScreenCloneRepo:
		body = a.viewCloneRepo()
	case ScreenMainMenu:
		body = a.viewMainMenu()
	case ScreenManageMods:
		body = a.viewManageMods()
	case ScreenManageLoader:
		body = a.viewManageLoader()
	case ScreenOutput:
		body = a.viewOutput()
	default:
		body = "unknown screen"
	}

	statusBar := a.viewStatusBar()
	return lipgloss.JoinVertical(lipgloss.Left, body, statusBar)
}

func (a *App) viewStatusBar() string {
	var hints []string
	switch a.screen {
	case ScreenRepoSelect:
		hints = []string{"↑↓ navigate", "enter select", "q quit"}
	case ScreenCloneRepo:
		hints = []string{"enter clone", "esc back", "ctrl+c quit"}
	case ScreenMainMenu:
		hints = []string{"↑↓ navigate", "enter select", "1-4 shortcut", "q quit"}
	case ScreenManageMods:
		hints = []string{"/ search", "n add", "d delete", "esc back"}
	case ScreenOutput:
		if a.outputDone {
			hints = []string{"enter continue"}
		} else {
			hints = []string{"running…"}
		}
	}

	// Render each hint as "key desc" with a separator between them.
	sep := "  " + styleStatusSep.Render("│") + "  "
	mutedStyle := lipgloss.NewStyle().Foreground(colorMuted)
	var rendered []string
	for _, h := range hints {
		idx := strings.Index(h, " ")
		if idx > 0 {
			rendered = append(rendered, styleStatusKey.Render(h[:idx])+mutedStyle.Render(h[idx:]))
		} else {
			rendered = append(rendered, mutedStyle.Render(h))
		}
	}

	var right string
	if a.statusMsg != "" {
		if a.statusIsErr {
			right = lipgloss.NewStyle().Foreground(colorDanger).Render("✗ " + a.statusMsg)
		} else {
			right = lipgloss.NewStyle().Foreground(colorSuccess).Render("✓ " + a.statusMsg)
		}
	} else if a.packName != "" {
		right = mutedStyle.Render("pack: ") + styleHighlight.Render(a.packName)
	}

	left := strings.Join(rendered, sep)
	gap := a.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 0 {
		gap = 0
	}

	return left + strings.Repeat(" ", gap) + right
}

// ── Parsing ───────────────────────────────────────────────────────────────────

func parsePackName(packToml string) string {
	data, err := os.ReadFile(packToml)
	if err != nil {
		return filepath.Base(filepath.Dir(packToml))
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.Trim(strings.TrimSpace(parts[1]), `"'`)
			}
		}
	}
	return filepath.Base(filepath.Dir(packToml))
}
