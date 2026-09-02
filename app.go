package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
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
	ScreenCreateRepo
	ScreenMainMenu
	ScreenManageMods
	ScreenManageLoader
	ScreenOutput
	ScreenInteractive
	ScreenServerIP
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
	sources []string
	cmd     *interactiveCmd
}
type msgSpinTick struct{}
type msgStatusExpire struct{}
type msgEditorDone struct {
	err      error
	filePath string
	modTime  time.Time
}
type msgLazygitDone struct{ err error }
type msgSelfCmdDone struct{ err error }
type msgHomeCmdLine struct{ line string }
type msgHomeCmdExit struct{ err error }
type msgCmdPaneAutoClose struct{ gen int }
type msgAddModDebounce struct{ seq int }
type msgAddModResults struct {
	seq  int
	hits []ModHit
	err  error
}
type msgAddModImg struct{ key, block string }

// testSummary holds the stats shown after a test run finishes.
type testSummary struct {
	passed  bool
	errText string
	elapsed string
	tps     []string
	shots   string
}

// summarizeTest extracts the interesting stats from a test run's output.
func summarizeTest(lines []string, err error) *testSummary {
	s := &testSummary{passed: err == nil}
	if err != nil {
		s.errText = err.Error()
	}
	for _, l := range lines {
		t := strings.TrimSpace(l)
		lower := strings.ToLower(t)
		switch {
		case strings.HasPrefix(lower, "elapsed:"):
			s.elapsed = strings.TrimSpace(t[len("elapsed:"):])
		case strings.Contains(lower, "screenshots in"):
			s.shots = t
		case strings.Contains(lower, "tps") && !strings.HasPrefix(lower, "sampling"):
			if len(s.tps) < 8 {
				s.tps = append(s.tps, t)
			}
		}
	}
	return s
}
type msgPrismDone struct {
	out string
	err error
}
type msgClientState struct{ up bool }
type msgRepoCreated struct{ path string }
type msgCreateRepoErr struct{ err error }
type msgPackwizInitDone struct {
	path string
	err  error
}

// agentEntry is one line of the embedded chat transcript.
type agentEntry struct {
	role string // "user", "agent", "error"
	text string
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

	// Create repo
	createNameInput textinput.Model
	createPrivate   bool
	createError     string

	// Server address prefill
	serverIPInput textinput.Model
	serverIPError string
	cloneError string

	// Pack state
	repoRoot string
	packDir  string
	packName string

	// Main menu (narrow list fallback)
	menuIdx int

	// Home dashboard (wide layout)
	homeFocus  int // 0 mods, 1 agent, 2 buttons, 3 IP box, 4 loader box
	homeBtnIdx   int
	serverAddr   string
	packMeta     PackMeta
	repoRemote   string
	clientUp     bool // the pack's client is running (polled)
	clientPollOn bool
	changedCount int // uncommitted files in the repo (shown on Push & Exit)

	// Streaming command pane (right of the agent chat)
	cmdRunning bool
	cmdDone    bool
	cmdErr     error
	cmdTitle   string
	cmdLines   []string
	cmdProc    *exec.Cmd
	cmdCh      chan tea.Msg
	cmdIsTest  bool         // tests end on a stats summary instead of closing
	cmdGen     int          // invalidates stale auto-close timers
	cmdCloseAt time.Time    // when a finished non-test pane auto-closes
	cmdSummary *testSummary // post-run stats for tests

	// Info popup (over the home screen)
	infoTitle string
	infoText  string
	infoErr   bool

	// Mod detail pane (left of the agent chat). Unpinned panes follow the
	// mod list selection and close when focus leaves the list; pinned panes
	// (opened with enter / focused directly) stay until closed manually.
	modDetail    *ModDetail
	detailPinned bool
	// Session caches so switching between mods doesn't hammer the APIs:
	// version lists by source:projectID, resolved numbers by file sha1.
	modVerCache map[string]cachedVersions
	modHashVer  map[string]string

	// Discard confirmation popup
	confirmDiscard bool

	// Sources popup
	sourcesModal bool
	sourcesSel   int // 0 = convert to curseforge, 1 = convert to modrinth
	sourceCounts SourceCounts

	// Embedded agent chat
	agentInput   textinput.Model
	agentEntries []agentEntry
	agentRunning bool
	agentStarted bool // a session exists → use --continue
	agentFull    bool
	// Headless -p turns can't show interactive permission prompts, so the
	// mode picks how much is pre-approved: 0 default (unapproved tools
	// fail), 1 AUTO (accept edits), 2 YOLO (skip all permission checks).
	agentMode int

	// Permission-prompt bridge (see approve.go)
	approveLn      net.Listener
	approveCh      chan *approveReq
	approvePending *approveReq
	mcpConfigPath  string
	// True while agent focus was gained by tab-cycling and nothing else has
	// happened yet — shift+tab keeps cycling instead of toggling auto, so
	// spamming shift+tab travels through the pane.
	agentFocusByCycle bool
	agentScroll  int  // lines scrolled up from the transcript bottom

	// Mods management
	mods            []ModFile
	modsFiltered    []ModFile
	modsIdx         int
	modsModified    map[string]bool // track modified mods by path (from git)
	modsAdded       map[string]bool // track added mods by path (from git)
	modsDeleted     map[string]bool // track deleted mods by path (from git)
	searchInput     textinput.Model
	searchFocus     bool
	addModModal     bool
	addModInput     textinput.Model
	returnToAddModal bool // flag to reopen add modal after command completes
	// Live search state for the add-mod popup: results from both APIs,
	// debounced so typing doesn't fire a request per keystroke.
	addModHits      []ModHit
	addModIdx       int
	addModSeq       int    // bumped on every edit; stale debounce ticks/results are dropped
	addModQuery     string // last query actually dispatched to the APIs
	addModSearching bool
	addModErr       string
	addModBtnIdx    int // focused preview button (install/open per source)
	addModShotIdx   int // first gallery screenshot of the visible page
	addModPrevScroll int // preview pane scroll offset (lines)
	// Fetched image blocks by imgKey (kitty placeholders or half-block art;
	// "" = failed, pendingImg = in flight), and the kitty image id counter.
	addModImgs   map[string]string
	kittyImgSeq  int

	// Output screen
	outputLines []string
	outputErr   bool
	outputDone  bool

	// Interactive prompt
	interactivePrompt   string
	interactiveOptions  []string
	interactiveSources  []string // Track source for each option
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

	createName := textinput.New()
	createName.Placeholder = "my-modpack"
	createName.CharLimit = 100
	createName.Width = 40

	serverIP := textinput.New()
	serverIP.Placeholder = "play.example.com or 1.2.3.4:25565"
	serverIP.CharLimit = 256
	serverIP.Width = 50

	agentIn := textinput.New()
	agentIn.Placeholder = "ask the agent about this pack…"
	agentIn.CharLimit = 2048
	agentIn.Width = 40
	// The pane renders its own "❯ " prefix; the default "> " prompt would
	// double up and overflow the row.
	agentIn.Prompt = ""

	return &App{
		screen:       ScreenLoading,
		loadingMsg:   "Detecting git repository…",
		cloneInput:    clone,
		createNameInput: createName,
		createPrivate:   true,
		searchInput:   search,
		addModInput:   addMod,
		serverIPInput: serverIP,
		agentInput:    agentIn,
		homeFocus:     2,
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

// createRepo creates a new GitHub repository via gh and clones it into
// ~/modpacks/<name>.
func (a *App) createRepo(name string, private bool) tea.Cmd {
	return func() tea.Msg {
		if _, err := exec.LookPath("gh"); err != nil {
			return msgCreateRepoErr{err: fmt.Errorf("gh CLI not found (install from https://cli.github.com)")}
		}
		if _, err := exec.Command("gh", "auth", "status").CombinedOutput(); err != nil {
			return msgCreateRepoErr{err: fmt.Errorf("gh is not authenticated (run 'gh auth login')")}
		}
		home, _ := os.UserHomeDir()
		parent := filepath.Join(home, "modpacks")
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return msgCreateRepoErr{err: err}
		}
		target := filepath.Join(parent, name)
		if _, err := os.Stat(target); err == nil {
			return msgCreateRepoErr{err: fmt.Errorf("%s already exists", target)}
		}
		visibility := "--public"
		if private {
			visibility = "--private"
		}
		c := exec.Command("gh", "repo", "create", name, visibility, "--clone")
		c.Dir = parent
		out, err := c.CombinedOutput()
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return msgCreateRepoErr{err: fmt.Errorf("gh repo create failed: %s", msg)}
		}
		return msgRepoCreated{path: target}
	}
}

// initPack runs `packwiz init` interactively in the real terminal so the user
// can answer its prompts (mc version, loader, etc.).
func (a *App) initPack(path string) tea.Cmd {
	c := exec.Command("packwiz", "init")
	c.Dir = path
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return msgPackwizInitDone{path: path, err: err}
	})
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
		// Check if this is a mod search command - use combined search
		if len(args) >= 3 && args[1] == "add" && (args[0] == "mr" || args[0] == "cf") {
			modName := args[2]
			out, prompt, err := RunBothPackwizSearches(a.packDir, modName)
			if prompt != nil {
				return msgInteractivePrompt{
					prompt:  prompt.Prompt,
					options: prompt.Options,
					sources: prompt.Sources,
					cmd: &interactiveCmd{
						packDir: a.packDir,
						args:    args,
						input:   "", // Will be determined based on selection
					},
				}
			}
			return msgCmdDone{output: out, err: err}
		}

		// Normal packwiz command
		out, prompt, err := RunPackwizInteractive(a.packDir, args...)
		if prompt != nil {
			// Interactive prompt detected
			return msgInteractivePrompt{
				prompt:  prompt.Prompt,
				options: prompt.Options,
				sources: prompt.Sources,
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
		out, prompt, err := RunPackwizWithInput(cmd.packDir, input, cmd.args...)
		if prompt != nil {
			// Additional interactive prompt detected (e.g., dependency install)
			// Store the accumulated input so we can continue building on it
			return msgInteractivePrompt{
				prompt:  prompt.Prompt,
				options: prompt.Options,
				sources: prompt.Sources,
				cmd: &interactiveCmd{
					packDir: cmd.packDir,
					args:    cmd.args,
					input:   input, // preserve the full input sent so far
				},
			}
		}
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

func (a *App) openLazygit() tea.Cmd {
	// Check if lazygit is installed
	if _, err := exec.LookPath("lazygit"); err != nil {
		return func() tea.Msg {
			return msgLazygitDone{err: fmt.Errorf("lazygit not found (install from https://github.com/jesseduffield/lazygit)")}
		}
	}

	c := exec.Command("lazygit")
	c.Dir = a.repoRoot
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return msgLazygitDone{err: err}
	})
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
		// The add-mod popup captures the mouse while open: wheel scrolls its
		// results, clicks only hit its own zones.
		if a.addModModal {
			if m.Button == tea.MouseButtonWheelUp || m.Button == tea.MouseButtonWheelDown {
				delta := 1
				if m.Button == tea.MouseButtonWheelUp {
					delta = -1
				}
				// Wheel over the preview scrolls it; over the list it moves
				// the selection.
				lw, rw := a.addModPaneWidths()
				fw, _ := styleModal.GetFrameSize()
				previewX := maxInt(0, (a.width-(lw+3+rw+fw))/2) + fw/2 + lw + 3
				if rw > 0 && m.X >= previewX {
					a.addModPrevScroll = maxInt(0, a.addModPrevScroll+delta*3)
					return a, nil
				}
				if len(a.addModHits) > 0 {
					a.addModIdx = clamp(a.addModIdx+delta, 0, len(a.addModHits)-1)
					a.addModBtnIdx, a.addModShotIdx, a.addModPrevScroll = 0, 0, 0
					return a, a.addModImageCmds()
				}
				return a, nil
			}
			if m.Action == tea.MouseActionRelease && m.Button == tea.MouseButtonLeft {
				for _, z := range a.clickZones {
					if m.X >= z.x && m.X < z.x+z.w && m.Y >= z.y && m.Y < z.y+z.h {
						if strings.HasPrefix(z.action, "addmod:row:") {
							var idx int
							fmt.Sscanf(z.action, "addmod:row:%d", &idx)
							if idx < len(a.addModHits) {
								a.addModIdx = idx
								a.addModBtnIdx, a.addModShotIdx, a.addModPrevScroll = 0, 0, 0
								return a, a.addModImageCmds()
							}
						} else if strings.HasPrefix(z.action, "addmod:btn:") {
							var idx int
							fmt.Sscanf(z.action, "addmod:btn:%d", &idx)
							a.addModBtnIdx = idx
							return a.activateAddModButton()
						} else if z.action == "addmod:shotprev" {
							return a, a.addModShotMove(-1)
						} else if z.action == "addmod:shotnext" {
							return a, a.addModShotMove(1)
						}
					}
				}
			}
			return a, nil
		}
		// Scroll wheel over the mod list moves the selection.
		if m.Button == tea.MouseButtonWheelUp || m.Button == tea.MouseButtonWheelDown {
			for _, z := range a.clickZones {
				if m.X >= z.x && m.X < z.x+z.w && m.Y >= z.y && m.Y < z.y+z.h &&
					(z.action == "home:focus:mods" || strings.HasPrefix(z.action, "home:modrow:") || strings.HasPrefix(z.action, "del:")) {
					delta := 3
					if m.Button == tea.MouseButtonWheelUp {
						delta = -3
					}
					if len(a.modsFiltered) > 0 {
						a.modsIdx = clamp(a.modsIdx+delta, 0, len(a.modsFiltered)-1)
						// Wheel-scrolling the list is interaction — open (or
						// follow with) the detail pane.
						return a, a.syncModDetail(a.modsFiltered[a.modsIdx])
					}
					return a, nil
				}
			}
			return a, nil
		}
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
						} else if strings.HasPrefix(z.action, "home:btn:") {
							var idx int
							fmt.Sscanf(z.action, "home:btn:%d", &idx)
							if idx < len(homeButtons) {
								a.setHomeFocus(2)
								a.homeBtnIdx = idx
								return a.homeActivate(homeButtons[idx].action)
							}
						} else if z.action == "repo:clone" {
							a.repoListIdx = len(a.repoList)
							a.screen = ScreenCloneRepo
							a.cloneInput.Focus()
							return a, textinput.Blink
						} else if z.action == "repo:create" {
							a.repoListIdx = len(a.repoList) + 1
							a.screen = ScreenCreateRepo
							a.createError = ""
							a.createNameInput.Focus()
							return a, textinput.Blink
						} else if strings.HasPrefix(z.action, "repo:") {
							var idx int
							fmt.Sscanf(z.action, "repo:%d", &idx)
							if idx < len(a.repoList) {
								a.repoListIdx = idx
								a.loadingMsg = "Loading pack…"
								a.screen = ScreenLoading
								return a, a.loadPackFromRepo(a.repoList[idx])
							}
						} else if strings.HasPrefix(z.action, "home:modrow:") {
							var idx int
							fmt.Sscanf(z.action, "home:modrow:%d", &idx)
							if idx < len(a.modsFiltered) {
								a.modsIdx = idx
								return a, a.openModDetail(a.modsFiltered[idx])
							}
							return a, nil
						} else if strings.HasPrefix(z.action, "home:mdetail:") {
							return a.modDetailClick(z.action)
						} else if z.action == "home:srcconv:cf" {
							a.sourcesModal = false
							return a, a.startHomeCommand("⇄ Convert to CurseForge", "convert-sources", "curseforge")
						} else if z.action == "home:srcconv:mr" {
							a.sourcesModal = false
							return a, a.startHomeCommand("⇄ Convert to Modrinth", "convert-sources", "modrinth")
						} else if z.action == "approve:allow" {
							return a, a.resolveApprove(true, false)
						} else if z.action == "approve:always" {
							return a, a.resolveApprove(true, true)
						} else if z.action == "approve:deny" {
							return a, a.resolveApprove(false, false)
						} else if z.action == "home:discardcancel" {
							a.confirmDiscard = false
							return a, nil
						} else if z.action == "home:srcdismiss" {
							a.sourcesModal = false
							return a, nil
						} else if z.action == "home:modaldismiss" {
							a.infoTitle = ""
							return a, nil
						} else if z.action == "home:cmdclose" {
							if a.cmdRunning && a.cmdProc != nil && a.cmdProc.Process != nil {
								syscall.Kill(-a.cmdProc.Process.Pid, syscall.SIGTERM)
							} else {
								a.cmdGen++ // cancel any pending auto-close
								a.cmdDone = false
								a.cmdTitle = ""
								a.cmdLines = nil
								a.cmdErr = nil
								a.cmdSummary = nil
								a.cmdCloseAt = time.Time{}
							}
							return a, nil
						} else if z.action == "home:repourl" {
							if url := repoWebURL(a.repoRemote); url != "" {
								go exec.Command("xdg-open", url).Start()
								a.statusMsg = "Opening " + url
								a.statusIsErr = false
								a.statusExpire = time.Now().Add(2 * time.Second)
								return a, a.expireStatus()
							}
							return a, nil
						} else if z.action == "home:loaderbox" {
							a.setHomeFocus(4)
							a.screen = ScreenManageLoader
							return a, nil
						} else if z.action == "home:editaddr" {
							a.serverIPInput.SetValue(ReadServerAddress(a.packDir))
							a.serverIPInput.Focus()
							a.screen = ScreenServerIP
							return a, textinput.Blink
						} else if z.action == "home:agentfull" {
							a.agentFull = !a.agentFull
							a.setHomeFocus(1)
							return a, textinput.Blink
						} else if z.action == "home:focus:mods" {
							return a, a.focusHome(0)
						} else if z.action == "home:focus:agent" {
							cmd := a.focusHome(1)
							a.agentFocusByCycle = false
							return a, tea.Batch(cmd, textinput.Blink)
						}
					}
				}
			}
		}
		return a, nil

	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		if a.addModModal {
			// The popup's panes (and therefore image cell sizes) follow the
			// terminal size — refetch/re-render what the new geometry needs.
			// The window recomputes from the same start image, which is the
			// closest surviving slide.
			return a, a.addModImageCmds()
		}
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
		EnsurePackIgnores(m.packDir)
		a.packDir = m.packDir
		a.packName = m.packName
		a.repoRoot = m.repoRoot
		a.serverAddr = ReadServerAddress(m.packDir)
		a.packMeta, _ = ParsePackMeta(m.packDir)
		a.repoRemote = GetGitRemote(m.repoRoot)
		a.screen = ScreenMainMenu
		a.menuIdx = 0
		a.homeFocus = 0 // start on the mod list
		a.homeBtnIdx = 0
		// Load mods in the background for the home screen's embedded pane,
		// and start polling for a running client (Stop Client button).
		cmds := []tea.Cmd{a.loadMods()}
		if !a.clientPollOn {
			a.clientPollOn = true
			cmds = append(cmds, a.pollClient())
		}
		return a, tea.Batch(cmds...)

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
		a.changedCount = len(m.modified) + len(m.added) + len(m.deleted)
		a.filterMods()
		// Close the detail pane if its mod's metafile is gone.
		if a.modDetail != nil {
			if _, err := os.Stat(a.modDetail.TomlPath); err != nil {
				a.modDetail = nil
				a.detailPinned = false
				if a.homeFocus == 5 {
					a.setHomeFocus(0)
				}
			}
		}
		// The detail pane only opens on interaction with the mod list
		// (scrolling, clicking, tabbing in) — never from a plain reload, so
		// startup lands on an uncluttered dashboard.
		// A refresh triggered from the home screen stays there; the dedicated
		// manage-mods screen is only entered from a loading transition.
		if a.screen != ScreenMainMenu {
			a.screen = ScreenManageMods
		}
		// Reopen add modal if we came from adding a mod
		if a.returnToAddModal {
			a.returnToAddModal = false
			a.addModModal = true
			a.addModInput.Focus()
			return a, textinput.Blink
		}
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

	case msgModVersions:
		if a.modVerCache == nil {
			a.modVerCache = map[string]cachedVersions{}
		}
		errStr := ""
		if m.err != nil {
			errStr = m.err.Error()
		}
		a.modVerCache[m.key] = cachedVersions{vers: m.vers, err: errStr, at: time.Now()}
		if a.modDetail != nil && a.modDetail.Slug == m.slug {
			a.modDetail.Versions = m.vers
			if m.err != nil {
				a.modDetail.VersionsErr = m.err.Error()
			}
			for _, v := range m.vers {
				if v.ID == a.modDetail.VersionID {
					a.modDetail.CurVersion = v.Number
				}
			}
		}
		return a, nil

	case msgModConvertDone:
		d := a.modDetail
		if d == nil || d.Slug != m.slug {
			return a, a.loadMods()
		}
		d.Converting = false
		// The re-added metafile may carry the target source's slug.
		if _, err := os.Stat(d.TomlPath); err != nil {
			if p, ferr := findTomlByFilename(a.packDir, d.Filename); ferr == nil {
				d.TomlPath = p
				d.Slug = strings.TrimSuffix(strings.TrimSuffix(filepath.Base(p), ".toml"), ".pw")
			}
		}
		oldSource := d.Source
		loadModDetailFields(d)
		switch {
		case m.err != nil:
			d.ConvertErr = m.err.Error()
		case d.Source == oldSource:
			d.ConvertErr = convertFailLine(m.report)
		default:
			d.Versions, d.VersionsErr, d.CurVersion = nil, "", ""
			a.statusMsg = "Converted to " + d.Source
			a.statusIsErr = false
			a.statusExpire = time.Now().Add(3 * time.Second)
			return a, tea.Batch(a.expireStatus(), a.loadMods(), a.modDetailFetches())
		}
		return a, a.loadMods()

	case msgModDetailFetch:
		// Debounced scroll-sync: only fetch if this mod is still the one shown.
		if a.modDetail != nil && a.modDetail.Slug == m.slug && len(a.modDetail.Versions) == 0 {
			return a, a.modDetailFetches()
		}
		return a, nil

	case msgModCurVer:
		if m.number != "" && m.sha1 != "" {
			if a.modHashVer == nil {
				a.modHashVer = map[string]string{}
			}
			a.modHashVer[m.sha1] = m.number
		}
		if a.modDetail != nil && a.modDetail.Slug == m.slug &&
			m.number != "" && a.modDetail.CurVersion == "" {
			a.modDetail.CurVersion = m.number
		}
		return a, nil

	case msgModDetailDone:
		if m.err != nil {
			a.statusMsg = ""
			a.showInfo("Version change failed", m.err.Error(), true)
			return a, nil
		}
		a.statusMsg = "Version changed"
		a.statusIsErr = false
		a.statusExpire = time.Now().Add(3 * time.Second)
		if a.modDetail != nil {
			a.modDetail.TomlPath = filepath.Join(a.packDir, "mods", a.modDetail.Slug+".pw.toml")
			loadModDetailFields(a.modDetail)
		}
		return a, tea.Batch(a.expireStatus(), a.loadMods())

	case msgClientState:
		a.clientUp = m.up
		return a, a.pollClient()

	case msgHomeCmdLine:
		a.cmdLines = append(a.cmdLines, m.line)
		if len(a.cmdLines) > 2000 {
			a.cmdLines = a.cmdLines[len(a.cmdLines)-2000:]
		}
		return a, a.waitHomeCmd()

	case msgHomeCmdExit:
		a.cmdRunning = false
		a.cmdDone = true
		a.cmdErr = m.err
		a.cmdProc = nil
		var cmds []tea.Cmd
		if a.cmdIsTest {
			// Tests end on a stats summary and stay up until closed.
			a.cmdSummary = summarizeTest(a.cmdLines, m.err)
		} else {
			// Everything else auto-closes shortly after finishing.
			a.cmdCloseAt = time.Now().Add(10 * time.Second)
			gen := a.cmdGen
			cmds = append(cmds, tea.Tick(10*time.Second, func(time.Time) tea.Msg {
				return msgCmdPaneAutoClose{gen: gen}
			}))
		}
		// Refresh the mod list / change count — fixers may have edited files.
		if a.screen == ScreenMainMenu {
			cmds = append(cmds, a.loadMods())
		}
		return a, tea.Batch(cmds...)

	case msgAddModDebounce:
		// Only the newest edit's tick fires a search.
		if a.addModModal && m.seq == a.addModSeq {
			return a, a.searchAddMod()
		}
		return a, nil

	case msgAddModResults:
		if !a.addModModal || m.seq != a.addModSeq {
			return a, nil // superseded by further typing
		}
		a.addModSearching = false
		a.addModHits = m.hits
		a.addModIdx = 0
		a.addModBtnIdx, a.addModShotIdx = 0, 0
		a.addModErr = ""
		if m.err != nil {
			a.addModErr = m.err.Error()
		}
		return a, a.addModImageCmds()

	case msgAddModImg:
		if a.addModImgs == nil {
			a.addModImgs = map[string]string{}
		}
		a.addModImgs[m.key] = m.block
		if a.addModModal {
			// Extend the gallery window: the next image's fetch only starts
			// once this one's width is known.
			return a, a.addModImageCmds()
		}
		return a, nil

	case msgCmdPaneAutoClose:
		if m.gen == a.cmdGen && a.cmdDone && !a.cmdRunning {
			a.cmdDone = false
			a.cmdTitle = ""
			a.cmdLines = nil
			a.cmdErr = nil
			a.cmdSummary = nil
			a.cmdCloseAt = time.Time{}
		}
		return a, nil

	case msgPrismDone:
		a.statusMsg = ""
		if m.err != nil {
			text := m.err.Error()
			if tailOut := strings.TrimSpace(m.out); tailOut != "" {
				text = tail(tailOut, 6) + "\n" + text
			}
			a.showInfo("Prism install failed", text, true)
		} else {
			a.showInfo("Added to Prism", "The instance is installed. Restart PrismLauncher to see it.", false)
		}
		return a, nil

	case msgApproveReq:
		a.approvePending = m.req
		return a, nil

	case msgAgentReply:
		a.agentRunning = false
		a.agentScroll = 0
		if m.err != nil {
			a.agentEntries = append(a.agentEntries, agentEntry{role: "error", text: m.err.Error()})
		} else {
			a.agentStarted = true
			a.agentEntries = append(a.agentEntries, agentEntry{role: "agent", text: m.text})
		}
		return a, nil

	case msgRepoCreated:
		return a, a.initPack(m.path)

	case msgCreateRepoErr:
		a.createError = m.err.Error()
		a.screen = ScreenCreateRepo
		a.createNameInput.Focus()
		return a, textinput.Blink

	case msgPackwizInitDone:
		// Same mouse-restore workaround as lazygit/editor.
		restoreMouse := func() tea.Msg {
			fmt.Print("\033[?1000h")
			fmt.Print("\033[?1002h")
			fmt.Print("\033[?1006h")
			return tea.WindowSizeMsg{Width: a.width, Height: a.height}
		}
		if m.err != nil {
			a.statusMsg = "packwiz init failed: " + m.err.Error()
			a.statusIsErr = true
			a.statusExpire = time.Now().Add(4 * time.Second)
			a.screen = ScreenRepoSelect
			return a, tea.Batch(restoreMouse, a.expireStatus())
		}
		a.loadingMsg = "Loading pack…"
		a.screen = ScreenLoading
		return a, tea.Batch(restoreMouse, a.loadPackFromRepo(RepoEntry{Path: m.path}))

	case msgInteractivePrompt:
		a.interactivePrompt = m.prompt
		a.interactiveOptions = m.options
		a.interactiveSources = m.sources
		a.interactiveSelected = 0
		// Skip headers in initial selection
		for a.interactiveSelected < len(a.interactiveOptions) &&
		    a.interactiveSelected < len(a.interactiveSources) &&
		    a.interactiveSources[a.interactiveSelected] == "header" {
			a.interactiveSelected++
		}
		a.interactivePending = m.cmd
		a.screen = ScreenInteractive
		return a, nil

	case msgEditorDone:
		// Manually re-enable mouse mode after editor (tea.ExecProcess bug workaround)
		restoreMouse := func() tea.Msg {
			// Send ANSI escape codes to re-enable mouse tracking
			fmt.Print("\033[?1000h")  // Enable mouse button tracking
			fmt.Print("\033[?1002h")  // Enable cell (button) motion tracking
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

	case msgLazygitDone:
		// Manually re-enable mouse mode after lazygit
		restoreMouse := func() tea.Msg {
			fmt.Print("\033[?1000h")
			fmt.Print("\033[?1002h")
			fmt.Print("\033[?1006h")
			return tea.WindowSizeMsg{Width: a.width, Height: a.height}
		}

		if m.err != nil {
			a.statusMsg = "Lazygit error: " + m.err.Error()
			a.statusIsErr = true
			a.statusExpire = time.Now().Add(4 * time.Second)
			return a, tea.Batch(restoreMouse, a.expireStatus())
		}

		// Reload mods to get updated git status
		a.statusMsg = "Lazygit closed, refreshing..."
		a.statusIsErr = false
		a.statusExpire = time.Now().Add(2 * time.Second)
		return a, tea.Batch(restoreMouse, a.expireStatus(), a.loadMods())

	case msgAgentDone, msgSelfCmdDone:
		// Same mouse-restore workaround as lazygit/editor.
		restoreMouse := func() tea.Msg {
			fmt.Print("\033[?1000h")
			fmt.Print("\033[?1002h")
			fmt.Print("\033[?1006h")
			return tea.WindowSizeMsg{Width: a.width, Height: a.height}
		}
		var err error
		switch mm := m.(type) {
		case msgAgentDone:
			err = mm.err
		case msgSelfCmdDone:
			err = mm.err
		}
		if err != nil {
			a.statusMsg = "Error: " + err.Error()
			a.statusIsErr = true
			a.statusExpire = time.Now().Add(4 * time.Second)
			return a, tea.Batch(restoreMouse, a.expireStatus())
		}
		return a, restoreMouse
	}

	// Global shortcut: ctrl+g opens lazygit from anywhere once a pack is open.
	if m, ok := msg.(tea.KeyMsg); ok && m.String() == "ctrl+g" && a.repoRoot != "" {
		return a, a.openLazygit()
	}

	// Delegate to the active screen.
	switch a.screen {
	case ScreenRepoSelect:
		return a.updateRepoSelect(msg)
	case ScreenCloneRepo:
		return a.updateCloneRepo(msg)
	case ScreenCreateRepo:
		return a.updateCreateRepo(msg)
	case ScreenMainMenu:
		return a.updateMainMenu(msg)
	case ScreenServerIP:
		return a.updateServerIP(msg)
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
	total := len(a.repoList) + 2 // +2 for "clone new" and "create new"
	switch m.String() {
	case "ctrl+c", "q":
		return a, tea.Quit
	case "up", "k":
		a.repoListIdx = (a.repoListIdx - 1 + total) % total
	case "down", "j":
		a.repoListIdx = (a.repoListIdx + 1) % total
	case "left", "h":
		if a.repoListIdx == len(a.repoList)+1 {
			a.repoListIdx--
		}
	case "right", "l":
		if a.repoListIdx == len(a.repoList) {
			a.repoListIdx++
		}
	case "enter", " ":
		if a.repoListIdx == len(a.repoList) {
			a.screen = ScreenCloneRepo
			a.cloneInput.Focus()
			return a, textinput.Blink
		}
		if a.repoListIdx == len(a.repoList)+1 {
			a.screen = ScreenCreateRepo
			a.createError = ""
			a.createNameInput.Focus()
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

func (a *App) updateCreateRepo(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(tea.KeyMsg); ok {
		switch m.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "esc":
			a.screen = ScreenRepoSelect
			a.createError = ""
			a.createNameInput.Blur()
			return a, nil
		case "tab", "shift+tab":
			a.createPrivate = !a.createPrivate
			return a, nil
		case "enter":
			name := strings.TrimSpace(a.createNameInput.Value())
			if name == "" {
				a.createError = "Please enter a repository name"
				return a, nil
			}
			if strings.ContainsAny(name, "/\\ ") {
				a.createError = "Name cannot contain spaces or slashes"
				return a, nil
			}
			a.createError = ""
			a.createNameInput.Blur()
			a.loadingMsg = "Creating repository…"
			a.screen = ScreenLoading
			return a, a.createRepo(name, a.createPrivate)
		}
	}
	var cmd tea.Cmd
	a.createNameInput, cmd = a.createNameInput.Update(msg)
	return a, cmd
}

var mainMenuItems = []struct{ icon, label string }{
	{"◈", "Manage Mods"},
	{"⚙", "Manage Loader"},
	{"▶", "Test Server"},
	{"◉", "Full-stack Test"},
	{"⇄", "Fix Mod Sources"},
	{"✦", "Agent Chat"},
	{"⇩", "Install to Prism"},
	{"◎", "Server Address"},
	{"↑", "Push & Exit"},
	{"✕", "Exit without Pushing"},
}

// homeButtons are the action chips on the wide home dashboard. The mods list
// and agent chat live in their own panes, so they aren't buttons here.
var homeButtons = []struct{ icon, label, action string }{
	{"▷", "Launch Client", "launch"},
	{"▶", "Test Server", "testserver"},
	{"◉", "Test Both", "testfull"},
	{"⇩", "Install to Prism", "prism"},
	{"⇄", "Sources", "sources"},
	{"↑", "Push", "push"},
	{"⟲", "Discard", "discard"},
	{"✕", "Exit", "exit"},
}

// homeBtnRightStart is the index from which buttons are right-aligned in the
// action row (push / discard / exit).
const homeBtnRightStart = 5

// homeBtnPlain is a button's unstyled label text (used for width math and
// styling alike). Push & Exit carries the uncommitted-change count.
func (a *App) homeBtnPlain(i int) string {
	b := homeButtons[i]
	if b.action == "launch" && a.clientUp {
		return "■ Stop Client"
	}
	label := b.icon + " " + b.label
	if b.action == "push" && a.changedCount > 0 {
		label += fmt.Sprintf(" (%d)", a.changedCount)
	}
	return label
}

// pollClient checks every few seconds whether the pack's client is running.
func (a *App) pollClient() tea.Cmd {
	packDir, meta := a.packDir, a.packMeta
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return msgClientState{up: clientProcRunning(packDir, meta)}
	})
}

// wideHome reports whether there is room for the full dashboard; below this
// the home screen falls back to the vertical list menu (mobile aspect ratio).
func (a *App) wideHome() bool {
	return a.width >= 96 && a.height >= 24
}

// setHomeFocus moves keyboard focus between the home panes, keeping the text
// inputs' focus state in sync. Prefer focusHome, which also handles the
// detail pane's auto-open/close behaviour.
func (a *App) setHomeFocus(n int) {
	a.homeFocus = n
	if n == 1 {
		a.agentInput.Focus()
	} else {
		a.agentInput.Blur()
	}
	if n != 0 {
		a.searchFocus = false
		a.searchInput.Blur()
	}
}

// focusHome moves focus with the detail-pane rules: focusing the pane pins
// it, leaving the mod list closes an unpinned pane, and entering the mod
// list auto-opens one for the current selection.
func (a *App) focusHome(n int) tea.Cmd {
	prev := a.homeFocus
	if n == 5 && a.modDetail != nil {
		a.detailPinned = true
	}
	if prev == 0 && n != 0 && n != 5 && a.modDetail != nil && !a.detailPinned {
		a.modDetail = nil
	}
	a.setHomeFocus(n)
	if n == 0 && a.modDetail == nil && len(a.modsFiltered) > 0 && a.modsIdx < len(a.modsFiltered) {
		a.detailPinned = false
		return a.syncModDetail(a.modsFiltered[a.modsIdx])
	}
	return nil
}

// cycleHomeFocus tabs through the panes in reading order — left-right,
// top-bottom: IP, loader, mods, detail (when open), agent, buttons.
func (a *App) cycleHomeFocus(dir int) tea.Cmd {
	order := []int{3, 4, 0}
	if a.modDetail != nil {
		order = append(order, 5)
	}
	order = append(order, 1, 2)
	cur := 0
	for i, v := range order {
		if v == a.homeFocus {
			cur = i
		}
	}
	return a.focusHome(order[(cur+dir+len(order))%len(order)])
}

func (a *App) updateMainMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	if !a.wideHome() {
		return a.updateMainMenuList(msg)
	}
	if a.addModModal {
		return a.updateAddModModal(msg)
	}
	// A pending permission request blocks everything until answered.
	if a.approvePending != nil {
		if m, ok := msg.(tea.KeyMsg); ok {
			switch m.String() {
			case "ctrl+c":
				return a, tea.Quit
			case "y", "enter":
				return a, a.resolveApprove(true, false)
			case "w":
				return a, a.resolveApprove(true, true)
			case "n", "d", "esc":
				return a, a.resolveApprove(false, false)
			}
		}
		return a, nil
	}
	// Discard confirmation captures input until answered.
	if a.confirmDiscard {
		if m, ok := msg.(tea.KeyMsg); ok {
			switch m.String() {
			case "ctrl+c":
				return a, tea.Quit
			case "enter", "y":
				a.confirmDiscard = false
				a.startOutput()
				return a, a.discardChanges()
			case "esc", "q", "n":
				a.confirmDiscard = false
			}
		}
		return a, nil
	}
	// Sources popup captures input until dismissed.
	if a.sourcesModal {
		if m, ok := msg.(tea.KeyMsg); ok {
			switch m.String() {
			case "ctrl+c":
				return a, tea.Quit
			case "esc", "q":
				a.sourcesModal = false
			case "left", "h", "right", "l", "tab":
				a.sourcesSel = 1 - a.sourcesSel
			case "enter", " ":
				a.sourcesModal = false
				if a.sourcesSel == 0 {
					return a, a.startHomeCommand("⇄ Convert to CurseForge", "convert-sources", "curseforge")
				}
				return a, a.startHomeCommand("⇄ Convert to Modrinth", "convert-sources", "modrinth")
			}
		}
		return a, nil
	}
	// Info popup captures input until dismissed.
	if a.infoTitle != "" {
		if m, ok := msg.(tea.KeyMsg); ok {
			switch m.String() {
			case "ctrl+c":
				return a, tea.Quit
			case "esc", "enter", " ", "q":
				a.infoTitle = ""
			}
		}
		return a, nil
	}
	if m, ok := msg.(tea.KeyMsg); ok {
		if m.String() == "ctrl+c" {
			return a, tea.Quit
		}
		if a.agentFull {
			return a.updateHomeAgent(m)
		}
		switch m.String() {
		case "tab":
			cmd := a.cycleHomeFocus(1)
			a.agentFocusByCycle = a.homeFocus == 1
			return a, tea.Batch(cmd, textinput.Blink)
		case "shift+tab":
			// The agent pane claims shift+tab for its auto-mode toggle
			// (mirroring interactive claude) — but only once the user has
			// actually interacted with the pane, so cycling straight through
			// with repeated shift+tabs still works.
			if a.homeFocus == 1 && !a.agentFocusByCycle {
				return a.updateHomeAgent(m)
			}
			cmd := a.cycleHomeFocus(-1)
			a.agentFocusByCycle = a.homeFocus == 1
			return a, tea.Batch(cmd, textinput.Blink)
		}
		switch a.homeFocus {
		case 0:
			return a.updateHomeMods(m)
		case 1:
			return a.updateHomeAgent(m)
		case 3, 4:
			return a.updateHomeHeaderBox(m)
		case 5:
			return a.updateModDetail(m)
		default:
			return a.updateHomeActions(m)
		}
	}
	// Non-key messages (cursor blink etc.) go to whichever input is focused.
	var cmd tea.Cmd
	if a.homeFocus == 1 {
		a.agentInput, cmd = a.agentInput.Update(msg)
	} else if a.homeFocus == 0 && a.searchFocus {
		a.searchInput, cmd = a.searchInput.Update(msg)
	}
	return a, cmd
}

// updateHomeMods handles keys while the embedded mods pane is focused —
// the same bindings as the dedicated manage-mods screen.
func (a *App) updateHomeMods(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.String() {
	case "esc":
		if a.searchFocus {
			if a.searchInput.Value() == "" {
				a.searchFocus = false
				a.searchInput.Blur()
			}
		} else {
			return a, a.focusHome(2)
		}
	case "right", "l":
		if !a.searchFocus {
			if a.modDetail != nil {
				return a, a.focusHome(5)
			}
			return a, a.focusHome(1)
		}
	case "/":
		if !a.searchFocus {
			a.searchFocus = true
			a.searchInput.Focus()
			return a, textinput.Blink
		}
	case "n":
		if !a.searchFocus {
			a.addModModal = true
			a.addModInput.Focus()
			return a, textinput.Blink
		}
	case "up", "k":
		if !a.searchFocus && a.modsIdx > 0 {
			a.modsIdx--
			// Scrolling counts as interaction — open (or follow with) the
			// detail pane.
			a.detailPinned = a.detailPinned && a.modDetail != nil
			return a, a.syncModDetail(a.modsFiltered[a.modsIdx])
		}
	case "down", "j":
		if !a.searchFocus && a.modsIdx < len(a.modsFiltered)-1 {
			a.modsIdx++
			a.detailPinned = a.detailPinned && a.modDetail != nil
			return a, a.syncModDetail(a.modsFiltered[a.modsIdx])
		}
	case "enter":
		if a.searchFocus {
			a.searchFocus = false
			a.searchInput.Blur()
		} else if len(a.modsFiltered) > 0 && a.modsIdx < len(a.modsFiltered) {
			return a, a.openModDetail(a.modsFiltered[a.modsIdx])
		}
	case "e":
		if !a.searchFocus && len(a.modsFiltered) > 0 && a.modsIdx < len(a.modsFiltered) {
			return a, a.openInEditor(a.modsFiltered[a.modsIdx].Path)
		}
	case "r":
		if !a.searchFocus {
			return a, a.loadMods()
		}
	case "g":
		if !a.searchFocus {
			return a, a.openLazygit()
		}
	case "d":
		if !a.searchFocus {
			return a.deleteMod()
		}
	// Detail-pane hotkeys also work from the list while the pane is open.
	case "s":
		if !a.searchFocus && a.modDetail != nil {
			return a.applyModSide(stepSide(a.modDetail.Side, 1))
		}
	case "o":
		if !a.searchFocus && a.modDetail != nil {
			return a.applyModOptional(!a.modDetail.Optional)
		}
	case "v":
		if !a.searchFocus && a.modDetail != nil {
			a.setHomeFocus(5)
			a.modDetail.ctlIdx = mdCtlVersion
			return a.activateModDetailCtl(mdCtlVersion)
		}
	}
	if a.searchFocus {
		prev := a.searchInput.Value()
		var cmd tea.Cmd
		a.searchInput, cmd = a.searchInput.Update(m)
		if a.searchInput.Value() != prev {
			a.filterMods()
			a.modsIdx = 0
		}
		return a, cmd
	}
	return a, nil
}

// updateHomeAgent handles keys while the embedded agent chat is focused.
func (a *App) updateHomeAgent(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Any real interaction ends tab-traversal mode (see agentFocusByCycle).
	a.agentFocusByCycle = false
	switch m.String() {
	case "esc":
		if a.agentFull {
			a.agentFull = false
			return a, nil
		}
		return a, a.focusHome(2)
	case "ctrl+f":
		a.agentFull = !a.agentFull
		return a, nil
	case "shift+tab":
		a.agentMode = (a.agentMode + 1) % 3
		return a, nil
	case "pgup":
		a.agentScroll += 5
		return a, nil
	case "pgdown":
		a.agentScroll -= 5
		if a.agentScroll < 0 {
			a.agentScroll = 0
		}
		return a, nil
	case "enter":
		prompt := strings.TrimSpace(a.agentInput.Value())
		if prompt == "" || a.agentRunning {
			return a, nil
		}
		bridgeCmd := a.ensureApproveBridge()
		a.agentInput.SetValue("")
		a.agentEntries = append(a.agentEntries, agentEntry{role: "user", text: prompt})
		a.agentRunning = true
		a.agentScroll = 0
		return a, tea.Batch(bridgeCmd,
			agentChatCmd(a.packDir, prompt, a.agentStarted, a.agentMode, a.mcpConfigPath))
	}
	var cmd tea.Cmd
	a.agentInput, cmd = a.agentInput.Update(m)
	return a, cmd
}

// updateHomeActions handles keys while the action button row is focused.
func (a *App) updateHomeActions(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := m.String()
	switch key {
	case "q":
		return a, tea.Quit
	case "esc":
		return a, nil // esc never activates anything
	case "left", "h":
		if a.homeBtnIdx == 0 {
			// Walked off the left edge — into the mod list.
			return a, a.focusHome(0)
		}
		a.homeBtnIdx--
	case "up", "k":
		a.homeBtnIdx = (a.homeBtnIdx - 1 + len(homeButtons)) % len(homeButtons)
	case "right", "l":
		if a.homeBtnIdx < len(homeButtons)-1 {
			a.homeBtnIdx++
		}
	case "down", "j":
		a.homeBtnIdx = (a.homeBtnIdx + 1) % len(homeButtons)
	case "enter", " ":
		return a.homeActivate(homeButtons[a.homeBtnIdx].action)
	case "e":
		a.serverIPInput.SetValue(ReadServerAddress(a.packDir))
		a.serverIPInput.Focus()
		a.screen = ScreenServerIP
		return a, textinput.Blink
	case "g":
		return a, a.openLazygit()
	case "m":
		return a, a.focusHome(0)
	case "c":
		return a, tea.Batch(a.focusHome(1), textinput.Blink)
	default:
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			idx := int(key[0] - '1')
			if idx < len(homeButtons) {
				a.homeBtnIdx = idx
				return a.homeActivate(homeButtons[idx].action)
			}
		}
	}
	return a, nil
}

// updateHomeHeaderBox handles keys while the IP (3) or loader (4) header box
// is focused.
func (a *App) updateHomeHeaderBox(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.String() {
	case "q":
		return a, tea.Quit
	case "esc":
		return a, a.focusHome(2)
	case "left", "h":
		if a.homeFocus == 4 {
			return a, a.focusHome(3)
		}
	case "right", "l":
		if a.homeFocus == 3 {
			return a, a.focusHome(4)
		}
	case "down", "j":
		return a, a.focusHome(0)
	case "enter", " ":
		if a.homeFocus == 3 {
			a.serverIPInput.SetValue(ReadServerAddress(a.packDir))
			a.serverIPInput.Focus()
			a.screen = ScreenServerIP
			return a, textinput.Blink
		}
		a.screen = ScreenManageLoader
	}
	return a, nil
}

func (a *App) homeActivate(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "launch":
		// Already running → the button reads Stop Client and kills it.
		if a.clientUp {
			if a.cmdRunning && a.cmdTitle == "▷ Launch Client" && a.cmdProc != nil && a.cmdProc.Process != nil {
				syscall.Kill(-a.cmdProc.Process.Pid, syscall.SIGTERM)
			}
			go stopClientProcs(a.packDir, a.packMeta)
			a.clientUp = false
			a.statusMsg = "Stopping client…"
			a.statusIsErr = false
			a.statusExpire = time.Now().Add(4 * time.Second)
			return a, a.expireStatus()
		}
		// Prefer the local PrismLauncher; fall back to portablemc (the same
		// launcher the test harness uses) streamed into the command pane.
		instName := artifactBase(a.packDir, a.packMeta)
		if c := launchPrismCmd(instName); c != nil {
			if !prismInstanceExists(instName) {
				a.showInfo("Not installed in Prism", "Run Install to Prism first, then launch again.", true)
				return a, nil
			}
			c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := c.Start(); err != nil {
				a.showInfo("Launch failed", err.Error(), true)
				return a, nil
			}
			go c.Wait()
			a.clientUp = true // optimistic; the poll confirms
			a.statusMsg = "Launching " + instName + " via PrismLauncher…"
			a.statusIsErr = false
			a.statusExpire = time.Now().Add(5 * time.Second)
			return a, a.expireStatus()
		}
		a.clientUp = true // optimistic; the poll confirms
		return a, a.startHomeCommand("▷ Launch Client", "launch-client")
	case "testserver":
		return a, a.startHomeCommand("▶ Test Server", "test", "server")
	case "testfull":
		return a, a.startHomeCommand("◉ Test Both", "test", "full")
	case "prism":
		if prismRunning() {
			a.showInfo("Prism is running", "Close PrismLauncher first, then install again — it rescans instances on startup.", true)
			return a, nil
		}
		a.statusMsg = "Installing to Prism…"
		a.statusIsErr = false
		a.statusExpire = time.Now().Add(30 * time.Second)
		packDir := a.packDir
		return a, func() tea.Msg {
			var buf strings.Builder
			err := InstallPrism(packDir, &buf)
			return msgPrismDone{out: buf.String(), err: err}
		}
	case "sources":
		a.sourceCounts = CountModSources(a.packDir)
		a.sourcesSel = 1
		a.sourcesModal = true
	case "push":
		a.startOutput()
		return a, a.gitPush()
	case "discard":
		if a.changedCount == 0 {
			a.statusMsg = "nothing to discard"
			a.statusIsErr = false
			a.statusExpire = time.Now().Add(3 * time.Second)
			return a, a.expireStatus()
		}
		a.confirmDiscard = true
	case "exit":
		return a, tea.Quit
	}
	return a, nil
}

// discardChanges resets the working tree and drops untracked files
// (gitignored state like .packwiz-tui/ is untouched).
func (a *App) discardChanges() tea.Cmd {
	repoRoot := a.repoRoot
	return func() tea.Msg {
		c := exec.Command("git", "reset", "--hard", "HEAD")
		c.Dir = repoRoot
		out, err := c.CombinedOutput()
		if err != nil {
			return msgCmdDone{output: string(out), err: err}
		}
		c = exec.Command("git", "clean", "-fd")
		c.Dir = repoRoot
		out2, err := c.CombinedOutput()
		return msgCmdDone{output: string(out) + string(out2), err: err}
	}
}

// updateMainMenuList is the narrow (mobile aspect ratio) fallback: the
// original vertical list menu.
func (a *App) updateMainMenuList(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, ok := msg.(tea.KeyMsg)
	if !ok {
		return a, nil
	}
	key := m.String()
	switch key {
	case "ctrl+c", "q":
		return a, tea.Quit
	case "up", "k":
		a.menuIdx = (a.menuIdx - 1 + len(mainMenuItems)) % len(mainMenuItems)
	case "down", "j":
		a.menuIdx = (a.menuIdx + 1) % len(mainMenuItems)
	case "g":
		return a, a.openLazygit()
	case "c":
		return a, a.openAgent()
	case "enter", " ":
		return a.activateMenuItem()
	default:
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			idx := int(key[0] - '1')
			if idx < len(mainMenuItems) {
				a.menuIdx = idx
				return a.activateMenuItem()
			}
		}
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
		return a, a.runSelfCommand("test", "server")
	case 3:
		return a, a.runSelfCommand("test", "full")
	case 4:
		return a, a.runSelfCommand("fix-sources")
	case 5:
		return a, a.openAgent()
	case 6:
		return a, a.runSelfCommand("install-prism")
	case 7:
		a.serverIPInput.SetValue(ReadServerAddress(a.packDir))
		a.serverIPInput.Focus()
		a.screen = ScreenServerIP
	case 8:
		a.startOutput()
		return a, a.gitPush()
	case 9:
		return a, tea.Quit
	}
	return a, nil
}

// startHomeCommand runs this binary's own CLI subcommand as a background
// subprocess, streaming its output into the home screen's command pane.
func (a *App) startHomeCommand(title string, args ...string) tea.Cmd {
	if a.cmdRunning {
		a.showInfo("Busy", "A command is already running — wait for it to finish or close it.", true)
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	c := exec.Command(self, args...)
	c.Dir = a.packDir
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := c.StdoutPipe()
	if err != nil {
		a.showInfo("Failed to start", err.Error(), true)
		return nil
	}
	c.Stderr = c.Stdout
	if err := c.Start(); err != nil {
		a.showInfo("Failed to start", err.Error(), true)
		return nil
	}
	a.cmdRunning, a.cmdDone = true, false
	a.cmdErr = nil
	a.cmdTitle = title
	a.cmdLines = nil
	a.cmdProc = c
	a.cmdIsTest = len(args) > 0 && args[0] == "test"
	a.cmdGen++
	a.cmdCloseAt = time.Time{}
	a.cmdSummary = nil
	ch := make(chan tea.Msg, 64)
	a.cmdCh = ch
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			ch <- msgHomeCmdLine{line: stripANSI(sc.Text())}
		}
		ch <- msgHomeCmdExit{err: c.Wait()}
		close(ch)
	}()
	return a.waitHomeCmd()
}

// waitHomeCmd pumps the next streamed line (or exit) into the update loop.
func (a *App) waitHomeCmd() tea.Cmd {
	ch := a.cmdCh
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// showInfo opens the info popup over the home screen.
func (a *App) showInfo(title, text string, isErr bool) {
	a.infoTitle, a.infoText, a.infoErr = title, text, isErr
}

// prismRunning reports whether PrismLauncher is currently open.
func prismRunning() bool {
	err := exec.Command("pgrep", "-f", "[Pp]rism[Ll]auncher").Run()
	return err == nil
}

// runSelfCommand suspends the TUI and runs this binary's own CLI subcommand
// in the real terminal, so long-running tests stream output live.
func (a *App) runSelfCommand(args ...string) tea.Cmd {
	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	// Hold the terminal after completion so the user can read the output.
	shellLine := shellQuote(append([]string{self}, args...)) +
		`; ec=$?; echo; echo "── done (exit $ec) — press enter ──"; read -r _`
	c := exec.Command("sh", "-c", shellLine)
	c.Dir = a.packDir
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return msgSelfCmdDone{err: err}
	})
}

func shellQuote(parts []string) string {
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}

func (a *App) updateServerIP(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(tea.KeyMsg); ok {
		switch m.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "esc":
			a.serverIPError = ""
			a.serverIPInput.Blur()
			a.screen = ScreenMainMenu
			return a, nil
		case "enter":
			addr := strings.TrimSpace(a.serverIPInput.Value())
			if err := WriteServerAddress(a.packDir, addr); err != nil {
				a.serverIPError = err.Error()
				return a, nil
			}
			a.serverAddr = addr
			a.serverIPError = ""
			a.serverIPInput.Blur()
			a.screen = ScreenMainMenu
			return a, nil
		}
	}
	var cmd tea.Cmd
	a.serverIPInput, cmd = a.serverIPInput.Update(msg)
	return a, cmd
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

// updateAddModModal handles input while the add-mod popup is open (shared by
// the manage-mods screen and the home screen's embedded mods pane). Typing
// live-searches both APIs (debounced); up/down pick a result; enter installs
// the selection from its own source.
func (a *App) updateAddModModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(tea.KeyMsg); ok {
		switch m.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "esc":
			a.closeAddModModal()
			return a, nil
		case "up", "ctrl+p":
			if a.addModIdx > 0 {
				a.addModIdx--
			}
			a.addModBtnIdx, a.addModShotIdx, a.addModPrevScroll = 0, 0, 0
			return a, a.addModImageCmds()
		case "down", "ctrl+n":
			if a.addModIdx < len(a.addModHits)-1 {
				a.addModIdx++
			}
			a.addModBtnIdx, a.addModShotIdx, a.addModPrevScroll = 0, 0, 0
			return a, a.addModImageCmds()
		case "pgup", "pgdown":
			// Scroll the preview pane; the view clamps to the content.
			if m.String() == "pgup" {
				a.addModPrevScroll = maxInt(0, a.addModPrevScroll-8)
			} else {
				a.addModPrevScroll += 8
			}
			return a, nil
		case "left", "right":
			// Cycle the preview's install/open buttons.
			if a.addModIdx < len(a.addModHits) {
				n := len(addModButtons(a.addModHits[a.addModIdx]))
				dir := 1
				if m.String() == "left" {
					dir = -1
				}
				a.addModBtnIdx = (a.addModBtnIdx + dir + n) % n
			}
			return a, nil
		case "shift+left", "shift+right":
			dir := 1
			if m.String() == "shift+left" {
				dir = -1
			}
			return a, a.addModShotMove(dir)
		case "enter":
			if a.addModIdx < len(a.addModHits) {
				return a.activateAddModButton()
			}
			// No results (yet) — fall back to packwiz's own search prompt.
			name := strings.TrimSpace(a.addModInput.Value())
			if name == "" {
				return a, nil
			}
			a.addModModal = false
			a.returnToAddModal = true
			a.startOutput()
			return a, a.runPackwiz("mr", "add", name)
		}
	}
	before := a.addModInput.Value()
	var cmd tea.Cmd
	a.addModInput, cmd = a.addModInput.Update(msg)
	if a.addModInput.Value() != before {
		// Debounce: each edit supersedes pending ticks and in-flight results.
		a.addModSeq++
		seq := a.addModSeq
		return a, tea.Batch(cmd, tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg {
			return msgAddModDebounce{seq: seq}
		}))
	}
	return a, cmd
}

// searchAddMod fires the live search for the popup's current query.
func (a *App) searchAddMod() tea.Cmd {
	q := strings.TrimSpace(a.addModInput.Value())
	a.addModQuery = q
	a.addModErr = ""
	if q == "" {
		a.addModHits = nil
		a.addModIdx = 0
		a.addModSearching = false
		return nil
	}
	a.addModSearching = true
	seq := a.addModSeq
	mc, loader := a.packMeta.Minecraft, a.packMeta.Loader
	return func() tea.Msg {
		hits, err := SearchBothSources(q, mc, loader, 10)
		return msgAddModResults{seq: seq, hits: hits, err: err}
	}
}

// addModPaneWidths mirrors viewAddModModal's geometry so image sizing and
// rendering agree: a slim list column, the rest to the preview. rw is 0 in
// the narrow single-column layout (no preview, no images).
func (a *App) addModPaneWidths() (lw, rw int) {
	w := clamp(a.width-8, 46, 130)
	fw, _ := styleModal.GetFrameSize()
	cw := w - fw
	if cw < 72 {
		return cw, 0
	}
	lw = clamp(cw/3, 24, 34)
	return lw, cw - lw - 3
}

// addModLogoSize is the preview logo's cell budget: small beside the title
// under kitty graphics (full-res anyway). Half-block art gets as many cells
// as the pane affords — every cell is a pixel — but stays compact when a
// screenshot needs the vertical space too.
func (a *App) addModLogoSize(rw int, hasShot bool) (cols, rows int) {
	if kittyGraphicsOK() {
		return 8, 4
	}
	c := clamp(rw*2/3, 16, 44)
	if hasShot {
		c = clamp(rw/4, 14, 24)
	}
	return c, c / 2
}

// addModImageCmds queues fetches for every image the popup currently wants:
// the selected hit's logo and first screenshot, plus (kitty graphics only)
// the row icons for the whole result list. Each key is fetched once per
// session; the bytes are disk-cached besides.
func (a *App) addModImageCmds() tea.Cmd {
	_, rw := a.addModPaneWidths()
	if !terminalDoesColor() || rw <= 0 {
		return nil
	}
	kitty := kittyGraphicsOK()
	if a.addModImgs == nil {
		a.addModImgs = map[string]string{}
	}
	var cmds []tea.Cmd
	fetch := func(u string, maxCols, maxRows int) {
		if u == "" || maxCols < 1 || maxRows < 1 {
			return
		}
		key := imgKey(u, maxCols, maxRows)
		if _, started := a.addModImgs[key]; started {
			return
		}
		a.addModImgs[key] = pendingImg
		id := 0
		if kitty {
			a.kittyImgSeq++
			id = kittyIDBase + a.kittyImgSeq
		}
		cmds = append(cmds, func() tea.Msg {
			block, err := fetchImageBlock(u, maxCols, maxRows, id)
			if err != nil {
				block = "" // remembered as failed so we don't refetch every keypress
			}
			return msgAddModImg{key: key, block: block}
		})
	}
	if a.addModIdx < len(a.addModHits) {
		hit := a.addModHits[a.addModIdx]
		lc, lr := a.addModLogoSize(rw, len(hit.Gallery) > 0)
		fetch(hit.IconURL, lc, lr)
		// Fetch gallery images one at a time from the page start —
		// msgAddModImg re-runs this, so the page fills until both its rows
		// are out of room.
		if len(hit.Gallery) > 0 {
			fetch(hit.Gallery[clamp(a.addModShotIdx, 0, len(hit.Gallery)-1)], rw-2, 14)
			_, end, shotRows, full := a.addModShotWindow(hit, rw)
			if len(shotRows) > 0 && !full && end < len(hit.Gallery) {
				fetch(hit.Gallery[end], rw-2, 14)
			}
		}
	}
	if kitty {
		for _, h := range a.addModHits {
			fetch(h.IconURL, 2, 1)
		}
	}
	return tea.Batch(cmds...)
}

// activateAddModButton runs the focused preview button (keyboard enter or a
// click on its box) — exact ids, so packwiz doesn't need to disambiguate.
// The query and results are kept so the reopened popup is ready for the
// next add.
func (a *App) activateAddModButton() (tea.Model, tea.Cmd) {
	if a.addModIdx >= len(a.addModHits) {
		return a, nil
	}
	hit := a.addModHits[a.addModIdx]
	btns := addModButtons(hit)
	btn := btns[clamp(a.addModBtnIdx, 0, len(btns)-1)]
	ref := hit.Refs[btn.source]
	if !btn.install {
		pageURL := "https://modrinth.com/mod/" + ref.Slug
		if btn.source == "curseforge" {
			pageURL = "https://www.curseforge.com/minecraft/mc-mods/" + ref.Slug
		}
		go exec.Command("xdg-open", pageURL).Start()
		a.statusMsg = "Opening " + pageURL
		a.statusIsErr = false
		a.statusExpire = time.Now().Add(3 * time.Second)
		return a, a.expireStatus()
	}
	a.addModModal = false
	a.returnToAddModal = true
	a.startOutput()
	if btn.source == "curseforge" {
		return a, a.runPackwiz("curseforge", "add", "--addon-id", ref.ProjectID, "-y")
	}
	return a, a.runPackwiz("modrinth", "add", "--project-id", ref.ProjectID, "-y")
}

// addModShotWindow returns the gallery page at the current pagination
// offset: whole images packed left to right (2-cell gaps) into one row.
// shotRows holds the gallery indices per displayed row; end is one past the
// last packed image; full reports the page stopped because the row was out
// of room (as opposed to running out of fetched images — unknown-size
// images end the page until their fetch lands).
func (a *App) addModShotWindow(hit ModHit, rw int) (start, end int, shotRows [][]int, full bool) {
	start = clamp(a.addModShotIdx, 0, len(hit.Gallery)-1)
	avail := rw - 2
	var cur []int
	curW := 0
	for k := start; k < len(hit.Gallery); k++ {
		block := a.addModImgs[imgKey(hit.Gallery[k], rw-2, 14)]
		if block == "" || block == pendingImg {
			break
		}
		w := lipgloss.Width(strings.Split(block, "\n")[0])
		gap := 0
		if len(cur) > 0 {
			gap = 2
		}
		if curW+gap+w > avail {
			full = true
			break
		}
		cur = append(cur, k)
		curW += gap + w
	}
	if len(cur) > 0 {
		shotRows = append(shotRows, cur)
		end = cur[len(cur)-1] + 1
	} else {
		end = start + 1 // the current slot always shows (spinner while loading)
	}
	return start, end, shotRows, full
}

// addModShotMove paginates the gallery window. Left slides back one image;
// right advances to the nearest window that reveals an image not currently
// on screen — a window that only re-shows visible images is skipped (three
// images where 1+2 fit together page [1,2] → [3], not [1,2] → [2]).
func (a *App) addModShotMove(dir int) tea.Cmd {
	if a.addModIdx >= len(a.addModHits) {
		return nil
	}
	hit := a.addModHits[a.addModIdx]
	if len(hit.Gallery) < 2 {
		return nil
	}
	_, rw := a.addModPaneWidths()
	oldStart, oldEnd, _, _ := a.addModShotWindow(hit, rw)
	a.addModPrevScroll = 0
	if dir < 0 {
		a.addModShotIdx = maxInt(0, oldStart-1)
		return a.addModImageCmds()
	}
	if oldEnd >= len(hit.Gallery) {
		return nil // the last image is already visible
	}
	for s := oldStart + 1; s < len(hit.Gallery); s++ {
		a.addModShotIdx = s
		_, e, _, full := a.addModShotWindow(hit, rw)
		if e > oldEnd {
			break
		}
		// An unknown-size image ends pages early — stop here and let its
		// fetch extend the page instead of skipping past it.
		if !full && e < len(hit.Gallery) {
			if block, known := a.addModImgs[imgKey(hit.Gallery[e], rw-2, 14)]; !known || block == pendingImg {
				break
			}
		}
	}
	return a.addModImageCmds()
}

// addModBtn is one action button in the popup's preview pane.
type addModBtn struct {
	label   string
	source  string
	install bool
}

// addModButtons builds the preview's action buttons for a hit: an install
// and an open-page button per platform hosting it, grouped by platform.
func addModButtons(h ModHit) []addModBtn {
	var btns []addModBtn
	for _, s := range h.Sources() {
		btns = append(btns,
			addModBtn{label: "⇩ install", source: s, install: true},
			addModBtn{label: "↗ website", source: s})
	}
	return btns
}

// closeAddModModal dismisses the popup and clears its search state.
func (a *App) closeAddModModal() {
	a.addModModal = false
	a.addModInput.SetValue("")
	a.addModInput.Blur()
	a.addModHits = nil
	a.addModIdx = 0
	a.addModBtnIdx, a.addModShotIdx, a.addModPrevScroll = 0, 0, 0
	a.addModQuery = ""
	a.addModErr = ""
	a.addModSearching = false
	a.addModSeq++ // orphan any in-flight search
}

func (a *App) updateManageMods(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Modal captures all input.
	if a.addModModal {
		return a.updateAddModModal(msg)
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
				a.searchInput.SetValue("")
				a.filterMods()
				return a, nil
			}
		case "/":
			if !a.searchFocus {
				a.searchFocus = true
				a.searchInput.Focus()
				return a, textinput.Blink
			}
		case "n":
			if !a.searchFocus {
				a.addModModal = true
				a.addModInput.Focus()
				return a, textinput.Blink
			}
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
		case "g":
			if !a.searchFocus {
				return a, a.openLazygit()
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
					a.returnToAddModal = false
				} else if a.wideHome() {
					// The home screen embeds the mod list — refresh in place.
					a.screen = ScreenMainMenu
					return a, a.loadMods()
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

	// Check if this is a yes/no prompt
	isYesNo := len(a.interactiveOptions) == 2
	if isYesNo {
		opt0 := strings.ToLower(a.interactiveOptions[0])
		opt1 := strings.ToLower(a.interactiveOptions[1])
		isYesNo = (opt0 == "yes" || opt0 == "y") && (opt1 == "no" || opt1 == "n")
	}

	switch m.String() {
	case "ctrl+c":
		return a, tea.Quit
	case "esc":
		// Cancel and go back
		if a.wideHome() {
			a.screen = ScreenMainMenu
		} else {
			a.screen = ScreenManageMods
		}
		a.returnToAddModal = false
		return a, nil
	case "up", "k":
		if !isYesNo && a.interactiveSelected > 0 {
			a.interactiveSelected--
			// Skip headers
			for a.interactiveSelected > 0 &&
			    a.interactiveSelected < len(a.interactiveSources) &&
			    a.interactiveSources[a.interactiveSelected] == "header" {
				a.interactiveSelected--
			}
		}
	case "down", "j":
		if !isYesNo && a.interactiveSelected < len(a.interactiveOptions)-1 {
			a.interactiveSelected++
			// Skip headers
			for a.interactiveSelected < len(a.interactiveOptions)-1 &&
			    a.interactiveSelected < len(a.interactiveSources) &&
			    a.interactiveSources[a.interactiveSelected] == "header" {
				a.interactiveSelected++
			}
		}
	case "left", "h":
		if isYesNo && a.interactiveSelected > 0 {
			a.interactiveSelected--
		}
	case "right", "l":
		if isYesNo && a.interactiveSelected < len(a.interactiveOptions)-1 {
			a.interactiveSelected++
		}
	case "enter", " ":
		// User selected an option
		var selection string

		// Check if this is a yes/no prompt (detect by option text)
		if len(a.interactiveOptions) == 2 {
			opt0 := strings.ToLower(a.interactiveOptions[0])
			opt1 := strings.ToLower(a.interactiveOptions[1])
			if (opt0 == "yes" || opt0 == "y") && (opt1 == "no" || opt1 == "n") {
				// This is a yes/no prompt - try both short and full form
				if a.interactiveSelected == 0 {
					selection = "y"
				} else {
					selection = "n"
				}
			}
		}

		// If not a y/n prompt, handle source-based selection
		if selection == "" {
			// Check if we have sources (combined modrinth/curseforge search)
			if len(a.interactiveSources) > 0 && a.interactiveSelected < len(a.interactiveSources) {
				source := a.interactiveSources[a.interactiveSelected]

				// Update command args to use the correct source
				if source == "modrinth" || source == "curseforge" {
					// Calculate the index within the source's options (skip headers)
					sourceIndex := 0
					for i := 0; i < a.interactiveSelected; i++ {
						if a.interactiveSources[i] == source {
							sourceIndex++
						}
					}

					// Update args to use correct source command
					updatedArgs := make([]string, len(a.interactivePending.args))
					copy(updatedArgs, a.interactivePending.args)
					if source == "modrinth" {
						updatedArgs[0] = "mr"
					} else {
						updatedArgs[0] = "cf"
					}
					a.interactivePending.args = updatedArgs

					selection = fmt.Sprintf("%d", sourceIndex)
				} else {
					// Regular numeric selection (0-indexed, 0 means cancel in packwiz)
					selection = fmt.Sprintf("%d", a.interactiveSelected)
				}
			} else {
				// No sources, use regular numeric selection
				selection = fmt.Sprintf("%d", a.interactiveSelected)
			}
		}

		// Combine with previous input if this is a chained prompt
		if a.interactivePending.input != "" {
			selection = a.interactivePending.input + "\n" + selection
		}

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
	case ScreenCreateRepo:
		body = a.viewCreateRepo()
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
	case ScreenServerIP:
		body = a.viewServerIP()
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
	case ScreenCreateRepo:
		hints = []string{"enter create", "tab visibility", "esc back", "ctrl+c quit"}
	case ScreenServerIP:
		hints = []string{"enter save", "esc cancel", "ctrl+c quit"}
	case ScreenMainMenu:
		if !a.wideHome() {
			hints = []string{"↑↓ navigate", "enter select", "1-9 shortcut", "g lazygit", "c agent", "q quit"}
		} else if a.agentFull || a.homeFocus == 1 {
			hints = []string{"enter send", "shift+tab mode", "ctrl+f fullscreen", "pgup/pgdn scroll", "tab next pane", "esc back"}
		} else if a.homeFocus == 0 {
			hints = []string{"enter details", "e edit", "/ search", "n add", "d delete/restore", "r refresh", "tab next pane"}
		} else if a.homeFocus == 3 || a.homeFocus == 4 {
			hints = []string{"enter edit", "tab next pane", "esc back", "q quit"}
		} else if a.homeFocus == 5 {
			hints = []string{"↑↓ navigate", "enter select", "s side", "o optional", "v versions", "esc close"}
		} else {
			hints = []string{"←→ navigate", "enter select", "1-8 shortcut", "e address", "g lazygit", "tab next pane", "q quit"}
		}
	case ScreenManageMods:
		hints = []string{"enter edit", "g lazygit", "r refresh", "/ search", "n add", "d delete/restore", "esc back"}
	case ScreenOutput:
		if a.outputDone {
			hints = []string{"enter continue"}
		} else {
			hints = []string{"running…"}
		}
	case ScreenInteractive:
		// Check if this is a yes/no prompt for appropriate hints
		isYesNo := len(a.interactiveOptions) == 2
		if isYesNo {
			opt0 := strings.ToLower(a.interactiveOptions[0])
			opt1 := strings.ToLower(a.interactiveOptions[1])
			isYesNo = (opt0 == "yes" || opt0 == "y") && (opt1 == "no" || opt1 == "n")
		}
		if isYesNo {
			hints = []string{"←→ navigate", "enter select", "esc cancel"}
		} else {
			hints = []string{"↑↓ navigate", "enter select", "esc cancel"}
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
