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
	"testing"
)

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
		want := Config{
			DefaultInstance: "ppm",
			Instances: map[string]InstanceConfig{
				"ppm": {URL: "https://pm.barta.cm", APIKey: "paimos_abc"},
			},
		}
		if err := saveConfig(want); err != nil {
			t.Fatalf("saveConfig: %v", err)
		}
		got, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig: %v", err)
		}
		if got.DefaultInstance != want.DefaultInstance {
			t.Errorf("DefaultInstance=%q, want %q", got.DefaultInstance, want.DefaultInstance)
		}
		if got.Instances["ppm"].APIKey != want.Instances["ppm"].APIKey {
			t.Errorf("api_key roundtrip broken")
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

func TestResolveInstance(t *testing.T) {
	cases := []struct {
		name          string
		cfg           Config
		flag          string
		wantInstance  string
		wantError     bool
	}{
		{
			name: "none configured",
			cfg:  Config{},
			wantError: true,
		},
		{
			name: "single instance, no flag",
			cfg: Config{Instances: map[string]InstanceConfig{
				"only": {URL: "u", APIKey: "k"},
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
			name, _, err := resolveInstance(tc.cfg)
			if (err != nil) != tc.wantError {
				t.Fatalf("err=%v, wantError=%v", err, tc.wantError)
			}
			if !tc.wantError && name != tc.wantInstance {
				t.Errorf("got %q, want %q", name, tc.wantInstance)
			}
		})
	}
}
