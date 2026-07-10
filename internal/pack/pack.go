// Package pack defines the Pastel modpack manifest format and loaders.
package pack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// SchemaVersion is the current manifest schema.
const SchemaVersion = 1

// Manifest is a published modpack desired state.
type Manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	Name          string            `json:"name"`
	Version       string            `json:"version"`
	Side          string            `json:"side,omitempty"` // server | client | both
	Dependencies  map[string]string `json:"dependencies,omitempty"`
	Files         []File            `json:"files"`
	Bundles       []Bundle          `json:"bundles,omitempty"`
	Launch        *Launch           `json:"launch,omitempty"`
}

// File is one managed path in the server tree (mods, launcher jar, …).
type File struct {
	Path      string            `json:"path"`
	Hashes    map[string]string `json:"hashes"`
	Downloads []string          `json:"downloads,omitempty"`
	Maven     string            `json:"maven,omitempty"`
	FileSize  int64             `json:"fileSize,omitempty"`
}

// Bundle is a zip (or zip-in-jar) tree published as one Maven artifact.
// Used for config/ overlays so we do not list every config file in the pack.
type Bundle struct {
	ID        string            `json:"id"`
	Path      string            `json:"path"`             // extract root, e.g. "config"
	Format    string            `json:"format,omitempty"` // "zip" (default)
	Hashes    map[string]string `json:"hashes"`
	Downloads []string          `json:"downloads,omitempty"`
	Maven     string            `json:"maven,omitempty"`     // group:artifact:version:classifier
	Extension string            `json:"extension,omitempty"` // maven file ext, default "jar"
	Mode      string            `json:"mode,omitempty"`      // "replace" (default)
	FileSize  int64             `json:"fileSize,omitempty"`
}

// Launch describes how to start the dedicated server after sync.
//
// Supported shapes:
//   - Fabric / Quilt / simple: -jar <jar> [nogui]
//   - NeoForge / modern Forge: @user_jvm_args.txt @libraries/.../unix_args.txt [nogui]
//   - Main class: -cp ... <mainClass> (rare; use ExtraArgs for classpath)
type Launch struct {
	// Kind is fabric | neoforge | forge | quilt | vanilla (optional; derived from deps if empty).
	Kind string `json:"kind,omitempty"`
	// Jar is a path relative to server root for -jar launch (Fabric, Quilt, some Forge).
	Jar string `json:"jar,omitempty"`
	// ArgsFile is a relative @args file (NeoForge/Forge unix_args.txt or win_args.txt).
	ArgsFile string `json:"argsFile,omitempty"`
	// JVMArgsFile is optional @user_jvm_args.txt (memory often also set via -Xmx by Pastel).
	JVMArgsFile string `json:"jvmArgsFile,omitempty"`
	// MainClass launches without -jar when set (and Jar empty).
	MainClass string `json:"mainClass,omitempty"`
	// ExtraArgs are appended (and may include -cp / classpath pieces).
	ExtraArgs []string `json:"extraArgs,omitempty"`
	// NoGUI when true (default) appends "nogui".
	NoGUI *bool `json:"nogui,omitempty"`
}

// Validate checks required fields.
func (m *Manifest) Validate() error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schemaVersion %d (want %d)", m.SchemaVersion, SchemaVersion)
	}
	if m.Name == "" {
		return fmt.Errorf("manifest name is required")
	}
	if m.Version == "" {
		return fmt.Errorf("manifest version is required")
	}
	for i, f := range m.Files {
		if err := validateMrpackPath(f.Path); err != nil {
			return fmt.Errorf("files[%d]: %w", i, err)
		}
		if len(f.Hashes) == 0 {
			return fmt.Errorf("files[%d]: at least one hash is required", i)
		}
		if len(f.Downloads) == 0 && f.Maven == "" {
			return fmt.Errorf("files[%d]: downloads or maven is required", i)
		}
	}
	for i, b := range m.Bundles {
		if err := validateBundleID(b.ID); err != nil {
			return fmt.Errorf("bundles[%d]: %w", i, err)
		}
		if err := validateMrpackPath(b.Path); err != nil {
			return fmt.Errorf("bundles[%d]: %w", i, err)
		}
		if len(b.Hashes) == 0 {
			return fmt.Errorf("bundles[%d]: at least one hash is required", i)
		}
		if len(b.Downloads) == 0 && b.Maven == "" {
			return fmt.Errorf("bundles[%d]: downloads or maven is required", i)
		}
	}
	return nil
}

func validateBundleID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("id is required")
	}
	if id == "." || id == ".." || strings.ContainsAny(id, `/\\`) || strings.ContainsRune(id, '\x00') {
		return fmt.Errorf("id must be a single safe filename component")
	}
	return nil
}

// Parse unmarshals and validates a manifest from JSON bytes.
func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("pack json: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// DecodeManifestBytes accepts Pastel pack JSON, mrpack index JSON, or a zip
// containing pack.json / modrinth.index.json. Prefer Resolve() for full mrpack
// override support (zip path is required for overrides).
func DecodeManifestBytes(data []byte) (*Manifest, error) {
	trim := bytes.TrimSpace(data)
	if len(trim) > 0 && trim[0] == '{' {
		if IsMrpackIndexJSON(data) {
			idx, err := ParseMrpackIndex(data)
			if err != nil {
				return nil, err
			}
			return (&LoadedMrpack{Index: idx}).ToManifest(SideServer), nil
		}
		return Parse(data)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("pack is neither JSON nor zip: %w", err)
	}
	// Prefer modrinth.index.json (mrpack)
	for _, zf := range zr.File {
		base := zf.Name
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		if base == "modrinth.index.json" {
			rc, err := zf.Open()
			if err != nil {
				return nil, err
			}
			body, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			idx, err := ParseMrpackIndex(body)
			if err != nil {
				return nil, err
			}
			return (&LoadedMrpack{Index: idx}).ToManifest(SideServer), nil
		}
	}
	for _, zf := range zr.File {
		base := zf.Name
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		if base == "pack.json" || base == "forever-world.json" || strings.HasSuffix(base, ".pastel.json") {
			rc, err := zf.Open()
			if err != nil {
				return nil, err
			}
			body, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			return Parse(body)
		}
	}
	// single json entry?
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(zf.Name), ".json") {
			rc, err := zf.Open()
			if err != nil {
				return nil, err
			}
			body, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			if IsMrpackIndexJSON(body) {
				idx, err := ParseMrpackIndex(body)
				if err != nil {
					return nil, err
				}
				return (&LoadedMrpack{Index: idx}).ToManifest(SideServer), nil
			}
			return Parse(body)
		}
	}
	return nil, fmt.Errorf("zip pack has no modrinth.index.json or pack.json")
}

// LoadFile reads a pack manifest from disk (JSON or zip/jar).
func LoadFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeManifestBytes(data)
}

// PreferredHash returns algorithm and hex digest, preferring stronger hashes.
func PreferredHash(hashes map[string]string) (algo, hex string, ok bool) {
	for _, a := range []string{"sha512", "sha256", "sha1"} {
		if h, exists := hashes[a]; exists && h != "" {
			return a, strings.ToLower(h), true
		}
	}
	for a, h := range hashes {
		if h != "" {
			return strings.ToLower(a), strings.ToLower(h), true
		}
	}
	return "", "", false
}

// PreferredHash on File.
func (f File) PreferredHash() (algo, hex string, ok bool) {
	return PreferredHash(f.Hashes)
}

// PreferredHash on Bundle.
func (b Bundle) PreferredHash() (algo, hex string, ok bool) {
	return PreferredHash(b.Hashes)
}

// Ext returns the Maven extension for a bundle.
func (b Bundle) Ext() string {
	if b.Extension != "" {
		return b.Extension
	}
	return "zip"
}
