// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestMain installs go-keyring's in-memory mock so tests never touch
// the real OS keychain. Tests that exercise the keyring interact with
// this fake; production code still calls the real backend.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

func withConfigDir(t *testing.T, fn func(path string)) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	old := flagConfigPath
	flagConfigPath = path
	t.Cleanup(func() { flagConfigPath = old })
	fn(path)
}

func TestSaveLoadConfig(t *testing.T) {
	withConfigDir(t, func(path string) {
		// API keys live in the keyring, not on disk. Defence-in-depth:
		// even if a caller hands an APIKey to saveConfig, it must NOT
		// be written to the YAML file.
		input := Config{
			DefaultInstance: "ppm",
			Instances: map[string]InstanceConfig{
				"ppm": {URL: "https://pm.barta.cm", APIKey: "paimos_abc"},
			},
		}
		if err := saveConfig(input); err != nil {
			t.Fatalf("saveConfig: %v", err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		if strings.Contains(string(raw), "paimos_abc") {
			t.Errorf("api key leaked into config.yaml: %q", raw)
		}
		if strings.Contains(string(raw), "api_key") {
			t.Errorf("api_key field present on disk: %q", raw)
		}
		got, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig: %v", err)
		}
		if got.DefaultInstance != input.DefaultInstance {
			t.Errorf("DefaultInstance=%q, want %q", got.DefaultInstance, input.DefaultInstance)
		}
		if got.Instances["ppm"].URL != input.Instances["ppm"].URL {
			t.Errorf("URL roundtrip broken: got %q", got.Instances["ppm"].URL)
		}
		if got.Instances["ppm"].APIKey != "" {
			t.Errorf("APIKey should never come back from disk; got %q", got.Instances["ppm"].APIKey)
		}
	})
}

// TestLoadConfig_MigratesAPIKeyToKeyring covers the upgrade path:
// pre-keyring CLIs left api_key in YAML, and the first loadConfig
// after upgrade must move it into the keyring and rewrite the file.
func TestLoadConfig_MigratesAPIKeyToKeyring(t *testing.T) {
	withConfigDir(t, func(path string) {
		// Plant a legacy config with an inline api_key. Write the YAML
		// directly so we don't rely on saveConfig (which now strips
		// api_key by design).
		legacy := []byte("default_instance: ppm\ninstances:\n  ppm:\n    url: https://pm.barta.cm\n    api_key: legacy_secret\n")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, legacy, 0o600); err != nil {
			t.Fatalf("write legacy config: %v", err)
		}
		// Make sure the keyring backend starts clean for this test.
		_ = keyringDelete("ppm")

		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig: %v", err)
		}
		if cfg.Instances["ppm"].APIKey != "" {
			t.Errorf("APIKey not stripped from in-memory config after migration: %q", cfg.Instances["ppm"].APIKey)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("re-read config: %v", err)
		}
		if strings.Contains(string(raw), "legacy_secret") || strings.Contains(string(raw), "api_key") {
			t.Errorf("api_key still on disk after migration: %q", raw)
		}
		got, _, err := keyringGet("ppm")
		if err != nil {
			t.Fatalf("keyringGet: %v", err)
		}
		if got != "legacy_secret" {
			t.Errorf("keyring value = %q, want %q", got, "legacy_secret")
		}
	})
}

// TestConfigMode0600 is the "don't leak API keys through lax perms"
// guard. Skipped on Windows since mode bits don't map cleanly there.
func TestConfigMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode semantics differ on windows")
	}
	withConfigDir(t, func(path string) {
		if err := saveConfig(Config{Instances: map[string]InstanceConfig{
			"ppm": {URL: "x", APIKey: "y"},
		}}); err != nil {
			t.Fatalf("saveConfig: %v", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("mode=%o, want 0600 (API keys live here)", info.Mode().Perm())
		}
	})
}

func TestLoadConfig_Missing_ReturnsEmpty(t *testing.T) {
	withConfigDir(t, func(path string) {
		// Don't create the file.
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig: %v", err)
		}
		if len(cfg.Instances) != 0 {
			t.Errorf("expected empty config for missing file; got %+v", cfg)
		}
	})
}

func TestPickInstance(t *testing.T) {
	cases := []struct {
		name         string
		cfg          Config
		flag         string
		wantInstance string
		wantError    bool
	}{
		{
			name:      "none configured",
			cfg:       Config{},
			wantError: true,
		},
		{
			name: "single instance, no flag",
			cfg: Config{Instances: map[string]InstanceConfig{
				"only": {URL: "u"},
			}},
			wantInstance: "only",
		},
		{
			name: "explicit default_instance",
			cfg: Config{
				DefaultInstance: "ppm",
				Instances: map[string]InstanceConfig{
					"ppm": {URL: "a"}, "bytepoets": {URL: "b"},
				},
			},
			wantInstance: "ppm",
		},
		{
			name: "--instance overrides default",
			cfg: Config{
				DefaultInstance: "ppm",
				Instances: map[string]InstanceConfig{
					"ppm": {URL: "a"}, "bytepoets": {URL: "b"},
				},
			},
			flag:         "bytepoets",
			wantInstance: "bytepoets",
		},
		{
			name: "--instance unknown → error",
			cfg: Config{
				Instances: map[string]InstanceConfig{"ppm": {URL: "a"}},
			},
			flag:      "typo",
			wantError: true,
		},
		{
			name: "multiple instances, no default, no flag → error",
			cfg: Config{
				Instances: map[string]InstanceConfig{
					"a": {URL: "a"}, "b": {URL: "b"},
				},
			},
			wantError: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			old := flagInstance
			flagInstance = tc.flag
			t.Cleanup(func() { flagInstance = old })
			name, _, err := pickInstance(tc.cfg)
			if (err != nil) != tc.wantError {
				t.Fatalf("err=%v, wantError=%v", err, tc.wantError)
			}
			if !tc.wantError && name != tc.wantInstance {
				t.Errorf("got %q, want %q", name, tc.wantInstance)
			}
		})
	}
}

// TestResolveInstance_KeyHydration exercises the credential-loading
// step layered on top of pickInstance: PAIMOS_API_KEY wins, then the
// keyring, and a missing credential surfaces as a usage error.
func TestResolveInstance_KeyHydration(t *testing.T) {
	cfg := Config{
		DefaultInstance: "ppm",
		Instances:       map[string]InstanceConfig{"ppm": {URL: "https://pm.barta.cm"}},
	}

	t.Run("env var wins", func(t *testing.T) {
		t.Setenv(envAPIKey, "env_key")
		_ = keyringSet("ppm", "kr_key") // env should still win
		t.Cleanup(func() { _ = keyringDelete("ppm") })
		_, inst, err := resolveInstance(cfg)
		if err != nil {
			t.Fatalf("resolveInstance: %v", err)
		}
		if inst.APIKey != "env_key" {
			t.Errorf("APIKey=%q, want env_key", inst.APIKey)
		}
	})

	t.Run("keyring fallback", func(t *testing.T) {
		t.Setenv(envAPIKey, "")
		if err := keyringSet("ppm", "kr_key"); err != nil {
			t.Fatalf("keyringSet: %v", err)
		}
		t.Cleanup(func() { _ = keyringDelete("ppm") })
		_, inst, err := resolveInstance(cfg)
		if err != nil {
			t.Fatalf("resolveInstance: %v", err)
		}
		if inst.APIKey != "kr_key" {
			t.Errorf("APIKey=%q, want kr_key", inst.APIKey)
		}
	})

	t.Run("no credential → usage error", func(t *testing.T) {
		t.Setenv(envAPIKey, "")
		_ = keyringDelete("ppm")
		_, _, err := resolveInstance(cfg)
		if err == nil {
			t.Fatal("expected usage error for missing credential")
		}
		if _, ok := err.(*usageError); !ok {
			t.Errorf("err type = %T, want *usageError", err)
		}
	})
}

func TestResolveActiveInstance_EnvPairBypassesConfig(t *testing.T) {
	withConfigDir(t, func(path string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(":\n"), 0o600); err != nil {
			t.Fatalf("write invalid config: %v", err)
		}
		t.Setenv(envURL, "pm.barta.cm")
		t.Setenv(envAPIKey, "env_key")

		name, inst, err := resolveActiveInstance()
		if err != nil {
			t.Fatalf("resolveActiveInstance: %v", err)
		}
		if name != "env" {
			t.Errorf("name=%q, want env", name)
		}
		if inst.URL != "https://pm.barta.cm" {
			t.Errorf("URL=%q, want https://pm.barta.cm", inst.URL)
		}
		if inst.APIKey != "env_key" {
			t.Errorf("APIKey=%q, want env_key", inst.APIKey)
		}
		if inst.URLSource != "env:"+envURL || inst.APIKeySource != "env:"+envAPIKey {
			t.Errorf("sources url=%q key=%q", inst.URLSource, inst.APIKeySource)
		}
	})
}

func TestResolveActiveInstance_PPMEnvPair(t *testing.T) {
	t.Setenv(envURL, "")
	t.Setenv(envAPIKey, "")
	t.Setenv(envPPMURL, "https://pm.barta.cm")
	t.Setenv(envPPMAPIKey, "ppm_key")

	name, inst, err := resolveActiveInstance()
	if err != nil {
		t.Fatalf("resolveActiveInstance: %v", err)
	}
	if name != "ppm-env" {
		t.Errorf("name=%q, want ppm-env", name)
	}
	if inst.APIKey != "ppm_key" {
		t.Errorf("APIKey=%q, want ppm_key", inst.APIKey)
	}
	if inst.URLSource != "env:"+envPPMURL || inst.APIKeySource != "env:"+envPPMAPIKey {
		t.Errorf("sources url=%q key=%q", inst.URLSource, inst.APIKeySource)
	}
}

func TestResolveEnvInstance_RequiresMatchingKey(t *testing.T) {
	t.Setenv(envURL, "https://pm.barta.cm")
	t.Setenv(envAPIKey, "")

	_, _, _, err := resolveEnvInstance()
	if err == nil {
		t.Fatal("expected error when PAIMOS_URL is set without PAIMOS_API_KEY")
	}
	if !strings.Contains(err.Error(), envAPIKey) {
		t.Errorf("error %q does not mention %s", err.Error(), envAPIKey)
	}
}

// TestKeyringDelete_Idempotent — `paimos auth logout` runs delete
// blindly; a missing entry must not surface as an error.
func TestKeyringDelete_Idempotent(t *testing.T) {
	if err := keyringDelete("never-existed"); err != nil {
		t.Errorf("delete on missing entry returned error: %v", err)
	}
}
