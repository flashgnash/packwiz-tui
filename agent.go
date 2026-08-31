package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Config holds user settings, stored next to the recents file.
type Config struct {
	// Agent is the chat agent command line, e.g. "claude" or "aider --model …".
	Agent string `json:"agent"`
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".packwiz-tui-config.json")
}

func LoadConfig() Config {
	cfg := Config{Agent: "claude"}
	if data, err := os.ReadFile(configPath()); err == nil {
		json.Unmarshal(data, &cfg)
	}
	if env := os.Getenv("PACKWIZ_TUI_AGENT"); env != "" {
		cfg.Agent = env
	}
	if cfg.Agent == "" {
		cfg.Agent = "claude"
	}
	return cfg
}

// agentCommand builds the agent process, run from the pack directory so the
// agent sees the pack (and its CLAUDE.md etc) as its working context. The
// agent gets packwiz-tui itself on PATH and a CLAUDE.md section documenting
// the tooling, so it can run the tests and fixers on its own.
func agentCommand(packDir string) (*exec.Cmd, error) {
	cfg := LoadConfig()
	parts := strings.Fields(cfg.Agent)
	if _, err := exec.LookPath(parts[0]); err != nil {
		return nil, err
	}
	ensureAgentContext(packDir)
	ensureAgentSettings(packDir)
	c := exec.Command(parts[0], parts[1:]...)
	c.Dir = packDir
	c.Env = agentEnv()
	return c, nil
}

// agentEnv extends PATH with the directory of the running packwiz-tui binary
// (the nix wrapper puts our tool deps on PATH, but not the tool itself).
func agentEnv() []string {
	self, err := os.Executable()
	if err != nil {
		return os.Environ()
	}
	env := os.Environ()
	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			env[i] = "PATH=" + filepath.Dir(self) + ":" + strings.TrimPrefix(e, "PATH=")
			return env
		}
	}
	return append(env, "PATH="+filepath.Dir(self))
}

const agentContextMarker = "<!-- packwiz-tui tooling (auto-maintained, do not edit between markers) -->"

const agentContextBlock = agentContextMarker + `
## packwiz-tui tooling

This pack is managed with packwiz-tui, which is on PATH here. Useful commands
(run from the pack directory; they find pack.toml automatically):

- ` + "`packwiz-tui test server`" + ` — install + boot this pack's server, verify it reaches "Done", sample TPS over RCON. Fails with log paths on crash.
- ` + "`packwiz-tui test full [--soak 90s]`" + ` — the above plus a real headless client (gamescope + portablemc, offline account) that auto-joins, soaks in spectator, samples TPS, and saves screenshots to ` + "`.packwiz-tui/last-test/`" + ` — read those screenshots to check for visual problems.
- ` + "`packwiz-tui fix-sources`" + ` — find CurseForge-API-blocked mods (breaks unattended installs) and swap them to byte-identical Modrinth files. Doubles as an install test.
- ` + "`packwiz-tui tag-sides <server-pack.zip>`" + ` — set side=client/both on all mods by diffing an official server pack.
- ` + "`packwiz-tui export prism|prism-preinstalled|mrpack|curseforge|server|all`" + ` — build importable artifacts into ` + "`.packwiz-tui/build/`" + `. The prism zips import into PrismLauncher and self-update from this repo via a packwiz-installer pre-launch hook (preinstalled bundles all mods too).
- ` + "`packwiz-tui install-prism`" + ` — write the self-updating instance straight into the local PrismLauncher (creates or refreshes; never touches worlds/options). User restarts Prism to see it.
- ` + "`packwiz-tui server-ip [address]`" + ` — get/set the pack's default server address; when set, prism exports/installs get a prefilled servers.dat (existing installs keep their own server list).
- ` + "`packwiz-tui nixos-config`" + ` — print a services.minecraft-servers block for the user's nix-minecraft flake, ready to paste into nixos-configuration (glados-style hosting).
- ` + "`packwiz-tui release [tag]`" + ` — export all + publish a GitHub release with gh (defaults to v<pack version>).
- ` + "`packwiz-tui init-workflow`" + ` — scaffold a GitHub Actions workflow: every push builds all artifacts (downloadable as workflow artifacts), and a v* tag push publishes them as a release.
- ` + "`packwiz`" + ` itself (add/remove/update/refresh) is also on PATH.

Test artifacts and server/client state live under ` + "`.packwiz-tui/`" + ` (gitignored). Logs on failure: ` + "`.packwiz-tui/test-server/harness-console.log`" + `, ` + "`.packwiz-tui/test-client/logs/latest.log`" + `.

One-time CI setup offer: if ` + "`.github/workflows/release.yml`" + ` does not exist
and there is no ` + "`workflow-offer`" + ` comment below this block, offer the user CI
setup (` + "`packwiz-tui init-workflow`" + ` — builds artifacts on every push, releases
on v* tags) exactly once at the start of the session. Whatever they decide,
record it by appending ` + "`<!-- packwiz-tui: workflow-offer: accepted|declined YYYY-MM-DD -->`" + `
on its own line directly after this block, so future agents don't ask again.
` + agentContextMarker + "\n"

// ensureAgentContext keeps the pack's CLAUDE.md carrying an up-to-date
// description of the packwiz-tui commands, replacing the marker-delimited
// block if its content drifted.
func ensureAgentContext(packDir string) {
	path := filepath.Join(packDir, "CLAUDE.md")
	data, _ := os.ReadFile(path)
	text := string(data)
	if strings.Count(text, agentContextMarker) == 2 {
		start := strings.Index(text, agentContextMarker)
		end := strings.LastIndex(text, agentContextMarker) + len(agentContextMarker)
		if end < len(text) && text[end] == '\n' {
			end++
		}
		current := text[start:end]
		if current == agentContextBlock {
			return
		}
		text = text[:start] + agentContextBlock + text[end:]
	} else {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if text != "" {
			text += "\n"
		}
		text += agentContextBlock
	}
	os.WriteFile(path, []byte(text), 0644)
}

// agentAllowRules are Claude Code permission rules for our own tooling, so
// the agent can run it without approval prompts.
var agentAllowRules = []string{
	"Bash(packwiz-tui:*)",
	"Bash(packwiz:*)",
	"Bash(mc-logs:*)",
}

// ensureAgentSettings merges agentAllowRules into the pack's project-level
// .claude/settings.json, preserving anything else in the file.
func ensureAgentSettings(packDir string) {
	// Keep agent config out of the pack index — a repeat of the CLAUDE.md
	// incident (docs hashed into the index break player installs).
	ensureLineInFile(filepath.Join(packDir, ".packwizignore"), ".claude/**")
	path := filepath.Join(packDir, ".claude", "settings.json")
	var root map[string]any
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &root)
	}
	if root == nil {
		root = map[string]any{}
	}
	perms, _ := root["permissions"].(map[string]any)
	if perms == nil {
		perms = map[string]any{}
	}
	existing, _ := perms["allow"].([]any)
	have := map[string]bool{}
	for _, e := range existing {
		if s, ok := e.(string); ok {
			have[s] = true
		}
	}
	changed := false
	for _, rule := range agentAllowRules {
		if !have[rule] {
			existing = append(existing, rule)
			changed = true
		}
	}
	if !changed && len(existing) > 0 {
		return
	}
	perms["allow"] = existing
	root["permissions"] = perms
	os.MkdirAll(filepath.Dir(path), 0755)
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(path, append(data, '\n'), 0644)
}

type msgAgentDone struct{ err error }

// openAgent suspends the TUI and hands the real terminal to the agent, the
// same way lazygit is embedded — every agent keybinding works natively
// because it owns the tty until it exits.
func (a *App) openAgent() tea.Cmd {
	c, err := agentCommand(a.packDir)
	if err != nil {
		return func() tea.Msg { return msgAgentDone{err: err} }
	}
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return msgAgentDone{err: err}
	})
}
