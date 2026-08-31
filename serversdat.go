package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// packConfigFile stores pack-level packwiz-tui settings (currently the
// default multiplayer server address). It lives in the pack repo so CI
// exports carry it, but is packwizignored so it never ships in the index.
const packConfigFile = "packwiz-tui.toml"

// ReadServerAddress returns the configured default server address, or "".
func ReadServerAddress(packDir string) string {
	data, err := os.ReadFile(filepath.Join(packDir, packConfigFile))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, val, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == "server-address" {
			return strings.Trim(strings.TrimSpace(val), `"'`)
		}
	}
	return ""
}

// WriteServerAddress persists the default server address ("" removes it).
func WriteServerAddress(packDir, addr string) error {
	path := filepath.Join(packDir, packConfigFile)
	data, _ := os.ReadFile(path)
	var kept []string
	for _, l := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		key, _, ok := strings.Cut(l, "=")
		if l != "" && (!ok || strings.TrimSpace(key) != "server-address") {
			kept = append(kept, l)
		}
	}
	if addr != "" {
		kept = append(kept, fmt.Sprintf("server-address = %q", addr))
	}
	ensureLineInFile(filepath.Join(packDir, ".packwizignore"), packConfigFile)
	if len(kept) == 0 {
		os.Remove(path)
		return nil
	}
	return os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0644)
}

// serversDatBytes builds an uncompressed NBT servers.dat with one entry, so
// fresh installs already have the pack's server in the multiplayer list.
func serversDatBytes(name, ip string) []byte {
	var b bytes.Buffer
	str := func(s string) {
		binary.Write(&b, binary.BigEndian, uint16(len(s)))
		b.WriteString(s)
	}
	b.WriteByte(10) // TAG_Compound (root)
	str("")
	b.WriteByte(9) // TAG_List "servers"
	str("servers")
	b.WriteByte(10) // of TAG_Compound
	binary.Write(&b, binary.BigEndian, int32(1))
	b.WriteByte(8) // TAG_String "ip"
	str("ip")
	str(ip)
	b.WriteByte(8) // TAG_String "name"
	str("name")
	str(name)
	b.WriteByte(0) // end of entry
	b.WriteByte(0) // end of root
	return b.Bytes()
}
