// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func authCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate with a PAIMOS instance",
	}
	c.AddCommand(authLoginCmd())
	c.AddCommand(authWhoAmICmd())
	return c
}

func authLoginCmd() *cobra.Command {
	var (
		urlFlag  string
		nameFlag string
		keyFlag  string
	)
	c := &cobra.Command{
		Use:   "login",
		Short: "Interactively configure a PAIMOS instance + API key",
		Long: `Configures a named PAIMOS instance in ~/.paimos/config.yaml.

Prompts for URL + API key unless --url and --api-key are passed
(useful for scripting). The first configured instance becomes
default_instance automatically.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			stdin := bufio.NewReader(os.Stdin)
			if nameFlag == "" {
				nameFlag = "default"
			}
			if urlFlag == "" {
				fmt.Fprintf(stdout, "Instance URL [https://pm.barta.cm]: ")
				line, _ := stdin.ReadString('\n')
				urlFlag = strings.TrimSpace(line)
				if urlFlag == "" {
					urlFlag = "https://pm.barta.cm"
				}
			}
			if !strings.HasPrefix(urlFlag, "http://") && !strings.HasPrefix(urlFlag, "https://") {
				urlFlag = "https://" + urlFlag
			}

			if keyFlag == "" {
				fmt.Fprintf(stdout, "API key (input hidden): ")
				key, err := term.ReadPassword(int(syscall.Stdin))
				fmt.Fprintln(stdout) // newline after hidden input
				if err != nil {
					return fmt.Errorf("read api key: %w", err)
				}
				keyFlag = strings.TrimSpace(string(key))
			}
			if keyFlag == "" {
				return &usageError{msg: "api key is required"}
			}

			// Verify the key works before writing config.
			probe := newClient(InstanceConfig{URL: urlFlag, APIKey: keyFlag})
			body, err := probe.do("GET", "/api/auth/me", nil)
			if err != nil {
				return reportError(err)
			}

			// Load existing config; append this instance.
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if cfg.Instances == nil {
				cfg.Instances = map[string]InstanceConfig{}
			}
			cfg.Instances[nameFlag] = InstanceConfig{URL: urlFlag, APIKey: keyFlag}
			if cfg.DefaultInstance == "" {
				cfg.DefaultInstance = nameFlag
			}
			if err := saveConfig(cfg); err != nil {
				return err
			}

			// /api/auth/me returns {user: {...}, access: {...}}.
			var me struct {
				User map[string]any `json:"user"`
			}
			_ = json.Unmarshal(body, &me)
			username, _ := me.User["username"].(string)
			path, _ := configPath()
			if flagJSON {
				out := map[string]any{
					"ok":       true,
					"instance": nameFlag,
					"url":      urlFlag,
					"config":   path,
					"user":     username,
				}
				return emitJSON(out)
			}
			fmt.Fprintf(stdout, "✓ logged in as %s at %s\n", username, urlFlag)
			fmt.Fprintf(stdout, "  saved to %s as instance %q\n", path, nameFlag)
			if cfg.DefaultInstance == nameFlag {
				fmt.Fprintf(stdout, "  default_instance = %q\n", nameFlag)
			}
			return nil
		},
	}
	c.Flags().StringVar(&urlFlag, "url", "", "instance URL (skips prompt)")
	c.Flags().StringVar(&nameFlag, "name", "", `name for this instance in config (default "default")`)
	c.Flags().StringVar(&keyFlag, "api-key", "", "API key (skips prompt; prefer the prompt for security)")
	return c
}

func authWhoAmICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the identity behind the current API key",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			name, inst, err := resolveInstance(cfg)
			if err != nil {
				return err
			}
			client := newClient(inst)
			body, err := client.do("GET", "/api/auth/me", nil)
			if err != nil {
				return reportError(err)
			}
			var me struct {
				User map[string]any `json:"user"`
			}
			_ = json.Unmarshal(body, &me)
			if flagJSON {
				out := map[string]any{
					"instance": name,
					"url":      inst.URL,
					"user":     me.User,
				}
				return emitJSON(out)
			}
			fmt.Fprintf(stdout, "instance: %s (%s)\n", name, inst.URL)
			fmt.Fprintf(stdout, "user:     %v (%v)\n", me.User["username"], me.User["role"])
			return nil
		},
	}
}

// emitJSON writes a JSON-encoded value to stdout with a trailing newline.
func emitJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, string(b))
	return nil
}
