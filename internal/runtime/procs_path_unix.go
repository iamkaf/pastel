//go:build !windows

package runtime

import (
	"path/filepath"
	"strings"
)

// commandContainsPath reports whether cmd mentions absRoot or a path inside it.
// A match must end at a path boundary, so /srv/mc does not claim /srv/mc-test.
func commandContainsPath(cmd, absRoot string) bool {
	if absRoot == "" {
		return false
	}
	root := strings.TrimRight(filepath.ToSlash(absRoot), "/")
	if root == "" {
		return false
	}
	for rest := cmd; ; {
		i := strings.Index(rest, root)
		if i < 0 {
			return false
		}
		end := i + len(root)
		if end == len(rest) || rest[end] == '/' || rest[end] == ' ' {
			return true
		}
		rest = rest[i+1:]
	}
}
