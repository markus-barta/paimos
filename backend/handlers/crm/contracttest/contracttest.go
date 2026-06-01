// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

// Package contracttest provides a shared provider-contract harness for
// in-tree and third-party CRM providers.
package contracttest

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/markus-barta/paimos/backend/handlers/crm"
)

type Fixture struct {
	ImportRef            string
	SyncExternalID       string
	WantImport           crm.CustomerImport
	WantSync             crm.PartialUpdate
	WantDeepLinkContains []string
}

func AssertProviderCoreFlows(t *testing.T, p crm.Provider, cfg crm.ProviderConfig, f Fixture) {
	t.Helper()

	if strings.TrimSpace(p.ID()) == "" {
		t.Fatal("provider ID must not be empty")
	}
	if strings.TrimSpace(p.Name()) == "" {
		t.Fatal("provider Name must not be empty")
	}
	if err := p.ValidateConfig(cfg.Values); err != nil {
		t.Fatalf("ValidateConfig: %v", err)
	}

	imp, err := p.ImportRef(context.Background(), f.ImportRef, cfg)
	if err != nil {
		t.Fatalf("ImportRef: %v", err)
	}
	if !reflect.DeepEqual(imp, f.WantImport) {
		t.Fatalf("ImportRef mismatch\n got: %#v\nwant: %#v", imp, f.WantImport)
	}

	upd, err := p.Sync(context.Background(), f.SyncExternalID, cfg)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !reflect.DeepEqual(upd, f.WantSync) {
		t.Fatalf("Sync mismatch\n got: %#v\nwant: %#v", upd, f.WantSync)
	}

	link := p.DeepLink(f.SyncExternalID, cfg)
	if link == "" {
		t.Fatal("DeepLink returned empty URL")
	}
	for _, want := range f.WantDeepLinkContains {
		if !strings.Contains(link, want) {
			t.Fatalf("DeepLink=%q, want substring %q", link, want)
		}
	}
}
