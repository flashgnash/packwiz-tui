package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// The test harness boots a real server (and optionally a real headless
// client) from the pack to prove it works end to end. Everything lives under
// <repo>/.packwiz-tui/ and is safe to delete.
//
// All output is streamed to `progress` (an io.Writer, typically os.Stdout for
// CLI use) so an agent or human can follow along.

type TestOptions struct {
	RAMGb       int           // heap for both server and client (default 8, clamped to system RAM)
	SoakTime    time.Duration // how long the client stays connected (default 90s)
	Port        int           // server port (default 25565)
	RconPort    int           // rcon port (default 25575)
	BootTimeout time.Duration // max wait for "Done" (default 15m: first boot downloads mods)
	Username    string        // offline test account name (default packwiz-test)
}

// defaultsFor fills in defaults; jvms is how many game JVMs run at once
// (1 for a server test, 2 for the full-stack test) so the default heap can
// be clamped to what the machine actually has.
func (o *TestOptions) defaultsFor(jvms int) {
	if o.RAMGb == 0 {
		o.RAMGb = clampHeapGb(8, jvms)
	}
	if o.SoakTime == 0 {
		o.SoakTime = 90 * time.Second
	}
	if o.Port == 0 {
		o.Port = 25565
	}
	if o.RconPort == 0 {
		o.RconPort = 25575
	}
	if o.BootTimeout == 0 {
		o.BootTimeout = 15 * time.Minute
	}
	if o.Username == "" {
		o.Username = "packwiz-test"
	}
}

const bootstrapURL = "https://github.com/packwiz/packwiz-installer-bootstrap/releases/latest/download/packwiz-installer-bootstrap.jar"

// systemRAMGb returns total system memory in GB, or 0 if unknown.
func systemRAMGb() int {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				var kb int64
				fmt.Sscanf(fields[1], "%d", &kb)
				return int(kb / 1024 / 1024)
			}
		}
	}
	return 0
}

// clampHeapGb caps a desired per-JVM heap so that `jvms` JVMs plus ~4GB of
// OS/overhead headroom fit in system memory. Never goes below 2GB.
func clampHeapGb(want, jvms int) int {
	total := systemRAMGb()
	if total == 0 {
		return want
	}
	per := (total - 4) / jvms
	if per < want {
		want = per
	}
	if want < 2 {
		want = 2
	}
	return want
}

// harnessDir returns <packDir>/.packwiz-tui/<sub>, creating it and making
// sure the harness tree stays out of the pack index and git.
func harnessDir(packDir, sub string) (string, error) {
	dir := filepath.Join(packDir, ".packwiz-tui", sub)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return dir, err
	}
	ensureLineInFile(filepath.Join(packDir, ".packwizignore"), ".packwiz-tui/**")
	ensureLineInFile(filepath.Join(packDir, ".gitignore"), ".packwiz-tui/")
	return dir, nil
}

// ensureLineInFile appends line to path unless already present.
func ensureLineInFile(path, line string) {
	data, _ := os.ReadFile(path)
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) == line {
			return
		}
	}
	text := string(data)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	os.WriteFile(path, []byte(text+line+"\n"), 0644)
}

func downloadFile(url, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return nil
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("GET %s: %d", url, resp.StatusCode)
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	return os.Rename(tmp, dest)
}

// RunPackwizInstaller installs the pack's files for a side into targetDir.
func RunPackwizInstaller(targetDir, packDir, side string) (string, error) {
	bootstrap := filepath.Join(targetDir, "packwiz-installer-bootstrap.jar")
	if err := downloadFile(bootstrapURL, bootstrap); err != nil {
		return "", fmt.Errorf("fetching packwiz-installer-bootstrap: %w", err)
	}
	cmd := exec.Command("java", "-jar", bootstrap, "-g", "-s", side,
		filepath.Join(packDir, "pack.toml"))
	cmd.Dir = targetDir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// installLoaderServer installs the pack's loader server into serverDir.
func installLoaderServer(meta PackMeta, serverDir string, progress io.Writer) error {
	marker := filepath.Join(serverDir, ".loader-"+meta.Loader+"-"+meta.LoaderVer)
	if _, err := os.Stat(marker); err == nil {
		return nil
	}
	var url, jar string
	var args []string
	switch meta.Loader {
	case "neoforge":
		url = fmt.Sprintf("https://maven.neoforged.net/releases/net/neoforged/neoforge/%s/neoforge-%s-installer.jar", meta.LoaderVer, meta.LoaderVer)
		jar = "loader-installer.jar"
		args = []string{"-jar", jar, "--install-server", "."}
	case "forge":
		mcLoader := meta.Minecraft + "-" + meta.LoaderVer
		url = fmt.Sprintf("https://maven.minecraftforge.net/net/minecraftforge/forge/%s/forge-%s-installer.jar", mcLoader, mcLoader)
		jar = "loader-installer.jar"
		args = []string{"-jar", jar, "--installServer"}
	case "fabric", "quilt":
		url = "https://maven.fabricmc.net/net/fabricmc/fabric-installer/1.1.1/fabric-installer-1.1.1.jar"
		jar = "loader-installer.jar"
		args = []string{"-jar", jar, "server", "-mcversion", meta.Minecraft, "-loader", meta.LoaderVer, "-downloadMinecraft"}
	default:
		return fmt.Errorf("unsupported loader %q", meta.Loader)
	}
	fmt.Fprintf(progress, "installing %s %s server…\n", meta.Loader, meta.LoaderVer)
	if err := downloadFile(url, filepath.Join(serverDir, jar)); err != nil {
		return err
	}
	// Loader installers hit Mojang/maven servers that reset connections now
	// and then — retry a couple of times before giving up.
	var lastOut string
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			fmt.Fprintf(progress, "install attempt %d/3…\n", attempt)
			time.Sleep(5 * time.Second)
		}
		cmd := exec.Command("java", args...)
		cmd.Dir = serverDir
		out, err := cmd.CombinedOutput()
		if err == nil {
			return os.WriteFile(marker, nil, 0644)
		}
		lastOut, lastErr = string(out), err
	}
	return fmt.Errorf("loader install failed: %v\n%s", lastErr, tail(lastOut, 30))
}

// serverLaunchArgs returns the java arguments to boot the installed server.
func serverLaunchArgs(meta PackMeta, serverDir string, ramGb int) ([]string, error) {
	heap := []string{fmt.Sprintf("-Xmx%dG", ramGb), fmt.Sprintf("-Xms%dG", min(ramGb, 2))}
	switch meta.Loader {
	case "neoforge":
		argsFile := filepath.Join(serverDir, "libraries", "net", "neoforged", "neoforge", meta.LoaderVer, "unix_args.txt")
		if _, err := os.Stat(argsFile); err != nil {
			return nil, fmt.Errorf("neoforge args file missing: %s", argsFile)
		}
		return append(heap, "@"+argsFile, "nogui"), nil
	case "forge":
		argsFile := filepath.Join(serverDir, "libraries", "net", "minecraftforge", "forge", meta.Minecraft+"-"+meta.LoaderVer, "unix_args.txt")
		if _, err := os.Stat(argsFile); err == nil {
			return append(heap, "@"+argsFile, "nogui"), nil
		}
		return append(heap, "-jar", fmt.Sprintf("forge-%s-%s-universal.jar", meta.Minecraft, meta.LoaderVer), "nogui"), nil
	case "fabric", "quilt":
		return append(heap, "-jar", "fabric-server-launch.jar", "nogui"), nil
	}
	return nil, fmt.Errorf("unsupported loader %q", meta.Loader)
}

// writeServerConfig accepts the EULA and configures an offline test server
// with RCON enabled. Returns the generated rcon password.
func writeServerConfig(serverDir string, opts TestOptions) (string, error) {
	if err := os.WriteFile(filepath.Join(serverDir, "eula.txt"), []byte("eula=true\n"), 0644); err != nil {
		return "", err
	}
	buf := make([]byte, 12)
	rand.Read(buf)
	pass := hex.EncodeToString(buf)
	props := fmt.Sprintf(`online-mode=false
server-port=%d
enable-rcon=true
rcon.port=%d
rcon.password=%s
motd=packwiz-tui test server
`, opts.Port, opts.RconPort, pass)
	return pass, os.WriteFile(filepath.Join(serverDir, "server.properties"), []byte(props), 0644)
}

var (
	reDone  = regexp.MustCompile(`Done \([0-9.]+s\)!`)
	reFatal = regexp.MustCompile(`Exception in server tick loop|Minecraft Crash Report|has failed to load correctly|Failed to start the minecraft server`)
)

// TestServer installs and boots the pack's server, waits for it to come up,
// samples TPS over RCON, then shuts it down. Artifacts land in
// <packDir>/.packwiz-tui/test-server/.
func TestServer(packDir string, opts TestOptions, progress io.Writer) error {
	opts.defaultsFor(1)
	meta, err := ParsePackMeta(packDir)
	if err != nil {
		return err
	}
	fmt.Fprintf(progress, "pack: %s (mc %s, %s %s)\n", meta.Name, meta.Minecraft, meta.Loader, meta.LoaderVer)

	serverDir, err := harnessDir(packDir, "test-server")
	if err != nil {
		return err
	}
	if err := installLoaderServer(meta, serverDir, progress); err != nil {
		return err
	}

	fmt.Fprintln(progress, "installing pack (server side)…")
	if out, err := RunPackwizInstaller(serverDir, packDir, "server"); err != nil {
		return fmt.Errorf("packwiz-installer failed:\n%s", tail(out, 40))
	}

	pass, err := writeServerConfig(serverDir, opts)
	if err != nil {
		return err
	}

	proc, logPath, err := startServer(meta, serverDir, opts, progress)
	if err != nil {
		return err
	}
	defer stopServer(proc, opts, pass, progress)

	if err := waitForDone(logPath, opts.BootTimeout, progress); err != nil {
		return err
	}

	rcon, err := RconDial(fmt.Sprintf("127.0.0.1:%d", opts.RconPort), pass)
	if err != nil {
		return fmt.Errorf("rcon connect: %w", err)
	}
	defer rcon.Close()
	sampleMetrics(rcon, meta, progress)

	fmt.Fprintln(progress, "server test PASSED")
	return nil
}

func startServer(meta PackMeta, serverDir string, opts TestOptions, progress io.Writer) (*exec.Cmd, string, error) {
	for _, p := range []int{opts.Port, opts.RconPort} {
		if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", p), time.Second); err == nil {
			c.Close()
			return nil, "", fmt.Errorf("port %d is already in use — is another test server still running?", p)
		}
	}
	args, err := serverLaunchArgs(meta, serverDir, opts.RAMGb)
	if err != nil {
		return nil, "", err
	}
	logPath := filepath.Join(serverDir, "harness-console.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, "", err
	}
	cmd := exec.Command("java", args...)
	cmd.Dir = serverDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	fmt.Fprintln(progress, "booting server…")
	if err := cmd.Start(); err != nil {
		return nil, "", err
	}
	deprioritize(cmd)
	return cmd, logPath, nil
}

// deprioritize drops a test process group to idle CPU/IO priority so the
// harness never competes with whatever else the machine is doing (a desktop
// running a game, a production server, …).
func deprioritize(cmd *exec.Cmd) {
	pgid := cmd.Process.Pid
	syscall.Setpriority(syscall.PRIO_PGRP, pgid, 19)
	// Best-effort idle IO class; ignore failure (needs ionice semantics via
	// syscall 251 ioprio_set: IOPRIO_CLASS_IDLE=3 << 13, IOPRIO_WHO_PGRP=2).
	syscall.Syscall(syscall.SYS_IOPRIO_SET, 2, uintptr(pgid), 3<<13)
}

func stopServer(proc *exec.Cmd, opts TestOptions, pass string, progress io.Writer) {
	if rcon, err := RconDial(fmt.Sprintf("127.0.0.1:%d", opts.RconPort), pass); err == nil {
		fmt.Fprintln(progress, "stopping server (rcon)…")
		rcon.Command("stop")
		rcon.Close()
	} else {
		proc.Process.Signal(syscall.SIGTERM)
	}
	done := make(chan struct{})
	go func() { proc.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(90 * time.Second):
		fmt.Fprintln(progress, "server did not stop in time, killing")
		syscall.Kill(-proc.Process.Pid, syscall.SIGKILL)
		<-done
	}
}

// waitForDone tails a log file until the Done marker, a fatal error, or timeout.
func waitForDone(logPath string, timeout time.Duration, progress io.Writer) error {
	deadline := time.Now().Add(timeout)
	var offset int64
	for time.Now().Before(deadline) {
		f, err := os.Open(logPath)
		if err == nil {
			f.Seek(offset, 0)
			data, _ := io.ReadAll(f)
			end, _ := f.Seek(0, io.SeekCurrent)
			f.Close()
			offset = end
			text := string(data)
			if reFatal.MatchString(text) {
				return fmt.Errorf("server crashed during boot — see %s", logPath)
			}
			if reDone.MatchString(text) {
				fmt.Fprintf(progress, "server up: %s\n", reDone.FindString(text))
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("server did not reach Done within %s — see %s", timeout, logPath)
}

// sampleMetrics collects TPS/health over rcon, trying loader-appropriate
// commands and degrading gracefully.
func sampleMetrics(rcon *RconClient, meta PackMeta, progress io.Writer) {
	cmds := []string{"spark tps"}
	switch meta.Loader {
	case "neoforge":
		cmds = append(cmds, "neoforge tps")
	case "forge":
		cmds = append(cmds, "forge tps")
	default:
		cmds = append(cmds, "tick query")
	}
	cmds = append(cmds, "list")
	for _, c := range cmds {
		resp, err := rcon.Command(c)
		if err != nil || strings.Contains(resp, "Unknown or incomplete command") {
			continue
		}
		fmt.Fprintf(progress, "[%s] %s\n", c, condenseMinecraftText(resp))
	}
}

// condenseMinecraftText strips §-style formatting codes and squeezes whitespace.
func condenseMinecraftText(s string) string {
	s = regexp.MustCompile(`§.`).ReplaceAllString(s, "")
	return strings.Join(strings.Fields(s), " ")
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

