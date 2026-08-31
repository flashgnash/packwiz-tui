package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Exporters produce importable/deployable pack artifacts in
// <pack>/.packwiz-tui/build/:
//
//   mmc        — MultiMC/Prism instance zip whose pre-launch hook runs
//                packwiz-installer against this repo's raw pack.toml URL, so
//                the instance self-updates on every launch
//   mrpack     — Modrinth pack via `packwiz modrinth export` (Prism imports these too)
//   curseforge — CurseForge zip via `packwiz curseforge export`
//   server     — ready-to-run server files zip (packwiz-installer -s server)

func buildDir(packDir string) (string, error) {
	return harnessDir(packDir, "build")
}

func packSlug(name string) string {
	slug := regexp.MustCompile(`[^a-zA-Z0-9._-]+`).ReplaceAllString(name, "-")
	return strings.Trim(slug, "-")
}

// artifactBase names artifacts after the git repo when there is one (e.g.
// "modified-atm10"), falling back to the pack name.
func artifactBase(packDir string, meta PackMeta) string {
	if root, err := DetectGitRepoFrom(packDir); err == nil {
		if remote := GetGitRemote(root); remote != "" {
			repo := strings.TrimSuffix(remote, ".git")
			repo = repo[strings.LastIndexAny(repo, "/:")+1:]
			if repo != "" {
				return packSlug(repo)
			}
		}
	}
	return packSlug(meta.Name)
}

// DetectPackURL derives the raw URL of pack.toml from the git remote and
// current branch. Works for github.com remotes (joelbotc-style deployment).
func DetectPackURL(packDir string) (string, error) {
	return detectPackURL(packDir, false)
}

// DetectPackURLDefaultBranch is DetectPackURL pinned to the remote's default
// branch — for long-lived references like server configs, which shouldn't
// track whatever feature branch the working copy happens to be on.
func DetectPackURLDefaultBranch(packDir string) (string, error) {
	return detectPackURL(packDir, true)
}

func detectPackURL(packDir string, useDefaultBranch bool) (string, error) {
	root, err := DetectGitRepoFrom(packDir)
	if err != nil {
		return "", fmt.Errorf("pack is not in a git repo: %w", err)
	}
	remote := GetGitRemote(root)
	if remote == "" {
		return "", fmt.Errorf("no git remote configured")
	}
	m := regexp.MustCompile(`github\.com[:/]([^/]+)/([^/.]+)`).FindStringSubmatch(remote)
	if m == nil {
		return "", fmt.Errorf("remote %q is not a github.com URL", remote)
	}
	branch := "HEAD"
	if !useDefaultBranch {
		branchOut, err := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD").Output()
		if err != nil {
			return "", err
		}
		branch = strings.TrimSpace(string(branchOut))
	}
	if branch == "HEAD" {
		// Detached HEAD (e.g. CI tag checkout) or default requested — use the
		// remote default branch.
		if out, err := exec.Command("git", "-C", root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD").Output(); err == nil {
			branch = strings.TrimPrefix(strings.TrimSpace(string(out)), "origin/")
		} else {
			branch = "master"
		}
	}
	rel, err := filepath.Rel(root, filepath.Join(packDir, "pack.toml"))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/refs/heads/%s/%s",
		m[1], m[2], branch, filepath.ToSlash(rel)), nil
}

var loaderComponentUIDs = map[string]string{
	"neoforge": "net.neoforged",
	"forge":    "net.minecraftforge",
	"fabric":   "net.fabricmc.fabric-loader",
	"quilt":    "org.quiltmc.quilt-loader",
}

// mmcInstanceFiles builds the file set for a MultiMC/Prism instance that
// contains no mods — just the loader components and a pre-launch
// packwiz-installer hook pointing at the repo, so it stays in sync.
func mmcInstanceFiles(packDir string) (map[string][]byte, PackMeta, string, error) {
	meta, err := ParsePackMeta(packDir)
	if err != nil {
		return nil, meta, "", err
	}
	packURL, err := DetectPackURL(packDir)
	if err != nil {
		return nil, meta, "", err
	}
	uid, ok := loaderComponentUIDs[meta.Loader]
	if !ok {
		return nil, meta, "", fmt.Errorf("unsupported loader %q", meta.Loader)
	}

	out, err := buildDir(packDir)
	if err != nil {
		return nil, meta, "", err
	}
	// Fetch the bootstrap jar (cached in the build dir).
	bootstrap := filepath.Join(out, "packwiz-installer-bootstrap.jar")
	if err := downloadFile(bootstrapURL, bootstrap); err != nil {
		return nil, meta, "", err
	}
	bootstrapBytes, err := os.ReadFile(bootstrap)
	if err != nil {
		return nil, meta, "", err
	}

	instanceCfg := fmt.Sprintf(`[General]
ConfigVersion=1.2
InstanceType=OneSix
name=%s
OverrideCommands=true
PreLaunchCommand="\"$INST_JAVA\" -jar packwiz-installer-bootstrap.jar %s"
`, meta.Name, packURL)

	mmcPack := fmt.Sprintf(`{
    "components": [
        {
            "important": true,
            "uid": "net.minecraft",
            "version": %q
        },
        {
            "uid": %q,
            "version": %q
        }
    ],
    "formatVersion": 1
}
`, meta.Minecraft, uid, meta.LoaderVer)

	files := map[string][]byte{
		"instance.cfg":  []byte(instanceCfg),
		"mmc-pack.json": []byte(mmcPack),
		".minecraft/packwiz-installer-bootstrap.jar": bootstrapBytes,
	}
	if icon, err := os.ReadFile(filepath.Join(packDir, "icon.png")); err == nil {
		files["icon.png"] = icon
	}
	if addr := ReadServerAddress(packDir); addr != "" {
		files[".minecraft/servers.dat"] = serversDatBytes(meta.Name, addr)
	}
	return files, meta, packURL, nil
}

// ExportMMC writes a MultiMC/Prism-importable instance zip.
func ExportMMC(packDir string, progress io.Writer) (string, error) {
	files, meta, packURL, err := mmcInstanceFiles(packDir)
	if err != nil {
		return "", err
	}
	out, err := buildDir(packDir)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(out, artifactBase(packDir, meta)+"-prism.zip")
	if err := writeZip(dest, files); err != nil {
		return "", err
	}
	fmt.Fprintf(progress, "prism instance: %s (updates from %s)\n", dest, packURL)
	return dest, nil
}

// ExportMMCPreinstalled writes a Prism-importable instance zip with the
// entire client side already installed — no first-launch download wait. The
// pre-launch hook is still present, so the instance keeps itself updated.
func ExportMMCPreinstalled(packDir string, progress io.Writer) (string, error) {
	files, meta, packURL, err := mmcInstanceFiles(packDir)
	if err != nil {
		return "", err
	}
	out, err := buildDir(packDir)
	if err != nil {
		return "", err
	}
	scratch, err := os.MkdirTemp("", "packwiz-tui-client-export-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)

	fmt.Fprintln(progress, "installing client files for preinstalled instance…")
	if instOut, err := RunPackwizInstaller(scratch, packDir, "client"); err != nil {
		return "", fmt.Errorf("packwiz-installer failed:\n%s", tail(instOut, 30))
	}
	dest := filepath.Join(out, artifactBase(packDir, meta)+"-prism-preinstalled.zip")
	if err := writeZipWithDir(dest, files, scratch, ".minecraft"); err != nil {
		return "", err
	}
	fmt.Fprintf(progress, "prism preinstalled instance: %s (updates from %s)\n", dest, packURL)
	return dest, nil
}

// InstallPrism writes the self-updating instance straight into the local
// PrismLauncher instances directory — no zip import needed. If the instance
// already exists, only its metadata files are refreshed; worlds, options and
// installed mods are left alone (the pre-launch hook keeps them synced).
func InstallPrism(packDir string, progress io.Writer) error {
	instancesDir, err := findPrismInstancesDir()
	if err != nil {
		return err
	}
	files, meta, packURL, err := mmcInstanceFiles(packDir)
	if err != nil {
		return err
	}
	// Give the local instance an 8GB heap if this machine can afford it
	// (exported zips stay unopinionated — other machines differ).
	memGb := clampHeapGb(8, 1)
	files["instance.cfg"] = append(files["instance.cfg"], []byte(fmt.Sprintf(
		"OverrideMemory=true\nMinMemAlloc=1024\nMaxMemAlloc=%d\n", memGb*1024))...)

	instDir := filepath.Join(instancesDir, artifactBase(packDir, meta))
	existing := false
	if _, err := os.Stat(instDir); err == nil {
		existing = true
	}
	for name, data := range files {
		dest := filepath.Join(instDir, filepath.FromSlash(name))
		// The server list belongs to the player once the instance exists —
		// only seed it on first install, never clobber it on refresh.
		if name == ".minecraft/servers.dat" {
			if _, err := os.Stat(dest); err == nil {
				continue
			}
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, data, 0644); err != nil {
			return err
		}
	}
	verb := "created"
	if existing {
		verb = "updated"
	}
	fmt.Fprintf(progress, "%s Prism instance %q in %s (updates from %s)\n", verb, artifactBase(packDir, meta), instancesDir, packURL)
	fmt.Fprintln(progress, "restart PrismLauncher (or refresh its instance list) to see it")
	return nil
}

// findPrismInstancesDir locates the local PrismLauncher instances folder,
// checking the standard and flatpak data dirs.
func findPrismInstancesDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	candidates := []string{
		filepath.Join(dataHome, "PrismLauncher"),
		filepath.Join(home, ".var", "app", "org.prismlauncher.PrismLauncher", "data", "PrismLauncher"),
	}
	for _, dir := range candidates {
		if _, err := os.Stat(dir); err == nil {
			inst := filepath.Join(dir, "instances")
			if err := os.MkdirAll(inst, 0755); err != nil {
				return "", err
			}
			return inst, nil
		}
	}
	return "", fmt.Errorf("no PrismLauncher data dir found (looked in %s) — is Prism installed?", strings.Join(candidates, ", "))
}

// ExportPackwiz runs a `packwiz <platform> export` and moves the artifact
// into the build dir. platform is "modrinth" or "curseforge".
func ExportPackwiz(packDir, platform string, progress io.Writer) (string, error) {
	meta, err := ParsePackMeta(packDir)
	if err != nil {
		return "", err
	}
	out, err := buildDir(packDir)
	if err != nil {
		return "", err
	}
	ext := ".zip"
	suffix := "-curseforge"
	if platform == "modrinth" {
		ext = ".mrpack"
		suffix = ""
	}
	dest := filepath.Join(out, artifactBase(packDir, meta)+suffix+ext)
	cmdOut, err := RunPackwiz(packDir, platform, "export", "-o", dest)
	if err != nil {
		return "", fmt.Errorf("packwiz %s export failed:\n%s", platform, tail(cmdOut, 15))
	}
	fmt.Fprintf(progress, "%s pack: %s\n", platform, dest)
	return dest, nil
}

// ExportServer produces a ready-to-run server files zip by installing the
// server side into a scratch dir.
func ExportServer(packDir string, progress io.Writer) (string, error) {
	meta, err := ParsePackMeta(packDir)
	if err != nil {
		return "", err
	}
	out, err := buildDir(packDir)
	if err != nil {
		return "", err
	}
	scratch, err := os.MkdirTemp("", "packwiz-tui-server-export-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)

	fmt.Fprintln(progress, "installing server files…")
	if instOut, err := RunPackwizInstaller(scratch, packDir, "server"); err != nil {
		return "", fmt.Errorf("packwiz-installer failed:\n%s", tail(instOut, 30))
	}
	dest := filepath.Join(out, artifactBase(packDir, meta)+"-server.zip")
	if err := zipDir(dest, scratch); err != nil {
		return "", err
	}
	fmt.Fprintf(progress, "server files: %s\n", dest)
	return dest, nil
}

// ExportAll builds every artifact and returns their paths.
func ExportAll(packDir string, progress io.Writer) ([]string, error) {
	var artifacts []string
	type step struct {
		name string
		run  func() (string, error)
	}
	steps := []step{
		{"prism", func() (string, error) { return ExportMMC(packDir, progress) }},
		{"prism-preinstalled", func() (string, error) { return ExportMMCPreinstalled(packDir, progress) }},
		{"mrpack", func() (string, error) { return ExportPackwiz(packDir, "modrinth", progress) }},
		{"curseforge", func() (string, error) { return ExportPackwiz(packDir, "curseforge", progress) }},
		{"server", func() (string, error) { return ExportServer(packDir, progress) }},
	}
	for _, s := range steps {
		path, err := s.run()
		if err != nil {
			return artifacts, fmt.Errorf("export %s: %w", s.name, err)
		}
		artifacts = append(artifacts, path)
	}
	return artifacts, nil
}

// ReleaseGithub exports all artifacts and publishes them as a GitHub release
// via the gh CLI (must be authenticated).
func ReleaseGithub(packDir, tag string, progress io.Writer) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI not found — install it or use `packwiz-tui init-workflow` for tag-triggered CI releases")
	}
	meta, err := ParsePackMeta(packDir)
	if err != nil {
		return err
	}
	if tag == "" {
		version, _ := readTomlField(filepath.Join(packDir, "pack.toml"), "version")
		if version == "" {
			return fmt.Errorf("no tag given and no version in pack.toml")
		}
		tag = "v" + version
	}
	artifacts, err := ExportAll(packDir, progress)
	if err != nil {
		return err
	}
	root, _ := DetectGitRepoFrom(packDir)
	fmt.Fprintf(progress, "creating release %s…\n", tag)
	args := append([]string{"release", "create", tag,
		"--title", fmt.Sprintf("%s %s", meta.Name, strings.TrimPrefix(tag, "v")),
		"--generate-notes"}, artifacts...)
	cmd := exec.Command("gh", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh release create failed: %s", tail(string(out), 10))
	}
	fmt.Fprintf(progress, "release %s published\n", tag)
	return nil
}

// ── zip helpers ──────────────────────────────────────────────────────────────

func writeZip(dest string, files map[string][]byte) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for name, data := range files {
		fw, err := w.Create(name)
		if err != nil {
			return err
		}
		if _, err := fw.Write(data); err != nil {
			return err
		}
	}
	return w.Close()
}

// writeZipWithDir writes the literal files plus everything under dirRoot,
// prefixed with dirPrefix inside the archive. Literal files win on conflict.
func writeZipWithDir(dest string, files map[string][]byte, dirRoot, dirPrefix string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for name, data := range files {
		fw, err := w.Create(name)
		if err != nil {
			return err
		}
		if _, err := fw.Write(data); err != nil {
			return err
		}
	}
	err = filepath.Walk(dirRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dirRoot, path)
		if err != nil {
			return err
		}
		name := dirPrefix + "/" + filepath.ToSlash(rel)
		if _, clash := files[name]; clash {
			return nil
		}
		fw, err := w.Create(name)
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(fw, src)
		return err
	})
	if err != nil {
		return err
	}
	return w.Close()
}

func zipDir(dest, root string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		fw, err := w.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(fw, src)
		return err
	})
	if err != nil {
		return err
	}
	return w.Close()
}
