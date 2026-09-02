package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CLI subcommands mirror the TUI's tooling so headless environments (servers,
// CI, AI agents) can drive everything without the interface:
//
//	packwiz-tui test server [--ram N] [--timeout 15m]
//	packwiz-tui test full   [--ram N] [--soak 90s] [--timeout 15m]
//	packwiz-tui tag-sides <server-pack.zip>
//	packwiz-tui fix-sources [--side client]
//	packwiz-tui agent
//
// All run against the pack found in (or above) the current directory.

func cliUsage() {
	fmt.Fprint(os.Stderr, `usage: packwiz-tui [command]

Run with no command for the interactive TUI.

commands:
  test server        install + boot the pack's server, verify it comes up, sample TPS
  test full          server + real headless client (gamescope), soak, screenshots
  tag-sides <zip>    set side=client/both on all mods by diffing a server-pack zip
  fix-sources        swap CurseForge-API-blocked mods to Modrinth (byte-identical files)
  search <query>     search Modrinth + CurseForge for mods matching this pack's
                     mc version/loader. Flags: --source modrinth|curseforge|all,
                     --limit N, --any-version (drop the version/loader filter)
  mod-info <slug>    project details + installable versions for this pack, with
                     the exact packwiz command to install a specific one.
                     Flags: --source, --limit, --any-version as above
  convert-sources <target>
                     convert every mod to modrinth (byte-identical, sha1) or
                     curseforge (by slug, rolled back if not found)
  export <what>      build artifacts into .packwiz-tui/build/ — prism (importable,
                     self-updating from this repo), prism-preinstalled (same but with
                     all mods bundled), mrpack, curseforge, server, or all
  install-prism      write the self-updating instance into the local PrismLauncher
  launch-client      install the pack client-side and start it via portablemc
                     (fallback launcher — the TUI prefers PrismLauncher)
  nixos-config       print a services.minecraft-servers block for the nix-minecraft
                     flake module, ready to paste into nixos-configuration (glados-style)
  changelog          markdown changelog between two refs: mod adds/removals diffed
                     from git, other changes described by the configured agent
                     (falls back to a file list). Flags: --from <ref> --to <ref>
                     (defaults: previous tag → HEAD)
  release [tag]      export all + publish a GitHub release via gh (default tag: v<pack version>)
  init-workflow      scaffold .github/workflows/release.yml (build artifacts on every
                     push; publish a GitHub release on v* tag push)
  agent              open the configured chat agent (default: claude) in the pack dir

flags for test:
  --ram N            heap in GB for server/client (default 8, clamped to system RAM)
  --soak DURATION    client soak time for 'test full' (default 90s)
  --timeout DURATION max wait for server boot / client join (default 15m)
  --port N           server port (default 25565)
  --rcon-port N      rcon port (default 25575)
`)
}

// parseFlagsAnywhere parses fs against args, allowing flags to appear after
// positionals (`search sodium --limit 3` — the natural order for agents);
// returns the positional args in order.
func parseFlagsAnywhere(fs *flag.FlagSet, args []string) []string {
	var pos []string
	for {
		fs.Parse(args)
		rest := fs.Args()
		if len(rest) == 0 {
			return pos
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

// RunCLI handles subcommands; returns false if none was given (start the TUI).
func RunCLI(args []string) (handled bool, exitCode int) {
	if len(args) == 0 {
		return false, 0
	}

	findPack := func() string {
		cwd, _ := os.Getwd()
		if toml, err := FindPackToml(cwd); err == nil {
			dir := filepath.Dir(toml)
			EnsurePackIgnores(dir)
			return dir
		}
		fmt.Fprintln(os.Stderr, "error: no pack.toml found in or below the current directory")
		os.Exit(1)
		return ""
	}

	switch args[0] {
	case "-h", "--help", "help":
		cliUsage()
		return true, 0

	case "test":
		if len(args) < 2 || (args[1] != "server" && args[1] != "full") {
			cliUsage()
			return true, 1
		}
		fs := flag.NewFlagSet("test", flag.ExitOnError)
		ram := fs.Int("ram", 0, "heap GB")
		soak := fs.Duration("soak", 0, "client soak duration")
		timeout := fs.Duration("timeout", 0, "boot/join timeout")
		port := fs.Int("port", 0, "server port")
		rconPort := fs.Int("rcon-port", 0, "rcon port")
		fs.Parse(args[2:])
		opts := TestOptions{
			RAMGb: *ram, SoakTime: *soak, BootTimeout: *timeout,
			Port: *port, RconPort: *rconPort,
		}
		packDir := findPack()
		start := time.Now()
		var err error
		if args[1] == "server" {
			err = TestServer(packDir, opts, os.Stdout)
		} else {
			err = TestFull(packDir, opts, os.Stdout)
		}
		fmt.Printf("elapsed: %s\n", time.Since(start).Round(time.Second))
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
			return true, 1
		}
		return true, 0

	case "tag-sides":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: packwiz-tui tag-sides <server-pack.zip>")
			return true, 1
		}
		report, err := TagSides(findPack(), args[1])
		fmt.Print(report)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
			return true, 1
		}
		return true, 0

	case "convert-sources":
		if len(args) < 2 || (args[1] != "modrinth" && args[1] != "curseforge") {
			fmt.Fprintln(os.Stderr, "usage: packwiz-tui convert-sources modrinth|curseforge [slug]")
			return true, 1
		}
		onlySlug := ""
		if len(args) > 2 {
			onlySlug = args[2]
		}
		if err := ConvertSources(findPack(), args[1], onlySlug, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
			return true, 1
		}
		return true, 0

	case "fix-sources":
		fs := flag.NewFlagSet("fix-sources", flag.ExitOnError)
		side := fs.String("side", "client", "side to test-install (client covers both)")
		fs.Parse(args[1:])
		report, err := FixModSources(findPack(), *side)
		fmt.Print(report)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
			return true, 1
		}
		return true, 0

	case "search":
		fs := flag.NewFlagSet("search", flag.ExitOnError)
		source := fs.String("source", "all", "modrinth|curseforge|all")
		limit := fs.Int("limit", 8, "results per source")
		anyVersion := fs.Bool("any-version", false, "don't filter by the pack's mc version/loader")
		query := strings.TrimSpace(strings.Join(parseFlagsAnywhere(fs, args[1:]), " "))
		if query == "" {
			fmt.Fprintln(os.Stderr, "usage: packwiz-tui search [flags] <query>")
			return true, 1
		}
		if err := CLISearch(findPack(), *source, query, *limit, *anyVersion, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
			return true, 1
		}
		return true, 0

	case "mod-info":
		fs := flag.NewFlagSet("mod-info", flag.ExitOnError)
		source := fs.String("source", "", "modrinth|curseforge (default: modrinth, then curseforge)")
		limit := fs.Int("limit", 10, "max versions listed")
		anyVersion := fs.Bool("any-version", false, "don't filter by the pack's mc version/loader")
		pos := parseFlagsAnywhere(fs, args[1:])
		if len(pos) < 1 {
			fmt.Fprintln(os.Stderr, "usage: packwiz-tui mod-info [flags] <slug>")
			return true, 1
		}
		if err := CLIModInfo(findPack(), *source, pos[0], *limit, *anyVersion, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
			return true, 1
		}
		return true, 0

	case "export":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: packwiz-tui export prism|prism-preinstalled|mrpack|curseforge|server|all")
			return true, 1
		}
		packDir := findPack()
		var err error
		switch args[1] {
		case "prism", "mmc":
			_, err = ExportMMC(packDir, os.Stdout)
		case "prism-preinstalled":
			_, err = ExportMMCPreinstalled(packDir, os.Stdout)
		case "mrpack":
			_, err = ExportPackwiz(packDir, "modrinth", os.Stdout)
		case "curseforge":
			_, err = ExportPackwiz(packDir, "curseforge", os.Stdout)
		case "server":
			_, err = ExportServer(packDir, os.Stdout)
		case "all":
			_, err = ExportAll(packDir, os.Stdout)
		default:
			fmt.Fprintf(os.Stderr, "unknown export %q\n", args[1])
			return true, 1
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
			return true, 1
		}
		return true, 0

	case "server-ip", "server-address":
		packDir := findPack()
		if len(args) > 1 {
			if err := WriteServerAddress(packDir, args[1]); err != nil {
				fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
				return true, 1
			}
			fmt.Printf("server address %q will be prefilled into prism exports/installs\n", args[1])
		} else if addr := ReadServerAddress(packDir); addr != "" {
			fmt.Println(addr)
		} else {
			fmt.Println("(no server address configured)")
		}
		return true, 0

	case "nixos-config":
		snippet, err := GenerateNixosConfig(findPack())
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
			return true, 1
		}
		fmt.Print(snippet)
		return true, 0

	case "launch-client":
		if err := LaunchClient(findPack(), os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
			return true, 1
		}
		return true, 0

	case "install-prism":
		if err := InstallPrism(findPack(), os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
			return true, 1
		}
		return true, 0

	case "changelog":
		fs := flag.NewFlagSet("changelog", flag.ExitOnError)
		from := fs.String("from", "", "base ref (default: previous tag)")
		to := fs.String("to", "", "target ref (default: HEAD)")
		fs.Parse(args[1:])
		md, err := Changelog(findPack(), *from, *to, os.Stderr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
			return true, 1
		}
		fmt.Print(md)
		return true, 0

	case "release":
		tag := ""
		if len(args) > 1 {
			tag = args[1]
		}
		if err := ReleaseGithub(findPack(), tag, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
			return true, 1
		}
		return true, 0

	case "init-workflow":
		if err := InitWorkflow(findPack(), os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
			return true, 1
		}
		return true, 0

	case "img-test":
		// Hidden diagnostic: check terminal image alignment outside the TUI.
		RunImgTest()
		return true, 0

	case "mcp-approve":
		// Internal: MCP permission-prompt server for the embedded agent chat.
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: packwiz-tui mcp-approve <socket>")
			return true, 1
		}
		if err := RunMCPApprove(args[1]); err != nil {
			return true, 1
		}
		return true, 0

	case "agent":
		c, err := agentCommand(findPack(), 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent not found: %v\n", err)
			return true, 1
		}
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			return true, 1
		}
		return true, 0
	}

	fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
	cliUsage()
	return true, 1
}
