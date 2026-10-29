// Package safefile writes a file so that it holds either its old bytes or its
// new ones. A write that fails half way, as on a full disk, leaves the old file
// as it was.
package safefile

import (
	"os"
	"path/filepath"
)

// WriteFile writes data to a temporary file in the directory of path and
// renames it to path.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, perm)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}
