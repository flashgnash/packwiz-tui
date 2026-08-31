package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// TestFull runs the whole stack: boots the server, then launches a real
// client headless (gamescope + portablemc, offline account), auto-joins the
// server, keeps it connected for the soak period while sampling TPS, and
// captures screenshots for visual inspection. Artifacts (screenshots,
// metrics, logs) land in <packDir>/.packwiz-tui/last-test/.
func TestFull(packDir string, opts TestOptions, progress io.Writer) error {
	opts.defaultsFor(2)
	meta, err := ParsePackMeta(packDir)
	if err != nil {
		return err
	}
	fmt.Fprintf(progress, "pack: %s (mc %s, %s %s)\n", meta.Name, meta.Minecraft, meta.Loader, meta.LoaderVer)

	for _, bin := range []string{"java", "gamescope", "portablemc"} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("%s not found in PATH (required for the full-stack test)", bin)
		}
	}

	reportDir, err := harnessDir(packDir, "last-test")
	if err != nil {
		return err
	}

	// ── Server ──
	serverDir, err := harnessDir(packDir, "test-server")
	if err != nil {
		return err
	}
	if err := installLoaderServer(meta, serverDir, progress); err != nil {
		return err
	}
	fmt.Fprintln(progress, "installing pack (server side)…")
	if out, err := RunPackwizInstaller(serverDir, packDir, "server"); err != nil {
		return fmt.Errorf("packwiz-installer (server) failed:\n%s", tail(out, 40))
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

	// ── Client ──
	clientDir, err := harnessDir(packDir, "test-client")
	if err != nil {
		return err
	}
	fmt.Fprintln(progress, "installing pack (client side)…")
	if out, err := RunPackwizInstaller(clientDir, packDir, "client"); err != nil {
		return fmt.Errorf("packwiz-installer (client) failed:\n%s", tail(out, 40))
	}
	// Suppress vanilla's first-run accessibility prompt, which blocks quick play.
	ensureOptionsLine(filepath.Join(clientDir, "options.txt"), "onboardAccessibility", "false")

	client, clientDone, gsSocket, err := startClient(meta, packDir, clientDir, opts, progress)
	if err != nil {
		return err
	}
	defer stopClient(client, clientDone, progress)

	// ── Join + soak ──
	rcon, err := RconDial(fmt.Sprintf("127.0.0.1:%d", opts.RconPort), pass)
	if err != nil {
		return fmt.Errorf("rcon connect: %w", err)
	}
	defer rcon.Close()

	if err := waitForJoin(rcon, opts.Username, opts.BootTimeout, clientDone, clientDir, progress); err != nil {
		return err
	}
	// Spectator so the idle test player can't die mid-soak.
	rcon.Command("gamemode spectator " + opts.Username)

	fmt.Fprintf(progress, "soaking for %s…\n", opts.SoakTime)
	interval := opts.SoakTime / 3
	for i := 1; i <= 3; i++ {
		time.Sleep(interval)
		fmt.Fprintf(progress, "── sample %d/3 ──\n", i)
		sampleMetrics(rcon, meta, progress)
		shot := filepath.Join(reportDir, fmt.Sprintf("soak-%d.png", i))
		if err := gamescopeScreenshot(gsSocket, shot, progress); err != nil {
			fmt.Fprintf(progress, "screenshot failed: %v\n", err)
		}
		if !playerOnline(rcon, opts.Username) {
			return fmt.Errorf("client disconnected during soak — check %s", filepath.Join(clientDir, "logs", "latest.log"))
		}
	}

	fmt.Fprintf(progress, "full-stack test PASSED — screenshots in %s\n", reportDir)
	return nil
}

// startClient launches the pack's client inside a headless gamescope, joining
// the local server via quick play. Returns the process, a channel closed when
// it exits, and the gamescope wayland socket name (for gamescopectl).
func startClient(meta PackMeta, packDir, clientDir string, opts TestOptions, progress io.Writer) (*exec.Cmd, chan error, string, error) {
	mainDir := filepath.Join(packDir, ".packwiz-tui", "pmc")
	pmcArgs := []string{
		"--main-dir", mainDir,
		"start", meta.PortablemcVersion(),
		"--mc-dir", clientDir,
		fmt.Sprintf("--jvm-arg=-Xmx%dG,-Xms2G", opts.RAMGb),
		"--join-server", "127.0.0.1", "--join-server-port", fmt.Sprint(opts.Port),
		"-u", opts.Username,
	}
	args := append([]string{"--backend", "headless", "-W", "1920", "-H", "1080", "--", "portablemc"}, pmcArgs...)

	logFile, err := os.Create(filepath.Join(clientDir, "harness-client.log"))
	if err != nil {
		return nil, nil, "", err
	}
	cmd := exec.Command("gamescope", args...)
	cmd.Dir = clientDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	fmt.Fprintln(progress, "launching headless client (first run downloads game files)…")
	if err := cmd.Start(); err != nil {
		return nil, nil, "", err
	}
	deprioritize(cmd)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	socket, err := findGamescopeSocket(done)
	if err != nil {
		fmt.Fprintf(progress, "note: %v (screenshots disabled)\n", err)
	}
	return cmd, done, socket, nil
}

func stopClient(cmd *exec.Cmd, done chan error, progress io.Writer) {
	select {
	case <-done: // already exited
		return
	default:
	}
	fmt.Fprintln(progress, "stopping client…")
	syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
	}
}

var reGamescopeSocket = regexp.MustCompile(`^gamescope-\d+$`)

// findGamescopeSocket waits for the newest gamescope wayland socket to
// appear, bailing out early if the client dies first.
func findGamescopeSocket(clientDone chan error) (string, error) {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		return "", fmt.Errorf("XDG_RUNTIME_DIR not set")
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-clientDone:
			return "", fmt.Errorf("client exited before gamescope came up")
		default:
		}
		matches, _ := filepath.Glob(filepath.Join(runtimeDir, "gamescope-*"))
		var newest string
		var newestT time.Time
		for _, m := range matches {
			if !reGamescopeSocket.MatchString(filepath.Base(m)) || !isSocket(m) {
				continue
			}
			if info, err := os.Stat(m); err == nil && info.ModTime().After(newestT) {
				newest, newestT = m, info.ModTime()
			}
		}
		if newest != "" {
			return filepath.Base(newest), nil
		}
		time.Sleep(time.Second)
	}
	return "", fmt.Errorf("no gamescope socket appeared in %s", runtimeDir)
}

func isSocket(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode()&os.ModeSocket != 0
}

func gamescopeScreenshot(socket, dest string, progress io.Writer) error {
	if socket == "" {
		return fmt.Errorf("no gamescope socket")
	}
	cmd := exec.Command("gamescopectl", "screenshot", dest)
	cmd.Env = append(os.Environ(), "GAMESCOPE_WAYLAND_DISPLAY="+socket)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, tail(string(out), 3))
	}
	// gamescope writes the file asynchronously; give it a moment.
	for i := 0; i < 20; i++ {
		if info, err := os.Stat(dest); err == nil && info.Size() > 0 {
			fmt.Fprintf(progress, "screenshot: %s\n", dest)
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("screenshot file never appeared: %s", dest)
}

// waitForJoin polls the player list until the test account appears. Fails
// early if the client process dies.
func waitForJoin(rcon *RconClient, username string, timeout time.Duration, clientDone chan error, clientDir string, progress io.Writer) error {
	fmt.Fprintln(progress, "waiting for client to join…")
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-clientDone:
			return fmt.Errorf("client exited before joining — see %s and %s",
				filepath.Join(clientDir, "harness-client.log"),
				filepath.Join(clientDir, "logs", "latest.log"))
		default:
		}
		if playerOnline(rcon, username) {
			fmt.Fprintf(progress, "%s joined the server\n", username)
			return nil
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("client did not join within %s", timeout)
}

func playerOnline(rcon *RconClient, username string) bool {
	resp, err := rcon.Command("list")
	return err == nil && strings.Contains(resp, username)
}

// ensureOptionsLine sets key:value in a Minecraft options.txt, creating the
// file if needed.
func ensureOptionsLine(path, key, value string) {
	data, _ := os.ReadFile(path)
	var lines []string
	found := false
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if k, _, ok := strings.Cut(line, ":"); ok && k == key {
			line = key + ":" + value
			found = true
		}
		if line != "" || found {
			lines = append(lines, line)
		}
	}
	if !found {
		lines = append(lines, key+":"+value)
	}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}
