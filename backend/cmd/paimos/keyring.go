// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/zalando/go-keyring"
)

// keyringServiceName is the service identifier used for every entry the
// CLI writes — accounts under this service are instance names. Keep it
// stable: changing it would orphan everyone's stored credentials.
const keyringServiceName = "paimos-cli"

// envURL/envAPIKey provide the generic non-interactive override path.
// When PAIMOS_URL is set, PAIMOS_API_KEY is required and the CLI bypasses
// ~/.paimos/config.yaml + keyring instance resolution entirely.
const envURL = "PAIMOS_URL"

// envAPIKey lets headless / CI environments without a session keyring
// supply the key directly. With PAIMOS_URL it selects an env-only
// instance; without PAIMOS_URL it overrides the keyring lookup for the
// configured instance.
const envAPIKey = "PAIMOS_API_KEY"

// PPM_* aliases match the personal-production secret files agents use
// while working against pm.barta.cm.
const envPPMURL = "PPM_URL"
const envPPMAPIKey = "PPMAPIKEY"

// keyringSet stores the API key for an instance in the OS keyring
// (Keychain on macOS, Secret Service / KWallet on Linux, Credential
// Manager on Windows).
func keyringSet(instance, key string) error {
	if instance == "" {
		return fmt.Errorf("instance name is empty")
	}
	if err := keyring.Set(keyringServiceName, instance, key); err != nil {
		return fmt.Errorf("write keyring (%s/%s): %w", keyringServiceName, instance, err)
	}
	return nil
}

// keyringGet reads the API key for an instance. The "found" return is
// false (with a nil error) when the entry simply does not exist —
// callers should distinguish that from a real backend failure so they
// can show a "run paimos auth login" hint instead of a stack trace.
func keyringGet(instance string) (key string, found bool, err error) {
	v, err := keyring.Get(keyringServiceName, instance)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read keyring (%s/%s): %w", keyringServiceName, instance, err)
	}
	return v, true, nil
}

// keyringDelete removes the entry. Idempotent — a missing entry is not
// an error so `paimos auth logout` is safe to re-run.
func keyringDelete(instance string) error {
	err := keyring.Delete(keyringServiceName, instance)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete keyring (%s/%s): %w", keyringServiceName, instance, err)
	}
	return nil
}

// resolveAPIKey returns the API key to use for an instance. Precedence:
//
//  1. PAIMOS_API_KEY env var — wins unconditionally so CI / headless
//     boxes that have no session keyring can still authenticate.
//  2. OS keyring entry under (paimos-cli, <instance>).
//  3. "" with a nil error — caller maps this to a usage error pointing
//     at `paimos auth login`.
//
// Backend errors (e.g. dbus refusing to talk) propagate; only "no such
// entry" falls through to the empty-string case.
func resolveAPIKey(instance string) (string, string, error) {
	if v := os.Getenv(envAPIKey); v != "" {
		return v, "env:" + envAPIKey, nil
	}
	key, ok, err := keyringGet(instance)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", nil
	}
	return key, "keyring:" + keyringServiceName + "/" + instance, nil
}
