//go:build windows

package runtime

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func commandContainsPath(cmd, absRoot string) bool {
	if absRoot == "" {
		return false
	}
	args, err := windows.DecomposeCommandLine(cmd)
	if err != nil {
		return false
	}
	root := filepath.Clean(absRoot)
	for _, arg := range args {
		candidate := strings.TrimPrefix(arg, "@")
		if !filepath.IsAbs(candidate) {
			continue
		}
		candidate = filepath.Clean(candidate)
		if strings.EqualFold(candidate, root) {
			return true
		}
		prefix := strings.TrimRight(root, `\\/`) + string(filepath.Separator)
		if strings.HasPrefix(strings.ToLower(candidate), strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}
