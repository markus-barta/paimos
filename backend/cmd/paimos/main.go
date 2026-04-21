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

// Command paimos is the agent-facing CLI for PAIMOS instances. It wraps
// the HTTP API so Claude Code / agent scripts / humans can work with
// issues without wrestling with shell-quoted JSON.
//
// Bootstrap surface (PAI-90):
//   paimos auth login              — write ~/.paimos/config.yaml
//   paimos auth whoami
//   paimos project list
//   paimos issue get <ref>
//   paimos issue list --project PAI --status backlog
//   paimos issue children <ref>
//
// Global flags: --instance <name>, --json, --config <path>.
//
// Exit codes: 0 ok, 1 API error, 2 usage/config error.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version is stamped by goreleaser/-ldflags for releases; "dev" for
// local builds. Surfaced via `paimos --version`.
var Version = "dev"

// Global flag values, populated by Cobra's PersistentFlags.
var (
	flagInstance   string
	flagJSON       bool
	flagConfigPath string
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		// Usage errors: our own error type → print + exit 2 (convention).
		// apiError has already been printed in the caller's chosen
		// format (pretty or --json), don't double up.
		// Everything else (config I/O, marshaling, etc.) → print + exit 1.
		if ue, ok := err.(*usageError); ok {
			fmt.Fprintln(os.Stderr, "paimos: "+ue.Error())
			os.Exit(2)
		}
		if _, ok := err.(*apiError); !ok {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "paimos",
		Short: "Agent-facing CLI for PAIMOS (Professional & Personal AI Project OS)",
		Long: `paimos — thin CLI wrapper over the PAIMOS HTTP API.

Commands accept issue keys (PAI-83) or numeric ids interchangeably.
Every mutation accepts file inputs for multiline fields (--description-file,
--ac-file) so there's no shell-quoted-JSON foot-gun. Pass --json on any
command to emit machine-readable output.

Get started:
  paimos auth login           # interactive; writes ~/.paimos/config.yaml
  paimos issue list --help`,
		Version:       Version,
		SilenceUsage:  true, // don't dump usage on every API error
		SilenceErrors: true, // we print errors ourselves in main()
	}

	cmd.PersistentFlags().StringVar(&flagInstance, "instance", "", "instance name from config (defaults to default_instance)")
	cmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "emit JSON output suitable for piping")
	cmd.PersistentFlags().StringVar(&flagConfigPath, "config", "", "path to config file (default ~/.paimos/config.yaml)")

	cmd.AddCommand(authCmd())
	cmd.AddCommand(projectCmd())
	cmd.AddCommand(issueCmd())
	cmd.AddCommand(relationCmd())
	cmd.AddCommand(schemaCmd())
	cmd.AddCommand(doctorCmd())

	// If no subcommand is given, Cobra's default is to print help with
	// exit 0. The AC wants exit 2 for that case (standard convention).
	cmd.Run = func(c *cobra.Command, args []string) {
		_ = c.Help()
		os.Exit(2)
	}

	return cmd
}

// usageError is returned for bad CLI invocations so main() can map to
// exit code 2. Use fmt.Errorf for actual runtime failures (→ exit 1).
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

// apiError wraps an HTTP API failure so main() can suppress Cobra's
// default "Error: ..." prefix (we've already printed it via the client
// in the caller's chosen format — pretty or JSON).
type apiError struct{ inner error }

func (e *apiError) Error() string { return e.inner.Error() }
