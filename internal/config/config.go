// Package config loads the friend-facing server.pastel instance file.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const DefaultFileName = "server.pastel"

// Config is the local instance file. It points at a pack and runtime knobs.
type Config struct {
	// Pack is a .mrpack path/URL, a Modrinth index path, a Maven coordinate
	// (com.iamkaf.modpacks:slug:version → .mrpack only), or a file: URL.
	Pack string `toml:"pack"`

	// Memory is the -Xmx value (e.g. "4G"). Empty uses launch default or 4G.
	Memory string `toml:"memory"`

	// Java is the java executable. Empty means "java" on PATH.
	Java string `toml:"java"`

	// Repositories is an ordered list of Maven repository base URLs used when
	// pack is a short coordinate (group:artifact:version). First hit wins.
	// There is NO default host — short coordinates require an explicit list
	// (or pin pack to a full https://…/.mrpack URL / local path).
	//
	//	repositories = ["https://maven.example.com"]
	Repositories []string `toml:"repositories"`

	// ServerDir is the Minecraft server root. Empty means the directory
	// containing server.pastel.
	ServerDir string `toml:"server_dir"`

	// ExtraJavaArgs are appended to the java command before -jar.
	ExtraJavaArgs []string `toml:"extra_java_args"`

	// NoGUI passes "nogui" to the server jar (default true when unset via Load defaults).
	NoGUI *bool `toml:"nogui"`

	// SyncOnRun, when false, makes ./pastel run start the server without
	// re-downloading pack files or re-applying overrides. Use this while
	// debugging local mods/ (e.g. deleting jars you don't want reintroduced).
	// Default true. ./pastel refresh always syncs regardless.
	//
	//	sync_on_run = false
	SyncOnRun *bool `toml:"sync_on_run"`

	// AutoRestart restarts a background server after it reached readiness and
	// then exited unexpectedly. Default true; Load writes the default into
	// older config files so the option is discoverable.
	AutoRestart *bool `toml:"auto_restart"`

	// path is the absolute path to the config file (not in TOML).
	path string
}

// Path returns the absolute path of the loaded config file.
func (c *Config) Path() string { return c.path }

// Root returns the absolute server root directory.
func (c *Config) Root() string {
	if c.ServerDir != "" {
		if filepath.IsAbs(c.ServerDir) {
			return filepath.Clean(c.ServerDir)
		}
		return filepath.Clean(filepath.Join(filepath.Dir(c.path), c.ServerDir))
	}
	return filepath.Dir(c.path)
}

// JavaBin returns the java executable to use.
func (c *Config) JavaBin() string {
	if c.Java != "" {
		return c.Java
	}
	return "java"
}

// Xmx returns the memory flag value without -Xmx prefix.
func (c *Config) Xmx() string {
	if c.Memory != "" {
		return strings.TrimPrefix(c.Memory, "-Xmx")
	}
	return "4G"
}

// UseNoGUI reports whether to pass nogui.
func (c *Config) UseNoGUI() bool {
	if c.NoGUI == nil {
		return true
	}
	return *c.NoGUI
}

// ShouldSyncOnRun reports whether ./pastel run should refresh pack files first.
// Default true when unset.
func (c *Config) ShouldSyncOnRun() bool {
	if c == nil || c.SyncOnRun == nil {
		return true
	}
	return *c.SyncOnRun
}

// ShouldAutoRestart reports whether crashed background servers should restart.
// Default true when unset.
func (c *Config) ShouldAutoRestart() bool {
	if c == nil || c.AutoRestart == nil {
		return true
	}
	return *c.AutoRestart
}

// MavenRepositories returns the ordered Maven base URLs for short coordinates.
// Empty when unset — Pastel does not invent a host.
func (c *Config) MavenRepositories() []string {
	if c == nil {
		return nil
	}
	return normalizeRepos(c.Repositories)
}

func normalizeRepos(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, b := range in {
		b = strings.TrimRight(strings.TrimSpace(b), "/")
		if b == "" {
			continue
		}
		if _, ok := seen[b]; ok {
			continue
		}
		seen[b] = struct{}{}
		out = append(out, b)
	}
	return out
}

// Load reads server.pastel from path (file or directory containing it).
func Load(path string) (*Config, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("server.pastel: %w", err)
	}
	if st.IsDir() {
		path = filepath.Join(path, DefaultFileName)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", abs, err)
	}
	var c Config
	if err := toml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", abs, err)
	}
	c.path = abs
	if strings.TrimSpace(c.Pack) == "" {
		return nil, fmt.Errorf("%s: pack is required", abs)
	}
	if c.AutoRestart == nil {
		if err := appendAutoRestartDefault(abs, data); err != nil {
			return nil, fmt.Errorf("add auto_restart to %s: %w", abs, err)
		}
		enabled := true
		c.AutoRestart = &enabled
	}
	return &c, nil
}

func appendAutoRestartDefault(path string, data []byte) error {
	var b strings.Builder
	b.Write(data)
	if len(data) > 0 && data[len(data)-1] != '\n' {
		b.WriteByte('\n')
	}
	b.WriteString("# Restart the server after an unexpected crash.\n")
	b.WriteString("auto_restart = true\n")
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(path, []byte(b.String()), mode)
}

// WriteOptions creates or overwrites a friend-facing server.pastel.
type WriteOptions struct {
	Path         string   // file path (required)
	Pack         string   // pack pin (required)
	Memory       string   // default 4G
	Repositories []string // optional Maven bases
}

// Write creates a minimal server.pastel for install / first-time setup.
func Write(opt WriteOptions) error {
	if opt.Path == "" {
		return fmt.Errorf("config path required")
	}
	if strings.TrimSpace(opt.Pack) == "" {
		return fmt.Errorf("pack is required")
	}
	if opt.Memory == "" {
		opt.Memory = "4G"
	}
	var b strings.Builder
	b.WriteString("# Pastel server pin — https://kaf.sh\n")
	b.WriteString("pack = " + tomlQuoted(opt.Pack) + "\n")
	b.WriteString("memory = " + tomlQuoted(opt.Memory) + "\n")
	// Explicit default so friends discover they can flip it while debugging mods/.
	b.WriteString("# Set false so ./pastel run starts without re-downloading the pack.\n")
	b.WriteString("sync_on_run = true\n")
	b.WriteString("# Restart the server after an unexpected crash.\n")
	b.WriteString("auto_restart = true\n")
	if repos := normalizeRepos(opt.Repositories); len(repos) > 0 {
		b.WriteString("repositories = [\n")
		for _, r := range repos {
			b.WriteString("  " + tomlQuoted(r) + ",\n")
		}
		b.WriteString("]\n")
	}
	if err := os.MkdirAll(filepath.Dir(opt.Path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(opt.Path, []byte(b.String()), 0o644)
}

// SetPack rewrites the pack = "…" line in server.pastel and updates c.Pack.
func (c *Config) SetPack(coord string) error {
	if c.path == "" {
		return fmt.Errorf("config path unknown")
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "pack") && strings.Contains(trim, "=") {
			// preserve indentation
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + "pack = " + tomlQuoted(coord)
			found = true
			break
		}
	}
	if !found {
		// prepend pack line
		lines = append([]string{"pack = " + tomlQuoted(coord), ""}, lines...)
	}
	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	if err := os.WriteFile(c.path, []byte(out), 0o644); err != nil {
		return err
	}
	c.Pack = coord
	return nil
}

func tomlQuoted(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Find walks from startDir upward looking for server.pastel, then tries startDir itself.
func Find(startDir string) (string, error) {
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	dir := abs
	for {
		candidate := filepath.Join(dir, DefaultFileName)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("no %s found from %s", DefaultFileName, abs)
}
