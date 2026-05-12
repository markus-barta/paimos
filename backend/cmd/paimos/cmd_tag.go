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

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var tagColors = []string{
	"gray", "slate", "blue", "indigo", "purple", "pink",
	"red", "orange", "yellow", "green", "teal", "cyan",
}

type cliTag struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
	System      bool   `json:"system"`
	CreatedAt   string `json:"created_at"`
}

func tagCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "tag",
		Short: "List and manage tags",
		Long: `List and manage the global tag catalog.

Tags are shared globally, then attached to issues or projects. Use
"paimos issue tag add/rm" for assigning existing tags to issues; use
"paimos tag create --project PAI" when bootstrapping a project taxonomy.`,
	}
	c.AddCommand(tagListCmd())
	c.AddCommand(tagCreateCmd())
	c.AddCommand(tagUpdateCmd())
	c.AddCommand(tagDeleteCmd())
	return c
}

func tagListCmd() *cobra.Command {
	var projectRef string
	c := &cobra.Command{
		Use:   "list",
		Short: "List tags",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := instanceClient()
			if err != nil {
				return err
			}
			path := "/api/tags"
			if strings.TrimSpace(projectRef) != "" {
				projectID, err := resolveProjectRefToID(client, projectRef)
				if err != nil {
					return reportError(err)
				}
				path = fmt.Sprintf("/api/projects/%d/tags", projectID)
			}
			body, err := client.do("GET", path, nil)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(body)))
				return nil
			}
			var tags []cliTag
			if err := json.Unmarshal(body, &tags); err != nil {
				return fmt.Errorf("decode tags: %w", err)
			}
			renderTagsPretty(tags)
			return nil
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id")
	return c
}

func tagCreateCmd() *cobra.Command {
	var (
		projectRef  string
		name        string
		color       string
		description string
		descFile    string
	)
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a tag",
		Long: `Creates a tag in the global catalog. When --project is passed,
the new tag is immediately attached to that project, so project taxonomy
bootstraps stay one command.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name = strings.TrimSpace(name)
			if name == "" {
				return &usageError{msg: "--name is required"}
			}
			color = strings.TrimSpace(color)
			if err := validateTagColor(color); err != nil {
				return err
			}
			description, _, err := readMultilineInput(description, descFile, "description")
			if err != nil {
				return err
			}

			client, err := instanceClient()
			if err != nil {
				return err
			}
			var projectID int64
			if strings.TrimSpace(projectRef) != "" {
				projectID, err = resolveProjectRefToID(client, projectRef)
				if err != nil {
					return reportError(err)
				}
			}

			body := map[string]any{"name": name}
			if color != "" {
				body["color"] = color
			}
			if description != "" {
				body["description"] = description
			}
			raw, err := client.do("POST", "/api/tags", body)
			if err != nil {
				return reportError(err)
			}
			var tag cliTag
			if err := json.Unmarshal(raw, &tag); err != nil {
				return fmt.Errorf("decode tag: %w", err)
			}
			attached := false
			if projectID > 0 {
				if _, err := client.do("POST", fmt.Sprintf("/api/projects/%d/tags", projectID), map[string]any{"tag_id": tag.ID}); err != nil {
					return reportError(err)
				}
				attached = true
			}

			if flagJSON {
				out := map[string]any{"tag": tag, "attached": attached}
				if projectID > 0 {
					out["project_id"] = projectID
				}
				return emitJSON(out)
			}
			if attached {
				fmt.Fprintf(stdout, "✓ created tag %s (#%d) and attached it to project %s\n", tag.Name, tag.ID, strings.TrimSpace(projectRef))
				return nil
			}
			fmt.Fprintf(stdout, "✓ created tag %s (#%d)\n", tag.Name, tag.ID)
			return nil
		},
	}
	c.Flags().StringVar(&projectRef, "project", "", "project key or numeric id to attach the new tag to")
	c.Flags().StringVar(&name, "name", "", "tag name (required)")
	c.Flags().StringVar(&color, "color", "", "tag color: "+strings.Join(tagColors, ", "))
	c.Flags().StringVar(&description, "description", "", "inline description")
	c.Flags().StringVar(&descFile, "description-file", "", "path to markdown description (or - for stdin)")
	return c
}

func tagUpdateCmd() *cobra.Command {
	var (
		name        string
		color       string
		description string
		descFile    string
	)
	c := &cobra.Command{
		Use:   "update <tag-id>",
		Short: "Rename or recolor a tag",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tagID, err := parsePositiveInt64Flag("tag-id", args[0])
			if err != nil {
				return err
			}
			nameChanged := cmd.Flags().Changed("name")
			if nameChanged {
				name = strings.TrimSpace(name)
				if name == "" {
					return &usageError{msg: "--name cannot be empty"}
				}
			}
			color = strings.TrimSpace(color)
			if err := validateTagColor(color); err != nil {
				return err
			}
			desc, descSet, err := readMultilineInput(description, descFile, "description")
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("description") && descFile == "" {
				desc = description
				descSet = true
			}
			if !nameChanged && color == "" && !descSet {
				return &usageError{msg: "at least one of --name, --color, --description, or --description-file is required"}
			}

			body := map[string]any{}
			if nameChanged {
				body["name"] = name
			}
			if color != "" {
				body["color"] = color
			}
			if descSet {
				body["description"] = desc
			}

			client, err := instanceClient()
			if err != nil {
				return err
			}
			raw, err := client.do("PUT", fmt.Sprintf("/api/tags/%d", tagID), body)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(raw)))
				return nil
			}
			var tag cliTag
			if err := json.Unmarshal(raw, &tag); err != nil {
				return fmt.Errorf("decode tag: %w", err)
			}
			fmt.Fprintf(stdout, "✓ updated tag %s (#%d)\n", tag.Name, tag.ID)
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", "", "new tag name")
	c.Flags().StringVar(&color, "color", "", "tag color: "+strings.Join(tagColors, ", "))
	c.Flags().StringVar(&description, "description", "", "inline description; pass an empty string to clear it")
	c.Flags().StringVar(&descFile, "description-file", "", "path to markdown description (or - for stdin)")
	return c
}

func tagDeleteCmd() *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use:   "delete <tag-id>",
		Short: "Delete a tag from the global catalog",
		Long: `Deletes a tag from the global catalog. This also removes existing
issue and project assignments through the API's cascade behavior. Pass
--yes for non-interactive use.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tagID, err := parsePositiveInt64Flag("tag-id", args[0])
			if err != nil {
				return err
			}
			if err := confirmTagDelete(tagID, yes); err != nil {
				return err
			}

			client, err := instanceClient()
			if err != nil {
				return err
			}
			resolved, err := resolveTagSelector(client, "", tagID)
			if err != nil {
				return reportError(err)
			}
			if _, err := client.do("DELETE", fmt.Sprintf("/api/tags/%d", tagID), nil); err != nil {
				return reportError(err)
			}
			if flagJSON {
				return emitJSON(map[string]any{
					"ok":     true,
					"tag_id": resolved.ID,
					"tag":    resolved.Name,
					"action": "delete",
				})
			}
			fmt.Fprintf(stdout, "✓ deleted tag %s (#%d)\n", resolved.Name, resolved.ID)
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "confirm deletion without an interactive prompt")
	return c
}

func validateTagColor(color string) error {
	color = strings.TrimSpace(color)
	if color == "" {
		return nil
	}
	for _, valid := range tagColors {
		if color == valid {
			return nil
		}
	}
	return &usageError{msg: fmt.Sprintf("--color must be one of: %s", strings.Join(tagColors, ", "))}
}

func confirmTagDelete(tagID int64, yes bool) error {
	if yes {
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return &usageError{msg: "refusing to delete tag without --yes in non-interactive mode"}
	}
	fmt.Fprintf(stderr, "Delete tag #%d and remove its issue/project assignments? Type delete to confirm: ", tagID)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return err
	}
	if strings.TrimSpace(line) != "delete" {
		return &usageError{msg: "delete cancelled"}
	}
	return nil
}

func renderTagsPretty(tags []cliTag) {
	if len(tags) == 0 {
		fmt.Fprintln(stdout, "(no tags)")
		return
	}
	fmt.Fprintln(stdout, "ID     NAME                       COLOR      SYSTEM  DESCRIPTION")
	for _, tag := range tags {
		system := "no"
		if tag.System {
			system = "yes"
		}
		fmt.Fprintf(stdout, "%-6d %-26s %-10s %-7s %s\n", tag.ID, tag.Name, tag.Color, system, tag.Description)
	}
}
