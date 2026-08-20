// Package buildinfo holds the binary version used by the CLI and HTTP clients.
package buildinfo

import (
	"net/http"
	"strings"
	"time"
)

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

// HTTPClient returns a shared HTTP client for outbound requests.
func HTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Minute}
}

// NewRequest builds a GET request with the Pastel User-Agent set.
func NewRequest(method, rawURL string) (*http.Request, error) {
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent())
	return req, nil
}
