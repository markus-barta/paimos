// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"io"
	"os"
)

// Indirection for os.Stdout/os.Stderr so tests can swap them. The
// functions are split out to keep client.go's init() clean.
func osStdout() io.Writer { return os.Stdout }
func osStderr() io.Writer { return os.Stderr }
