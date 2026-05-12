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
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type cliAttachment struct {
	ID          int64  `json:"id"`
	IssueID     int64  `json:"issue_id"`
	ObjectKey   string `json:"object_key"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	UploadedBy  int64  `json:"uploaded_by"`
	Uploader    string `json:"uploader,omitempty"`
	CreatedAt   string `json:"created_at"`
}

func attachCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "attach <issue-ref> <file>",
		Short: "Upload and manage issue attachments",
		Long: `Upload and manage issue attachments.

The top-level form is the agent-friendly one-shot:
  paimos attach PAI-83 ./screenshot.png

It uploads the file as a pending attachment, links it to the issue, and
rolls the pending row back if linking fails.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAttachOneShot(args[0], args[1])
		},
	}
	c.AddCommand(attachListCmd())
	c.AddCommand(attachGetCmd())
	c.AddCommand(attachRmCmd())
	return c
}

func runAttachOneShot(issueRef, filePath string) error {
	client, err := instanceClient()
	if err != nil {
		return err
	}
	issueID, err := resolveIssueRefToID(client, issueRef)
	if err != nil {
		return reportError(err)
	}
	raw, err := client.doMultipartFile("/api/attachments", "file", filePath)
	if err != nil {
		return reportError(err)
	}
	var uploaded cliAttachment
	if err := json.Unmarshal(raw, &uploaded); err != nil {
		return fmt.Errorf("decode uploaded attachment: %w", err)
	}
	if uploaded.ID <= 0 {
		return fmt.Errorf("upload response missing attachment id")
	}

	linkRaw, err := client.do("PATCH", "/api/attachments/link", map[string]any{
		"issue_id":       issueID,
		"attachment_ids": []int64{uploaded.ID},
	})
	if err != nil {
		rollbackAttachment(client, uploaded.ID)
		return reportError(err)
	}
	var linked struct {
		Linked int `json:"linked"`
	}
	if err := json.Unmarshal(linkRaw, &linked); err != nil {
		rollbackAttachment(client, uploaded.ID)
		return fmt.Errorf("decode link response: %w", err)
	}
	if linked.Linked != 1 {
		rollbackAttachment(client, uploaded.ID)
		return fmt.Errorf("link failed: attachment %d was not linked to issue %s", uploaded.ID, issueRef)
	}

	meta, err := fetchAttachmentMeta(client, uploaded.ID)
	if err != nil {
		uploaded.IssueID = issueID
		meta = uploaded
	}
	if flagJSON {
		return emitJSON(meta)
	}
	fmt.Fprintf(stdout, "✓ attached %s (#%d) to %s\n", meta.Filename, meta.ID, issueRef)
	return nil
}

func attachListCmd() *cobra.Command {
	var issueRef string
	c := &cobra.Command{
		Use:   "list --issue <ref>",
		Short: "List attachments on an issue",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(issueRef) == "" {
				return &usageError{msg: "--issue is required"}
			}
			client, err := instanceClient()
			if err != nil {
				return err
			}
			issueID, err := resolveIssueRefToID(client, issueRef)
			if err != nil {
				return reportError(err)
			}
			raw, err := client.do("GET", fmt.Sprintf("/api/issues/%d/attachments", issueID), nil)
			if err != nil {
				return reportError(err)
			}
			if flagJSON {
				fmt.Fprintln(stdout, strings.TrimSpace(string(raw)))
				return nil
			}
			var rows []cliAttachment
			if err := json.Unmarshal(raw, &rows); err != nil {
				return fmt.Errorf("decode attachments: %w", err)
			}
			renderAttachments(rows)
			return nil
		},
	}
	c.Flags().StringVar(&issueRef, "issue", "", "issue key or id")
	return c
}

func attachGetCmd() *cobra.Command {
	var downloadPath string
	c := &cobra.Command{
		Use:   "get <id>",
		Short: "Get attachment metadata, optionally downloading the file",
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
			meta, err := fetchAttachmentMeta(client, id)
			if err != nil {
				return reportError(err)
			}
			if downloadPath != "" {
				if flagJSON && downloadPath == "-" {
					return &usageError{msg: "--json cannot be combined with --download -"}
				}
				data, err := client.doDownload(fmt.Sprintf("/api/attachments/%d", id))
				if err != nil {
					return reportError(err)
				}
				if downloadPath == "-" {
					_, err = stdout.Write(data)
					return err
				}
				if err := os.WriteFile(downloadPath, data, 0600); err != nil {
					return err
				}
				if flagJSON {
					return emitJSON(map[string]any{
						"attachment":    meta,
						"downloaded_to": downloadPath,
					})
				}
				fmt.Fprintf(stdout, "✓ downloaded %s (#%d) to %s\n", meta.Filename, meta.ID, downloadPath)
				return nil
			}
			if flagJSON {
				return emitJSON(meta)
			}
			renderAttachments([]cliAttachment{meta})
			return nil
		},
	}
	c.Flags().StringVar(&downloadPath, "download", "", "write file bytes to path, or - for stdout")
	return c
}

func attachRmCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "rm <id>",
		Aliases: []string{"delete"},
		Short:   "Delete an attachment",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parsePositiveInt64Flag("id", args[0])
			if err != nil {
				return err
			}
			client, err := instanceClient()
			if err != nil {
				return err
			}
			if _, err := client.do("DELETE", fmt.Sprintf("/api/attachments/%d", id), nil); err != nil {
				return reportError(err)
			}
			if flagJSON {
				return emitJSON(map[string]any{"deleted": true, "id": id})
			}
			fmt.Fprintf(stdout, "✓ deleted attachment #%d\n", id)
			return nil
		},
	}
	return c
}

func fetchAttachmentMeta(client *Client, id int64) (cliAttachment, error) {
	raw, err := client.do("GET", fmt.Sprintf("/api/attachments/%d/meta", id), nil)
	if err != nil {
		return cliAttachment{}, err
	}
	var meta cliAttachment
	if err := json.Unmarshal(raw, &meta); err != nil {
		return cliAttachment{}, fmt.Errorf("decode attachment metadata: %w", err)
	}
	return meta, nil
}

func rollbackAttachment(client *Client, id int64) {
	if id <= 0 {
		return
	}
	_, _ = client.do("DELETE", fmt.Sprintf("/api/attachments/%d", id), nil)
}

func renderAttachments(rows []cliAttachment) {
	if len(rows) == 0 {
		fmt.Fprintln(stdout, "(no attachments)")
		return
	}
	fmt.Fprintln(stdout, "ID      ISSUE     SIZE        TYPE                          FILENAME")
	for _, a := range rows {
		fmt.Fprintf(stdout, "%-7d %-9s %-11s %-29s %s\n",
			a.ID,
			attachmentIssueLabel(a.IssueID),
			formatBytes(a.SizeBytes),
			truncate(a.ContentType, 29),
			a.Filename,
		)
	}
}

func attachmentIssueLabel(id int64) string {
	if id <= 0 {
		return "pending"
	}
	return strconv.FormatInt(id, 10)
}

func formatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
