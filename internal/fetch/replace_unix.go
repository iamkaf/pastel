//go:build !windows

package fetch

import "os"

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
