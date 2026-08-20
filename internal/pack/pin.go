package pack

import (
	"strings"

	"github.com/iamkaf/pastel/internal/modrinth"
)

// PinKind classifies a pack reference.
type PinKind string

const (
	PinURL      PinKind = "url"
	PinFile     PinKind = "file"
	PinModrinth PinKind = "modrinth"
	PinMaven    PinKind = "maven"
	PinPath     PinKind = "path"
)

// ClassifyPin is the single place that distinguishes an https URL, file: URL,
// Modrinth pin (page URL, modrinth: ref, slug shorthand, bare slug), Maven
// coordinate, and local path. It never touches the filesystem.
func ClassifyPin(raw string) PinKind {
	s := strings.TrimSpace(raw)
	if s == "" {
		return PinPath
	}
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
		if _, _, ok := modrinth.ParsePageURL(s); ok {
			return PinModrinth
		}
		return PinURL
	}
	if strings.HasPrefix(low, "file:") {
		return PinFile
	}
	if _, _, ok := modrinth.ParseRef(s); ok {
		return PinModrinth
	}
	if _, _, ok := modrinth.ParseSlugVersion(s); ok {
		return PinModrinth
	}
	if IsMavenCoordinate(s) {
		return PinMaven
	}
	if modrinth.LooksLikeSlug(s) {
		return PinModrinth
	}
	return PinPath
}

// IsMavenCoordinate reports whether s looks like a Maven coordinate
// (group:artifact:version with dotted group).
func IsMavenCoordinate(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, `/\`) {
		return false
	}
	parts := strings.Split(s, ":")
	return len(parts) >= 3 && len(parts) <= 4 && strings.Contains(parts[0], ".")
}

// IsRelativePathPin reports whether raw is a plain filesystem path that should
// be joined relative to the config directory.
func IsRelativePathPin(raw string) bool {
	return strings.TrimSpace(raw) != "" && ClassifyPin(raw) == PinPath
}
