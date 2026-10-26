// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"strings"
	"testing"

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

func parseConfig(t *testing.T, config map[string]any, env map[string]string) settings {
	t.Helper()

	parsed, err := parseSettings(config, func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("parseSettings() error = %v", err)
	}
	return parsed
}

// TestParseSettingsResolvesTokenEnv proves the happy path: one target with
// its credential resolved from the environment and the default result bound
// selected when the host omits the limits stanza.
func TestParseSettingsResolvesTokenEnv(t *testing.T) {
	t.Parallel()

	parsed := parseConfig(t, map[string]any{
		"targets": []any{validTarget()},
	}, map[string]string{"TEMPO_TOKEN": "token-value"})
	if len(parsed.targets) != 1 {
		t.Fatalf("targets = %d, want 1", len(parsed.targets))
	}
	if parsed.targets[0].Token != "token-value" {
		t.Fatal("target token was not resolved from the environment")
	}
	if parsed.maxResultBytes != DefaultMaxResultBytes {
		t.Fatalf("maxResultBytes = %d, want default %d", parsed.maxResultBytes, DefaultMaxResultBytes)
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
	}, map[string]string{"TEMPO_TOKEN": "token-value", "TEMPO_TENANT_ID": "env-tenant"})
	if parsed.targets[0].TenantID != "env-tenant" {
		t.Fatalf("tenant = %q, want the environment override", parsed.targets[0].TenantID)
	}

	plain := validTarget()
	plain["tenant_id"] = "static-tenant"
	parsed = parseConfig(t, map[string]any{
		"targets": []any{plain},
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
	}, map[string]string{"TEMPO_TOKEN": "token-value"})
	if len(parsed.targets) != 1 {
		t.Fatalf("targets = %d, want 1", len(parsed.targets))
	}

	bad := validTarget()
	bad["stale_after"] = "-1m"
	if _, err := parseSettings(map[string]any{
		"targets": []any{bad},
	}, func(string) string { return "x" }); err == nil {
		t.Fatal("negative stale_after: parseSettings() error = nil, want non-nil")
	}
}

// TestParseSettingsRequiresTargets proves an activation without targets
// fails before any client exists. No grant material is required: the host
// enforces authorization at emission.
func TestParseSettingsRequiresTargets(t *testing.T) {
	t.Parallel()

	if _, err := parseSettings(map[string]any{},
		func(string) string { return "x" }); err == nil {
		t.Fatal("config without targets: parseSettings() error = nil, want non-nil")
	}
	parsed, err := parseSettings(map[string]any{"targets": []any{validTarget()}},
		func(string) string { return "token-value" })
	if err != nil {
		t.Fatalf("config with targets but no grant: parseSettings() error = %v, want nil", err)
	}
	if len(parsed.targets) != 1 {
		t.Fatalf("targets = %d, want 1", len(parsed.targets))
	}
}

// TestParseSettingsAcceptsLiveActivationEnvelope proves the host contract:
// the activation config block arrives verbatim, including the shared
// host-level limits stanza and the per-target provider key, with no grant
// material. This is the literal ops-qa activation (eshu-hq/eshu#9172);
// rejecting it fails production closed with config_invalid.
func TestParseSettingsAcceptsLiveActivationEnvelope(t *testing.T) {
	t.Parallel()

	target := map[string]any{
		"provider":                "tempo",
		"scope_id":                "tempo:ops-qa",
		"instance_id":             "ops-qa",
		"base_url":                "http://monitoring-tempo-query-frontend.monitoring.svc.cluster.local:3200",
		"resource_limit":          float64(50),
		"enabled":                 true,
		"freshness_probe_enabled": true,
		"lookback":                "1h",
	}
	parsed, err := parseSettings(map[string]any{
		"targets": []any{target},
		"limits": map[string]any{
			"maxRecordsPerClaim":  float64(50000),
			"maxPayloadBytes":     float64(1048576),
			"maxResultBytes":      float64(3145728),
			"claimTimeoutSeconds": float64(300),
		},
	}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("parseSettings(live envelope) error = %v", err)
	}
	if len(parsed.targets) != 1 {
		t.Fatalf("targets = %d, want 1", len(parsed.targets))
	}
	if parsed.targets[0].ScopeID != "tempo:ops-qa" {
		t.Fatalf("ScopeID = %q, want tempo:ops-qa", parsed.targets[0].ScopeID)
	}
	if parsed.maxResultBytes != 3145728 {
		t.Fatalf("maxResultBytes = %d, want 3145728 from the limits stanza", parsed.maxResultBytes)
	}
}

// TestParseSettingsAcceptsProviderTempoAndRejectsForeign proves the
// per-target provider key is allowlisted: empty or tempo parses, anything
// else fails closed as a typo.
func TestParseSettingsAcceptsProviderTempoAndRejectsForeign(t *testing.T) {
	t.Parallel()

	good := validTarget()
	good["provider"] = "tempo"
	if _, err := parseSettings(map[string]any{"targets": []any{good}},
		func(string) string { return "token-value" }); err != nil {
		t.Fatalf("provider tempo: parseSettings() error = %v, want nil", err)
	}

	bad := validTarget()
	bad["provider"] = "loki"
	if _, err := parseSettings(map[string]any{"targets": []any{bad}},
		func(string) string { return "token-value" }); err == nil {
		t.Fatal("foreign provider: parseSettings() error = nil, want non-nil")
	}
}

// TestParseSettingsRejectsGrantMaterial proves grant material in the config
// block fails closed: authorization is host-enforced at emission and never
// crosses stdio.
func TestParseSettingsRejectsGrantMaterial(t *testing.T) {
	t.Parallel()

	config := map[string]any{
		"targets": []any{validTarget()},
		"grant":   map[string]any{"producer_id": ProducerID},
	}
	if _, err := parseSettings(config, func(string) string { return "x" }); err == nil {
		t.Fatal("config with grant: parseSettings() error = nil, want non-nil")
	}
}

// TestParseSettingsRejectsUnknownKeys proves a typo in the activation fails
// closed instead of silently running with defaults. The shared host-level
// limits stanza is allowlisted; anything else unknown still fails.
func TestParseSettingsRejectsUnknownKeys(t *testing.T) {
	t.Parallel()

	if _, err := parseSettings(map[string]any{
		"targets":  []any{validTarget()},
		"typo_key": map[string]any{"max_pages": float64(2)},
	}, func(string) string { return "x" }); err == nil {
		t.Fatal("config with unknown key: parseSettings() error = nil, want non-nil")
	}
	if _, err := parseSettings(map[string]any{
		"targets": []any{validTarget()},
		"limits":  map[string]any{"bogus": float64(1)},
	}, func(string) string { return "x" }); err == nil {
		t.Fatal("limits with unknown key: parseSettings() error = nil, want non-nil")
	}
	if _, err := parseSettings(map[string]any{
		"targets": []any{validTarget()},
		"limits":  map[string]any{"maxResultBytes": float64(maxResultBytesCeiling + 1)},
	}, func(string) string { return "x" }); err == nil {
		t.Fatal("limits over ceiling: parseSettings() error = nil, want non-nil")
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
