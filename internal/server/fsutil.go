package server

import (
	"os"
	"path/filepath"
)

// ensureWritable verifies the data directory exists and can be written. It is
// the Milestone 1 stand-in for the "SQLite writable" check in §30; when the
// store lands in Milestone 4 this becomes a real database ping.
func ensureWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".writecheck-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(filepath.Clean(name))
}
