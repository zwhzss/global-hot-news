package config

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

// Config holds application-wide configuration.
type Config struct {
	AppDataDir      string `json:"appDataDir"`
	MaxConcurrency  int    `json:"maxConcurrency"`
	DefaultInterval int    `json:"defaultInterval"`
	ArticleTTLDays  int    `json:"articleTtlDays"`
	LogLevel        string `json:"logLevel"`
	APIPort         int    `json:"apiPort"`
	APIToken        string `json:"apiKey"`
}

const defaultConfigPath = "config.json"

// DefaultConfig returns the default configuration values.
func DefaultConfig() *Config {
	return &Config{
		AppDataDir:      "",
		MaxConcurrency:  3,
		DefaultInterval: 300,
		ArticleTTLDays:  90,
		LogLevel:        "info",
		APIPort:         8765,
		APIToken:        generateToken(),
	}
}

// Load reads configuration from the app data directory.
func Load(appDataDir string) (*Config, error) {
	cfg := DefaultConfig()
	cfg.AppDataDir = appDataDir

	configPath := filepath.Join(appDataDir, defaultConfigPath)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return cfg, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return cfg, err
	}

	if cfg.APIToken == "" {
		cfg.APIToken = generateToken()
		_ = saveConfig(appDataDir, cfg)
	}

	return cfg, nil
}

// Save persistently writes the configuration.
func (c *Config) Save(appDataDir string) error {
	return saveConfig(appDataDir, c)
}

func saveConfig(appDataDir string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(appDataDir, defaultConfigPath), data, 0644)
}

func generateToken() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, 16)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := range b {
		b[i] = chars[r.Intn(len(chars))]
	}
	return string(b)
}
