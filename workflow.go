package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// InitWorkflow scaffolds a GitHub Actions workflow in the pack repo
// (joelbotc-style): every push builds all artifacts (mmc/mrpack/curseforge/
// server) and uploads them as workflow artifacts; pushing a v* tag
// additionally publishes them as a GitHub release. The MMC zip self-updates
// from the repo, so old releases keep working.
const releaseWorkflow = `name: Build Modpack

on:
  push:
    branches: ["**"]
    tags: ["v*"]

permissions:
  contents: write

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-java@v4
        with:
          distribution: temurin
          java-version: "21"

      - uses: actions/setup-go@v5
        with:
          go-version: stable

      - name: Install packwiz + packwiz-tui
        run: |
          go install github.com/packwiz/packwiz@latest
          go install github.com/flashgnash/packwiz-tui@master

      - name: Build artifacts
        run: packwiz-tui export all

      - name: Upload workflow artifacts
        uses: actions/upload-artifact@v4
        with:
          name: modpack
          retention-days: 14
          path: |
            .packwiz-tui/build/*.zip
            .packwiz-tui/build/*.mrpack

      - name: Update rolling latest release
        if: github.ref == format('refs/heads/{0}', github.event.repository.default_branch)
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          # Keep the newest build of every artifact on a rolling release.
          # Numbered prefixes order the asset list: Prism installer first.
          mkdir -p .packwiz-tui/ordered
          cd .packwiz-tui/build
          for f in *-prism-installer.zip; do cp "$f" "../ordered/01-$f"; done
          for f in *-curseforge.zip;      do cp "$f" "../ordered/02-$f"; done
          for f in *.mrpack;              do cp "$f" "../ordered/03-$f"; done
          for f in *-server.zip;          do cp "$f" "../ordered/04-$f"; done
          cd ../..
          {
            echo "Rolling build of the default branch. Assets in order:"
            echo
            echo "1. **Prism installer** — import into PrismLauncher; the instance auto-updates from this repo on every launch (recommended)"
            echo "2. **CurseForge pack** — import into the CurseForge app"
            echo "3. **Modrinth pack** (.mrpack) — import into Prism or the Modrinth app"
            echo "4. **Server files** — ready-to-run server"
          } > .packwiz-tui/release-notes.md
          gh release delete latest --yes 2>/dev/null || true
          gh release create latest --prerelease --title "Latest build (rolling)" \
            --notes-file .packwiz-tui/release-notes.md \
            .packwiz-tui/ordered/*

      - name: Create release
        if: startsWith(github.ref, 'refs/tags/')
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          gh release create "${GITHUB_REF_NAME}" \
            --title "${GITHUB_REF_NAME}" \
            --generate-notes \
            .packwiz-tui/build/*.zip .packwiz-tui/build/*.mrpack
`

func InitWorkflow(packDir string, progress io.Writer) error {
	root, err := DetectGitRepoFrom(packDir)
	if err != nil {
		return fmt.Errorf("pack is not in a git repo: %w", err)
	}
	dir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, "release.yml")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists — remove it first to regenerate", path)
	}
	if err := os.WriteFile(path, []byte(releaseWorkflow), 0644); err != nil {
		return err
	}
	fmt.Fprintf(progress, "wrote %s\nevery push now builds artifacts; release with: git tag v<version> && git push --tags\n", path)
	return nil
}
