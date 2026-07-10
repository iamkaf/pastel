// Package ui provides friendly, pastel-colored terminal output.
package ui

import (
	"fmt"
	"os"
	"strings"
)

// Pastel palette (truecolor RGB).
var (
	Pink     = rgb(248, 165, 194) // soft pink
	Blue     = rgb(162, 210, 255) // soft sky blue
	Lavender = rgb(198, 180, 232)
	Mint     = rgb(167, 227, 194) // success
	Peach    = rgb(255, 179, 148) // warn
	Coral    = rgb(255, 138, 148) // error
	Muted    = rgb(160, 160, 175)
	White    = rgb(245, 245, 250)

	// Loader brand-ish accents
	Fabric   = rgb(219, 176, 255) // Fabric purple
	NeoForge = rgb(241, 120, 80)  // warm orange
	Forge    = rgb(180, 140, 100) // muted bronze
)

// Loader colors the loader name in a theme-appropriate accent.
func Loader(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "fabric":
		return Fabric(name)
	case "neoforge":
		return NeoForge(name)
	case "forge":
		return Forge(name)
	case "quilt":
		return Lavender(name)
	case "vanilla":
		return Mint(name)
	default:
		if name == "" {
			return ""
		}
		return Blue(name)
	}
}

var enabled = true

func init() {
	enabled = detectColor()
	enableWindowsANSI()
}

// Enabled reports whether color escapes will be emitted.
func Enabled() bool { return enabled }

// SetEnabled forces color on or off (tests, flags).
func SetEnabled(v bool) { enabled = v }

func detectColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("PASTEL_FORCE_COLOR") != "" {
		return true
	}
	// Only color interactive terminals by default.
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		if fi, err := f.Stat(); err == nil {
			if (fi.Mode() & os.ModeCharDevice) != 0 {
				return true
			}
		}
	}
	return false
}

func rgb(r, g, b int) func(string) string {
	return func(s string) string {
		if !enabled || s == "" {
			return s
		}
		return fmt.Sprintf("\033[38;2;%d;%d;%dm%s\033[0m", r, g, b, s)
	}
}

// Bold wraps s in bold if color is on.
func Bold(s string) string {
	if !enabled {
		return s
	}
	return "\033[1m" + s + "\033[0m"
}

// Brand is the product name "Pastel" — always capitalized, bold, pink/blue letters.
func Brand() string {
	if !enabled {
		return "Pastel"
	}
	// bold + truecolor per letter (avoid nested \033[0m wiping bold)
	type ch struct {
		r, g, b int
		c       string
	}
	letters := []ch{
		{248, 165, 194, "P"},
		{162, 210, 255, "a"},
		{248, 165, 194, "s"},
		{162, 210, 255, "t"},
		{248, 165, 194, "e"},
		{162, 210, 255, "l"},
	}
	var b strings.Builder
	for _, L := range letters {
		fmt.Fprintf(&b, "\033[1;38;2;%d;%d;%dm%s", L.r, L.g, L.b, L.c)
	}
	b.WriteString("\033[0m")
	return b.String()
}

// Dim is an alias for muted secondary text.
func Dim(s string) string { return Muted(s) }

// Sprint joins parts with spaces (no trailing newline).
func Sprint(parts ...string) string {
	return strings.Join(parts, " ")
}
