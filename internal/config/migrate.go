package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// legacyConfigNames are the iroffer-dinoex config filenames detected for
// migration, in precedence order.
var legacyConfigNames = []string{"iroffer.config", "sample.config"}

// Resolve returns the config file to load: an existing dinoga.config wins, then
// a legacy iroffer config (migrated in place), else the canonical path. It
// returns the resolved path and whether a migration was performed.
func Resolve(dir, name string) (path string, migrated bool, err error) {
	if name == "" {
		name = "dinoga.config"
	}
	target := filepath.Join(dir, name)

	if _, statErr := os.Stat(target); statErr == nil {
		return target, false, nil
	}

	for _, legacy := range legacyConfigNames {
		src := filepath.Join(dir, legacy)
		if _, statErr := os.Stat(src); statErr == nil {
			if err := Migrate(src, target); err != nil {
				return "", false, err
			}
			return target, true, nil
		}
	}

	return target, false, nil
}

// Migrate loads the legacy config at src, writes the canonical dinoga config to
// dst, and renames src to src+".bak" so the migration is recoverable.
func Migrate(src, dst string) error {
	c, err := Load(src)
	if err != nil {
		return fmt.Errorf("config: migrate load %s: %w", src, err)
	}
	if err := WriteFile(dst, c); err != nil {
		return fmt.Errorf("config: migrate write %s: %w", dst, err)
	}
	if err := os.Rename(src, src+".bak"); err != nil {
		return fmt.Errorf("config: migrate rename %s: %w", src, err)
	}
	return nil
}

// WriteFile writes c to path atomically (via a temp file + rename).
func WriteFile(path string, c Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".dinoga-config-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := Write(tmp, c); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
