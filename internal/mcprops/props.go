// Package mcprops reads a few keys from server.properties.
package mcprops

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Info is the friend-facing server identity from server.properties.
type Info struct {
	MOTD      string
	Port      string
	Whitelist bool
	// Present is false if server.properties is missing.
	Present bool
}

// Read loads server.properties under serverRoot.
func Read(serverRoot string) Info {
	path := filepath.Join(serverRoot, "server.properties")
	f, err := os.Open(path)
	if err != nil {
		return Info{}
	}
	defer f.Close()

	info := Info{
		MOTD:    "A Minecraft Server",
		Port:    "25565",
		Present: true,
	}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case "motd":
			if v != "" {
				info.MOTD = unescapeMOTD(v)
			}
		case "server-port":
			if v != "" {
				info.Port = v
			}
		case "white-list", "whitelist":
			info.Whitelist = isTrue(v)
		case "enforce-whitelist":
			// If whitelist is on via enforce, still show whitelist status from white-list primarily.
			// Some setups only set enforce-whitelist; treat either as "whitelist mode".
			if isTrue(v) {
				info.Whitelist = true
			}
		}
	}
	return info
}

func isTrue(v string) bool {
	switch strings.ToLower(v) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

// unescapeMOTD handles common escapes in server.properties motd values.
func unescapeMOTD(s string) string {
	s = strings.ReplaceAll(s, "\\n", " ")
	s = strings.ReplaceAll(s, "\\u00a7", "§")
	// Strip section-sign color codes for plain terminal display
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '§' && i+1 < len(s) {
			i++ // skip code char
			continue
		}
		b.WriteByte(s[i])
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "A Minecraft Server"
	}
	return out
}
