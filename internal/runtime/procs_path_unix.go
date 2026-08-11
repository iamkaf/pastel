//go:build !windows

package runtime

import (
	"path/filepath"
	"strings"
)

func commandContainsPath(cmd, absRoot string) bool {
	if absRoot == "" {
		return false
	}
	if strings.Contains(cmd, absRoot) {
		return true
	}
	slash := filepath.ToSlash(absRoot)
	return slash != absRoot && strings.Contains(cmd, slash)
}
