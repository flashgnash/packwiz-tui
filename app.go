package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	ScreenInteractive
)

// ── Messages ─────────────────────────────────────────────────────────────────

type msgReposLoaded struct{ repos []RepoEntry }
type msgPackFound struct {
	packDir  string
	packName string
	repoRoot string
}
type msgPackError struct{ err error }
type msgModsLoaded struct {
	mods     []ModFile
	modified map[string]bool
	added    map[string]bool
	deleted  map[string]bool
}
type msgCmdDone struct {
	output string
	err    error
}
type msgInteractivePrompt struct {
	prompt  string
	options []string
	cmd     *interactiveCmd
}
type msgSpinTick struct{}
type msgStatusExpire struct{}
type msgEditorDone struct {
	err      error
	filePath string
	modTime  time.Time
}

// interactiveCmd holds state for a command waiting on user input.
type interactiveCmd struct {
	packDir string
	args    []string
	input   string
}

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
	modsModified map[string]bool // track modified mods by path (from git)
	modsAdded    map[string]bool // track added mods by path (from git)
	modsDeleted  map[string]bool // track deleted mods by path (from git)
	searchInput  textinput.Model
	searchFocus  bool
	addModModal  bool
	addModInput  textinput.Model

	// Output screen
	outputLines []string
	outputErr   bool
	outputDone  bool

	// Interactive prompt
	interactivePrompt   string
	interactiveOptions  []string
	interactiveSelected int
	interactivePending  *interactiveCmd

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
		screen:       ScreenLoading,
		loadingMsg:   "Detecting git repository…",
		cloneInput:   clone,
		searchInput:  search,
		addModInput:  addMod,
		modsDeleted:  make(map[string]bool),
		modsModified: make(map[string]bool),
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
		// Get git status for modified, added, and deleted files
		modified, added, deleted, _ := GetGitStatus(a.repoRoot)

		// Add deleted files to the mods list so they show up
		modsDir := filepath.Join(a.packDir, "mods")
		modsDirNorm := filepath.Clean(modsDir) + string(filepath.Separator) // Add separator for proper prefix matching

		for deletedPath := range deleted {
			deletedNorm := filepath.Clean(deletedPath)

			// Check if this deleted file is in the mods directory
			// Must either be exact dir match or have dir as prefix with separator
			inModsDir := strings.HasPrefix(deletedNorm+string(filepath.Separator), modsDirNorm) ||
				filepath.Dir(deletedNorm) == filepath.Clean(modsDir)

			if !inModsDir {
				continue
			}

			// Check if already in list (shouldn't be, but just in case)
			found := false
			for _, m := range mods {
				if m.Path == deletedPath {
					found = true
					break
				}
			}
			if !found {
				filename := filepath.Base(deletedPath)
				if strings.HasSuffix(filename, ".toml") {
					mods = append(mods, ModFile{
						Name:     strings.TrimSuffix(filename, ".toml"),
						Filename: filename,
						Path:     filepath.Clean(deletedPath),
					})
				}
			}
		}

		// Sort all mods alphabetically by name
		sort.Slice(mods, func(i, j int) bool {
			return strings.ToLower(mods[i].Name) < strings.ToLower(mods[j].Name)
		})

		return msgModsLoaded{mods: mods, modified: modified, added: added, deleted: deleted}
	}
}

func (a *App) runPackwiz(args ...string) tea.Cmd {
	return func() tea.Msg {
		out, prompt, err := RunPackwizInteractive(a.packDir, args...)
		if prompt != nil {
			// Interactive prompt detected
			return msgInteractivePrompt{
				prompt:  prompt.Prompt,
				options: prompt.Options,
				cmd: &interactiveCmd{
					packDir: a.packDir,
					args:    args,
				},
			}
		}
		return msgCmdDone{output: out, err: err}
	}
}

func (a *App) runPackwizWithInput(input string, cmd *interactiveCmd) tea.Cmd {
	return func() tea.Msg {
		out, err := RunPackwizWithInput(cmd.packDir, input, cmd.args...)
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

func (a *App) openInEditor(filePath string) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		// Try common fallbacks
		for _, e := range []string{"vim", "vi", "nano", "emacs"} {
			if _, err := exec.LookPath(e); err == nil {
				editor = e
				break
			}
		}
	}
	if editor == "" {
		return func() tea.Msg {
			return msgEditorDone{err: fmt.Errorf("no editor found (set $EDITOR)"), filePath: filePath}
		}
	}

	// Get modification time before opening
	var modTime time.Time
	if info, err := os.Stat(filePath); err == nil {
		modTime = info.ModTime()
	}

	c := exec.Command(editor, filePath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return msgEditorDone{err: err, filePath: filePath, modTime: modTime}
	})
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
								// Temporarily set index for deleteMod to work on the right item
								oldIdx := a.modsIdx
								a.modsIdx = idx
								model, cmd := a.deleteMod()
								// Restore original selection to prevent jumping
								a.modsIdx = oldIdx
								return model, cmd
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
		a.modsModified = m.modified
		a.modsAdded = m.added
		a.modsDeleted = m.deleted
		a.filterMods()
		a.screen = ScreenManageMods
		return a, nil

	case msgCmdDone:
		// Add error details to output for debugging
		outputText := m.output
		if m.err != nil && outputText == "" {
			outputText = "Error: " + m.err.Error()
		} else if m.err != nil {
			outputText = outputText + "\n\nError: " + m.err.Error()
		}
		a.outputLines = strings.Split(strings.TrimSpace(outputText), "\n")
		a.outputErr = m.err != nil
		a.outputDone = true
		return a, nil

	case msgInteractivePrompt:
		a.interactivePrompt = m.prompt
		a.interactiveOptions = m.options
		a.interactiveSelected = 0
		a.interactivePending = m.cmd
		a.screen = ScreenInteractive
		return a, nil

	case msgEditorDone:
		// Manually re-enable mouse mode after editor (tea.ExecProcess bug workaround)
		restoreMouse := func() tea.Msg {
			// Send ANSI escape codes to re-enable mouse tracking
			fmt.Print("\033[?1000h")  // Enable mouse button tracking
			fmt.Print("\033[?1003h")  // Enable all mouse motion tracking
			fmt.Print("\033[?1006h")  // Enable SGR extended mouse mode
			return tea.WindowSizeMsg{Width: a.width, Height: a.height}
		}

		if m.err != nil {
			a.statusMsg = "Editor error: " + m.err.Error()
			a.statusIsErr = true
			a.statusExpire = time.Now().Add(4 * time.Second)
			return a, tea.Batch(restoreMouse, a.expireStatus())
		}

		// Check if file was modified
		fileChanged := false
		if info, err := os.Stat(m.filePath); err == nil {
			fileChanged = info.ModTime().After(m.modTime)
		}

		if fileChanged {
			// Run packwiz refresh in background, then reload mods
			go func() {
				RunPackwiz(a.packDir, "refresh")
			}()
			a.statusMsg = "File saved, refreshing... (press r if mouse broken)"
			a.statusIsErr = false
			a.statusExpire = time.Now().Add(4 * time.Second)
			// Reload mods to get updated list and git status
			return a, tea.Batch(restoreMouse, a.expireStatus(), a.loadMods())
		}

		a.statusMsg = "No changes (press r if mouse broken)"
		a.statusIsErr = false
		a.statusExpire = time.Now().Add(4 * time.Second)
		// Still reload to update git status
		return a, tea.Batch(restoreMouse, a.expireStatus(), a.loadMods())
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
	case ScreenInteractive:
		return a.updateInteractive(msg)
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
			} else if len(a.modsFiltered) > 0 && a.modsIdx < len(a.modsFiltered) {
				return a, a.openInEditor(a.modsFiltered[a.modsIdx].Path)
			}
		case "r":
			if !a.searchFocus {
				// Force reload to refresh git status and UI
				return a, a.loadMods()
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

	// Check if already deleted (according to git) - if so, restore it
	if a.modsDeleted[mod.Path] {
		if err := GitCheckoutFile(a.repoRoot, mod.Path); err != nil {
			a.statusMsg = "Restore failed: " + err.Error()
			a.statusIsErr = true
			a.statusExpire = time.Now().Add(4 * time.Second)
			return a, a.expireStatus()
		}
		// Run packwiz refresh and reload
		go func() {
			RunPackwiz(a.packDir, "refresh")
		}()
		a.statusMsg = "Restored " + mod.Name
		a.statusIsErr = false
		a.statusExpire = time.Now().Add(2 * time.Second)
		// Reload to update git status
		return a, tea.Batch(a.expireStatus(), a.loadMods())
	}

	// Not deleted, so delete it
	if err := os.Remove(mod.Path); err != nil {
		a.statusMsg = "Delete failed: " + err.Error()
		a.statusIsErr = true
		a.statusExpire = time.Now().Add(4 * time.Second)
		return a, a.expireStatus()
	}
	// Run packwiz refresh and reload
	go func() {
		RunPackwiz(a.packDir, "refresh")
	}()
	a.statusMsg = "Deleted " + mod.Name
	a.statusIsErr = false
	a.statusExpire = time.Now().Add(2 * time.Second)
	// Reload to update git status
	return a, tea.Batch(a.expireStatus(), a.loadMods())
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

func (a *App) updateInteractive(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, ok := msg.(tea.KeyMsg)
	if !ok {
		return a, nil
	}
	switch m.String() {
	case "ctrl+c":
		return a, tea.Quit
	case "esc":
		// Cancel and go back
		a.screen = ScreenManageMods
		return a, nil
	case "up", "k":
		if a.interactiveSelected > 0 {
			a.interactiveSelected--
		}
	case "down", "j":
		if a.interactiveSelected < len(a.interactiveOptions)-1 {
			a.interactiveSelected++
		}
	case "enter", " ":
		// User selected an option (0-indexed, packwiz uses 0 for cancel)
		selection := fmt.Sprintf("%d", a.interactiveSelected)
		a.startOutput()
		return a, a.runPackwizWithInput(selection, a.interactivePending)
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
	case ScreenInteractive:
		body = a.viewInteractive()
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
		hints = []string{"enter edit", "r refresh", "/ search", "n add", "d delete/restore", "esc back"}
	case ScreenOutput:
		if a.outputDone {
			hints = []string{"enter continue"}
		} else {
			hints = []string{"running…"}
		}
	case ScreenInteractive:
		hints = []string{"↑↓ navigate", "enter select", "esc cancel"}
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
