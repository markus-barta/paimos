// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see <https://www.gnu.org/licenses/>.

// PAI-261 phase 3 — `paimos secrets ...` operator subcommand. Lives
// in package main next to the existing dev-seed entrypoint; both
// branch off os.Args before the HTTP server boots.
//
// The only verb today is `rotate`, but `secrets` is the namespace so
// future tooling (status, list, …) has a clean home.

package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/markus-barta/paimos/backend/db"
	"github.com/markus-barta/paimos/backend/secretvault"
)

func runSecretsSubcommand(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: paimos secrets <command>")
		fmt.Fprintln(os.Stderr, "  rotate --new-key <base64> [--dry-run]")
		os.Exit(2)
	}
	switch args[0] {
	case "rotate":
		runSecretsRotate(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown secrets command: %s\n", args[0])
		os.Exit(2)
	}
}

func runSecretsRotate(args []string) {
	fs := flag.NewFlagSet("secrets rotate", flag.ExitOnError)
	newKeyB64 := fs.String("new-key", "", "base64 of the 32-byte new master key (e.g. `openssl rand -base64 32`)")
	dryRun := fs.Bool("dry-run", false, "decrypt with current key, count rows that would rotate, but don't write")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *newKeyB64 == "" {
		fmt.Fprintln(os.Stderr, "secrets rotate: --new-key is required")
		fmt.Fprintln(os.Stderr, "  generate one with: openssl rand -base64 32")
		os.Exit(2)
	}
	newKey, err := base64.StdEncoding.DecodeString(*newKeyB64)
	if err != nil || len(newKey) != 32 {
		fmt.Fprintln(os.Stderr, "secrets rotate: --new-key must be base64 of exactly 32 bytes")
		os.Exit(2)
	}

	ctx := context.Background()
	report, err := secretvault.Rotate(ctx, db.DB, secretvault.RotateOptions{
		NewKey: newKey,
		DryRun: *dryRun,
	})
	if err != nil {
		// Rotate's transaction has already been rolled back by the
		// time we reach here — the data is unchanged. Tell the
		// operator both facts so they don't accidentally roll back
		// their env-var update too.
		if errors.Is(err, secretvault.ErrPartialRotation) {
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "ROTATION ABORTED — no rows changed.")
			fmt.Fprintln(os.Stderr, "Your data is unchanged; PAIMOS_SECRET_KEY can stay on its current value.")
			fmt.Fprintln(os.Stderr)
		}
		log.Fatalf("secrets rotate: %v", err)
	}

	if *dryRun {
		fmt.Printf("dry-run: would rotate %d CRM provider config row(s) and %d AI settings row(s).\n",
			report.CRMRows, report.AIRows)
		fmt.Println("(decrypts confirmed under the current PAIMOS_SECRET_KEY; nothing was written.)")
		return
	}

	fmt.Printf("✔ rotated %d CRM provider config row(s) and %d AI settings row(s).\n",
		report.CRMRows, report.AIRows)
	fmt.Println()
	fmt.Println("Next steps (the existing service is still running on the OLD key):")
	fmt.Println("  1. Stop the service.")
	fmt.Println("  2. Update PAIMOS_SECRET_KEY (or replace $DATA_DIR/.secret-key) with the new value.")
	fmt.Println("  3. Start the service.")
	fmt.Println()
	fmt.Println("If anything goes wrong on restart, switch the env var back to the OLD key —")
	fmt.Println("the rotation is complete (encrypted under the new key), so the OLD key will")
	fmt.Println("no longer decrypt the data. Roll forward (re-run rotation with the env var")
	fmt.Println("you actually want active) rather than backward.")
}
