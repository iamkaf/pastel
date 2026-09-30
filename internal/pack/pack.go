// Package pack defines the internal pack model used after an .mrpack is resolved.
package pack

import (
	"fmt"
	"strings"
)

// Manifest is the server-side desired state derived from a Modrinth .mrpack.
type Manifest struct {
	Name         string
	Version      string
	Side         string
	Dependencies map[string]string
	Files        []File
	Launch       *Launch
}

// File is one managed path in the server tree.
type File struct {
	Path      string
	Hashes    map[string]string
	Downloads []string
	FileSize  int64
}

// Launch describes how to start the dedicated server after sync. EnsureLoader sets it.
//
// Supported shapes:
//   - Fabric / Quilt / vanilla / legacy Forge: -jar <jar> [nogui]
//   - NeoForge / modern Forge: @user_jvm_args.txt @libraries/.../unix_args.txt [nogui]
type Launch struct {
	// Kind is fabric | neoforge | forge | quilt | vanilla.
	Kind string
	// Jar is a path relative to server root for -jar launch.
	Jar string
	// ArgsFile is a relative @args file (NeoForge/Forge unix_args.txt or win_args.txt).
	ArgsFile string
	// JVMArgsFile is optional @user_jvm_args.txt (memory often also set via -Xmx by Pastel).
	JVMArgsFile string
}

// Validate checks required fields.
func (m *Manifest) Validate() error {
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
		if len(f.Downloads) == 0 {
			return fmt.Errorf("files[%d]: downloads is required", i)
		}
	}
	return nil
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
