package config

import (
	"os"
	"path/filepath"
)

// Config holds the configuration for the dunbar CLI
type Config struct {
	DunbarDir string
}

// New creates a new Config instance with defaults
func New() *Config {
	cfg := &Config{
		DunbarDir: getDefaultDunbarDir(),
	}

	// Override with environment variable if set
	if envDir := os.Getenv("DUNBAR_DIR"); envDir != "" {
		cfg.DunbarDir = envDir
	}

	return cfg
}

// getDefaultDunbarDir returns the default directory for dunbar data
func getDefaultDunbarDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".dunbar"
	}
	return filepath.Join(home, ".config", "dunbar")
}

// SetDunbarDir sets the dunbar directory and creates it if it doesn't exist
func (c *Config) SetDunbarDir(dir string) error {
	c.DunbarDir = dir
	return os.MkdirAll(dir, 0755)
}

// EnsureDunbarDir creates the dunbar directory if it doesn't exist
func (c *Config) EnsureDunbarDir() error {
	return os.MkdirAll(c.DunbarDir, 0755)
}
