// Package httpx holds Pastel's outbound HTTP conventions: the shared client
// timeout and the Pastel User-Agent header.
package httpx

import (
	"net/http"
	"time"

	"github.com/iamkaf/pastel/internal/buildinfo"
)

// Client returns an HTTP client with Pastel's default timeout.
func Client() *http.Client {
	return &http.Client{Timeout: 10 * time.Minute}
}

// NewRequest builds a request with the Pastel User-Agent set.
func NewRequest(method, rawURL string) (*http.Request, error) {
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", buildinfo.UserAgent())
	return req, nil
}
