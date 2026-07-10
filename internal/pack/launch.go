package pack

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// UseNoGUI reports whether to pass nogui (default true).
func (l *Launch) UseNoGUI() bool {
	if l == nil || l.NoGUI == nil {
		return true
	}
	return *l.NoGUI
}

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
// xmx is e.g. "4G" (without -Xmx). root is the server directory for resolving paths.
func (m *Manifest) BuildJavaArgs(root, xmx string) ([]string, error) {
	if xmx == "" {
		xmx = "4G"
	}
	args := []string{"-Xmx" + xmx}

	l := m.Launch
	if l == nil {
		// Infer a jar from common names
		if jar := findExistingJar(root, commonServerJarNames(m.ResolvedKind())...); jar != "" {
			args = append(args, "-jar", jar)
			if true {
				args = append(args, "nogui")
			}
			return args, nil
		}
		return nil, fmt.Errorf("pack has no launch config and no known server jar was found")
	}

	// Optional JVM args file (Forge/NeoForge user_jvm_args.txt) — skip -Xmx duplication if file sets it;
	// Pastel still passes -Xmx first so friend memory setting wins when the args file allows override.
	if l.JVMArgsFile != "" {
		p := filepath.Join(root, filepath.FromSlash(l.JVMArgsFile))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			args = append(args, "@"+p)
		}
	}

	if l.ArgsFile != "" {
		p := filepath.Join(root, filepath.FromSlash(l.ArgsFile))
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			// Try platform alternate: unix_args.txt <-> win_args.txt
			alt := alternateArgsFile(l.ArgsFile)
			if alt != "" {
				p2 := filepath.Join(root, filepath.FromSlash(alt))
				if st, err := os.Stat(p2); err == nil && !st.IsDir() {
					p = p2
				} else {
					return nil, fmt.Errorf("launch args file not found: %s", l.ArgsFile)
				}
			} else {
				return nil, fmt.Errorf("launch args file not found: %s", l.ArgsFile)
			}
		}
		args = append(args, "@"+p)
		args = append(args, l.ExtraArgs...)
		if l.UseNoGUI() {
			args = append(args, "nogui")
		}
		return args, nil
	}

	if l.Jar != "" {
		p := filepath.Join(root, filepath.FromSlash(l.Jar))
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			// bare name in root
			p = filepath.Join(root, filepath.Base(l.Jar))
			if st, err := os.Stat(p); err != nil || st.IsDir() {
				return nil, fmt.Errorf("launch jar not found: %s", l.Jar)
			}
		}
		args = append(args, l.ExtraArgs...)
		args = append(args, "-jar", p)
		if l.UseNoGUI() {
			args = append(args, "nogui")
		}
		return args, nil
	}

	if l.MainClass != "" {
		args = append(args, l.ExtraArgs...)
		args = append(args, l.MainClass)
		if l.UseNoGUI() {
			args = append(args, "nogui")
		}
		return args, nil
	}

	return nil, fmt.Errorf("launch config needs jar, argsFile, or mainClass")
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

func commonServerJarNames(kind string) []string {
	switch kind {
	case "fabric":
		return []string{} // prefix scan elsewhere
	case "quilt":
		return []string{"quilt-server-launch.jar", "server.jar"}
	case "neoforge", "forge":
		return []string{"server.jar", "forge-server.jar"}
	default:
		return []string{"server.jar"}
	}
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
