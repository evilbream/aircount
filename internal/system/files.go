package system

import (
	"os"
	"path/filepath"
)

func createTempFile(filename string) (string, error) {
	f, err := os.CreateTemp("", filename)
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		return "", err
	}
	return name, nil

}

// GetOrCreateTempFile get or creates temp file
func GetOrCreateTempFile(filename string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), filename))
	if err != nil {
		return "", err
	}

	if len(matches) == 0 {
		return createTempFile(filename)
	}
	return matches[0], nil

}
