package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func testApp(w, h int) *App {
	a := NewApp()
	a.width, a.height = w, h
	a.packName = "ATM 10 Custom"
	a.packDir = "/home/flashgnash/modpacks/modified-atm10"
	a.serverAddr = "modified-atm10.mc.flashgnash.co.uk"
	a.packMeta = PackMeta{Minecraft: "1.21.1", Loader: "neoforge", LoaderVer: "21.1.77"}
	for i := 0; i < 600; i++ {
		a.mods = append(a.mods, ModFile{Name: fmt.Sprintf("some-fairly-long-mod-name-%d.pw", i), Filename: "x.toml", Path: fmt.Sprintf("/tmp/x%d", i)})
	}
	a.filterMods()
	return a
}

// Rendering must not mutate shared package-level styles (lipgloss v0.10
// setters share the rules map — deriving without Copy() poisons the
// original, which showed up as struck-through text and truncated buttons).
func TestHomeStylePurity(t *testing.T) {
	a := testApp(160, 40)
	a.setHomeFocus(2)
	a.viewHome()
	body := a.viewHome() // a poisoned style only shows from the second render
	for _, want := range []string{"✕ Exit", "⟲ Discard", "↑ Push", "▶ Test Server", "⇩ Install to Prism", "/ "} {
		if !strings.Contains(body, want) {
			t.Errorf("second render lost %q — a shared style was likely mutated", want)
		}
	}
}

// The add-mod modal must overlay as a popup: full-width lines, background
// visible around it.
func TestHomeModalOverlay(t *testing.T) {
	a := testApp(160, 40)
	a.setHomeFocus(0)
	a.addModModal = true
	body := a.viewHome()
	if !strings.Contains(body, "Add Mod from Modrinth") {
		t.Fatalf("modal not rendered")
	}
	for i, l := range strings.Split(body, "\n") {
		if lipgloss.Width(l) > a.width {
			t.Errorf("modal overlay line %d overflows (%d > %d)", i, lipgloss.Width(l), a.width)
		}
	}
	// A mod row on the same line as the modal should still be visible.
	if !strings.Contains(body, "some-fairly-long-mod-name-2") {
		t.Errorf("background wiped by modal overlay")
	}
}

// The add (+) button in the search row must sit in the same column as the
// per-row delete (−) buttons.
func TestHomeAddDeleteAligned(t *testing.T) {
	a := testApp(160, 40)
	a.setHomeFocus(0)
	lines := strings.Split(a.renderHomeModsPane(64, 33, 4), "\n")
	visCol := func(line string, target rune) int {
		col := 0
		for _, r := range line {
			if r == target {
				return col
			}
			col += lipgloss.Width(string(r))
		}
		return -1
	}
	plus, minus := visCol(lines[2], '+'), visCol(lines[4], '−')
	if plus != minus {
		t.Errorf("+ at column %d but − at column %d", plus, minus)
	}
}

// With a command streaming, the agent area splits and geometry must hold.
func TestHomeCmdPane(t *testing.T) {
	a := testApp(160, 40)
	a.setHomeFocus(2)
	a.cmdTitle = "▶ Test Server"
	a.cmdRunning = true
	for i := 0; i < 300; i++ {
		a.cmdLines = append(a.cmdLines, strings.Repeat("server log output line ", 4))
	}
	body := a.viewHome()
	lines := strings.Split(body, "\n")
	if len(lines) != a.height-1 {
		t.Errorf("body is %d lines, want %d", len(lines), a.height-1)
	}
	for i, l := range lines {
		if lipgloss.Width(l) > a.width {
			t.Errorf("line %d overflows (%d > %d)", i, lipgloss.Width(l), a.width)
		}
	}
	if !strings.Contains(body, "Test Server") || !strings.Contains(body, "server log output") {
		t.Errorf("command pane content missing")
	}
	if !strings.Contains(body, "✦ Agent") {
		t.Errorf("agent pane missing from split view")
	}
}

// The mod detail pane splits the agent area (optionally three ways with the
// command pane) — geometry must hold.
func TestHomeModDetailPane(t *testing.T) {
	for _, withCmd := range []bool{false, true} {
		a := testApp(160, 40)
		if withCmd {
			a.cmdTitle = "▶ Test Server"
			a.cmdRunning = true
			a.cmdLines = []string{"booting"}
		}
		cmd := a.openModDetail(a.modsFiltered[0])
		_ = cmd
		a.modDetail.Source = "modrinth"
		a.modDetail.Versions = []modVersion{{ID: "v1", Number: "1.0.0"}, {ID: "v2", Number: "1.1.0"}}
		body := a.viewHome()
		lines := strings.Split(body, "\n")
		if len(lines) != a.height-1 {
			t.Errorf("withCmd=%v: body is %d lines, want %d", withCmd, len(lines), a.height-1)
		}
		for i, l := range lines {
			if lipgloss.Width(l) > a.width {
				t.Errorf("withCmd=%v: line %d overflows (%d > %d)", withCmd, i, lipgloss.Width(l), a.width)
			}
		}
		for _, want := range []string{"Side", "Optional", "Version", "Project page", "✦ Agent"} {
			if !strings.Contains(body, want) {
				t.Errorf("withCmd=%v: detail pane missing %q", withCmd, want)
			}
		}
	}
}

// The sources popup renders with counts and both convert buttons.
func TestHomeSourcesModal(t *testing.T) {
	a := testApp(160, 40)
	a.sourcesModal = true
	a.sourceCounts = SourceCounts{Curseforge: 5, Modrinth: 25, Local: 3}
	body := a.viewHome()
	for _, want := range []string{"CURSEFORGE (5)", "LOCAL (3)", "MODRINTH (25)", "Convert All"} {
		if !strings.Contains(body, want) {
			t.Errorf("sources modal missing %q", want)
		}
	}
	for i, l := range strings.Split(body, "\n") {
		if lipgloss.Width(l) > a.width {
			t.Errorf("line %d overflows (%d > %d)", i, lipgloss.Width(l), a.width)
		}
	}
}

// The home dashboard must always render exactly height-1 lines (the last
// line is the hint bar) with no line wider than the terminal — any overflow
// wraps, staggers the panes, and pushes the button borders under the hint bar.
func TestHomeLayoutHeight(t *testing.T) {
	for _, size := range [][2]int{{96, 24}, {120, 30}, {160, 40}, {220, 56}, {250, 67}} {
		for focus := 0; focus <= 4; focus++ {
			a := NewApp()
			a.width, a.height = size[0], size[1]
			a.packName = "ATM 10 Custom"
			a.packDir = "/home/flashgnash/modpacks/modified-atm10"
			a.serverAddr = "modified-atm10.mc.flashgnash.co.uk"
			a.packMeta = PackMeta{Minecraft: "1.21.1", Loader: "neoforge", LoaderVer: "21.1.77"}
			for i := 0; i < 600; i++ {
				a.mods = append(a.mods, ModFile{Name: fmt.Sprintf("some-fairly-long-mod-name-%d.pw", i), Filename: "x.toml", Path: fmt.Sprintf("/tmp/x%d", i)})
			}
			a.filterMods()
			a.setHomeFocus(focus)
			if focus == 1 {
				a.agentInput.SetValue("please test the server for me and report back")
				a.agentEntries = []agentEntry{
					{role: "user", text: "hello"},
					{role: "agent", text: strings.Repeat("a fairly long reply line ", 20)},
				}
			}

			body := a.viewHome()
			lines := strings.Split(body, "\n")
			bodyH := a.height - 1
			if len(lines) != bodyH {
				t.Errorf("%dx%d focus=%d: body is %d lines, want %d", size[0], size[1], focus, len(lines), bodyH)
			}
			for i, l := range lines {
				if lipgloss.Width(l) > a.width {
					t.Errorf("%dx%d focus=%d: line %d overflows (%d > %d)", size[0], size[1], focus, i, lipgloss.Width(l), a.width)
				}
			}
			// The bottom border of the mods panel and the buttons must share
			// the last body line.
			last := lines[len(lines)-1]
			if !strings.Contains(last, "╰") {
				t.Errorf("%dx%d focus=%d: last line has no bottom borders: %q", size[0], size[1], focus, last)
			}
		}
	}
}
