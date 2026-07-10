package author

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/iamkaf/pastel/internal/pack"
)

var fabricLauncherRE = regexp.MustCompile(`^fabric-server-mc\.(.+)-loader\.(.+)-launcher\.(.+)\.jar$`)

// launchProfile is detected server bootstrap for pack build.
type launchProfile struct {
	Launch pack.Launch
	// RootJars are root-level jars to include in files[] (with optional download URL).
	RootJars []rootJar
	// Deps hint for dependencies map
	Deps map[string]string
}

type rootJar struct {
	Name string
	URL  string // optional download
}

// detectLaunch inspects a production server directory for Fabric/Quilt/NeoForge/Forge/vanilla.
func detectLaunch(server string) (launchProfile, error) {
	// Prefer modern args-file loaders (NeoForge / Forge 1.17+)
	if p, deps, ok := findArgsFileLaunch(server); ok {
		return launchProfile{Launch: p, Deps: deps}, nil
	}

	// Fabric installer jar
	if name, url, ok := findFabricLauncher(server); ok {
		return launchProfile{
			Launch:   pack.Launch{Kind: "fabric", Jar: name},
			RootJars: []rootJar{{Name: name, URL: url}},
			Deps:     map[string]string{},
		}, nil
	}

	// Quilt
	for _, n := range []string{"quilt-server-launch.jar"} {
		if fileExists(filepath.Join(server, n)) {
			return launchProfile{
				Launch:   pack.Launch{Kind: "quilt", Jar: n},
				RootJars: []rootJar{{Name: n}},
			}, nil
		}
	}

	// Generic server.jar / forge universal in root
	if name, kind, ok := findGenericServerJar(server); ok {
		return launchProfile{
			Launch:   pack.Launch{Kind: kind, Jar: name},
			RootJars: []rootJar{{Name: name}},
		}, nil
	}

	return launchProfile{}, nil
}

func findFabricLauncher(server string) (name, url string, ok bool) {
	entries, err := os.ReadDir(server)
	if err != nil {
		return "", "", false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, "fabric-server-") && strings.HasSuffix(n, ".jar") {
			m := fabricLauncherRE.FindStringSubmatch(n)
			if m != nil {
				u := "https://meta.fabricmc.net/v2/versions/loader/" + m[1] + "/" + m[2] + "/" + m[3] + "/server/jar"
				return n, u, true
			}
			return n, "", true
		}
	}
	return "", "", false
}

func findArgsFileLaunch(server string) (pack.Launch, map[string]string, bool) {
	// NeoForge: libraries/net/neoforged/neoforge/<ver>/unix_args.txt
	// Forge:    libraries/net/minecraftforge/forge/<ver>/unix_args.txt
	type hit struct {
		kind, ver, argsRel string
	}
	var hits []hit

	neoRoot := filepath.Join(server, "libraries", "net", "neoforged", "neoforge")
	if entries, err := os.ReadDir(neoRoot); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			ver := e.Name()
			for _, base := range []string{pack.PreferredArgsFileName(), "unix_args.txt", "win_args.txt"} {
				rel := filepath.ToSlash(filepath.Join("libraries", "net", "neoforged", "neoforge", ver, base))
				if fileExists(filepath.Join(server, filepath.FromSlash(rel))) {
					hits = append(hits, hit{kind: "neoforge", ver: ver, argsRel: rel})
					break
				}
			}
		}
	}

	forgeRoot := filepath.Join(server, "libraries", "net", "minecraftforge", "forge")
	if entries, err := os.ReadDir(forgeRoot); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			ver := e.Name()
			for _, base := range []string{pack.PreferredArgsFileName(), "unix_args.txt", "win_args.txt"} {
				rel := filepath.ToSlash(filepath.Join("libraries", "net", "minecraftforge", "forge", ver, base))
				if fileExists(filepath.Join(server, filepath.FromSlash(rel))) {
					hits = append(hits, hit{kind: "forge", ver: ver, argsRel: rel})
					break
				}
			}
		}
	}

	if len(hits) == 0 {
		return pack.Launch{}, nil, false
	}
	// Prefer NeoForge over Forge if both somehow exist; take last path sorted by name as newest-ish
	h := hits[len(hits)-1]
	for _, c := range hits {
		if c.kind == "neoforge" {
			h = c
		}
	}

	l := pack.Launch{
		Kind:     h.kind,
		ArgsFile: h.argsRel,
	}
	if fileExists(filepath.Join(server, "user_jvm_args.txt")) {
		l.JVMArgsFile = "user_jvm_args.txt"
	}
	deps := map[string]string{}
	if h.kind == "neoforge" {
		deps["neoforge"] = h.ver
	} else {
		deps["forge"] = h.ver
	}
	return l, deps, true
}

func findGenericServerJar(server string) (name, kind string, ok bool) {
	// Prefer more specific names first
	candidates := []struct{ name, kind string }{
		{"forge-server.jar", "forge"},
		{"server.jar", "vanilla"},
	}
	entries, err := os.ReadDir(server)
	if err != nil {
		return "", "", false
	}
	// neoforge-*-universal.jar / forge-*-universal.jar / forge-*-shim.jar
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
			continue
		}
		n := e.Name()
		lower := strings.ToLower(n)
		if strings.HasPrefix(lower, "neoforge-") && strings.Contains(lower, "universal") {
			return n, "neoforge", true
		}
		if strings.HasPrefix(lower, "forge-") && (strings.Contains(lower, "universal") || strings.Contains(lower, "shim")) {
			return n, "forge", true
		}
	}
	for _, c := range candidates {
		if fileExists(filepath.Join(server, c.name)) {
			return c.name, c.kind, true
		}
	}
	return "", "", false
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
