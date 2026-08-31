package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PackMeta holds the version info parsed from pack.toml.
type PackMeta struct {
	Name      string
	Minecraft string
	Loader    string // "neoforge", "forge", "fabric", "quilt"
	LoaderVer string
}

var knownLoaders = []string{"neoforge", "forge", "fabric", "quilt"}

// ParsePackMeta reads name and [versions] from a pack.toml.
func ParsePackMeta(packDir string) (PackMeta, error) {
	meta := PackMeta{}
	data, err := os.ReadFile(filepath.Join(packDir, "pack.toml"))
	if err != nil {
		return meta, err
	}
	inVersions := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inVersions = line == "[versions]"
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if !inVersions {
			if key == "name" && meta.Name == "" {
				meta.Name = val
			}
			continue
		}
		switch key {
		case "minecraft":
			meta.Minecraft = val
		default:
			for _, l := range knownLoaders {
				if key == l {
					meta.Loader = l
					meta.LoaderVer = val
				}
			}
		}
	}
	if meta.Minecraft == "" {
		return meta, fmt.Errorf("no minecraft version in pack.toml")
	}
	if meta.Loader == "" {
		return meta, fmt.Errorf("no supported loader (neoforge/forge/fabric/quilt) in pack.toml")
	}
	return meta, nil
}

// PortablemcVersion returns the version string portablemc expects for this pack.
func (m PackMeta) PortablemcVersion() string {
	switch m.Loader {
	case "neoforge":
		return "neoforge::" + m.LoaderVer
	case "forge":
		return "forge::" + m.Minecraft + "-" + m.LoaderVer
	case "fabric":
		return "fabric:" + m.Minecraft + ":" + m.LoaderVer
	case "quilt":
		return "quilt:" + m.Minecraft + ":" + m.LoaderVer
	}
	return m.Minecraft
}
