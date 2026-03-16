package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	Starred      []string          `json:"starred"`
	Descriptions map[string]string `json:"descriptions"`
	Custom       []string          `json:"custom,omitempty"`
}

func configPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "cli", "config.json")
}

func loadConfig() Config {
	cfg := Config{
		Descriptions: make(map[string]string),
	}
	data, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	json.Unmarshal(data, &cfg)
	if cfg.Descriptions == nil {
		cfg.Descriptions = make(map[string]string)
	}
	return cfg
}

func saveConfig(cfg Config) error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0644)
}

func (c *Config) IsStarred(name string) bool {
	for _, s := range c.Starred {
		if s == name {
			return true
		}
	}
	return false
}

func (c *Config) ToggleStar(name string) {
	for i, s := range c.Starred {
		if s == name {
			c.Starred = append(c.Starred[:i], c.Starred[i+1:]...)
			return
		}
	}
	c.Starred = append(c.Starred, name)
}
