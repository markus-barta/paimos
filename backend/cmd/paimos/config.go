// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the CLI's persistent configuration. Stored at
// $XDG_CONFIG_HOME/paimos/config.yaml (or ~/.paimos/config.yaml) with
// mode 0600 since it holds API keys.
type Config struct {
	DefaultInstance string                     `yaml:"default_instance"`
	Instances       map[string]InstanceConfig  `yaml:"instances"`
}

// InstanceConfig is one named target: a URL and an API key.
type InstanceConfig struct {
	URL    string `yaml:"url"`
	APIKey string `yaml:"api_key"`
}

// defaultConfigPath returns ~/.paimos/config.yaml (cross-platform via
// os.UserHomeDir). The CLI deliberately does NOT consult XDG_CONFIG_HOME
// in v1 to keep discovery simple — one place to look, one place to edit.
func defaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate home directory: %w", err)
	}
	return filepath.Join(home, ".paimos", "config.yaml"), nil
}

// configPath returns the path the CLI will read/write. --config
// overrides the default.
func configPath() (string, error) {
	if flagConfigPath != "" {
		return flagConfigPath, nil
	}
	return defaultConfigPath()
}

// loadConfig reads the YAML config file. Returns an empty Config when
// the file doesn't exist (first run).
func loadConfig() (Config, error) {
	var cfg Config
	path, err := configPath()
	if err != nil {
		return cfg, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

// saveConfig writes the config atomically with mode 0600. Creates the
// parent directory if missing.
func saveConfig(cfg Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	b, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	// Write to a temp file in the same dir, then rename — atomic on
	// POSIX. Otherwise a crash mid-write corrupts the file in place.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config.yaml.*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("chmod 0600: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp → %s: %w", path, err)
	}
	return nil
}

// resolveInstance picks the instance to use for a command. Precedence:
//
//  1. --instance flag (hard override; error if named instance missing)
//  2. config.default_instance (the value set during `paimos auth login`)
//  3. sole instance if there's only one configured
//  4. error — user must run `paimos auth login` or specify --instance
func resolveInstance(cfg Config) (string, InstanceConfig, error) {
	if len(cfg.Instances) == 0 {
		return "", InstanceConfig{}, &usageError{
			msg: "no instance configured — run `paimos auth login` first",
		}
	}
	pick := flagInstance
	if pick == "" {
		pick = cfg.DefaultInstance
	}
	if pick == "" && len(cfg.Instances) == 1 {
		for name := range cfg.Instances {
			pick = name
		}
	}
	if pick == "" {
		return "", InstanceConfig{}, &usageError{
			msg: fmt.Sprintf("multiple instances configured; pass --instance <name> (configured: %s)", listInstances(cfg)),
		}
	}
	inst, ok := cfg.Instances[pick]
	if !ok {
		return "", InstanceConfig{}, &usageError{
			msg: fmt.Sprintf("instance %q not found (configured: %s)", pick, listInstances(cfg)),
		}
	}
	return pick, inst, nil
}

// listInstances returns a comma-separated list of configured instance
// names — for friendlier error messages.
func listInstances(cfg Config) string {
	if len(cfg.Instances) == 0 {
		return "(none)"
	}
	out := ""
	for name := range cfg.Instances {
		if out != "" {
			out += ", "
		}
		out += name
	}
	return out
}
