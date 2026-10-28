// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// errTestFactory is the canned client-construction failure.
var errTestFactory = errors.New("test factory failure")

// TestValidateTargetFailsClosed proves every malformed target is rejected
// with a naming error before any provider contact.
func TestValidateTargetFailsClosed(t *testing.T) {
	t.Parallel()

	valid := TargetConfig{
		ScopeID:    "tempo:trace:prod",
		InstanceID: "tempo-main",
		BaseURL:    "https://tempo.example.test/",
		Enabled:    true,
	}
	for _, row := range []struct {
		name    string
		mutate  func(*TargetConfig)
		wantErr string
	}{
		{"blank scope", func(c *TargetConfig) { c.ScopeID = "  " }, "scope_id is required"},
		{"blank instance", func(c *TargetConfig) { c.InstanceID = "" }, "instance_id is required"},
		{"blank base URL", func(c *TargetConfig) { c.BaseURL = " " }, "base_url is required"},
		{"negative resource limit", func(c *TargetConfig) { c.ResourceLimit = -1 }, "resource_limit must be between"},
		{"oversized resource limit", func(c *TargetConfig) { c.ResourceLimit = maxResourceLimit + 1 }, "resource_limit must be between"},
		{"negative tag value limit", func(c *TargetConfig) { c.MaxTagValuesPerTag = -1 }, "max_tag_values_per_tag must be between"},
		{"oversized tag value limit", func(c *TargetConfig) { c.MaxTagValuesPerTag = maxTagValueLimit + 1 }, "max_tag_values_per_tag must be between"},
		{"negative lookback", func(c *TargetConfig) { c.Lookback = -time.Minute }, "lookback must be positive"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			candidate := valid
			row.mutate(&candidate)
			if _, err := validateTarget(candidate); err == nil || !strings.Contains(err.Error(), row.wantErr) {
				t.Fatalf("validateTarget() error = %v, want containing %q", err, row.wantErr)
			}
		})
	}
}

// TestValidateTargetAppliesDefaults proves unset limits resolve to the
// bounded defaults while the base URL and path prefix normalize.
func TestValidateTargetAppliesDefaults(t *testing.T) {
	t.Parallel()

	got, err := validateTarget(TargetConfig{
		ScopeID:    "  tempo:trace:prod ",
		InstanceID: "tempo-main",
		BaseURL:    "https://tempo.example.test/",
		PathPrefix: "///",
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("validateTarget() error = %v, want nil", err)
	}
	if got.ScopeID != "tempo:trace:prod" {
		t.Fatalf("ScopeID = %q, want trimmed", got.ScopeID)
	}
	if got.BaseURL != "https://tempo.example.test" {
		t.Fatalf("BaseURL = %q, want trailing slash trimmed", got.BaseURL)
	}
	if got.PathPrefix != "" {
		t.Fatalf("PathPrefix = %q, want bare slash collapsed", got.PathPrefix)
	}
	if got.ResourceLimit != defaultResourceLimit {
		t.Fatalf("ResourceLimit = %d, want default %d", got.ResourceLimit, defaultResourceLimit)
	}
	if got.MaxTagValuesPerTag != defaultTagValueLimit {
		t.Fatalf("MaxTagValuesPerTag = %d, want default %d", got.MaxTagValuesPerTag, defaultTagValueLimit)
	}
	if got.Lookback != defaultLookback {
		t.Fatalf("Lookback = %v, want default %v", got.Lookback, defaultLookback)
	}
}

// TestNewClaimedSourceFailsClosed proves source construction rejects a
// missing identity, an invalid target, a duplicate scope, and a factory
// failure before any provider contact.
func TestNewClaimedSourceFailsClosed(t *testing.T) {
	t.Parallel()

	validTarget := TargetConfig{
		ScopeID:    "tempo:trace:prod",
		InstanceID: "tempo-main",
		BaseURL:    "https://tempo.example.test",
		Enabled:    true,
	}
	failingFactory := func(TargetConfig) (EvidenceClient, error) {
		return nil, errTestFactory
	}
	for _, row := range []struct {
		name    string
		config  SourceConfig
		wantErr string
	}{
		{
			"blank collector identity",
			SourceConfig{CollectorInstanceID: "  ", Targets: []TargetConfig{validTarget}},
			"collector instance ID is required",
		},
		{
			"invalid target",
			SourceConfig{CollectorInstanceID: "collector-1", Targets: []TargetConfig{{Enabled: true}}},
			"scope_id is required",
		},
		{
			"duplicate scope",
			SourceConfig{CollectorInstanceID: "collector-1", Targets: []TargetConfig{validTarget, validTarget}},
			"duplicate tempo target scope_id",
		},
		{
			"factory failure",
			SourceConfig{CollectorInstanceID: "collector-1", Targets: []TargetConfig{validTarget}, ClientFactory: failingFactory},
			"client",
		},
		{
			"no enabled targets",
			SourceConfig{CollectorInstanceID: "collector-1", Targets: []TargetConfig{{Enabled: false}}},
			"at least one enabled tempo target is required",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			if _, err := NewClaimedSource(row.config); err == nil || !strings.Contains(err.Error(), row.wantErr) {
				t.Fatalf("NewClaimedSource() error = %v, want containing %q", err, row.wantErr)
			}
		})
	}
}
