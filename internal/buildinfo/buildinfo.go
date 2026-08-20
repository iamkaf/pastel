// Package buildinfo holds the binary version used by the CLI and HTTP clients.
package buildinfo

import "strings"

// Version is the Pastel version. Release builds set it via -ldflags.
var Version = "0.1.0-dev"

const productURL = "https://kaf.sh/pastel"

// UserAgent is the HTTP User-Agent for outbound Pastel requests.
func UserAgent() string {
	v := strings.TrimSpace(Version)
	if v == "" {
		v = "dev"
	}
	return "Pastel/" + v + " (+" + productURL + ")"
}
