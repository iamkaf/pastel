// Package author builds and publishes .mrpack artifacts for Kaf Maven.
package author

import (
	"archive/zip"
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/iamkaf/pastel/internal/pack"
)

// BuildOptions configures pack build.
type BuildOptions struct {
	ServerDir string
	// Mrpack is a path to modrinth.index.json (required), or a directory containing it.
	Mrpack  string
	Name    string
	Slug    string
	Version string
	OutDir  string
	Group   string // default com.iamkaf.modpacks
	Side    string // default server
}

// BuildResult paths written.
type BuildResult struct {
	OutDir     string
	MrpackPath string
	POM        string
	Index      *pack.MrpackIndex
	FileCount  int
}

// Build creates a .mrpack (index + server-overrides from server/config), pom, and publish metadata.
func Build(opt BuildOptions) (*BuildResult, error) {
	if opt.Slug == "" || opt.Version == "" || opt.OutDir == "" {
		return nil, fmt.Errorf("slug, version, and out are required")
	}
	if opt.Mrpack == "" {
		return nil, fmt.Errorf("mrpack is required (path to modrinth.index.json or pack directory)")
	}
	if opt.Name == "" {
		opt.Name = opt.Slug
	}
	if opt.Group == "" {
		opt.Group = "com.iamkaf.modpacks"
	}
	if err := validateMavenPart("slug", opt.Slug, false); err != nil {
		return nil, err
	}
	if err := validateMavenPart("version", opt.Version, true); err != nil {
		return nil, err
	}
	if err := validateMavenGroup(opt.Group); err != nil {
		return nil, err
	}
	if opt.Side == "" {
		opt.Side = "server"
	}

	out, err := filepath.Abs(opt.OutDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}

	// Load source index
	idxPath, err := resolveIndexPath(opt.Mrpack)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(idxPath)
	if err != nil {
		return nil, err
	}
	idx, err := pack.ParseMrpackIndex(data)
	if err != nil {
		return nil, err
	}
	// Author overrides for display identity
	idx.Name = opt.Name
	idx.VersionID = opt.Version
	if idx.Game == "" {
		idx.Game = "minecraft"
	}
	if idx.FormatVersion == 0 {
		idx.FormatVersion = 1
	}

	// Serialize index
	indexBytes, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return nil, err
	}
	indexBytes = append(indexBytes, '\n')

	mrpackName := fmt.Sprintf("%s-%s.mrpack", opt.Slug, opt.Version)
	mrpackPath := filepath.Join(out, mrpackName)
	if err := writeMrpackZip(mrpackPath, indexBytes, opt.ServerDir); err != nil {
		return nil, err
	}
	sum, err := hashFileSHA512(mrpackPath)
	if err != nil {
		return nil, err
	}
	if err := writeSHA512(mrpackPath, sum); err != nil {
		return nil, err
	}

	pomPath := filepath.Join(out, fmt.Sprintf("%s-%s.pom", opt.Slug, opt.Version))
	pom := minimalPOM(opt.Group, opt.Slug, opt.Version, opt.Name)
	if err := os.WriteFile(pomPath, []byte(pom), 0o644); err != nil {
		return nil, err
	}
	if err := writeSHA512(pomPath, sha512Hex([]byte(pom))); err != nil {
		return nil, err
	}

	metaPath := filepath.Join(out, "maven-metadata.xml")
	meta := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<metadata>
  <groupId>%s</groupId>
  <artifactId>%s</artifactId>
  <versioning>
    <latest>%s</latest>
    <release>%s</release>
    <versions>
      <version>%s</version>
    </versions>
  </versioning>
</metadata>
`, xmlText(opt.Group), xmlText(opt.Slug), xmlText(opt.Version), xmlText(opt.Version), xmlText(opt.Version))
	if err := os.WriteFile(metaPath, []byte(meta), 0o644); err != nil {
		return nil, err
	}

	metaJSON := map[string]any{
		"group":    opt.Group,
		"artifact": opt.Slug,
		"version":  opt.Version,
		"packFile": mrpackName,
		"pom":      filepath.Base(pomPath),
	}
	mb, _ := json.MarshalIndent(metaJSON, "", "  ")
	_ = os.WriteFile(filepath.Join(out, "publish.json"), append(mb, '\n'), 0o644)

	return &BuildResult{
		OutDir:     out,
		MrpackPath: mrpackPath,
		POM:        pomPath,
		Index:      idx,
		FileCount:  len(idx.Files),
	}, nil
}

func resolveIndexPath(p string) (string, error) {
	st, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		cand := filepath.Join(p, "modrinth.index.json")
		if _, err := os.Stat(cand); err != nil {
			return "", fmt.Errorf("directory %s has no modrinth.index.json", p)
		}
		return cand, nil
	}
	return p, nil
}

func writeMrpackZip(dest string, indexBytes []byte, serverDir string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)

	zw, err := w.Create("modrinth.index.json")
	if err != nil {
		_ = w.Close()
		return err
	}
	if _, err := zw.Write(indexBytes); err != nil {
		_ = w.Close()
		return err
	}

	// server-overrides from production server config (and optional defaultconfigs)
	if serverDir != "" {
		server, err := filepath.Abs(serverDir)
		if err != nil {
			_ = w.Close()
			return err
		}
		for _, pair := range []struct{ src, zipPrefix string }{
			{filepath.Join(server, "config"), "server-overrides/config"},
			{filepath.Join(server, "defaultconfigs"), "server-overrides/defaultconfigs"},
		} {
			if st, err := os.Stat(pair.src); err != nil || !st.IsDir() {
				continue
			}
			if err := addDirToZip(w, pair.src, pair.zipPrefix, skipConfig); err != nil {
				_ = w.Close()
				return err
			}
		}
	}

	return w.Close()
}

func addDirToZip(w *zip.Writer, srcDir, zipPrefix string, skip func(string, os.FileInfo) bool) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if skip != nil && skip(path, info) {
			if info.IsDir() && path != srcDir {
				return filepath.SkipDir
			}
			if !info.IsDir() {
				return nil
			}
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := filepath.ToSlash(filepath.Join(zipPrefix, rel))
		if info.IsDir() {
			_, err := w.Create(name + "/")
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symbolic link in pack overrides: %s", path)
		}
		zw, err := w.Create(name)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(zw, in)
		in.Close()
		return copyErr
	})
}

func skipConfig(path string, info os.FileInfo) bool {
	base := filepath.Base(path)
	if base == ".DS_Store" || strings.HasPrefix(base, "._") {
		return true
	}
	if strings.HasSuffix(base, ".bak") {
		return true
	}
	return false
}

func hashFileSHA512(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha512.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sha512Hex(b []byte) string {
	sum := sha512.Sum512(b)
	return hex.EncodeToString(sum[:])
}

func writeSHA512(path, hexSum string) error {
	return os.WriteFile(path+".sha512", []byte(hexSum+"  "+filepath.Base(path)+"\n"), 0o644)
}

func minimalPOM(group, artifact, version, name string) string {
	if name == "" {
		name = artifact
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 https://maven.apache.org/xsd/maven-4.0.0.xsd">
  <modelVersion>4.0.0</modelVersion>
  <groupId>%s</groupId>
  <artifactId>%s</artifactId>
  <version>%s</version>
  <packaging>pom</packaging>
  <name>%s</name>
  <description>Minecraft modpack (.mrpack) published for Pastel</description>
</project>
`, xmlText(group), xmlText(artifact), xmlText(version), xmlText(name))
}

var mavenPartPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.+-]*$`)

func validateMavenPart(field, value string, allowPlus bool) error {
	if value == "" || value == "." || value == ".." || !mavenPartPattern.MatchString(value) {
		return fmt.Errorf("%s %q is not a safe Maven identifier", field, value)
	}
	if !allowPlus && strings.Contains(value, "+") {
		return fmt.Errorf("%s %q is not a safe Maven identifier", field, value)
	}
	return nil
}

func validateMavenGroup(group string) error {
	if group == "" {
		return fmt.Errorf("group is required")
	}
	for _, part := range strings.Split(group, ".") {
		if err := validateMavenPart("group", part, false); err != nil {
			return err
		}
	}
	return nil
}

func xmlText(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}
