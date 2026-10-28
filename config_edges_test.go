// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"strings"
	"testing"
)

// edgeTarget returns one minimal valid target map for config edge tests.
func edgeTarget() map[string]any {
	return map[string]any{
		"provider":    "tempo",
		"scope_id":    "tempo:trace:example-prod",
		"instance_id": "tempo-example",
		"base_url":    "https://tempo.example.test",
		"enabled":     true,
	}
}

// TestParseSettingsRejectsUnencodableConfig proves a config carrying a
// non-JSON value fails before decoding, never echoing the value.
func TestParseSettingsRejectsUnencodableConfig(t *testing.T) {
	t.Parallel()

	config := map[string]any{
		"targets": []any{edgeTarget()},
		"boom":    func() {},
	}
	if _, err := parseSettings(config, nil); err == nil || !strings.Contains(err.Error(), "encode config") {
		t.Fatalf("parseSettings() error = %v, want an encode failure", err)
	}
}

// TestMapTargetResolvesCredentialEdges proves tenant indirection, the
// accepted-but-dropped stale window, and the lookback each fail closed on
// bad input.
func TestMapTargetResolvesCredentialEdges(t *testing.T) {
	t.Parallel()

	empty := func(string) string { return "" }
	for _, row := range []struct {
		name    string
		mutate  func(map[string]any)
		wantErr string
	}{
		{"unset tenant env", func(m map[string]any) { m["tenant_id_env"] = "MISSING_TENANT" }, "did not resolve a value"},
		{"bad stale_after", func(m map[string]any) { m["stale_after"] = "not-a-duration" }, "parse stale_after"},
		{"bad lookback", func(m map[string]any) { m["lookback"] = "not-a-duration" }, "parse lookback"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			target := edgeTarget()
			row.mutate(target)
			raw := map[string]any{"targets": []any{target}}
			if _, err := parseSettings(raw, empty); err == nil || !strings.Contains(err.Error(), row.wantErr) {
				t.Fatalf("parseSettings() error = %v, want containing %q", err, row.wantErr)
			}
		})
	}
}

// TestStringSetBuildsIdentitySet proves blank entries collapse, values
// trim, duplicates merge, and an empty list stays nil.
func TestStringSetBuildsIdentitySet(t *testing.T) {
	t.Parallel()

	if got := stringSet(nil); got != nil {
		t.Fatalf("stringSet(nil) = %#v, want nil", got)
	}
	got := stringSet([]string{" b ", "", "a", "b", "  ", "a"})
	if len(got) != 2 {
		t.Fatalf("stringSet() = %#v, want two members", got)
	}
	for _, want := range []string{"a", "b"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("stringSet() = %#v, want member %q", got, want)
		}
	}
}
