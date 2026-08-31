package main

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// TagSides sets side="client"/"both" on every mod metafile by diffing the
// pack against an official server-pack zip: mods whose jar is absent from the
// server pack are client-only. Shaderpack/resourcepack metafiles are always
// client. Returns a human-readable report.
func TagSides(packDir, serverZip string) (string, error) {
	serverJars, err := listZipModJars(serverZip)
	if err != nil {
		return "", fmt.Errorf("reading server pack: %w", err)
	}

	var report strings.Builder
	clientCount, bothCount := 0, 0
	clientJars := map[string]bool{}

	modTomls, _ := filepath.Glob(filepath.Join(packDir, "mods", "*.pw.toml"))
	sort.Strings(modTomls)
	for _, toml := range modTomls {
		fname, err := readTomlField(toml, "filename")
		if err != nil || fname == "" {
			continue
		}
		clientJars[fname] = true
		side := "both"
		if !serverJars[fname] {
			side = "client"
			clientCount++
			fmt.Fprintf(&report, "client: %s (%s)\n", filepath.Base(toml), fname)
		} else {
			bothCount++
		}
		if err := writeTomlSide(toml, side); err != nil {
			return report.String(), err
		}
	}

	for _, dir := range []string{"shaderpacks", "resourcepacks"} {
		tomls, _ := filepath.Glob(filepath.Join(packDir, dir, "*.pw.toml"))
		for _, toml := range tomls {
			if err := writeTomlSide(toml, "client"); err != nil {
				return report.String(), err
			}
		}
	}

	fmt.Fprintf(&report, "\n%d mods tagged client-only, %d both\n", clientCount, bothCount)

	// Server jars with no client metafile — worth flagging (side=server candidates).
	var orphans []string
	for jar := range serverJars {
		if !clientJars[jar] {
			orphans = append(orphans, jar)
		}
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		report.WriteString("\nserver jars with no metafile (check if side=server needed):\n")
		for _, o := range orphans {
			fmt.Fprintf(&report, "  %s\n", o)
		}
	}

	out, err := RunPackwiz(packDir, "refresh")
	if err != nil {
		return report.String() + "\n" + out, err
	}
	return report.String(), nil
}

// listZipModJars returns the basenames of all jars under a mods/ folder in
// the zip, tolerating an optional top-level directory.
func listZipModJars(zipPath string) (map[string]bool, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	re := regexp.MustCompile(`(^|/)mods/[^/]+\.jar$`)
	jars := map[string]bool{}
	for _, f := range r.File {
		if re.MatchString(f.Name) {
			jars[filepath.Base(f.Name)] = true
		}
	}
	return jars, nil
}

func readTomlField(path, field string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && strings.TrimSpace(key) == field {
			return strings.Trim(strings.TrimSpace(val), `"'`), nil
		}
	}
	return "", nil
}

func writeTomlSide(path, side string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "side") {
			lines[i] = fmt.Sprintf("side = %q", side)
			found = true
			break
		}
	}
	if !found {
		// Insert after the filename line (top-level section).
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "filename") {
				lines = append(lines[:i+1], append([]string{fmt.Sprintf("side = %q", side)}, lines[i+1:]...)...)
				break
			}
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}
