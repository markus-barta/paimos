// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type cliTimeEntry struct {
	ID                 int64    `json:"id"`
	IssueID            int64    `json:"issue_id"`
	UserID             int64    `json:"user_id"`
	Username           string   `json:"username,omitempty"`
	StartedAt          string   `json:"started_at"`
	StoppedAt          *string  `json:"stopped_at"`
	Override           *float64 `json:"override"`
	Comment            string   `json:"comment"`
	CreatedAt          string   `json:"created_at"`
	InternalRateHourly *float64 `json:"internal_rate_hourly"`
	Hours              *float64 `json:"hours,omitempty"`
	IssueKey           string   `json:"issue_key,omitempty"`
	IssueTitle         string   `json:"issue_title,omitempty"`
	ProjectID          int64    `json:"project_id,omitempty"`
}

func timeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "time",
		Short: "Manage issue time entries",
	}
	c.AddCommand(timeStartCmd())
	c.AddCommand(timeStopCmd())
	c.AddCommand(timeListCmd())
	c.AddCommand(timeSetCmd())
	c.AddCommand(timeGetCmd())
	return c
}

func timeStartCmd() *cobra.Command {
	var note, startedAt string
	c := &cobra.Command{
		Use:   "start <issue-ref>",
		Short: "Start a running time entry on an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := instanceClient()
			if err != nil {
				return err
			}
			issueID, err := resolveIssueRefToID(client, args[0])
			if err != nil {
				return reportError(err)
			}
			body := map[string]any{}
			if strings.TrimSpace(note) != "" {
				body["comment"] = note
			}
			if strings.TrimSpace(startedAt) != "" {
				body["started_at"] = startedAt
			}
			raw, err := client.do("POST", fmt.Sprintf("/api/issues/%d/time-entries", issueID), body)
			if err != nil {
				return reportError(err)
			}
			return emitTimeEntry(raw, "started")
		},
	}
	c.Flags().StringVar(&note, "note", "", "entry note/comment")
	c.Flags().StringVar(&startedAt, "started-at", "", "override start timestamp (RFC3339 recommended)")
	return c
}

func timeStopCmd() *cobra.Command {
	var stoppedAt string
	c := &cobra.Command{
		Use:   "stop [id]",
		Short: "Stop a running time entry",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := instanceClient()
			if err != nil {
				return err
			}
			id := int64(0)
			if len(args) == 1 {
				id, err = parsePositiveInt64Flag("id", args[0])
				if err != nil {
					return err
				}
			} else {
				running, err := fetchRunningTimers(client)
				if err != nil {
					return reportError(err)
				}
				if len(running) == 0 {
					if flagJSON {
						return emitJSON(map[string]any{"running": false})
					}
					fmt.Fprintln(stdout, "(no running timer)")
					return nil
				}
				if len(running) > 1 {
					return fmt.Errorf("multiple running timers; pass an id to stop one explicitly")
				}
				id = running[0].ID
			}
			if strings.TrimSpace(stoppedAt) == "" {
				stoppedAt = time.Now().UTC().Format("2006-01-02T15:04:05Z")
			}
			raw, err := client.do("PUT", fmt.Sprintf("/api/time-entries/%d", id), map[string]any{"stopped_at": stoppedAt})
			if err != nil {
				return reportError(err)
			}
			return emitTimeEntry(raw, "stopped")
		},
	}
	c.Flags().StringVar(&stoppedAt, "stopped-at", "", "override stop timestamp (RFC3339 recommended)")
	return c
}

func timeListCmd() *cobra.Command {
	var running, recent bool
	var issueRef string
	var limit int
	c := &cobra.Command{
		Use:   "list",
		Short: "List running, recent, or issue time entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			selected := 0
			if running {
				selected++
			}
			if recent {
				selected++
			}
			if strings.TrimSpace(issueRef) != "" {
				selected++
			}
			if selected != 1 {
				return &usageError{msg: "choose exactly one of --running, --recent, or --issue"}
			}
			if limit < 0 {
				return &usageError{msg: "--limit must be 0 or greater"}
			}
			client, err := instanceClient()
			if err != nil {
				return err
			}
			path := "/api/time-entries/running"
			if recent {
				path = "/api/time-entries/recent"
			}
			if issueRef != "" {
				issueID, err := resolveIssueRefToID(client, issueRef)
				if err != nil {
					return reportError(err)
				}
				path = fmt.Sprintf("/api/issues/%d/time-entries", issueID)
			}
			raw, err := client.do("GET", path, nil)
			if err != nil {
				return reportError(err)
			}
			var entries []cliTimeEntry
			if err := json.Unmarshal(raw, &entries); err != nil {
				return fmt.Errorf("decode time entries: %w", err)
			}
			if limit > 0 && len(entries) > limit {
				entries = entries[:limit]
			}
			if flagJSON {
				return emitJSON(entries)
			}
			renderTimeEntries(entries)
			return nil
		},
	}
	c.Flags().BoolVar(&running, "running", false, "list running timers")
	c.Flags().BoolVar(&recent, "recent", false, "list recently stopped timers")
	c.Flags().StringVar(&issueRef, "issue", "", "issue key or id")
	c.Flags().IntVar(&limit, "limit", 0, "client-side maximum rows")
	return c
}

func timeSetCmd() *cobra.Command {
	var durationRaw, note, startedAt, stoppedAt string
	var clearDuration bool
	c := &cobra.Command{
		Use:   "set <id>",
		Short: "Edit a time entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parsePositiveInt64Flag("id", args[0])
			if err != nil {
				return err
			}
			body := map[string]any{}
			if cmd.Flags().Changed("note") {
				body["comment"] = note
			}
			if strings.TrimSpace(startedAt) != "" {
				body["started_at"] = startedAt
			}
			if strings.TrimSpace(stoppedAt) != "" {
				body["stopped_at"] = stoppedAt
			}
			if strings.TrimSpace(durationRaw) != "" {
				if clearDuration {
					return &usageError{msg: "--duration and --clear-duration cannot be combined"}
				}
				hours, err := parseDurationHours(durationRaw)
				if err != nil {
					return err
				}
				body["override"] = hours
			}
			if clearDuration {
				body["clear_override"] = true
			}
			if len(body) == 0 {
				return &usageError{msg: "nothing to update — pass --duration, --clear-duration, --note, --started-at, or --stopped-at"}
			}
			client, err := instanceClient()
			if err != nil {
				return err
			}
			raw, err := client.do("PUT", fmt.Sprintf("/api/time-entries/%d", id), body)
			if err != nil {
				return reportError(err)
			}
			return emitTimeEntry(raw, "updated")
		},
	}
	c.Flags().StringVar(&durationRaw, "duration", "", "manual duration override (Go duration, e.g. 90m, 1h30m)")
	c.Flags().BoolVar(&clearDuration, "clear-duration", false, "clear manual duration override")
	c.Flags().StringVar(&note, "note", "", "entry note/comment")
	c.Flags().StringVar(&startedAt, "started-at", "", "set start timestamp (RFC3339 recommended)")
	c.Flags().StringVar(&stoppedAt, "stopped-at", "", "set stop timestamp (RFC3339 recommended)")
	return c
}

func timeGetCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "get <id>",
		Short: "Get one time entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parsePositiveInt64Flag("id", args[0])
			if err != nil {
				return err
			}
			client, err := instanceClient()
			if err != nil {
				return err
			}
			raw, err := client.do("GET", "/api/time-entries/"+url.PathEscape(strconv.FormatInt(id, 10)), nil)
			if err != nil {
				return reportError(err)
			}
			return emitTimeEntry(raw, "")
		},
	}
	return c
}

func fetchRunningTimers(client *Client) ([]cliTimeEntry, error) {
	raw, err := client.do("GET", "/api/time-entries/running", nil)
	if err != nil {
		return nil, err
	}
	var entries []cliTimeEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("decode running timers: %w", err)
	}
	return entries, nil
}

func emitTimeEntry(raw []byte, verb string) error {
	if flagJSON {
		fmt.Fprintln(stdout, strings.TrimSpace(string(raw)))
		return nil
	}
	var entry cliTimeEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return fmt.Errorf("decode time entry: %w", err)
	}
	if verb == "" {
		renderTimeEntries([]cliTimeEntry{entry})
		return nil
	}
	fmt.Fprintf(stdout, "✓ %s time entry #%d on %s\n", verb, entry.ID, timeEntryIssueLabel(entry))
	return nil
}

func renderTimeEntries(entries []cliTimeEntry) {
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "(no time entries)")
		return
	}
	fmt.Fprintln(stdout, "ID      ISSUE          STARTED              STOPPED              HOURS    NOTE")
	for _, e := range entries {
		fmt.Fprintf(stdout, "%-7d %-14s %-20s %-20s %-8s %s\n",
			e.ID,
			timeEntryIssueLabel(e),
			compactTimestamp(e.StartedAt),
			compactOptionalTimestamp(e.StoppedAt),
			timeEntryHours(e),
			truncate(e.Comment, 50),
		)
	}
}

func parseDurationHours(raw string) (float64, error) {
	d, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || d <= 0 {
		return 0, &usageError{msg: "--duration must be a positive Go duration, e.g. 90m or 1h30m"}
	}
	return d.Hours(), nil
}

func timeEntryIssueLabel(e cliTimeEntry) string {
	if strings.TrimSpace(e.IssueKey) != "" {
		return e.IssueKey
	}
	return strconv.FormatInt(e.IssueID, 10)
}

func compactTimestamp(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 19 {
		return s[:19]
	}
	return s
}

func compactOptionalTimestamp(s *string) string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return "running"
	}
	return compactTimestamp(*s)
}

func timeEntryHours(e cliTimeEntry) string {
	if e.Hours == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f", *e.Hours)
}
