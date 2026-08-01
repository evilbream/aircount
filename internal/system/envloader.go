package system

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func EnsureEnvLoaded(envFile ...string) error {
	filename := ".env"
	if len(envFile) > 0 {
		filename = envFile[0]
	}
	return LoadEnv(filename)

}

// find in root
func findFileInProjectRoot(filename string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, filename)); err == nil {
			return dir, nil // Found the project root
		}
		parentDir := filepath.Dir(dir)
		if parentDir == dir { // Reached the root of the filesystem
			break
		}
		dir = parentDir
	}
	return "", os.ErrNotExist // file not found in project root
}

// LoadEnv loads environment variables from a .env file. If the file is not found in the current directory,
// it searches for it in the project root directory.
func LoadEnv(envFilename string) error {
	f, err := os.Open(envFilename)
	if err != nil {
		rootDir, rootErr := findFileInProjectRoot(envFilename)
		if rootErr != nil {
			return rootErr // Return error if project root not found
		}

		f, err = os.Open(filepath.Join(rootDir, envFilename))
		if err != nil {
			return err
		}
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])

		if _, exists := os.LookupEnv(key); exists {
			continue // Skip if the key is already set
		}

		value := strings.TrimSpace(parts[1])

		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func EnvInt(key string, def int) int {
	cleaned := strings.TrimSpace(key)
	if cleaned == "" {
		return def
	}
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
}

// EnvDuration: a bare number is treated as seconds ("30"), or Go's
// duration syntax is accepted ("30s", "2m"). An invalid non-empty
// value is logged and replaced with def.
func EnvDuration(key string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	if v, err := strconv.Atoi(raw); err == nil && v > 0 {
		return time.Duration(v) * time.Second
	}
	// Otherwise try Go's duration syntax: "1s", "1ms", "2m", "1h30m".
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	return def
}

func EnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func EnvList(key string, def []string) []string {
	if v := os.Getenv(key); v != "" {
		return strings.Split(v, ",")
	}
	return def
}
