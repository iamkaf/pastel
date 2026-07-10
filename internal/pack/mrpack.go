// Package pack — Modrinth mrpack (modrinth.index.json + .mrpack zip) support.
package pack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Mrpack side constants for env filtering.
const (
	SideServer = "server"
	SideClient = "client"
)

// MrpackIndex is formatVersion 1 of modrinth.index.json.
// Spec: https://support.modrinth.com/en/articles/8802351-modrinth-modpack-format-mrpack
type MrpackIndex struct {
	FormatVersion int               `json:"formatVersion"`
	Game          string            `json:"game"`
	VersionID     string            `json:"versionId"`
	Name          string            `json:"name"`
	Summary       string            `json:"summary,omitempty"`
	Files         []MrpackFile      `json:"files"`
	Dependencies  map[string]string `json:"dependencies"`
}

// MrpackFile is one downloadable path in an mrpack.
type MrpackFile struct {
	Path      string            `json:"path"`
	Hashes    map[string]string `json:"hashes"`
	Env       *MrpackEnv        `json:"env,omitempty"`
	Downloads []string          `json:"downloads"`
	FileSize  int64             `json:"fileSize"`
}

// MrpackEnv is client/server requirement: required | optional | unsupported.
type MrpackEnv struct {
	Client string `json:"client"`
	Server string `json:"server"`
}

// LoadedMrpack is a resolved mrpack on disk (zip and/or index directory).
type LoadedMrpack struct {
	Index *MrpackIndex
	// ZipPath is set when the source is a .mrpack zip.
	ZipPath string
	// DirPath is the directory containing modrinth.index.json and optional override trees
	// (used when loading a bare index or an extracted pack).
	DirPath string
}

// IsMrpackIndexJSON reports whether raw looks like modrinth.index.json (not Pastel schema).
func IsMrpackIndexJSON(data []byte) bool {
	trim := bytes.TrimSpace(data)
	if len(trim) == 0 || trim[0] != '{' {
		return false
	}
	// Quick structural check without full parse.
	var probe struct {
		FormatVersion int    `json:"formatVersion"`
		SchemaVersion int    `json:"schemaVersion"`
		VersionID     string `json:"versionId"`
		Game          string `json:"game"`
	}
	if err := json.Unmarshal(trim, &probe); err != nil {
		return false
	}
	if probe.SchemaVersion != 0 && probe.FormatVersion == 0 {
		return false // Pastel pack
	}
	return probe.FormatVersion > 0 || probe.VersionID != "" || strings.EqualFold(probe.Game, "minecraft")
}

// ParseMrpackIndex unmarshals and lightly validates modrinth.index.json.
func ParseMrpackIndex(data []byte) (*MrpackIndex, error) {
	var idx MrpackIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("mrpack index: %w", err)
	}
	if idx.FormatVersion != 0 && idx.FormatVersion != 1 {
		return nil, fmt.Errorf("unsupported mrpack formatVersion %d (want 1)", idx.FormatVersion)
	}
	if idx.FormatVersion == 0 {
		idx.FormatVersion = 1
	}
	if idx.Name == "" {
		return nil, fmt.Errorf("mrpack: name is required")
	}
	if idx.VersionID == "" {
		return nil, fmt.Errorf("mrpack: versionId is required")
	}
	for i, f := range idx.Files {
		if err := validateMrpackPath(f.Path); err != nil {
			return nil, fmt.Errorf("files[%d]: %w", i, err)
		}
		if len(f.Hashes) == 0 {
			return nil, fmt.Errorf("files[%d]: at least one hash is required", i)
		}
		if len(f.Downloads) == 0 {
			return nil, fmt.Errorf("files[%d]: downloads is required", i)
		}
	}
	return &idx, nil
}

func validateMrpackPath(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return fmt.Errorf("path is required")
	}
	if strings.ContainsRune(p, '\x00') {
		return fmt.Errorf("path must not contain NUL")
	}
	// mrpack paths are portable slash-separated relative paths. Rejecting
	// backslashes avoids treating the same pack differently on Windows.
	if strings.Contains(p, `\`) {
		return fmt.Errorf("path must use forward slashes")
	}
	if strings.HasPrefix(p, "/") || path.IsAbs(p) {
		return fmt.Errorf("path must be relative")
	}
	if len(p) >= 2 && p[1] == ':' { // Windows drive
		return fmt.Errorf("path must not be absolute")
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("path contains an invalid component")
		}
	}
	if path.Clean(p) != p {
		return fmt.Errorf("path must be clean")
	}
	return nil
}

// LoadMrpack loads a .mrpack zip, a modrinth.index.json file, or a directory containing the index.
func LoadMrpack(path string) (*LoadedMrpack, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		indexPath := filepath.Join(path, "modrinth.index.json")
		data, err := os.ReadFile(indexPath)
		if err != nil {
			return nil, fmt.Errorf("mrpack dir: %w", err)
		}
		idx, err := ParseMrpackIndex(data)
		if err != nil {
			return nil, err
		}
		return &LoadedMrpack{Index: idx, DirPath: path}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".mrpack") || isZipBytes(data) {
		return loadMrpackZip(path, data)
	}

	// Bare index JSON
	if IsMrpackIndexJSON(data) {
		idx, err := ParseMrpackIndex(data)
		if err != nil {
			return nil, err
		}
		return &LoadedMrpack{Index: idx, DirPath: filepath.Dir(path)}, nil
	}
	return nil, fmt.Errorf("%s is not a .mrpack or modrinth.index.json", path)
}

func loadMrpackZip(path string, data []byte) (*LoadedMrpack, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("mrpack zip: %w", err)
	}
	var indexData []byte
	for _, zf := range zr.File {
		name := filepath.ToSlash(zf.Name)
		if name == "modrinth.index.json" || strings.HasSuffix(name, "/modrinth.index.json") {
			rc, err := zf.Open()
			if err != nil {
				return nil, err
			}
			indexData, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			break
		}
	}
	if indexData == nil {
		return nil, fmt.Errorf("mrpack zip missing modrinth.index.json")
	}
	idx, err := ParseMrpackIndex(indexData)
	if err != nil {
		return nil, err
	}
	return &LoadedMrpack{Index: idx, ZipPath: path}, nil
}

func isZipBytes(data []byte) bool {
	return len(data) >= 4 && data[0] == 'P' && data[1] == 'K'
}

// IncludeForSide reports whether a file should be installed for side (server|client).
// Optional files are included (friend servers want a complete pack; no interactive picker).
func IncludeForSide(env *MrpackEnv, side string) bool {
	if env == nil {
		return true
	}
	var v string
	switch side {
	case SideClient:
		v = env.Client
	default:
		v = env.Server
	}
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return true
	}
	return v != "unsupported"
}

// ToManifest converts the index into Pastel's internal Manifest for download/sync.
// Only files for the given side are included. Launch is left empty; EnsureLoader fills it.
func (l *LoadedMrpack) ToManifest(side string) *Manifest {
	if l == nil || l.Index == nil {
		return nil
	}
	idx := l.Index
	if side == "" {
		side = SideServer
	}
	m := &Manifest{
		SchemaVersion: SchemaVersion,
		Name:          idx.Name,
		Version:       idx.VersionID,
		Side:          side,
		Dependencies:  map[string]string{},
		Files:         nil,
	}
	for k, v := range idx.Dependencies {
		m.Dependencies[k] = v
	}
	for _, f := range idx.Files {
		if !IncludeForSide(f.Env, side) {
			continue
		}
		hashes := map[string]string{}
		for a, h := range f.Hashes {
			hashes[strings.ToLower(a)] = strings.ToLower(h)
		}
		m.Files = append(m.Files, File{
			Path:      filepath.ToSlash(f.Path),
			Hashes:    hashes,
			Downloads: append([]string{}, f.Downloads...),
			FileSize:  f.FileSize,
		})
	}
	return m
}

// ApplyOverrides copies overrides/ then server-overrides/ (server side) into root.
// client-overrides is ignored on the server side.
// Returns slash-relative paths of files written (for prune allowlisting).
func (l *LoadedMrpack) ApplyOverrides(root string, side string) (written []string, err error) {
	if l == nil {
		return nil, nil
	}
	if side == "" {
		side = SideServer
	}
	layers := []string{"overrides"}
	if side == SideServer {
		layers = append(layers, "server-overrides")
	} else if side == SideClient {
		layers = append(layers, "client-overrides")
	}

	if l.ZipPath != "" {
		for _, layer := range layers {
			paths, err := extractZipPrefix(l.ZipPath, layer+"/", root)
			if err != nil {
				return written, err
			}
			written = append(written, paths...)
		}
		return written, nil
	}
	if l.DirPath != "" {
		for _, layer := range layers {
			src := filepath.Join(l.DirPath, layer)
			paths, err := copyTree(src, root)
			if err != nil {
				return written, err
			}
			written = append(written, paths...)
		}
	}
	return written, nil
}

// OverrideModJars returns basenames of jars written under mods/ by override layers.
func OverrideModJars(written []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, p := range written {
		slash := filepath.ToSlash(p)
		if !strings.HasPrefix(slash, "mods/") {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(slash), ".jar") {
			continue
		}
		base := filepath.Base(slash)
		if _, ok := seen[base]; ok {
			continue
		}
		seen[base] = struct{}{}
		out = append(out, base)
	}
	return out
}

// ListOverrideModJars lists mods/*.jar paths present in override layers without extracting.
func (l *LoadedMrpack) ListOverrideModJars(side string) ([]string, error) {
	if l == nil {
		return nil, nil
	}
	if side == "" {
		side = SideServer
	}
	layers := []string{"overrides"}
	if side == SideServer {
		layers = append(layers, "server-overrides")
	} else if side == SideClient {
		layers = append(layers, "client-overrides")
	}
	var rels []string
	if l.ZipPath != "" {
		zr, err := zip.OpenReader(l.ZipPath)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		for _, layer := range layers {
			prefix := layer + "/"
			for _, zf := range zr.File {
				name := filepath.ToSlash(zf.Name)
				if !strings.HasPrefix(name, prefix) {
					continue
				}
				rel := strings.TrimPrefix(name, prefix)
				if strings.HasSuffix(name, "/") || zf.FileInfo().IsDir() {
					continue
				}
				rels = append(rels, rel)
			}
		}
	} else if l.DirPath != "" {
		for _, layer := range layers {
			src := filepath.Join(l.DirPath, layer)
			_ = filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
				if walkErr != nil || info == nil || info.IsDir() {
					return walkErr
				}
				rel, err := filepath.Rel(src, path)
				if err != nil {
					return err
				}
				rels = append(rels, filepath.ToSlash(rel))
				return nil
			})
		}
	}
	return OverrideModJars(rels), nil
}

func extractZipPrefix(zipPath, prefix, destRoot string) ([]string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	root, err := os.OpenRoot(destRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	var written []string
	prefix = filepath.ToSlash(prefix)
	for _, zf := range zr.File {
		name := filepath.ToSlash(zf.Name)
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rel := strings.TrimPrefix(name, prefix)
		if rel == "" {
			continue
		}
		if err := validateMrpackPath(rel); err != nil {
			return written, fmt.Errorf("override %s: %w", name, err)
		}
		// Skip directory entries
		if strings.HasSuffix(name, "/") || zf.FileInfo().IsDir() {
			if err := root.MkdirAll(filepath.FromSlash(rel), 0o755); err != nil {
				return written, err
			}
			continue
		}
		relPath := filepath.FromSlash(rel)
		if err := root.MkdirAll(filepath.Dir(relPath), 0o755); err != nil {
			return written, err
		}
		rc, err := zf.Open()
		if err != nil {
			return written, err
		}
		out, err := root.OpenFile(relPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			rc.Close()
			return written, err
		}
		_, copyErr := io.Copy(out, rc)
		rc.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return written, copyErr
		}
		if closeErr != nil {
			return written, closeErr
		}
		written = append(written, filepath.ToSlash(rel))
	}
	return written, nil
}

func copyTree(src, destRoot string) ([]string, error) {
	st, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("override source is not a directory: %s", src)
	}
	root, err := os.OpenRoot(destRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	var written []string
	err = filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if err := validateMrpackPath(relSlash); err != nil {
			return fmt.Errorf("override %s: %w", relSlash, err)
		}
		if info.IsDir() {
			return root.MkdirAll(rel, 0o755)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("override %s: symbolic links are not supported", relSlash)
		}
		if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
			return err
		}
		if err := copyFileToRoot(path, root, rel); err != nil {
			return err
		}
		written = append(written, relSlash)
		return nil
	})
	return written, err
}

func copyFileToRoot(src string, root *os.Root, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := root.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// DecodeMrpackBytes loads an mrpack from zip bytes (must contain modrinth.index.json).
func DecodeMrpackBytes(data []byte) (*LoadedMrpack, error) {
	if !isZipBytes(data) {
		if IsMrpackIndexJSON(data) {
			idx, err := ParseMrpackIndex(data)
			if err != nil {
				return nil, err
			}
			return &LoadedMrpack{Index: idx}, nil
		}
		return nil, fmt.Errorf("not an mrpack zip or index JSON")
	}
	// Write is not needed; parse from memory
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var indexData []byte
	for _, zf := range zr.File {
		name := filepath.ToSlash(zf.Name)
		if name == "modrinth.index.json" || strings.HasSuffix(name, "/modrinth.index.json") {
			rc, err := zf.Open()
			if err != nil {
				return nil, err
			}
			indexData, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			break
		}
	}
	if indexData == nil {
		return nil, fmt.Errorf("mrpack zip missing modrinth.index.json")
	}
	idx, err := ParseMrpackIndex(indexData)
	if err != nil {
		return nil, err
	}
	return &LoadedMrpack{Index: idx}, nil
}
