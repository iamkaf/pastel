// Package jre maps Minecraft versions to required Java majors and manages JREs.
package jre

import (
	"fmt"
	"strconv"
	"strings"
)

// RequireMajor returns the minimum Java major version for a Minecraft version.
//
// Floors (Pastel policy):
//   - Minecraft 26.1+ (year-based Mojang scheme) → Java 25
//   - Minecraft 1.20.5+ → Java 21
//   - Minecraft 1.17+ → Java 17
//   - older → Java 8
//
// Unknown/empty MC versions default to Java 25 (current modern floor).
func RequireMajor(minecraft string) int {
	mc := strings.TrimSpace(minecraft)
	if mc == "" {
		return 25
	}
	// Snapshot-style or weird tokens: try leading number
	parts := strings.Split(mc, ".")
	if len(parts) == 0 {
		return 25
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 25
	}

	// Year-based Java versions (2026+ → 26.x)
	if major >= 26 {
		// 26.1 is the first that requires Java 25
		if major > 26 {
			return 25
		}
		// major == 26
		minor := 0
		if len(parts) > 1 {
			minor, _ = strconv.Atoi(strings.Split(parts[1], "-")[0])
		}
		if minor >= 1 {
			return 25
		}
		// 26.0 if it ever exists — treat as modern
		return 25
	}

	// Classic 1.x numbering
	if major == 1 {
		minor := 0
		patch := 0
		if len(parts) > 1 {
			minor, _ = strconv.Atoi(strings.Split(parts[1], "-")[0])
		}
		if len(parts) > 2 {
			patch, _ = strconv.Atoi(strings.Split(parts[2], "-")[0])
		}
		// 1.20.5+ → 21
		if minor > 20 || (minor == 20 && patch >= 5) {
			return 21
		}
		// 1.17+ → 17
		if minor >= 17 {
			return 17
		}
		return 8
	}

	// Unknown scheme ≥ 2 but < 26: be conservative modern
	if major >= 2 {
		return 21
	}
	return 8
}

// FormatRequirement is a short label for UI.
func FormatRequirement(minecraft string) string {
	maj := RequireMajor(minecraft)
	if minecraft == "" {
		return fmt.Sprintf("Java %d", maj)
	}
	return fmt.Sprintf("Java %d (for Minecraft %s)", maj, minecraft)
}
