// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func curlCmd() *cobra.Command {
	var method, data, dataFile string
	cmd := &cobra.Command{
		Use:   "curl <api-path>",
		Short: "Call a raw PAIMOS API path with configured auth",
		Long: `Call a raw PAIMOS API path with the active instance URL and API key.

The path may be either /api/... or a shorthand path such as /portal/overview.
Shorthand paths are prefixed with /api. The response body is written raw to
stdout so callers can pipe it to jq.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if data != "" && dataFile != "" {
				return &usageError{msg: "--data and --data-file are mutually exclusive"}
			}
			client, err := instanceClient()
			if err != nil {
				return err
			}
			path := normalizeRawAPIPath(args[0])
			method = strings.ToUpper(strings.TrimSpace(method))
			if method == "" {
				method = http.MethodGet
			}
			var body []byte
			if dataFile != "" {
				if dataFile == "-" {
					body, err = io.ReadAll(os.Stdin)
				} else {
					body, err = os.ReadFile(dataFile)
				}
				if err != nil {
					return fmt.Errorf("read data file: %w", err)
				}
			} else if data != "" {
				body = []byte(data)
			}
			raw, err := client.doRaw(method, path, body)
			if err != nil {
				return &apiError{inner: err}
			}
			if _, err := stdout.Write(raw); err != nil {
				return err
			}
			if len(raw) == 0 || raw[len(raw)-1] != '\n' {
				_, _ = fmt.Fprintln(stdout)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&method, "method", "X", http.MethodGet, "HTTP method")
	cmd.Flags().StringVar(&data, "data", "", "inline request body")
	cmd.Flags().StringVar(&dataFile, "data-file", "", "request body file, or - for stdin")
	return cmd
}

func normalizeRawAPIPath(raw string) string {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "/api"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if strings.HasPrefix(path, "/api/") || path == "/api" {
		return path
	}
	return "/api" + path
}

func (c *Client) doRaw(method, path string, body []byte) ([]byte, error) {
	var r io.Reader
	if len(body) > 0 {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.baseURL+path, r)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	c.prepareRequest(req, len(body) > 0, "application/json", "*/*")
	return c.doRequest(req)
}
