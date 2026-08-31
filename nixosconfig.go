package main

import (
	"fmt"
	"strconv"
	"strings"
)

// GenerateNixosConfig emits a services.minecraft-servers block for the
// user's nix-minecraft flake module, ready to paste into a host config
// organised like GLaDOS (inputs.minecraft-server.nixosModules.default plus
// per-server attrsets).
func GenerateNixosConfig(packDir string) (string, error) {
	meta, err := ParsePackMeta(packDir)
	if err != nil {
		return "", err
	}
	packURL, err := DetectPackURLDefaultBranch(packDir)
	if err != nil {
		return "", err
	}
	name := artifactBase(packDir, meta)

	var b strings.Builder
	fmt.Fprintf(&b, "# %s — paste inside a module that imports inputs.minecraft-server.nixosModules.default\n", meta.Name)
	fmt.Fprintf(&b, "# (as on GLaDOS; needs a { pkgs, ... }: module argument for javaPackage)\n")
	fmt.Fprintf(&b, "services.minecraft-servers.%s = {\n", name)
	fmt.Fprintf(&b, "  enable = true;\n")
	fmt.Fprintf(&b, "  acceptEULA = true;\n")
	fmt.Fprintf(&b, "  openFirewall = true;\n")
	fmt.Fprintf(&b, "  port = 25565; # adjust if another server already uses it\n")
	fmt.Fprintf(&b, "  javaPackage = pkgs.%s;\n", javaPackageForMC(meta.Minecraft))
	fmt.Fprintf(&b, "  loader = %q;\n", meta.Loader)
	fmt.Fprintf(&b, "  minecraftVersion = %q;\n", meta.Minecraft)
	fmt.Fprintf(&b, "  forgeVersion = %q; # %s loader version\n", meta.LoaderVer, meta.Loader)
	fmt.Fprintf(&b, "  ramGb = 8;\n")
	fmt.Fprintf(&b, "  packwizUrl = %q;\n", packURL)
	fmt.Fprintf(&b, "};\n")
	return b.String(), nil
}

// javaPackageForMC maps a Minecraft version to the nixpkgs JDK it needs:
// <=1.16 → 8, 1.17–1.20.4 → 17, 1.20.5+ → 21.
func javaPackageForMC(mc string) string {
	parts := strings.Split(mc, ".")
	if len(parts) < 2 || parts[0] != "1" {
		return "jdk21"
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return "jdk21"
	}
	patch := 0
	if len(parts) > 2 {
		patch, _ = strconv.Atoi(parts[2])
	}
	switch {
	case minor <= 16:
		return "jdk8"
	case minor < 20 || (minor == 20 && patch < 5):
		return "jdk17"
	default:
		return "jdk21"
	}
}
