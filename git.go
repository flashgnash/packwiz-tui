package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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
				Path:     filepath.Join(modsDir, e.Name()),
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

// CloneRepo clones url into targetDir, returning combined output.
func CloneRepo(url, targetDir string) (string, error) {
	cmd := exec.Command("git", "clone", url, targetDir)
	out, err := cmd.CombinedOutput()
	return string(out), err
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
