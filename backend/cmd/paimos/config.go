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
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the CLI's persistent configuration. Stored at
// ~/.paimos/config.yaml with mode 0600. API keys live in the OS
// keyring, but the restrictive mode is cheap defence-in-depth.
type Config struct {
	DefaultInstance string                    `yaml:"default_instance"`
	Instances       map[string]InstanceConfig `yaml:"instances"`
}

// InstanceConfig is one named target. The URL is persisted to disk;
// APIKey is a runtime-only field — it is never serialised back to
// config.yaml (saveConfig strips it defensively) and is hydrated from
// the OS keyring (or PAIMOS_API_KEY) when a command needs to make a
// request. The yaml tag is kept so legacy config files written by
// pre-keyring versions still parse cleanly during the one-time
// migration in loadConfig.
type InstanceConfig struct {
	URL          string `yaml:"url"`
	APIKey       string `yaml:"api_key,omitempty"`
	URLSource    string `yaml:"-"`
	APIKeySource string `yaml:"-"`
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
//
// On first read after upgrading from a pre-keyring CLI, any inline
// api_key fields in the YAML are migrated into the OS keyring and
// removed from disk. The config is then re-saved without the field. A
// one-time notice is printed to stderr so users understand where their
// credential moved.
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
	if err := migrateAPIKeysToKeyring(&cfg, path); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// migrateAPIKeysToKeyring moves any api_key found in the parsed config
// into the OS keyring and rewrites the config file without the field.
// Called once per loadConfig; a no-op when the keys are already gone
// (the common steady-state case after the first migration).
func migrateAPIKeysToKeyring(cfg *Config, path string) error {
	if os.Getenv(envAPIKey) != "" {
		for name, inst := range cfg.Instances {
			inst.APIKey = ""
			cfg.Instances[name] = inst
		}
		return nil
	}
	migrated := make([]string, 0)
	for name, inst := range cfg.Instances {
		if inst.APIKey == "" {
			continue
		}
		if err := keyringSet(name, inst.APIKey); err != nil {
			return fmt.Errorf("migrate api_key for instance %q to keyring: %w (set %s to bypass)", name, err, envAPIKey)
		}
		inst.APIKey = ""
		cfg.Instances[name] = inst
		migrated = append(migrated, name)
	}
	if len(migrated) == 0 {
		return nil
	}
	if err := saveConfig(*cfg); err != nil {
		return fmt.Errorf("rewrite %s after keyring migration: %w", path, err)
	}
	fmt.Fprintf(stderr, "paimos: migrated API key(s) for %v from %s into the OS keyring (service %q)\n", migrated, path, keyringServiceName)
	return nil
}

// saveConfig writes the config atomically with mode 0600. Creates the
// parent directory if missing.
//
// API keys are stripped before serialisation — they live in the OS
// keyring, not on disk. Mode 0600 stays as defence-in-depth: the URL +
// instance-name list are not secrets, but treating the whole file as
// sensitive is cheap.
func saveConfig(cfg Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	scrubbed := Config{DefaultInstance: cfg.DefaultInstance}
	if len(cfg.Instances) > 0 {
		scrubbed.Instances = make(map[string]InstanceConfig, len(cfg.Instances))
		for name, inst := range cfg.Instances {
			inst.APIKey = ""
			scrubbed.Instances[name] = inst
		}
	}
	cfg = scrubbed
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

func normalizeInstanceURL(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return u
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "https://" + u
	}
	return u
}

// pickInstance picks the instance to use for a command without loading
// its API key. Precedence:
//
//  1. --instance flag (hard override; error if named instance missing)
//  2. config.default_instance (the value set during `paimos auth login`)
//  3. sole instance if there's only one configured
//  4. error — user must run `paimos auth login` or specify --instance
//
// Use this when you need the instance metadata (URL, name) but not the
// credential, e.g. `paimos auth logout` should still work after the
// keyring entry has already been deleted.
func pickInstance(cfg Config) (string, InstanceConfig, error) {
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

// resolveInstance picks the instance and hydrates its API key from
// PAIMOS_API_KEY or the OS keyring. Used by every authenticated
// command. Returns a usage error when the instance is configured but
// no credential is available, so the caller can map it to exit code 2
// and a "run paimos auth login" hint.
func resolveInstance(cfg Config) (string, InstanceConfig, error) {
	name, inst, err := pickInstance(cfg)
	if err != nil {
		return name, inst, err
	}
	key, keySource, err := resolveAPIKey(name)
	if err != nil {
		return name, inst, err
	}
	if key == "" {
		return name, inst, &usageError{
			msg: fmt.Sprintf("no API key for instance %q — run `paimos auth login --name %s` (or set %s)", name, name, envAPIKey),
		}
	}
	inst.APIKey = key
	if inst.URLSource == "" {
		inst.URLSource = "config:" + name
	}
	inst.APIKeySource = keySource
	return name, inst, nil
}

func resolveEnvInstance() (string, InstanceConfig, bool, error) {
	if rawURL := strings.TrimSpace(os.Getenv(envURL)); rawURL != "" {
		key := strings.TrimSpace(os.Getenv(envAPIKey))
		if key == "" {
			return "", InstanceConfig{}, true, &usageError{
				msg: fmt.Sprintf("%s is set but %s is missing", envURL, envAPIKey),
			}
		}
		return "env", InstanceConfig{
			URL:          normalizeInstanceURL(rawURL),
			APIKey:       key,
			URLSource:    "env:" + envURL,
			APIKeySource: "env:" + envAPIKey,
		}, true, nil
	}
	if rawURL := strings.TrimSpace(os.Getenv(envPPMURL)); rawURL != "" {
		key := strings.TrimSpace(os.Getenv(envPPMAPIKey))
		if key == "" {
			return "", InstanceConfig{}, true, &usageError{
				msg: fmt.Sprintf("%s is set but %s is missing", envPPMURL, envPPMAPIKey),
			}
		}
		return "ppm-env", InstanceConfig{
			URL:          normalizeInstanceURL(rawURL),
			APIKey:       key,
			URLSource:    "env:" + envPPMURL,
			APIKeySource: "env:" + envPPMAPIKey,
		}, true, nil
	}
	return "", InstanceConfig{}, false, nil
}

func resolveActiveInstance() (string, InstanceConfig, error) {
	if name, inst, ok, err := resolveEnvInstance(); ok || err != nil {
		return name, inst, err
	}
	cfg, err := loadConfig()
	if err != nil {
		return "", InstanceConfig{}, err
	}
	return resolveInstance(cfg)
}

func resolvedInstanceDetail(name string, inst InstanceConfig) string {
	detail := fmt.Sprintf("%s (%s)", name, inst.URL)
	var meta []string
	if inst.URLSource != "" {
		meta = append(meta, "url="+inst.URLSource)
	}
	if inst.APIKeySource != "" {
		meta = append(meta, "credential="+inst.APIKeySource)
	}
	if len(meta) > 0 {
		detail += " [" + strings.Join(meta, ", ") + "]"
	}
	return detail
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
