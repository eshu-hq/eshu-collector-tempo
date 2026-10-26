// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	sdk "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu-collector-tempo/tempo"
)

// stubTempoClient serves one canned Tempo metadata snapshot without a
// network.
type stubTempoClient struct {
	snapshot tempo.CollectionResult
	err      error
}

func (s stubTempoClient) CollectObservedMetadata(
	context.Context, tempo.TargetConfig,
) (tempo.CollectionResult, error) {
	return s.snapshot, s.err
}

// fixtureSnapshot is the canonical synthetic Tempo snapshot this package's
// tests collect: one source instance and one tag-value signal.
func fixtureSnapshot() tempo.CollectionResult {
	return tempo.CollectionResult{
		Source: tempo.SourceInstance{
			Provider:         tempo.ProviderTempo,
			SourceInstanceID: "tempo-example",
		},
		Signals: []tempo.TraceSignal{{
			ProviderObjectID: "tagvalues:resource.service.name",
			SignalKind:       tempo.SignalKindTagValues,
			TagScope:         "resource",
			TagName:          "resource.service.name",
			TagValueCount:    2,
			TagValueHashes:   []string{"recorded-a", "recorded-b"},
		}},
		ObservedAt: time.Now().UTC().Truncate(time.Second),
	}
}

// testClaim returns the claim every handler test serves.
func testClaim() sdk.Claim {
	return sdk.Claim{
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
	}
}

// testConfig returns the activation config every handler test serves: one
// token_env Tempo target with the shared limits stanza and no grant material.
// Authorization is host-enforced at emission.
func testConfig(t *testing.T) map[string]any {
	t.Helper()

	return map[string]any{
		"targets": []any{map[string]any{
			"provider":               "tempo",
			"scope_id":               "tempo:trace:example-prod",
			"instance_id":            "tempo-example",
			"base_url":               "https://tempo.example.test",
			"token_env":              "TEMPO_TOKEN",
			"resource_limit":         float64(50),
			"max_tag_values_per_tag": float64(20),
			"enabled":                true,
			"lookback":               "1h",
		}},
		"limits": map[string]any{
			"maxRecordsPerClaim":  float64(50000),
			"maxPayloadBytes":     float64(1048576),
			"maxResultBytes":      float64(3500000),
			"claimTimeoutSeconds": float64(300),
		},
	}
}

// testHandler returns a Handler wired to the stub client and a static env.
func testHandler(env map[string]string, snapshot tempo.CollectionResult) Handler {
	return Handler{
		Getenv: func(key string) string { return env[key] },
		Now:    func() time.Time { return time.Now().UTC() },
		ClientFactory: func(tempo.TargetConfig) (tempo.EvidenceClient, error) {
			return stubTempoClient{snapshot: snapshot}, nil
		},
	}
}

// TestHandleServesOneValidatorCleanResult serves one claim against the stub
// client and asserts the SDK result is complete, carries source and signal
// facts, and passes the published SDK validator against this package's
// contract.
func TestHandleServesOneValidatorCleanResult(t *testing.T) {
	t.Parallel()

	env := map[string]string{"TEMPO_TOKEN": "token-value"}
	handler := testHandler(env, fixtureSnapshot())
	result := handler.Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           testClaim(),
		Contract:        Contract(),
		Config:          testConfig(t),
	})
	if result.State != sdk.ResultComplete {
		t.Fatalf("State = %q, want complete", result.State)
	}
	if len(result.Facts) != 2 {
		t.Fatalf("facts = %d, want 2 (source instance plus one signal)", len(result.Facts))
	}
	if result.Facts[0].Kind != FactKindSourceInstance {
		t.Fatalf("Kind = %q, want %q", result.Facts[0].Kind, FactKindSourceInstance)
	}
	if result.Claim.InstanceID != "tempo-primary" {
		t.Fatalf("echoed InstanceID = %q, want tempo-primary", result.Claim.InstanceID)
	}
	if _, err := sdk.NewValidator(Contract()).ValidateResult(result); err != nil {
		t.Fatalf("ValidateResult() error = %v", err)
	}
}

// TestHandleServesLiveActivationEnvelope serves the literal ops-qa envelope
// (per-target provider, shared limits stanza, no grant) and asserts it
// passes validation. This mirrors the pagerduty live-envelope proof.
func TestHandleServesLiveActivationEnvelope(t *testing.T) {
	t.Parallel()

	env := map[string]string{}
	handler := Handler{
		Getenv: func(key string) string { return env[key] },
		Now:    func() time.Time { return time.Now().UTC() },
		ClientFactory: func(tempo.TargetConfig) (tempo.EvidenceClient, error) {
			return stubTempoClient{snapshot: fixtureSnapshot()}, nil
		},
	}
	claim := testClaim()
	claim.Scope.ID = "tempo:ops-qa"
	config := map[string]any{
		"targets": []any{map[string]any{
			"provider":       "tempo",
			"scope_id":       "tempo:ops-qa",
			"instance_id":    "ops-qa",
			"base_url":       "https://tempo.example.test",
			"resource_limit": float64(50),
			"enabled":        true,
		}},
		"limits": map[string]any{
			"maxRecordsPerClaim":  float64(50000),
			"maxPayloadBytes":     float64(1048576),
			"maxResultBytes":      float64(3145728),
			"claimTimeoutSeconds": float64(300),
		},
	}
	result := handler.Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           claim,
		Contract:        Contract(),
		Config:          config,
	})
	// The stub scope is example-prod, so an ops-qa claim routes to a retryable
	// miss; the point is the activation parses (no invalid_config). Serve the
	// matching scope to prove the full path is validator-clean.
	if failureClass(result) == FailureInvalidConfig {
		t.Fatalf("live envelope failed with invalid_config; want it to parse")
	}
	claim.Scope.ID = "tempo:trace:example-prod"
	config["targets"] = []any{map[string]any{
		"provider":               "tempo",
		"scope_id":               "tempo:trace:example-prod",
		"instance_id":            "tempo-example",
		"base_url":               "https://tempo.example.test",
		"token_env":              "TEMPO_TOKEN",
		"resource_limit":         float64(50),
		"max_tag_values_per_tag": float64(20),
		"enabled":                true,
		"lookback":               "1h",
	}}
	handler = testHandler(map[string]string{"TEMPO_TOKEN": "token-value"}, fixtureSnapshot())
	result = handler.Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           testClaim(),
		Contract:        Contract(),
		Config:          config,
	})
	if result.State != sdk.ResultComplete {
		t.Fatalf("State = %q, want complete for the live-shaped envelope", result.State)
	}
	if _, err := sdk.NewValidator(Contract()).ValidateResult(result); err != nil {
		t.Fatalf("ValidateResult() error = %v", err)
	}
}

// TestHandleResultTooLargeFailsTerminal proves the result byte bound fails
// terminal with result_too_large instead of reaching the host.
func TestHandleResultTooLargeFailsTerminal(t *testing.T) {
	t.Parallel()

	env := map[string]string{"TEMPO_TOKEN": "token-value"}
	handler := testHandler(env, fixtureSnapshot())
	config := testConfig(t)
	config["limits"] = map[string]any{"maxResultBytes": float64(10)}
	result := handler.Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           testClaim(),
		Contract:        Contract(),
		Config:          config,
	})
	if result.State != sdk.ResultTerminal {
		t.Fatalf("State = %q, want terminal", result.State)
	}
	if failureClass(result) != FailureResultTooLarge {
		t.Fatalf("failure class = %q, want %q", failureClass(result), FailureResultTooLarge)
	}
	if _, err := sdk.NewValidator(Contract()).ValidateResult(result); err != nil {
		t.Fatalf("ValidateResult() error = %v", err)
	}
}

// TestHandleRejectsUnknownScope maps an unconfigured scope to a retryable
// result with no facts, never a terminal failure for a routing miss.
func TestHandleRejectsUnknownScope(t *testing.T) {
	t.Parallel()

	env := map[string]string{"TEMPO_TOKEN": "token-value"}
	handler := testHandler(env, fixtureSnapshot())
	claim := testClaim()
	claim.Scope.ID = "tempo:trace:unknown"
	result := handler.Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           claim,
		Contract:        Contract(),
		Config:          testConfig(t),
	})
	if result.State != sdk.ResultRetryable {
		t.Fatalf("State = %q, want retryable", result.State)
	}
	if len(result.Facts) != 0 {
		t.Fatalf("facts = %d, want 0 with a failure state", len(result.Facts))
	}
	if _, err := sdk.NewValidator(Contract()).ValidateResult(result); err != nil {
		t.Fatalf("ValidateResult() error = %v", err)
	}
}

// TestHandleKeepsCredentialsOutOfTheLog proves the diagnostic line carries
// claim identity but never the resolved credential.
func TestHandleKeepsCredentialsOutOfTheLog(t *testing.T) {
	t.Parallel()

	env := map[string]string{"TEMPO_TOKEN": "token-value"}
	var logged strings.Builder
	handler := testHandler(env, fixtureSnapshot())
	handler.Log = &logged
	handler.Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           testClaim(),
		Contract:        Contract(),
		Config:          testConfig(t),
	})
	if strings.Contains(logged.String(), "token-value") {
		t.Fatalf("diagnostic line leaked the credential: %q", logged.String())
	}
}

// failureClass returns the failure class of a failure-state result.
func failureClass(result sdk.Result) string {
	for _, status := range result.Statuses {
		if status.Class == sdk.StatusFailure {
			return status.FailureClass
		}
	}
	return ""
}

// TestRequestRoundTrip proves the stdio wire shape decodes: one JSON request
// in, the same claim echoed on the result.
func TestRequestRoundTrip(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           testClaim(),
		Contract:        Contract(),
		Config:          map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := ParseRequest(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}
	if request.Claim.WorkItemID != "work-1" {
		t.Fatalf("WorkItemID = %q, want work-1", request.Claim.WorkItemID)
	}
}
