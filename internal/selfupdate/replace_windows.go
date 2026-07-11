//go:build windows

package selfupdate

import (
	"os"
	"path/filepath"
)

func replaceExecutable(path string, body []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pastel-update-*.exe")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	oldPath := path + ".old"
	_ = os.Remove(oldPath)
	if err := os.Rename(path, oldPath); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Rename(oldPath, path)
		return err
	}
	_ = os.Remove(oldPath)
	return nil
}
