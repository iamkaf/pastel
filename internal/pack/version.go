package pack

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// Minecraft returns the pack's Minecraft version from dependencies, if set.
func (m *Manifest) Minecraft() string {
	if m == nil || m.Dependencies == nil {
		return ""
	}
	if v := m.Dependencies["minecraft"]; v != "" {
		return v
	}
	return m.Dependencies["Minecraft"]
}

// Loader returns fabric-loader version (or similar) from dependencies.
func (m *Manifest) Loader() string {
	if m == nil || m.Dependencies == nil {
		return ""
	}
	for _, k := range []string{"fabric-loader", "fabric_loader", "loader", "neoforge", "forge"} {
		if v := m.Dependencies[k]; v != "" {
			return v
		}
	}
	return ""
}

// LoaderKind returns a display name: Fabric, NeoForge, Forge, or empty.
func (m *Manifest) LoaderKind() string {
	if m == nil || m.Dependencies == nil {
		return ""
	}
	switch {
	case m.Dependencies["fabric-loader"] != "" || m.Dependencies["fabric_loader"] != "":
		return "Fabric"
	case m.Dependencies["neoforge"] != "":
		return "NeoForge"
	case m.Dependencies["forge"] != "":
		return "Forge"
	case m.Dependencies["quilt-loader"] != "":
		return "Quilt"
	default:
		return ""
	}
}

// ModCount counts pack files under mods/.
func (m *Manifest) ModCount() int {
	if m == nil {
		return 0
	}
	n := 0
	for _, f := range m.Files {
		if strings.HasPrefix(filepath.ToSlash(f.Path), "mods/") && strings.HasSuffix(strings.ToLower(f.Path), ".jar") {
			n++
		}
	}
	return n
}

// CompareVersions compares dotted numeric versions (1.0.1 vs 1.1.0).
// Returns -1 if a < b, 0 if equal, 1 if a > b. Non-numeric segments compare as strings.
func CompareVersions(a, b string) int {
	a = strings.TrimPrefix(strings.TrimSpace(a), "v")
	b = strings.TrimPrefix(strings.TrimSpace(b), "v")
	if a == b {
		return 0
	}
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var ai, bi string
		if i < len(as) {
			ai = as[i]
		}
		if i < len(bs) {
			bi = bs[i]
		}
		// strip build metadata
		ai = strings.Split(ai, "-")[0]
		bi = strings.Split(bi, "-")[0]
		an, aErr := strconv.Atoi(ai)
		bn, bErr := strconv.Atoi(bi)
		if aErr == nil && bErr == nil {
			if an < bn {
				return -1
			}
			if an > bn {
				return 1
			}
			continue
		}
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
	}
	return 0
}

// FormatPackLine is a short human label: "FOREVER WORLD v1.1.0 (Minecraft 26.2)".
func FormatPackLine(name, version, minecraft string) string {
	s := fmt.Sprintf("%s v%s", name, version)
	if minecraft != "" {
		s += fmt.Sprintf("  ·  Minecraft %s", minecraft)
	}
	return s
}
