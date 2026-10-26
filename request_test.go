// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

func stringReader(s string) *strings.Reader { return strings.NewReader(s) }

// marshalRequestForTest returns one valid stdio request document.
func marshalRequestForTest() (string, error) {
	raw, err := json.Marshal(Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim: sdk.Claim{
			ComponentID:   ComponentID,
			InstanceID:    "tempo-primary",
			CollectorKind: CollectorKind,
			SourceSystem:  CollectorKind,
			Scope:         sdk.Scope{ID: "tempo:trace:example-prod", Kind: "trace_source"},
			SourceRunID:   "run-1",
			GenerationID:  "tempo:generation-9",
			WorkItemID:    "work-1",
			FencingToken:  "7",
			Attempt:       1,
			Deadline:      time.Now().Add(time.Hour).UTC(),
			ConfigHandle:  "component-config:tempo",
		},
		Contract: Contract(),
		Config:   map[string]any{},
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// TestParseRequestAcceptsMinimalValidRequest proves the documented wire shape
// decodes, including the DisallowUnknownFields-tolerant empty config.
func TestParseRequestAcceptsMinimalValidRequest(t *testing.T) {
	t.Parallel()

	base, err := marshalRequestForTest()
	if err != nil {
		t.Fatal(err)
	}
	request, err := ParseRequest(stringReader(base))
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}
	if request.Claim.InstanceID != "tempo-primary" {
		t.Fatalf("InstanceID = %q, want tempo-primary", request.Claim.InstanceID)
	}
}
