package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// launchPrismCmd builds a command that launches the named instance in the
// local PrismLauncher (native binary or flatpak), or nil if Prism isn't
// available.
func launchPrismCmd(instName string) *exec.Cmd {
	if _, err := exec.LookPath("prismlauncher"); err == nil {
		return exec.Command("prismlauncher", "--launch", instName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	if _, err := os.Stat(filepath.Join(home, ".var", "app", "org.prismlauncher.PrismLauncher")); err == nil {
		if _, err := exec.LookPath("flatpak"); err == nil {
			return exec.Command("flatpak", "run", "org.prismlauncher.PrismLauncher", "--launch", instName)
		}
	}
	return nil
}

// prismInstanceExists reports whether the pack's instance is installed in the
// local PrismLauncher.
func prismInstanceExists(instName string) bool {
	instancesDir, err := findPrismInstancesDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(instancesDir, instName))
	return err == nil
}

// clientDirPatterns are the process cmdline substrings that identify this
// pack's running client (Prism instance dir, or the portablemc mc-dir).
func clientDirPatterns(packDir string, meta PackMeta) []string {
	patterns := []string{filepath.Join(packDir, ".packwiz-tui", "client")}
	if dir, err := findPrismInstancesDir(); err == nil {
		patterns = append(patterns, filepath.Join(dir, artifactBase(packDir, meta)))
	}
	return patterns
}

// clientProcRunning reports whether the pack's client appears to be running.
func clientProcRunning(packDir string, meta PackMeta) bool {
	for _, p := range clientDirPatterns(packDir, meta) {
		if exec.Command("pgrep", "-f", p).Run() == nil {
			return true
		}
	}
	return false
}

// stopClientProcs terminates any running client of this pack.
func stopClientProcs(packDir string, meta PackMeta) {
	for _, p := range clientDirPatterns(packDir, meta) {
		exec.Command("pkill", "-TERM", "-f", p).Run()
	}
}

// LaunchClient installs the pack client-side and starts it via portablemc —
// the same launcher the test harness uses, but windowed and attached to the
// user's session. Fallback for machines without PrismLauncher.
func LaunchClient(packDir string, progress io.Writer) error {
	meta, err := ParsePackMeta(packDir)
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("portablemc"); err != nil {
		return fmt.Errorf("neither PrismLauncher nor portablemc found — install one to launch the client")
	}
	clientDir, err := harnessDir(packDir, "client")
	if err != nil {
		return err
	}
	fmt.Fprintln(progress, "installing pack (client side)…")
	if out, err := RunPackwizInstaller(clientDir, packDir, "client"); err != nil {
		return fmt.Errorf("packwiz-installer failed:\n%s", tail(out, 40))
	}
	mainDir := filepath.Join(packDir, ".packwiz-tui", "pmc")
	fmt.Fprintf(progress, "launching %s via portablemc…\n", meta.PortablemcVersion())
	c := exec.Command("portablemc",
		"--main-dir", mainDir,
		"start", meta.PortablemcVersion(),
		"--mc-dir", clientDir)
	c.Dir = clientDir
	c.Stdout = progress
	c.Stderr = progress
	return c.Run()
}
