package pack

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ResolvedKind returns launch kind from Launch.Kind or manifest dependencies.
func (m *Manifest) ResolvedKind() string {
	if m != nil && m.Launch != nil && m.Launch.Kind != "" {
		return strings.ToLower(m.Launch.Kind)
	}
	switch m.LoaderKind() {
	case "Fabric":
		return "fabric"
	case "NeoForge":
		return "neoforge"
	case "Forge":
		return "forge"
	case "Quilt":
		return "quilt"
	default:
		return "vanilla"
	}
}

// BuildJavaArgs builds the argument list after the java binary for this pack.
// xmx is e.g. "4G" (without -Xmx); jvmArgs follow it. root resolves launch paths.
func (m *Manifest) BuildJavaArgs(root, xmx string, jvmArgs []string, nogui bool) ([]string, error) {
	if xmx == "" {
		xmx = "4G"
	}
	l := m.Launch
	if l == nil {
		return nil, fmt.Errorf("the pack's loader is not installed yet; run ./pastel refresh")
	}
	args := append([]string{"-Xmx" + xmx}, jvmArgs...)

	// Pastel passes -Xmx first so the server.pastel memory setting wins when the
	// Forge/NeoForge user_jvm_args.txt allows an override.
	if l.JVMArgsFile != "" {
		p := filepath.Join(root, filepath.FromSlash(l.JVMArgsFile))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			args = append(args, "@"+p)
		}
	}

	switch {
	case l.ArgsFile != "":
		p, err := existingArgsFile(root, l.ArgsFile)
		if err != nil {
			return nil, err
		}
		args = append(args, "@"+p)
	case l.Jar != "":
		p := filepath.Join(root, filepath.FromSlash(l.Jar))
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return nil, fmt.Errorf("launch jar not found: %s", l.Jar)
		}
		args = append(args, "-jar", p)
	default:
		return nil, fmt.Errorf("launch config needs a jar or an args file")
	}
	if nogui {
		args = append(args, "nogui")
	}
	return args, nil
}

// existingArgsFile returns rel, or its unix/win counterpart, as an absolute path.
func existingArgsFile(root, rel string) (string, error) {
	for _, candidate := range []string{rel, alternateArgsFile(rel)} {
		if candidate == "" {
			continue
		}
		p := filepath.Join(root, filepath.FromSlash(candidate))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("launch args file not found: %s", rel)
}

func alternateArgsFile(path string) string {
	slash := filepath.ToSlash(path)
	switch {
	case strings.HasSuffix(slash, "unix_args.txt"):
		return strings.TrimSuffix(slash, "unix_args.txt") + "win_args.txt"
	case strings.HasSuffix(slash, "win_args.txt"):
		return strings.TrimSuffix(slash, "win_args.txt") + "unix_args.txt"
	default:
		return ""
	}
}

// PreferredArgsFileName returns the platform-appropriate args file basename.
func PreferredArgsFileName() string {
	if runtime.GOOS == "windows" {
		return "win_args.txt"
	}
	return "unix_args.txt"
}

func findExistingJar(root string, names ...string) string {
	for _, n := range names {
		if n == "" {
			continue
		}
		p := filepath.Join(root, n)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// IsManagedRootJar reports whether name is a root-level server launcher jar
// Pastel may prune when not in the pack (any supported loader).
func IsManagedRootJar(name string) bool {
	lower := strings.ToLower(name)
	if !strings.HasSuffix(lower, ".jar") {
		return false
	}
	prefixes := []string{
		"fabric-server-",
		"quilt-server-",
		"forge-",
		"neoforge-",
		"minecraft_server.",
		"server",
	}
	for _, p := range prefixes {
		if p == "server" {
			if lower == "server.jar" || lower == "forge-server.jar" {
				return true
			}
			continue
		}
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}
