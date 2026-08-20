package pack

import (
	"strings"

	"github.com/iamkaf/pastel/internal/modrinth"
)

// PinKind classifies a pack reference without re-parsing in multiple places.
type PinKind string

const (
	PinURL      PinKind = "url"
	PinFile     PinKind = "file"
	PinModrinth PinKind = "modrinth"
	PinMaven    PinKind = "maven"
	PinSlug     PinKind = "slug"
	PinPath     PinKind = "path"
)

// Pin is a parsed pack reference. It is the single place that knows how to
// distinguish a URL, file: URL, modrinth: pin, slug shorthand, Maven
// coordinate, and local path.
type Pin struct {
	Kind PinKind
	Raw  string
}

// ParsePin classifies raw without touching the filesystem.
func ParsePin(raw string) Pin {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Pin{Kind: PinPath, Raw: raw}
	}
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
		if _, _, ok := modrinth.ParsePageURL(s); ok {
			return Pin{Kind: PinModrinth, Raw: s}
		}
		return Pin{Kind: PinURL, Raw: s}
	}
	if strings.HasPrefix(low, "file:") {
		return Pin{Kind: PinFile, Raw: s}
	}
	if strings.HasPrefix(low, "modrinth:") {
		return Pin{Kind: PinModrinth, Raw: s}
	}
	if _, _, ok := modrinth.ParseSlugVersion(s); ok {
		return Pin{Kind: PinModrinth, Raw: s}
	}
	if modrinth.LooksLikeSlug(s) {
		return Pin{Kind: PinModrinth, Raw: s}
	}
	if IsMavenCoordinate(s) {
		return Pin{Kind: PinMaven, Raw: s}
	}
	// Anything else is treated as a filesystem path (relative or absolute).
	return Pin{Kind: PinPath, Raw: s}
}

// IsMavenCoordinate reports whether s looks like a Maven coordinate
// (group:artifact:version with dotted group).
func IsMavenCoordinate(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.Contains(s, "/") || strings.Contains(s, `\`) || strings.HasPrefix(s, "file:") {
		return false
	}
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "modrinth:") {
		return false
	}
	parts := strings.Split(s, ":")
	return len(parts) >= 3 && len(parts) <= 4 && strings.Contains(parts[0], ".")
}

// IsRelativePathPin reports whether raw is a plain filesystem path that should
// be joined relative to the config directory.
func IsRelativePathPin(raw string) bool {
	pin := ParsePin(raw)
	return pin.Kind == PinPath
}
