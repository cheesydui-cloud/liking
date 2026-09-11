package db

import (
	"os"
	"path/filepath"
)

func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0o750)
}

func DataDir(dbPath string) string {
	return filepath.Dir(dbPath)
}
