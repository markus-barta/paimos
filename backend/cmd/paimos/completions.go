// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"github.com/spf13/cobra"
)

// registerEnumCompletions wires tab-completion for flags whose values
// are a bounded enum. Reads from the local schema cache (no network
// on tab press), so it only kicks in after `paimos schema` has run
// at least once per instance.
//
// Unknown flag names are silently ignored — safe to call with a
// superset.
func registerEnumCompletions(c *cobra.Command, flagNames ...string) {
	for _, name := range flagNames {
		name := name // capture
		_ = c.RegisterFlagCompletionFunc(name, func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			values := enumFromCachedSchema(name)
			if len(values) == 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return values, cobra.ShellCompDirectiveNoFileComp
		})
	}
}

// enumFromCachedSchema reads enums from any instance's schema cache.
// We pick the default instance's cache to avoid a config roundtrip
// in the completion hotpath. Returns nil if no cache exists yet.
func enumFromCachedSchema(enumName string) []string {
	cfg, err := loadConfig()
	if err != nil {
		return nil
	}
	instance := flagInstance
	if instance == "" {
		instance = cfg.DefaultInstance
	}
	if instance == "" && len(cfg.Instances) == 1 {
		for k := range cfg.Instances {
			instance = k
		}
	}
	if instance == "" {
		return nil
	}
	sch, err := loadCachedSchema(instance)
	if err != nil || sch == nil {
		return nil
	}
	return sch.Enums[enumName]
}
