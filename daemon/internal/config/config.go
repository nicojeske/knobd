// Package config loads, saves, and migrates knobd's on-disk
// configuration (~/.config/knobd/config.json). The document shape lives
// in daemon/internal/model; this package only deals with getting bytes
// on and off disk safely and keeping old files loadable.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/njeske/knobd/internal/model"
)

// DirName is the directory under $XDG_CONFIG_HOME (or ~/.config) that
// holds knobd's configuration.
const DirName = "knobd"

// FileName is the configuration file within DirName.
const FileName = "config.json"

// Path returns the absolute path to the config file, honoring
// $XDG_CONFIG_HOME per the XDG Base Directory spec, falling back to
// ~/.config.
func Path() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DirName, FileName), nil
}

func configDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return xdg, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config"), nil
}

// Load reads and migrates the config at path. If the file does not
// exist, Load returns model.Default() and no error — callers that need
// to distinguish "file absent" from "file present" should stat the path
// themselves first.
func Load(path string) (model.Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return model.Default(), nil
	}
	if err != nil {
		return model.Config{}, fmt.Errorf("config: read %s: %w", path, err)
	}

	// Decode into a generic document, not model.Config, before
	// migrating: a version older than CurrentSchemaVersion may have
	// fields the current model.Config struct has no place for, and
	// those need to survive into Migrate, not be dropped by decoding
	// straight into today's shape first.
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return model.Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}

	doc, err = Migrate(doc)
	if err != nil {
		return model.Config{}, fmt.Errorf("config: migrate %s: %w", path, err)
	}

	cfg, err := decode(doc)
	if err != nil {
		return model.Config{}, fmt.Errorf("config: decode %s after migration: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return model.Config{}, fmt.Errorf("config: %s failed validation after migration: %w", path, err)
	}

	return cfg, nil
}

// decode re-marshals a migrated document (already at
// model.CurrentSchemaVersion, so every field model.Config expects is
// present under the name it expects) into a model.Config.
func decode(doc map[string]any) (model.Config, error) {
	data, err := json.Marshal(doc)
	if err != nil {
		return model.Config{}, fmt.Errorf("marshal migrated document: %w", err)
	}
	var cfg model.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return model.Config{}, err
	}
	return cfg, nil
}

// Save validates cfg, then writes it to path atomically (write to a
// temp file in the same directory, then rename) so a crash or power
// loss mid-write can never leave a truncated or half-written config
// behind. The parent directory is created if it does not exist.
func Save(path string, cfg model.Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config: refusing to save invalid config: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config: create %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".config-*.json.tmp")
	if err != nil {
		return fmt.Errorf("config: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("config: write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("config: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return fmt.Errorf("config: chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("config: rename into place: %w", err)
	}
	return nil
}
