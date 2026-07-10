package pack

import (
	"fmt"
	"strings"

	"github.com/iamkaf/pastel/internal/maven"
)

// Ref is a pack identity on Maven (group + artifact + optional pinned version).
type Ref struct {
	Group    string
	Artifact string
	Version  string // empty or "latest" means track release channel
	Raw      string
}

// ParseRef parses group:artifact:version or group:artifact.
// Local paths / file: URLs return ok=false.
func ParseRef(raw string) (Ref, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "file:") || strings.Contains(raw, "/") || strings.Contains(raw, `\`) {
		return Ref{}, false
	}
	parts := strings.Split(raw, ":")
	if len(parts) < 2 || len(parts) > 4 {
		return Ref{}, false
	}
	if !strings.Contains(parts[0], ".") {
		return Ref{}, false
	}
	r := Ref{Group: parts[0], Artifact: parts[1], Raw: raw}
	if len(parts) >= 3 {
		r.Version = parts[2]
	}
	return r, true
}

// Coordinate builds group:artifact:version.
func (r Ref) Coordinate(version string) string {
	if version == "" {
		version = r.Version
	}
	return r.Group + ":" + r.Artifact + ":" + version
}

// CheckResult is the outcome of looking for a pack upgrade.
type CheckResult struct {
	Ref             Ref
	InstalledVer    string
	InstalledMC     string
	PinnedVer       string
	LatestVer       string
	LatestMC        string
	LatestName      string
	LatestManifest  *Manifest
	UpdateAvailable bool
	// TargetVer is what update would install (latest when newer than installed/pin baseline).
	TargetVer string
}

// CheckUpdate queries Maven for the latest release of ref's artifact.
// baseline is the version we consider "current" (installed pack version preferred).
// repositories must be non-empty (no default host).
func CheckUpdate(repositories []string, ref Ref, baselineVer string) (*CheckResult, error) {
	repos := maven.NormalizeRepositories(repositories)
	if len(repos) == 0 {
		return nil, maven.ErrNoRepositories
	}
	cl := maven.NewClient(repos...)
	latest, err := cl.LatestVersion(ref.Group, ref.Artifact)
	if err != nil {
		return nil, fmt.Errorf("check latest pack version: %w", err)
	}
	coord := maven.Coordinate{Group: ref.Group, Artifact: ref.Artifact, Version: latest}
	data, err := cl.FetchPack(coord)
	if err != nil {
		return nil, fmt.Errorf("fetch latest pack %s: %w", latest, err)
	}
	m, err := DecodeManifestBytes(data)
	if err != nil {
		return nil, err
	}
	res := &CheckResult{
		Ref:            ref,
		InstalledVer:   baselineVer,
		PinnedVer:      ref.Version,
		LatestVer:      latest,
		LatestMC:       m.Minecraft(),
		LatestName:     m.Name,
		LatestManifest: m,
		TargetVer:      latest,
	}
	if baselineVer == "" {
		// Nothing installed — treat any latest as available to install via update,
		// but pin version may still be applied via sync.
		res.UpdateAvailable = true
		return res, nil
	}
	res.UpdateAvailable = CompareVersions(baselineVer, latest) < 0
	return res, nil
}
