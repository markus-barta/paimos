// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package contracts

import (
	"os"

	"gopkg.in/yaml.v3"
)

type ContractFixture struct {
	Version int            `yaml:"version"`
	Cases   []ContractCase `yaml:"cases"`
}

type ContractCase struct {
	Name      string              `yaml:"name"`
	Operation string              `yaml:"operation"`
	Inputs    map[string]any      `yaml:"inputs"`
	Expect    ContractExpectation `yaml:"expect"`
}

type ContractExpectation struct {
	Status     string                `yaml:"status"`
	Body       map[string]any        `yaml:"body"`
	Normalized map[string]any        `yaml:"normalized"`
	Error      ContractErrorExpected `yaml:"error"`
}

type ContractErrorExpected struct {
	Status      int      `yaml:"status"`
	Code        string   `yaml:"code"`
	Field       string   `yaml:"field"`
	ValidValues []string `yaml:"valid_values"`
}

func LoadContractFixture(path string) (ContractFixture, error) {
	var fixture ContractFixture
	// #nosec G304 -- path is supplied by test code pointing at in-repo fixture files, not user input.
	raw, err := os.ReadFile(path)
	if err != nil {
		return fixture, err
	}
	err = yaml.Unmarshal(raw, &fixture)
	return fixture, err
}
