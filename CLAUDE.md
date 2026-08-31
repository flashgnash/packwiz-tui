# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

packwiz-tui is a Terminal UI wrapper for packwiz, a Minecraft modpack management CLI tool. The application is built with Go using the Bubble Tea TUI framework and provides an interactive interface for managing Minecraft modpacks.

## Development Commands

### Build and Run
```bash
# Run from source
go run .

# Build with Nix
nix build

# Enter development shell (Nix)
nix develop
```

### Dependencies
The project requires:
- `packwiz` CLI tool (must be in PATH)
- `git` (for repository operations)
- Go 1.21+

**Important:** When adding or removing Go dependencies (via `go get`), you must update the Nix build hash:
```bash
./update-hash.sh
```
This script automatically updates the `vendorHash` in `flake.nix` to match the new Go modules.

## Architecture

### Bubble Tea Pattern
The application follows the Elm architecture via Bubble Tea:
- **Model**: The `App` struct in app.go:47-95 holds all application state
- **Update**: `App.Update()` in app.go:240 handles messages and returns new state + commands
- **View**: `App.View()` in app.go:615 renders the current state to strings

### Screen Navigation
The app uses a screen-based navigation system defined by the `Screen` enum in app.go:16-26:
- `ScreenLoading`: Initial loading and async operations
- `ScreenRepoSelect`: Choose from recent repos or clone new
- `ScreenCloneRepo`: Input form for cloning a repository
- `ScreenMainMenu`: Primary navigation hub
- `ScreenManageMods`: Browse/search/add/delete mods
- `ScreenManageLoader`: Instructions for loader management
- `ScreenOutput`: Display command output (packwiz, git)

Each screen has its own update handler (e.g., `updateMainMenu()`) and view renderer (e.g., `viewMainMenu()`).

### Message-Driven State Changes
Asynchronous operations return custom message types that trigger state updates:
- `msgPackFound`: Transition to main menu with pack loaded
- `msgModsLoaded`: Display mod list in manage mods screen
- `msgCmdDone`: Show command output after running packwiz/git
- `msgReposLoaded`: Display repo selection screen

Commands are spawned via methods like `detectRepo()`, `loadMods()`, `runPackwiz()` in app.go:139-236.

### File Organization
- **main.go**: Entry point — dispatches CLI subcommands (cli.go) or starts the TUI
- **cli.go**: Headless subcommands (`test server`, `test full`, `tag-sides`, `fix-sources`, `agent`) mirroring the TUI tooling
- **app.go**: Core application state, screen logic, update handlers
- **views.go**: All view rendering functions for each screen
- **styles.go**: Lipgloss style definitions and color palette
- **git.go**: Git operations, repository detection, pack.toml discovery
- **helpers.go**: Utility functions (truncate, clamp, visibleWindow)
- **packmeta.go**: Parses mc/loader versions from pack.toml (`PackMeta`)
- **harness.go**: Server test harness — loader install, packwiz-installer, boot, log watch, RCON metrics. State under `<pack>/.packwiz-tui/` (auto-excluded from git + packwiz index)
- **soak.go**: Full-stack test — headless client via gamescope+portablemc (offline account, quick-play join), spectator soak, `gamescopectl` screenshots into `.packwiz-tui/last-test/`
- **rcon.go**: Minimal native Minecraft RCON client
- **sides.go**: `TagSides` — side=client/both tagging by diffing a server-pack zip
- **sources.go**: `FixModSources` — detects CurseForge-API-blocked mods (by running packwiz-installer into a scratch dir) and swaps them to byte-identical Modrinth files via sha1 lookup, preserving side
- **agent.go**: Chat agent integration (`tea.ExecProcess`, like lazygit) + `~/.packwiz-tui-config.json` / `PACKWIZ_TUI_AGENT` config; keeps a marker-delimited tooling section in the pack's CLAUDE.md and puts this binary on the agent's PATH
- **export.go**: Artifact builders into `.packwiz-tui/build/` — `ExportMMC` (Prism-importable instance zip, self-updating via packwiz-installer pre-launch hook pointed at the repo's raw pack.toml URL), `ExportPackwiz` (mrpack/curseforge), `ExportServer`, `ReleaseGithub` (gh CLI)
- **workflow.go**: `InitWorkflow` — scaffolds a tag-triggered GitHub Actions release workflow (installs packwiz + this module via `go install`, so go.mod's module path must stay `github.com/flashgnash/packwiz-tui`)

### Test Harness Notes
- Everything is generic across packs/loaders (neoforge/forge/fabric/quilt) — versions come from pack.toml; no pack-specific logic
- Long-running tests are TUI-launched via `runSelfCommand` (tea.ExecProcess of this binary's own CLI subcommand) so output streams in the real terminal
- Test player is put in spectator via RCON right after join so it can't die during the soak
- On NeoForge the TPS command is `neoforge tps` (not `forge tps`); `spark tps` is tried first when the pack ships spark
- The nix wrapper provides java 21, portablemc, and gamescope; `test full` refuses to run if they're missing from PATH

### Data Flow
1. User in git repo → `detectRepo()` → finds pack.toml → loads main menu
2. User selects "Manage Mods" → `loadMods()` → lists *.toml in `<packDir>/mods/`
3. User adds mod → `runPackwiz("mr", "add", name)` → shows output → reloads mod list
4. User deletes mod → removes .toml file → `runPackwiz("refresh")` → reloads list
5. User exits with push → `gitPush()` → stages all, commits with timestamp, pushes

### State Persistence
Recent repositories are stored in `~/.packwiz-tui-recents.json` (max 20 entries). Each entry tracks:
- Display name (derived from git remote)
- Filesystem path
- Git remote URL
- Last used timestamp (RFC3339)

### Mouse Support
The app registers clickable zones in `App.clickZones` during rendering (views.go). Click handlers are processed in `Update()` for:
- Main menu items (views.go:117-122)
- Add mod button (views.go:180-183)
- Delete mod buttons per row (views.go:201-206)

### External Commands
All external commands run via `os/exec`:
- `packwiz` commands run in `packDir` via `RunPackwiz()` (git.go:151)
- `git` operations via `DetectGitRepo()`, `CloneRepo()`, `GitPushAll()` (git.go:29-182)
- Commands return combined stdout/stderr for display in output screen

## packwiz Integration
The app expects a `pack.toml` file in the repository, typically at the repo root or in a subdirectory. The pack directory is where all packwiz commands are executed. Mods are stored as individual `.toml` files in `<packDir>/mods/`.

## Styling Guidelines
The app uses a dark "forge" theme with molten orange accents:
- Primary accent: `#ff6b2b` (orange)
- Background: `#0d0f14` (very dark blue)
- Panels: `#141720`
- Use existing lipgloss styles from styles.go rather than inline styling
- All UI elements center vertically with `lipgloss.Place()` except the manage mods screen which uses `lipgloss.Top`

## Common Patterns

### Adding a New Screen
1. Add enum value to `Screen` type (app.go:16)
2. Add case in `App.Update()` switch (app.go:319)
3. Create `updateScreenName()` handler function
4. Create `viewScreenName()` renderer function in views.go
5. Add case in `App.View()` switch (app.go:620)
6. Add keyboard hints to status bar (app.go:645)

### Running Async Operations
1. Create command function returning `tea.Cmd` (see app.go:133-236)
2. Define result message type (see app.go:28-43)
3. Return command from update handler
4. Handle result message in `App.Update()`
5. Use `ScreenLoading` with `loadingMsg` for user feedback

### Text Input Fields
Use `github.com/charmbracelet/bubbles/textinput`:
1. Initialize in `NewApp()` with placeholder and width
2. Call `.Focus()` when activating
3. Update with `input.Update(msg)` when focused
4. Call `.Blur()` when deactivating
5. See `cloneInput`, `searchInput`, `addModInput` for examples
