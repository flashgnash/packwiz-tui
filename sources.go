package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// FixModSources finds CurseForge mods whose authors block API distribution
// (they break unattended packwiz-installer runs) and swaps their metafiles to
// Modrinth when a byte-identical file exists there, preserving the side tag.
//
// Detection runs packwiz-installer for the given side into a scratch dir and
// parses its "excluded from the CurseForge API" errors, so it doubles as an
// install test: a clean run means the pack installs unattended.
func FixModSources(packDir, side string) (string, error) {
	var report strings.Builder

	blocked, installOut, err := detectBlockedMods(packDir, side)
	if err != nil && len(blocked) == 0 {
		return installOut, err
	}
	if len(blocked) == 0 {
		return fmt.Sprintf("no CF-API-blocked mods on side %q — pack installs unattended\n", side), nil
	}

	fmt.Fprintf(&report, "%d CF-API-blocked mod(s) found:\n", len(blocked))
	for _, jar := range blocked {
		toml, terr := findTomlByFilename(packDir, jar)
		if terr != nil {
			fmt.Fprintf(&report, "  ✗ %s: %v\n", jar, terr)
			continue
		}
		origSide, _ := readTomlField(toml, "side")
		sha1, _ := readTomlField(toml, "hash")
		hashFormat, _ := readTomlField(toml, "hash-format")
		if hashFormat != "sha1" || sha1 == "" {
			fmt.Fprintf(&report, "  ✗ %s: no sha1 hash in metafile\n", jar)
			continue
		}

		proj, ver, merr := modrinthLookupByHash(sha1)
		if merr != nil {
			fmt.Fprintf(&report, "  ✗ %s: not on Modrinth (%v) — jar must be shipped manually\n", jar, merr)
			continue
		}

		slug := strings.TrimSuffix(filepath.Base(toml), ".pw.toml")
		if out, rerr := RunPackwiz(packDir, "remove", slug); rerr != nil {
			fmt.Fprintf(&report, "  ✗ %s: packwiz remove failed: %s\n", jar, out)
			continue
		}
		if out, aerr := RunPackwiz(packDir, "modrinth", "add", "--project-id", proj, "--version-id", ver, "-y"); aerr != nil {
			fmt.Fprintf(&report, "  ✗ %s: modrinth add failed: %s\n", jar, out)
			continue
		}

		// Modrinth's side metadata can differ from what the pack ships —
		// restore whatever the metafile said before the swap.
		if origSide != "" {
			if newToml, ferr := findTomlByFilename(packDir, jar); ferr == nil {
				writeTomlSide(newToml, origSide)
			}
		}
		fmt.Fprintf(&report, "  ✓ %s → modrinth %s/%s\n", jar, proj, ver)
	}

	if out, rerr := RunPackwiz(packDir, "refresh"); rerr != nil {
		return report.String() + "\n" + out, rerr
	}
	return report.String(), nil
}

// SourceCounts tallies the pack's mods by update source.
type SourceCounts struct {
	Curseforge int
	Modrinth   int
	Local      int // no update source — manually shipped files
}

// CountModSources classifies every mod metafile by its update source.
func CountModSources(packDir string) SourceCounts {
	var c SourceCounts
	tomls, _ := filepath.Glob(filepath.Join(packDir, "mods", "*.toml"))
	for _, t := range tomls {
		data, err := os.ReadFile(t)
		if err != nil {
			continue
		}
		switch s := string(data); {
		case strings.Contains(s, "[update.curseforge]"):
			c.Curseforge++
		case strings.Contains(s, "[update.modrinth]"):
			c.Modrinth++
		default:
			c.Local++
		}
	}
	return c
}

// ConvertSources attempts to move every mod (or just onlySlug, when set) to
// the given source ("modrinth" or "curseforge"). Modrinth conversions are
// byte-identical (sha1 lookup); curseforge conversions re-add by slug and
// roll back to the original modrinth version if the slug isn't found. Local
// mods are left alone.
func ConvertSources(packDir, target, onlySlug string, progress io.Writer) error {
	if target != "modrinth" && target != "curseforge" {
		return fmt.Errorf("unknown target %q (want modrinth or curseforge)", target)
	}
	tomls, _ := filepath.Glob(filepath.Join(packDir, "mods", "*.pw.toml"))
	sort.Strings(tomls)

	converted, failed, skipped := 0, 0, 0
	for _, toml := range tomls {
		data, err := os.ReadFile(toml)
		if err != nil {
			continue
		}
		s := string(data)
		isCF := strings.Contains(s, "[update.curseforge]")
		isMR := strings.Contains(s, "[update.modrinth]")
		if (target == "modrinth" && !isCF) || (target == "curseforge" && !isMR) {
			continue // already on the target, or local
		}
		slug := strings.TrimSuffix(filepath.Base(toml), ".pw.toml")
		if onlySlug != "" && slug != onlySlug {
			continue
		}
		jar, _ := readTomlField(toml, "filename")
		side, _ := readTomlField(toml, "side")

		if target == "modrinth" {
			sha1v, _ := readTomlField(toml, "hash")
			hashFormat, _ := readTomlField(toml, "hash-format")
			if hashFormat != "sha1" || sha1v == "" {
				skipped++
				fmt.Fprintf(progress, "  ✗ %s: no sha1 hash in metafile\n", slug)
				continue
			}
			proj, ver, merr := modrinthLookupByHash(sha1v)
			if merr != nil {
				failed++
				fmt.Fprintf(progress, "  ✗ %s: not on modrinth (%v)\n", slug, merr)
				continue
			}
			if out, rerr := RunPackwiz(packDir, "remove", slug); rerr != nil {
				failed++
				fmt.Fprintf(progress, "  ✗ %s: packwiz remove failed: %s\n", slug, tail(out, 3))
				continue
			}
			if out, aerr := RunPackwiz(packDir, "modrinth", "add", "--project-id", proj, "--version-id", ver, "-y"); aerr != nil {
				failed++
				fmt.Fprintf(progress, "  ✗ %s: modrinth add failed: %s\n", slug, tail(out, 3))
				continue
			}
			restoreSide(packDir, slug, jar, side)
			converted++
			fmt.Fprintf(progress, "  ✓ %s → modrinth\n", slug)
			continue
		}

		// modrinth → curseforge: re-add by slug, roll back on failure.
		modID, _ := readTomlField(toml, "mod-id")
		verID, _ := readTomlField(toml, "version")
		if out, rerr := RunPackwiz(packDir, "remove", slug); rerr != nil {
			failed++
			fmt.Fprintf(progress, "  ✗ %s: packwiz remove failed: %s\n", slug, tail(out, 3))
			continue
		}
		if out, aerr := RunPackwiz(packDir, "curseforge", "add", slug, "-y"); aerr != nil {
			failed++
			if modID != "" && verID != "" {
				RunPackwiz(packDir, "modrinth", "add", "--project-id", modID, "--version-id", verID, "-y")
				restoreSide(packDir, slug, jar, side)
				fmt.Fprintf(progress, "  ✗ %s: not found on curseforge — kept on modrinth (%s)\n", slug, tail(out, 1))
			} else {
				fmt.Fprintf(progress, "  ✗ %s: curseforge add failed and no rollback info: %s\n", slug, tail(out, 3))
			}
			continue
		}
		restoreSide(packDir, slug, jar, side)
		converted++
		fmt.Fprintf(progress, "  ✓ %s → curseforge\n", slug)
	}

	if out, rerr := RunPackwiz(packDir, "refresh"); rerr != nil {
		return fmt.Errorf("packwiz refresh failed: %s", tail(out, 5))
	}
	fmt.Fprintf(progress, "done: %d converted, %d failed, %d skipped\n", converted, failed, skipped)
	return nil
}

// restoreSide re-applies the original side tag to a freshly re-added mod,
// locating it by slug first and file name second.
func restoreSide(packDir, slug, jar, side string) {
	if side == "" {
		return
	}
	path := filepath.Join(packDir, "mods", slug+".pw.toml")
	if _, err := os.Stat(path); err == nil {
		writeTomlSide(path, side)
		return
	}
	if jar != "" {
		if toml, err := findTomlByFilename(packDir, jar); err == nil {
			writeTomlSide(toml, side)
		}
	}
}

var reOptionalLine = regexp.MustCompile(`(?m)^\s*optional\s*=\s*(true|false)\s*$`)

// writeTomlOptional sets (or adds) the [option] optional flag in a metafile.
func writeTomlOptional(path string, optional bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(data)
	line := fmt.Sprintf("optional = %v", optional)
	if reOptionalLine.MatchString(s) {
		s = reOptionalLine.ReplaceAllString(s, line)
	} else if optional {
		if !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		s += "\n[option]\n" + line + "\n"
	} else {
		return nil // already non-optional
	}
	return os.WriteFile(path, []byte(s), 0644)
}

var reDefaultLine = regexp.MustCompile(`(?m)^\s*default\s*=\s*(true|false)\s*$`)

// writeTomlDefault sets (or adds) the [option] default flag — whether an
// optional mod is enabled by default in installers.
func writeTomlDefault(path string, def bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(data)
	line := fmt.Sprintf("default = %v", def)
	if reDefaultLine.MatchString(s) {
		s = reDefaultLine.ReplaceAllString(s, line)
	} else if loc := reOptionalLine.FindStringSubmatchIndex(s); loc != nil {
		// Insert directly after the optional value (loc[3]), before any
		// trailing whitespace the line pattern swallowed.
		s = s[:loc[3]] + "\n" + line + s[loc[3]:]
	} else {
		if !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		s += "\n[option]\noptional = true\n" + line + "\n"
	}
	return os.WriteFile(path, []byte(s), 0644)
}

var reBlockedJar = regexp.MustCompile(`save this file to .*[/\\]([^/\\]+\.jar)`)

// detectBlockedMods runs packwiz-installer into a scratch dir and collects
// the jar names it reports as CF-API-excluded.
func detectBlockedMods(packDir, side string) ([]string, string, error) {
	scratch, err := os.MkdirTemp("", "packwiz-tui-install-test-")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(scratch)

	out, err := RunPackwizInstaller(scratch, packDir, side)
	seen := map[string]bool{}
	var blocked []string
	for _, m := range reBlockedJar.FindAllStringSubmatch(out, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			blocked = append(blocked, m[1])
		}
	}
	sort.Strings(blocked)
	if len(blocked) > 0 {
		// The failure was the expected one; not an error for our purposes.
		return blocked, out, nil
	}
	return nil, out, err
}

func findTomlByFilename(packDir, jar string) (string, error) {
	tomls, _ := filepath.Glob(filepath.Join(packDir, "mods", "*.pw.toml"))
	for _, toml := range tomls {
		fname, _ := readTomlField(toml, "filename")
		if fname == jar {
			return toml, nil
		}
	}
	return "", fmt.Errorf("no metafile with filename %s", jar)
}

// modrinthLookupByHash resolves a file sha1 to (projectID, versionID).
func modrinthLookupByHash(sha1 string) (string, string, error) {
	data, err := cachedGET("https://api.modrinth.com/v2/version_file/"+sha1+"?algorithm=sha1", nil)
	if err == errAPINotFound {
		return "", "", fmt.Errorf("hash not found")
	}
	if err != nil {
		return "", "", err
	}
	var v struct {
		ID        string `json:"id"`
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return "", "", err
	}
	if v.ID == "" || v.ProjectID == "" {
		return "", "", fmt.Errorf("unexpected modrinth response")
	}
	return v.ProjectID, v.ID, nil
}
