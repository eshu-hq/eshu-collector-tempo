// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"strings"
	"testing"
	"time"

	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// validTarget returns the minimal token_env target the config tests reuse.
func validTarget() map[string]any {
	return map[string]any{
		"scope_id":                "tempo:trace:example-prod",
		"instance_id":             "tempo-example",
		"base_url":                "https://tempo.example.test",
		"token_env":               "TEMPO_TOKEN",
		"resource_limit":          float64(50),
		"max_tag_values_per_tag":  float64(20),
		"enabled":                 true,
		"freshness_probe_enabled": true,
		"lookback":                "1h",
	}
}

// liveGrant returns a grant object valid at time.Now: the three observed
// kinds at 1.0.0 inside the 720h lifetime bound.
func liveGrant() map[string]any {
	now := time.Now().UTC()
	return map[string]any{
		"producer_id": ProducerID,
		"version":     "0.1.0",
		"scope":       CollectorKind,
		"kinds": []any{
			map[string]any{"kind": FactKindSourceInstance, "schema_versions": []any{"1.0.0"}},
			map[string]any{"kind": FactKindObservedTraceSignal, "schema_versions": []any{"1.0.0"}},
			map[string]any{"kind": FactKindCoverageWarning, "schema_versions": []any{"1.0.0"}},
		},
		"issued_at":  now.Format(time.RFC3339),
		"expires_at": now.Add(time.Hour).UTC().Format(time.RFC3339),
	}
}

func parseConfig(t *testing.T, config map[string]any, env map[string]string) settings {
	t.Helper()

	parsed, err := parseSettings(config, func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("parseSettings() error = %v", err)
	}
	return parsed
}

// TestParseSettingsResolvesTokenEnv proves the happy path: one target with
// its credential resolved from the environment and the grant carried through
// with its issued_at bound.
func TestParseSettingsResolvesTokenEnv(t *testing.T) {
	t.Parallel()

	parsed := parseConfig(t, map[string]any{
		"targets": []any{validTarget()},
		"grant":   liveGrant(),
	}, map[string]string{"TEMPO_TOKEN": "token-value"})
	if len(parsed.targets) != 1 {
		t.Fatalf("targets = %d, want 1", len(parsed.targets))
	}
	if parsed.targets[0].Token != "token-value" {
		t.Fatal("target token was not resolved from the environment")
	}
	if parsed.grant.ProducerID != ProducerID {
		t.Fatalf("grant producer = %q, want %q", parsed.grant.ProducerID, ProducerID)
	}
	if parsed.grant.IssuedAt.IsZero() || parsed.grant.ExpiresAt.IsZero() {
		t.Fatal("grant must carry the issued_at/expires_at lifetime bound")
	}
}

// TestParseSettingsAllowsEmptyTokenEnv proves an empty token_env means
// unauthenticated Tempo instead of a failure: the target parses with no
// credential and emission proceeds without an Authorization header.
func TestParseSettingsAllowsEmptyTokenEnv(t *testing.T) {
	t.Parallel()

	target := validTarget()
	target["token_env"] = ""
	parsed := parseConfig(t, map[string]any{
		"targets": []any{target},
		"grant":   liveGrant(),
	}, map[string]string{})
	if parsed.targets[0].Token != "" {
		t.Fatalf("token = %q, want empty for unauthenticated Tempo", parsed.targets[0].Token)
	}
}

// TestParseSettingsRejectsUnresolvableToken fails closed naming the
// variable, never the credential value.
func TestParseSettingsRejectsUnresolvableToken(t *testing.T) {
	t.Parallel()

	_, err := parseSettings(map[string]any{
		"targets": []any{validTarget()},
		"grant":   liveGrant(),
	}, func(string) string { return "" })
	if err == nil {
		t.Fatal("parseSettings() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "TEMPO_TOKEN") {
		t.Fatalf("error %q does not name the variable", err.Error())
	}
}

// TestParseSettingsResolvesTenantIDEnv proves tenant_id_env overrides the
// static tenant_id, and the static value stands when no variable is named.
func TestParseSettingsResolvesTenantIDEnv(t *testing.T) {
	t.Parallel()

	target := validTarget()
	target["tenant_id"] = "static-tenant"
	target["tenant_id_env"] = "TEMPO_TENANT_ID"
	parsed := parseConfig(t, map[string]any{
		"targets": []any{target},
		"grant":   liveGrant(),
	}, map[string]string{"TEMPO_TOKEN": "token-value", "TEMPO_TENANT_ID": "env-tenant"})
	if parsed.targets[0].TenantID != "env-tenant" {
		t.Fatalf("tenant = %q, want the environment override", parsed.targets[0].TenantID)
	}

	plain := validTarget()
	plain["tenant_id"] = "static-tenant"
	parsed = parseConfig(t, map[string]any{
		"targets": []any{plain},
		"grant":   liveGrant(),
	}, map[string]string{"TEMPO_TOKEN": "token-value"})
	if parsed.targets[0].TenantID != "static-tenant" {
		t.Fatalf("tenant = %q, want the static value", parsed.targets[0].TenantID)
	}
}

// TestParseSettingsAcceptsCoreOwnedStaleAfter proves stale_after is accepted
// for activation parity and validated, but never reaches the producer: the
// extracted TargetConfig carries no such field because freshness windowing
// stays core-owned.
func TestParseSettingsAcceptsCoreOwnedStaleAfter(t *testing.T) {
	t.Parallel()

	target := validTarget()
	target["stale_after"] = "30m"
	parsed := parseConfig(t, map[string]any{
		"targets": []any{target},
		"grant":   liveGrant(),
	}, map[string]string{"TEMPO_TOKEN": "token-value"})
	if len(parsed.targets) != 1 {
		t.Fatalf("targets = %d, want 1", len(parsed.targets))
	}

	bad := validTarget()
	bad["stale_after"] = "-1m"
	if _, err := parseSettings(map[string]any{
		"targets": []any{bad},
		"grant":   liveGrant(),
	}, func(string) string { return "x" }); err == nil {
		t.Fatal("negative stale_after: parseSettings() error = nil, want non-nil")
	}
}

// TestParseSettingsRequiresTargetsAndGrant proves an activation without
// targets or without the host-delivered grant fails before any client exists.
func TestParseSettingsRequiresTargetsAndGrant(t *testing.T) {
	t.Parallel()

	if _, err := parseSettings(map[string]any{"grant": liveGrant()},
		func(string) string { return "x" }); err == nil {
		t.Fatal("config without targets: parseSettings() error = nil, want non-nil")
	}
	if _, err := parseSettings(map[string]any{"targets": []any{validTarget()}},
		func(string) string { return "x" }); err == nil {
		t.Fatal("config without grant: parseSettings() error = nil, want non-nil")
	}
}

// TestParseSettingsRejectsUnknownKeys proves a typo in the activation fails
// closed instead of silently running with defaults.
func TestParseSettingsRejectsUnknownKeys(t *testing.T) {
	t.Parallel()

	if _, err := parseSettings(map[string]any{
		"targets": []any{validTarget()},
		"grant":   liveGrant(),
		"limits":  map[string]any{"max_pages": float64(2)},
	}, func(string) string { return "x" }); err == nil {
		t.Fatal("config with unknown key: parseSettings() error = nil, want non-nil")
	}
}

// TestParseRequestRejectsBadShapes proves the stdio boundary fails closed on
// trailing JSON, unknown fields, a foreign protocol, a foreign collector
// kind, and a missing generation.
func TestParseRequestRejectsBadShapes(t *testing.T) {
	t.Parallel()

	base, err := marshalRequestForTest()
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(string) string{
		"trailing JSON":    func(s string) string { return s + " {}" },
		"unknown field":    func(s string) string { return strings.Replace(s, `"claim":`, `"bogus":1,"claim":`, 1) },
		"foreign protocol": func(s string) string { return strings.Replace(s, sdk.ProtocolVersionV1Alpha1, "other/v9", 1) },
		"foreign kind": func(s string) string {
			return strings.Replace(s, `"collector_kind":"tempo"`, `"collector_kind":"package_registry"`, 1)
		},
		"missing generation": func(s string) string {
			return strings.Replace(s, `"generation_id":"tempo:generation-9",`, "", 1)
		},
	}
	for name, mutate := range cases {
		if _, err := ParseRequest(strings.NewReader(mutate(base))); err == nil {
			t.Fatalf("%s: ParseRequest() error = nil, want non-nil", name)
		}
	}
}
