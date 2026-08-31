package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
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
	client := &http.Client{Timeout: 15 * time.Second}
	url := "https://api.modrinth.com/v2/version_file/" + sha1 + "?algorithm=sha1"
	resp, err := client.Get(url)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return "", "", fmt.Errorf("hash not found")
	}
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("modrinth API returned %d", resp.StatusCode)
	}
	var v struct {
		ID        string `json:"id"`
		ProjectID string `json:"project_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", "", err
	}
	if v.ID == "" || v.ProjectID == "" {
		return "", "", fmt.Errorf("unexpected modrinth response")
	}
	return v.ProjectID, v.ID, nil
}
