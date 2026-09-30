package pack

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/iamkaf/pastel/internal/httpx"
	"github.com/iamkaf/pastel/internal/jre"
)

// fabric-server-mc.{mc}-loader.{loader}-launcher.{installer}.jar
var fabricLauncherRE = regexp.MustCompile(`^fabric-server-mc\.(.+)-loader\.(.+)-launcher\.(.+)\.jar$`)

// EnsureLoader installs the loader the pack declares when it is missing and sets
// m.Launch so the server can start. Safe to call after mrpack file sync and overrides.
//
// javaBin is optional; when empty, a managed JRE is ensured for installer runs.
//
// An installed launcher is reused only when it matches the exact Minecraft and loader
// versions in the pack's dependencies, so pack upgrades and loader switches install the
// launcher the pack asks for instead of starting a stale one.
func EnsureLoader(root string, m *Manifest, javaBin string) (changed bool, err error) {
	if m == nil {
		return false, fmt.Errorf("manifest is required")
	}
	kind := m.ResolvedKind()
	mc := m.Minecraft()

	switch kind {
	case "fabric":
		return ensureFabricLoader(root, m)
	case "neoforge", "forge":
		ver := m.Dependencies[kind]
		if ver == "" {
			return false, fmt.Errorf("%s pack needs dependencies.%s", kind, kind)
		}
		artVer := installerArtifactVersion(kind, mc, ver)
		if l, ok := argsFileLaunch(root, kind, artVer); ok {
			m.Launch = &l
			return false, nil
		}
		// Forge before 1.17 has no args file; a pack may ship its server jar instead.
		if name, found, ok := findGenericServerJarName(root); ok && found == kind {
			m.Launch = &Launch{Kind: kind, Jar: name}
			return false, nil
		}
		java, err := resolveInstallerJava(root, mc, javaBin)
		if err != nil {
			return false, err
		}
		launch, err := installForgeFamilyServer(root, kind, artVer, java)
		if err != nil {
			return false, err
		}
		m.Launch = &launch
		return true, nil
	case "quilt":
		if findExistingJar(root, "quilt-server-launch.jar") != "" {
			m.Launch = &Launch{Kind: "quilt", Jar: "quilt-server-launch.jar"}
			return false, nil
		}
		return false, fmt.Errorf("quilt loader install is not automatic yet; include quilt-server-launch.jar in the pack or overrides")
	default:
		if findExistingJar(root, "server.jar") != "" {
			m.Launch = &Launch{Kind: "vanilla", Jar: "server.jar"}
			return false, nil
		}
		return false, fmt.Errorf("vanilla pack needs server.jar in the pack or overrides")
	}
}

func ensureFabricLoader(root string, m *Manifest) (bool, error) {
	mc := m.Minecraft()
	loader := ""
	if m.Dependencies != nil {
		loader = m.Dependencies["fabric-loader"]
		if loader == "" {
			loader = m.Dependencies["fabric_loader"]
		}
	}
	if mc == "" || loader == "" {
		return false, fmt.Errorf("fabric pack needs dependencies.minecraft and fabric-loader")
	}
	jar, did, err := ensureFabricServerJar(root, mc, loader)
	if err != nil {
		return false, err
	}
	m.Launch = &Launch{Kind: "fabric", Jar: jar}
	return did, nil
}

func resolveInstallerJava(root, mc, override string) (string, error) {
	if override != "" && override != "java" {
		return override, nil
	}
	return jre.Ensure(root, jre.RequireMajor(mc), override)
}

// installForgeFamilyServer downloads the official installer and runs --installServer.
func installForgeFamilyServer(root, kind, artVer, java string) (Launch, error) {
	cacheDir := filepath.Join(root, ".pastel", "cache", "installers")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return Launch{}, err
	}
	installerPath := filepath.Join(cacheDir, fmt.Sprintf("%s-%s-installer.jar", kind, artVer))
	if st, err := os.Stat(installerPath); err != nil || st.Size() == 0 {
		if err := downloadFile(installerDownloadURL(kind, artVer), installerPath); err != nil {
			return Launch{}, fmt.Errorf("download %s installer: %w", kind, err)
		}
	}

	// Official installers write libraries/ + unix_args.txt under the server root.
	cmd := exec.Command(java, "-jar", installerPath, "--installServer", ".")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 2000 {
			msg = msg[len(msg)-2000:]
		}
		return Launch{}, fmt.Errorf("%s installer failed: %w\n%s", kind, err, msg)
	}

	// Ensure a minimal user_jvm_args.txt (memory still overridden by Pastel -Xmx).
	ujvm := filepath.Join(root, "user_jvm_args.txt")
	if st, err := os.Stat(ujvm); err != nil || st.IsDir() {
		if err := os.WriteFile(ujvm, []byte("# Managed by Pastel — memory is set via server.pastel\n"), 0o644); err != nil {
			return Launch{}, err
		}
	}

	l, ok := argsFileLaunch(root, kind, artVer)
	if !ok {
		return Launch{}, fmt.Errorf("%s installer did not create an args file for %s", kind, artVer)
	}
	return l, nil
}

func installerArtifactVersion(kind, mc, version string) string {
	if kind == "forge" && mc != "" && !strings.Contains(version, "-") {
		return mc + "-" + version
	}
	return version
}

func installerDownloadURL(kind, artVer string) string {
	switch kind {
	case "neoforge":
		return fmt.Sprintf(
			"https://maven.neoforged.net/releases/net/neoforged/neoforge/%s/neoforge-%s-installer.jar",
			artVer, artVer,
		)
	default: // forge
		return fmt.Sprintf(
			"https://maven.minecraftforge.net/net/minecraftforge/forge/%s/forge-%s-installer.jar",
			artVer, artVer,
		)
	}
}

// argsFileLaunch finds the installed args file for exactly this loader version.
func argsFileLaunch(root, kind, artVer string) (Launch, bool) {
	dir := "libraries/net/minecraftforge/forge/" + artVer
	if kind == "neoforge" {
		dir = "libraries/net/neoforged/neoforge/" + artVer
	}
	for _, base := range []string{PreferredArgsFileName(), "unix_args.txt", "win_args.txt"} {
		rel := dir + "/" + base
		if st, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil && !st.IsDir() {
			l := Launch{Kind: kind, ArgsFile: rel}
			if st, err := os.Stat(filepath.Join(root, "user_jvm_args.txt")); err == nil && !st.IsDir() {
				l.JVMArgsFile = "user_jvm_args.txt"
			}
			return l, true
		}
	}
	return Launch{}, false
}

// findFabricLauncherMatching returns a fabric-server jar for the given MC + loader versions.
func findFabricLauncherMatching(root, mc, loader string) (string, bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if fabricJarMatches(n, mc, loader) {
			return n, true
		}
	}
	return "", false
}

// fabricJarMatches reports whether name is a Fabric server launcher for mc + loader.
// Installer build may differ; we only require mc and loader to match the pack.
func fabricJarMatches(name, mc, loader string) bool {
	m := fabricLauncherRE.FindStringSubmatch(name)
	if m == nil {
		return false
	}
	return m[1] == mc && m[2] == loader
}

// removeOtherFabricLaunchers deletes root fabric-server-*.jar except keep (if non-empty).
func removeOtherFabricLaunchers(root, keep string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasPrefix(n, "fabric-server-") || !strings.HasSuffix(n, ".jar") {
			continue
		}
		if keep != "" && n == keep {
			continue
		}
		_ = os.Remove(filepath.Join(root, n))
	}
}

// findGenericServerJarName finds a pack-provided Forge or NeoForge server jar.
func findGenericServerJarName(root string) (name, kind string, ok bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", "", false
	}
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
	for _, c := range []struct{ name, kind string }{
		{"forge-server.jar", "forge"},
		{"server.jar", "vanilla"},
	} {
		if findExistingJar(root, c.name) != "" {
			return c.name, c.kind, true
		}
	}
	return "", "", false
}

func ensureFabricServerJar(root, mc, loader string) (jarName string, changed bool, err error) {
	// Reuse only a launcher that matches this pack's Minecraft + Fabric Loader.
	if name, ok := findFabricLauncherMatching(root, mc, loader); ok {
		removeOtherFabricLaunchers(root, name)
		return name, false, nil
	}
	// Drop stale launchers from older pack versions before installing.
	removeOtherFabricLaunchers(root, "")

	installer, err := latestFabricInstaller()
	if err != nil {
		return "", false, err
	}
	jarName = fmt.Sprintf("fabric-server-mc.%s-loader.%s-launcher.%s.jar", mc, loader, installer)
	dest := filepath.Join(root, jarName)
	url := fmt.Sprintf(
		"https://meta.fabricmc.net/v2/versions/loader/%s/%s/%s/server/jar",
		mc, loader, installer,
	)
	if err := downloadFile(url, dest); err != nil {
		return "", false, fmt.Errorf("download Fabric server jar: %w", err)
	}
	return jarName, true, nil
}

func latestFabricInstaller() (string, error) {
	req, err := httpx.NewRequest(http.MethodGet, "https://meta.fabricmc.net/v2/versions/installer")
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 60 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fabric installer meta: %s", res.Status)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	var list []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "", fmt.Errorf("fabric installer meta empty")
	}
	for _, e := range list {
		if e.Stable && e.Version != "" {
			return e.Version, nil
		}
	}
	return list[0].Version, nil
}

func downloadFile(rawURL, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".pastel-tmp"
	defer os.Remove(tmp)

	req, err := httpx.NewRequest(http.MethodGet, rawURL)
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout: 10 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", rawURL, res.Status)
	}
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, res.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp, dest)
}
