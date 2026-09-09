package main

import (
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
)

// Changelog builds a markdown changelog between two git refs: a deterministic
// mod added/removed/updated section diffed from mods/*.toml. It is purely
// mechanical — no LLM involvement — so it produces identical output for the
// same diff every time.
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
			added = append(added, modEntry(root, to, fields[1], ""))
		case fields[0] == "D":
			removed = append(removed, modEntry(root, from, fields[1], ""))
		case fields[0] == "M":
			oldFile := modFieldAt(root, from, fields[1], "filename")
			newFile := modFieldAt(root, to, fields[1], "filename")
			change := ""
			if oldFile != newFile && oldFile != "" && newFile != "" {
				change = fmt.Sprintf("`%s` → `%s`", oldFile, newFile)
			}
			updated = append(updated, modEntry(root, to, fields[1], change))
		case strings.HasPrefix(fields[0], "R") && len(fields) >= 3:
			updated = append(updated, modEntry(root, to, fields[2], ""))
		}
	}
	return added, removed, updated, nil
}

// modEntry formats one changelog line body for a mod toml at a given ref:
// the display name, a styled link to its source (Modrinth/CurseForge) pinned
// to the exact version, and either an explicit change note or the mod's
// version (its jar filename).
func modEntry(root, ref, path, change string) string {
	entry := modNameAt(root, ref, path)
	if link := modLinkAt(root, ref, path); link != "" {
		entry += " — " + link
	}
	switch {
	case change != "":
		entry += " " + change
	default:
		if file := modFieldAt(root, ref, path, "filename"); file != "" {
			entry += " `" + file + "`"
		}
	}
	return entry
}

// modLinkAt builds styled markdown links for a mod at a given ref. It always
// links the platform the mod is installed from (pinned to the exact
// version/file), listed first; then, only when the same mod is confirmed to
// exist on the other platform via its API, it appends a link there too. The
// counterpart is verified so we never emit a dead link — mods that only live
// on one site get a single link. Returns "" for mods with no update source.
func modLinkAt(root, ref, path string) string {
	slug := strings.TrimSuffix(filepath.Base(path), ".pw.toml")
	content, err := gitOut(root, "show", ref+":"+path)
	if err != nil {
		return ""
	}
	section := ""
	f := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			section = strings.Trim(t, "[]")
			continue
		}
		if i := strings.IndexByte(t, '='); i > 0 {
			key := strings.TrimSpace(t[:i])
			val := strings.Trim(strings.TrimSpace(t[i+1:]), `"'`)
			f[section+"."+key] = val
		}
	}
	switch {
	case f["update.modrinth.mod-id"] != "":
		mr := "https://modrinth.com/mod/" + slug
		if v := f["update.modrinth.version"]; v != "" {
			mr += "/version/" + v
		}
		link := "[modrinth](" + mr + ")"
		if cf := curseforgeSlugFor(slug); cf != "" {
			link += " · [curseforge](https://www.curseforge.com/minecraft/mc-mods/" + cf + ")"
		}
		return link
	case f["update.curseforge.project-id"] != "":
		cf := "https://www.curseforge.com/minecraft/mc-mods/" + slug
		if v := f["update.curseforge.file-id"]; v != "" {
			cf += "/files/" + v
		}
		link := "[curseforge](" + cf + ")"
		if mr := modrinthSlugFor(slug); mr != "" {
			link += " · [modrinth](https://modrinth.com/mod/" + mr + ")"
		}
		return link
	}
	return ""
}

// modrinthSlugFor returns a mod's Modrinth page slug if a project with this
// slug exists, else "" — used to confirm a CurseForge-installed mod also has a
// Modrinth page before linking it.
func modrinthSlugFor(slug string) string {
	var p struct {
		Slug string `json:"slug"`
	}
	if err := modrinthGet("/project/"+url.PathEscape(slug), nil, &p); err != nil {
		return ""
	}
	if p.Slug != "" {
		return p.Slug
	}
	return slug
}

// curseforgeSlugFor returns a mod's CurseForge page slug if a project with this
// slug exists, else "" — used to confirm a Modrinth-installed mod also has a
// CurseForge page before linking it.
func curseforgeSlugFor(slug string) string {
	q := url.Values{"gameId": {"432"}, "classId": {"6"}, "slug": {slug}}
	var res struct {
		Data []struct {
			Slug string `json:"slug"`
		} `json:"data"`
	}
	if err := curseforgeGet("/mods/search", q, &res); err != nil || len(res.Data) == 0 {
		return ""
	}
	if s := res.Data[0].Slug; s != "" {
		return s
	}
	return slug
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
