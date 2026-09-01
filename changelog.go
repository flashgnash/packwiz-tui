package main

import (
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

// Changelog builds a markdown changelog between two git refs: a deterministic
// mod added/removed/updated section diffed from mods/*.toml, plus an
// LLM-written summary of everything else (configs, scripts, pack metadata).
// When no LLM is available the second section falls back to a changed-file
// list, so CI without an API key still gets a useful changelog.
func Changelog(packDir, from, to string, progress io.Writer) (string, error) {
	root, err := DetectGitRepoFrom(packDir)
	if err != nil {
		return "", fmt.Errorf("pack is not in a git repo: %w", err)
	}
	if to == "" {
		to = "HEAD"
	}
	if from == "" {
		if from = previousTag(root, to); from == "" {
			return "", fmt.Errorf("no previous tag found — pass --from explicitly")
		}
		fmt.Fprintf(progress, "changelog: %s..%s\n", from, to)
	}

	rel, err := filepath.Rel(root, packDir)
	if err != nil {
		rel = "."
	}
	modsPath := filepath.ToSlash(filepath.Join(rel, "mods"))
	indexPath := filepath.ToSlash(filepath.Join(rel, "index.toml"))

	var b strings.Builder

	// ── Mods (deterministic, from the mods/*.toml diff) ──
	added, removed, updated, err := diffMods(root, from, to, modsPath)
	if err != nil {
		return "", err
	}
	if len(added)+len(removed)+len(updated) > 0 {
		b.WriteString("## Mods\n\n")
		writeModSection(&b, "Added", added)
		writeModSection(&b, "Removed", removed)
		writeModSection(&b, "Updated", updated)
	}

	// ── Everything else (LLM-described, with a file-list fallback) ──
	otherDiff, _ := gitOut(root, "diff", from+".."+to, "--",
		".", ":(exclude)"+modsPath, ":(exclude)"+indexPath)
	if strings.TrimSpace(otherDiff) != "" {
		b.WriteString("## Other changes\n\n")
		if desc, err := describeDiffLLM(packDir, otherDiff); err == nil {
			b.WriteString(strings.TrimSpace(desc) + "\n")
		} else {
			fmt.Fprintf(progress, "changelog: LLM summary unavailable (%v), listing files\n", err)
			files, _ := gitOut(root, "diff", "--name-only", from+".."+to, "--",
				".", ":(exclude)"+modsPath, ":(exclude)"+indexPath)
			for _, f := range strings.Split(strings.TrimSpace(files), "\n") {
				if f != "" {
					b.WriteString("- `" + f + "`\n")
				}
			}
		}
	}

	if b.Len() == 0 {
		return "No changes.\n", nil
	}
	return b.String(), nil
}

// diffMods classifies mods/*.toml changes between two refs, resolving each
// file to its display name at the relevant ref.
func diffMods(root, from, to, modsPath string) (added, removed, updated []string, err error) {
	out, err := gitOut(root, "diff", "--name-status", "-M", from+".."+to, "--", modsPath)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasSuffix(fields[len(fields)-1], ".toml") {
			continue
		}
		switch {
		case fields[0] == "A":
			added = append(added, modNameAt(root, to, fields[1]))
		case fields[0] == "D":
			removed = append(removed, modNameAt(root, from, fields[1]))
		case fields[0] == "M":
			oldFile := modFieldAt(root, from, fields[1], "filename")
			newFile := modFieldAt(root, to, fields[1], "filename")
			name := modNameAt(root, to, fields[1])
			if oldFile != newFile && oldFile != "" && newFile != "" {
				updated = append(updated, fmt.Sprintf("%s: `%s` → `%s`", name, oldFile, newFile))
			} else {
				updated = append(updated, name)
			}
		case strings.HasPrefix(fields[0], "R") && len(fields) >= 3:
			updated = append(updated, modNameAt(root, to, fields[2]))
		}
	}
	return added, removed, updated, nil
}

func writeModSection(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "### %s\n", title)
	for _, it := range items {
		fmt.Fprintf(b, "- %s\n", it)
	}
	b.WriteString("\n")
}

// modNameAt reads a mod toml's display name at a given ref, falling back to
// the file's base name.
func modNameAt(root, ref, path string) string {
	if name := modFieldAt(root, ref, path, "name"); name != "" {
		return name
	}
	return strings.TrimSuffix(filepath.Base(path), ".toml")
}

// modFieldAt extracts a top-level `key = "value"` field from a mod toml at a
// given git ref.
func modFieldAt(root, ref, path, key string) string {
	content, err := gitOut(root, "show", ref+":"+path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key) {
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) == 2 && strings.TrimSpace(strings.TrimSuffix(parts[0], " ")) == key {
				return strings.Trim(strings.TrimSpace(parts[1]), `"'`)
			}
		}
	}
	return ""
}

// previousTag finds the most recent tag strictly before ref.
func previousTag(root, ref string) string {
	out, err := gitOut(root, "describe", "--tags", "--abbrev=0", ref+"^")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

const changelogPrompt = `You are writing release notes for a Minecraft modpack. Below is a git diff of the pack repository EXCLUDING the mod list itself (mod additions/removals are covered elsewhere). Summarise the player-visible changes as short markdown bullet points with no heading: config changes, recipe/script tweaks, performance settings, world-gen, pack metadata, launcher behaviour. Ignore lockfile/hash/index churn and CI plumbing. Be concrete but brief. If nothing player-visible changed, output exactly: No notable changes.`

// describeDiffLLM asks the configured agent (claude -p by default) to
// describe a diff for the changelog. Works headlessly in CI when an API key
// is present.
func describeDiffLLM(packDir, diff string) (string, error) {
	cfg := LoadConfig()
	parts := strings.Fields(cfg.Agent)
	if _, err := exec.LookPath(parts[0]); err != nil {
		return "", err
	}
	const maxDiff = 200_000
	if len(diff) > maxDiff {
		diff = diff[:maxDiff] + "\n… (diff truncated)"
	}
	args := append(parts[1:], "-p", changelogPrompt)
	c := exec.Command(parts[0], args...)
	c.Dir = packDir
	c.Stdin = strings.NewReader(diff)
	out, err := c.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text != "" {
			return "", fmt.Errorf("%s", text)
		}
		return "", err
	}
	if text == "" {
		return "", fmt.Errorf("agent returned no output")
	}
	return text, nil
}

// gitOut runs git in root and returns stdout.
func gitOut(root string, args ...string) (string, error) {
	c := exec.Command("git", args...)
	c.Dir = root
	out, err := c.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}
