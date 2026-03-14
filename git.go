package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/creack/pty"
)

const recentReposFile = ".packwiz-tui-recents.json"

// RepoEntry stores metadata about a known repo.
type RepoEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Remote   string `json:"remote"`
	LastUsed string `json:"last_used"`
}

// ModFile represents a mod toml file in the mods directory.
type ModFile struct {
	Name     string
	Filename string
	Path     string
}

// DetectGitRepo returns the root of the git repo containing cwd, or an error.
func DetectGitRepo() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// GetGitRemote returns the origin remote URL for the repo at repoPath.
func GetGitRemote(repoPath string) string {
	cmd := exec.Command("git", "-C", repoPath, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// GetRepoName derives a friendly display name from a remote URL or path.
func GetRepoName(remote, path string) string {
	if remote != "" {
		r := strings.TrimSuffix(remote, ".git")
		parts := strings.Split(r, "/")
		if len(parts) >= 2 {
			return parts[len(parts)-2] + "/" + parts[len(parts)-1]
		}
		return parts[len(parts)-1]
	}
	return filepath.Base(path)
}

// LoadRecentRepos reads the recents list from ~/.packwiz-tui-recents.json.
func LoadRecentRepos() []RepoEntry {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(home, recentReposFile))
	if err != nil {
		return nil
	}
	var repos []RepoEntry
	if err := json.Unmarshal(data, &repos); err != nil {
		return nil
	}
	return repos
}

// SaveRecentRepos writes the recents list to disk.
func SaveRecentRepos(repos []RepoEntry) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	data, err := json.MarshalIndent(repos, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(home, recentReposFile), data, 0644)
}

// AddRecentRepo upserts an entry into the recents list (max 20).
func AddRecentRepo(entry RepoEntry) {
	repos := LoadRecentRepos()
	filtered := repos[:0]
	for _, r := range repos {
		if r.Path != entry.Path {
			filtered = append(filtered, r)
		}
	}
	updated := append([]RepoEntry{entry}, filtered...)
	if len(updated) > 20 {
		updated = updated[:20]
	}
	SaveRecentRepos(updated)
}

// FindPackToml walks root looking for the first pack.toml.
func FindPackToml(root string) (string, error) {
	var found string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && (info.Name() == ".git" || info.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if !info.IsDir() && info.Name() == "pack.toml" {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if found == "" {
		return "", os.ErrNotExist
	}
	return found, nil
}

// ListModFiles returns all .toml files in <packDir>/mods/.
func ListModFiles(packDir string) ([]ModFile, error) {
	modsDir := filepath.Join(packDir, "mods")
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return nil, err
	}
	var mods []ModFile
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".toml") {
			mods = append(mods, ModFile{
				Name:     strings.TrimSuffix(e.Name(), ".toml"),
				Filename: e.Name(),
				Path:     filepath.Clean(filepath.Join(modsDir, e.Name())),
			})
		}
	}
	return mods, nil
}

// RunPackwiz runs packwiz with args in packDir, returning combined output.
func RunPackwiz(packDir string, args ...string) (string, error) {
	cmd := exec.Command("packwiz", args...)
	cmd.Dir = packDir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// InteractivePrompt represents a detected interactive prompt from packwiz.
type InteractivePrompt struct {
	Prompt  string
	Options []string
	Output  string
}

// RunPackwizInteractive runs packwiz and detects if it's asking for interactive input.
// Returns (output, prompt, error). If prompt is non-nil, user input is needed.
func RunPackwizInteractive(packDir string, args ...string) (string, *InteractivePrompt, error) {
	cmd := exec.Command("packwiz", args...)
	cmd.Dir = packDir

	// Capture combined output
	output := &strings.Builder{}
	cmd.Stdout = output
	cmd.Stderr = output

	// Start the command
	if err := cmd.Start(); err != nil {
		return "", nil, err
	}

	// Wait for command with timeout
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// Give command time to complete
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()

	select {
	case err := <-done:
		// Command finished
		outputStr := output.String()
		prompt := detectInteractivePrompt(outputStr)
		if prompt != nil {
			return outputStr, prompt, nil
		}
		return strings.TrimSpace(outputStr), nil, err

	case <-timer.C:
		// Timeout - check if it's waiting for input
		outputStr := output.String()
		prompt := detectInteractivePrompt(outputStr)
		if prompt != nil {
			// Interactive prompt detected
			cmd.Process.Kill()
			return outputStr, prompt, nil
		}
		// Not a prompt, just a slow command - keep waiting
		err := <-done
		return strings.TrimSpace(output.String()), nil, err
	}
}

// RunPackwizWithInput runs packwiz with the given input string using a pseudo-terminal.
// This allows interaction with programs that read from /dev/tty instead of stdin.
// Returns (output, prompt, error). If prompt is non-nil, additional input is needed.
func RunPackwizWithInput(packDir, input string, args ...string) (string, *InteractivePrompt, error) {
	cmd := exec.Command("packwiz", args...)
	cmd.Dir = packDir

	// Start the command with a pty
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return "", nil, err
	}
	defer ptmx.Close()

	// Send the input to the pty immediately
	go func() {
		inputLines := strings.Split(input, "\n")
		for _, line := range inputLines {
			if line != "" {
				ptmx.Write([]byte(line + "\n"))
				time.Sleep(50 * time.Millisecond) // Small delay between lines only
			}
		}
	}()

	// Check if we've already sent a y/n response (to avoid re-detecting the prompt)
	lowerInput := strings.ToLower(input)
	alreadyAnswered := strings.Contains(lowerInput, "\ny") ||
	                   strings.Contains(lowerInput, "\nn") ||
	                   strings.HasSuffix(lowerInput, "y") ||
	                   strings.HasSuffix(lowerInput, "n")

	// Read output in background
	var outputLines []string
	outputChan := make(chan string, 100)
	go func() {
		reader := make([]byte, 1024)
		for {
			n, err := ptmx.Read(reader)
			if err != nil {
				close(outputChan)
				return
			}
			if n > 0 {
				outputChan <- string(reader[:n])
			}
		}
	}()

	// Wait for command completion in background
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// Process output as it arrives
	for {
		select {
		case chunk, ok := <-outputChan:
			if !ok {
				// Output channel closed, command finished
				outputStr := strings.Join(outputLines, "")
				return strings.TrimSpace(outputStr), nil, nil
			}

			outputLines = append(outputLines, chunk)
			currentOutput := strings.Join(outputLines, "")
			lowerOutput := strings.ToLower(currentOutput)

			// Check for y/n prompt immediately (but only if we haven't already answered)
			if !alreadyAnswered && (strings.Contains(lowerOutput, "[y/n]") ||
			   strings.Contains(lowerOutput, "(y/n)")) {

				// Kill the process since we found the prompt
				cmd.Process.Kill()

				// Extract the prompt with context
				lines := strings.Split(strings.TrimSpace(currentOutput), "\n")
				var promptLines []string

				foundPrompt := false
				for i := len(lines) - 1; i >= 0 && len(promptLines) < 15; i-- {
					line := strings.TrimSpace(lines[i])
					if line == "" {
						continue
					}

					lowerLine := strings.ToLower(line)
					if strings.Contains(lowerLine, "y/n") && !foundPrompt {
						promptLines = append([]string{line}, promptLines...)
						foundPrompt = true
					} else if foundPrompt {
						// Add context lines before the prompt
						promptLines = append([]string{line}, promptLines...)
						if strings.Contains(lowerLine, "dependencies found") {
							break
						}
					}
				}

				return currentOutput, &InteractivePrompt{
					Prompt:  strings.Join(promptLines, "\n"),
					Options: []string{"Yes", "No"},
					Output:  currentOutput,
				}, nil
			}

		case <-done:
			// Command finished
			time.Sleep(50 * time.Millisecond) // Give output channel time to drain
			outputStr := strings.Join(outputLines, "")
			return strings.TrimSpace(outputStr), nil, nil
		}
	}
}

// detectInteractivePrompt parses packwiz output to detect interactive prompts.
func detectInteractivePrompt(output string) *InteractivePrompt {
	lines := strings.Split(output, "\n")

	// Look for numbered list patterns that indicate multiple choices
	// Common patterns:
	// [1] option1
	// [2] option2
	// Or:
	// 1. option1
	// 2. option2
	var options []string
	var promptLine string
	inList := false
	listStartIdx := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		// Check for patterns indicating multiple matches/options
		if strings.Contains(lower, "multiple") && (strings.Contains(lower, "found") || strings.Contains(lower, "match")) {
			promptLine = trimmed
			inList = true
			listStartIdx = i
			continue
		}

		// Check if this is a numbered option: [1], [2], etc. or 1., 2., etc. or 0), 1), 2), etc.
		if inList || (listStartIdx == -1 && (strings.HasPrefix(trimmed, "[1]") || strings.HasPrefix(trimmed, "1.") || strings.HasPrefix(trimmed, "0)") || strings.HasPrefix(trimmed, "1)"))) {
			var option string
			matched := false

			// Pattern: [N] option or [N]: option
			if strings.HasPrefix(trimmed, "[") {
				if idx := strings.Index(trimmed, "]"); idx > 0 && idx < len(trimmed)-1 {
					option = strings.TrimSpace(trimmed[idx+1:])
					option = strings.TrimPrefix(option, ":")
					option = strings.TrimSpace(option)
					matched = true
				}
			} else if len(trimmed) > 2 && trimmed[0] >= '0' && trimmed[0] <= '9' && (trimmed[1] == '.' || trimmed[1] == ')') {
				// Pattern: N. option or N) option (including 0)
				option = strings.TrimSpace(trimmed[2:])
				option = strings.TrimPrefix(option, "*") // Remove * prefix that marks default
				option = strings.TrimSpace(option)
				matched = true
			}

			if matched && option != "" {
				options = append(options, option)
				inList = true
				if listStartIdx == -1 {
					listStartIdx = i
				}
				continue
			}
		}

		// Check if we've hit a selection prompt
		if inList && len(options) > 0 {
			if strings.Contains(lower, "select") || strings.Contains(lower, "choose") ||
				strings.Contains(lower, "which") || strings.Contains(trimmed, "[1-") ||
				strings.Contains(lower, "enter") && (strings.Contains(lower, "number") || strings.Contains(lower, "choice")) {
				if promptLine == "" {
					promptLine = trimmed
				} else {
					promptLine += " " + trimmed
				}
				break
			}
		}

		// Stop collecting if we've moved past the list
		if inList && len(options) > 0 && trimmed != "" && !strings.HasPrefix(trimmed, "[") &&
			!(len(trimmed) > 0 && trimmed[0] >= '1' && trimmed[0] <= '9') {
			// Might be the prompt
			if strings.Contains(lower, "select") || strings.Contains(lower, "choose") {
				promptLine = trimmed
				break
			}
		}
	}

	// If we found options but no explicit prompt, create a default one
	if len(options) > 0 {
		if promptLine == "" {
			promptLine = fmt.Sprintf("Select an option [1-%d]:", len(options))
		}
		return &InteractivePrompt{
			Prompt:  promptLine,
			Options: options,
			Output:  output,
		}
	}

	// Fallback: if we see common patterns like "Multiple" + "found/matches"
	// even without numbered options, try to parse it
	if strings.Contains(strings.ToLower(output), "multiple") &&
		(strings.Contains(strings.ToLower(output), "found") ||
			strings.Contains(strings.ToLower(output), "match")) {
		// Create a basic prompt to let user know something needs selection
		return &InteractivePrompt{
			Prompt:  "Multiple matches found. Please re-run with more specific search.",
			Options: []string{"OK"},
			Output:  output,
		}
	}

	return nil
}

// CloneRepo clones url into targetDir, returning combined output.
func CloneRepo(url, targetDir string) (string, error) {
	cmd := exec.Command("git", "clone", url, targetDir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// GitCheckoutFile restores a deleted file using git checkout.
func GitCheckoutFile(repoRoot, filePath string) error {
	cmd := exec.Command("git", "checkout", "--", filePath)
	cmd.Dir = repoRoot
	return cmd.Run()
}

// GetGitStatus returns maps of file paths by their git status.
// Returns: modified (M), added (A), deleted (D)
func GetGitStatus(repoRoot string) (modified map[string]bool, added map[string]bool, deleted map[string]bool, err error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, nil, err
	}

	modified = make(map[string]bool)
	added = make(map[string]bool)
	deleted = make(map[string]bool)
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}
		// Format: "XY filename" where X is staged, Y is unstaged
		status := line[0:2]
		filename := strings.TrimSpace(line[3:])
		if filename == "" {
			continue
		}

		// Convert to absolute path and normalize
		absPath := filepath.Clean(filepath.Join(repoRoot, filename))

		// Check if file is added (A in first or second position)
		if status[0] == 'A' || status[1] == 'A' {
			added[absPath] = true
		} else if status[0] == 'M' || status[1] == 'M' {
			// Modified but not added
			modified[absPath] = true
		}

		// Check if file is deleted (D in first or second position)
		if status[0] == 'D' || status[1] == 'D' {
			deleted[absPath] = true
		}
	}
	return modified, added, deleted, nil
}

// GitPushAll stages everything, commits, and pushes from repoRoot.
func GitPushAll(repoRoot string) (string, error) {
	run := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	out1, err := run("add", ".")
	if err != nil {
		return out1, err
	}
	msg := "chore: update modpack via packwiz-tui [" + time.Now().Format("2006-01-02 15:04") + "]"
	out2, _ := run("commit", "-m", msg) // ignore "nothing to commit"
	out3, err := run("push")
	return out1 + out2 + out3, err
}
