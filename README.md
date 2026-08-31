# Packwiz TUI

Vibe coded TUI wrapper
Works reasonably well, looks pretty, partly to test out claude code (cost around $20 in credits so far, expensive but to be fair it's made a pretty good tool)

Made to solve a problem, I've got a lot of nix workflow setup to easily deploy packwiz servers

A friend of mine is co admin of this server and needs to be able to work with it, so built this for a more user friendly means of doing that

(also, the reason for it being a TUI is so he can do so on the server itsself over SSH and not have to install anything locally)

Video demonstration
https://minecraft.flashgnash.co.uk/uploads/26-03-14-23:27:44-kitty.mp4

The main page is the mod list
Search with /, navigate with arrow keys or vim motions, enter to edit the selected file manually with $EDITOR, D to delete or restore mod
G is a shortcut to lazygit as I don't want to automatically create git commits, plus why reinvent the wheel

Requirements:
- Packwiz
- Lazygit
- A text editor

(the nix package wraps all of these plus the test-harness tools — java 21, portablemc, gamescope — so `nix run` works standalone on a server)

## CLI mode

Everything the TUI can do is also a headless subcommand, so servers, CI, and
AI agents can drive it over SSH with no interface:

```
packwiz-tui test server        # install + boot the pack's server, verify Done, sample TPS
packwiz-tui test full          # server + real headless client (gamescope), 90s soak, screenshots
packwiz-tui tag-sides <zip>    # set side=client/both on every mod by diffing a server-pack zip
packwiz-tui fix-sources        # swap CurseForge-API-blocked mods to Modrinth equivalents
packwiz-tui agent              # open the configured chat agent in the pack dir
packwiz-tui export all         # build mmc/mrpack/curseforge/server artifacts into .packwiz-tui/build/
packwiz-tui release [tag]      # export all + publish a GitHub release via gh
packwiz-tui init-workflow      # scaffold a release-on-tag GitHub Actions workflow
```

The `mmc` export is a PrismLauncher-importable instance zip whose pre-launch
hook runs packwiz-installer against this repo's raw pack.toml URL — imported
instances self-update on every launch (the joelbotc deployment pattern).

Tests run against the pack found in (or above) the current directory. Harness
state and artifacts (server dir, client dir, soak screenshots, metrics) live
under `<pack>/.packwiz-tui/` which is auto-excluded from git and the packwiz
index. The full-stack test launches a real client in a virtual display
(gamescope headless) with an offline account, auto-joins the test server,
holds the connection while sampling TPS over RCON, and saves screenshots to
`.packwiz-tui/last-test/` for visual inspection.

## Agent chat

`c` in the main menu (or `packwiz-tui agent`) opens a chat agent in the pack
directory — Claude Code by default. The TUI hands over the whole terminal
while the agent runs (same mechanism as the lazygit integration), so every
agent keybinding works exactly as it does standalone. Configure the command
via `~/.packwiz-tui-config.json`:

```json
{ "agent": "claude" }
```

or the `PACKWIZ_TUI_AGENT` environment variable.
