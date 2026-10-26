// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu-sdk/collector/transport"
)

type stubEvidenceClient struct {
	result CollectionResult
	err    error
}

func (c *stubEvidenceClient) CollectObservedMetadata(context.Context, TargetConfig) (CollectionResult, error) {
	return c.result, c.err
}

// testClaimSpec overrides one field of the default test claim.
type testClaimSpec struct {
	ScopeID      string
	GenerationID string
	FencingToken string
}

func defaultClaimSpec() testClaimSpec {
	return testClaimSpec{
		ScopeID:      "tempo:parity:prod",
		GenerationID: "tempo:8867:prod:1",
		FencingToken: "7",
	}
}

func parityTestClaim(t *testing.T, spec testClaimSpec) collector.Claim {
	t.Helper()

	claim := testClaim()
	if spec.ScopeID != "" {
		claim.Scope.ID = spec.ScopeID
	}
	if spec.GenerationID != "" {
		claim.GenerationID = spec.GenerationID
	}
	if spec.FencingToken != "" {
		claim.FencingToken = spec.FencingToken
	}
	return claim
}

func testClaim() collector.Claim {
	return collector.Claim{
		ComponentID:   "dev.eshu.collector.tempo",
		InstanceID:    "collector-tempo-parity",
		CollectorKind: CollectorKind,
		SourceSystem:  CollectorKind,
		Scope:         collector.Scope{ID: "tempo:parity:prod", Kind: ScopeKindTraceSource},
		SourceRunID:   "run-1",
		GenerationID:  "tempo:8867:prod:1",
		WorkItemID:    "work-1",
		FencingToken:  "7",
		Attempt:       1,
		Deadline:      time.Now().UTC().Add(time.Hour),
		ConfigHandle:  "config-1",
	}
}

func testSource(t *testing.T, client EvidenceClient) *ClaimedSource {
	t.Helper()

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-tempo-parity",
		Targets: []TargetConfig{{
			ScopeID:       "tempo:parity:prod",
			InstanceID:    "tempo-parity",
			BaseURL:       "https://tempo.example.test",
			ResourceLimit: 50,
			Enabled:       true,
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) { return client, nil },
		Now:           func() time.Time { return parityObservedAt },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	return source
}

func stubSnapshot() CollectionResult {
	return CollectionResult{
		Source: SourceInstance{
			Provider:         ProviderTempo,
			SourceInstanceID: "tempo-parity",
		},
		Signals: []TraceSignal{{
			ProviderObjectID: "tagset:service.name",
			SignalKind:       SignalKindTagSet,
			TagKeys:          []string{"service.name", "http.route"},
		}, {
			ProviderObjectID: "tagvalues:resource.service.name",
			SignalKind:       SignalKindTagValues,
			TagScope:         "resource",
			TagName:          "resource.service.name",
			TagValueCount:    2,
			TagValueHashes:   []string{"recorded-a", "recorded-b"},
		}},
		Warnings: []Warning{{
			ResourceClass: ResourceClassTraceSignal,
			ResourceID:    "tag:resource.service.name",
			Reason:        WarningRateLimited,
		}},
		ObservedAt: parityObservedAt,
		Stats: CollectionStats{
			PagesFetched: 3,
			Redactions:   2,
		},
	}
}

// grantContract mirrors the grant draft: the three observed kinds at schema
// 1.0.0 with reported confidence. The validator self-check below proves the
// producer's output satisfies the published SDK's own admission rules.
func grantContract() collector.Contract {
	declare := func(kind string) collector.FactDeclaration {
		return collector.FactDeclaration{
			Kind:             kind,
			SchemaVersions:   []string{SchemaVersionV1},
			SourceConfidence: []collector.SourceConfidence{collector.SourceConfidenceReported},
		}
	}
	return collector.Contract{
		ProtocolVersion: collector.ProtocolVersionV1Alpha1,
		Facts: []collector.FactDeclaration{
			declare(KindSourceInstance),
			declare(KindObservedTraceSignal),
			declare(KindCoverageWarning),
		},
	}
}

// testResult wraps a Collect result for the parity assertions.
type testResult struct {
	result collector.Result
}

func (r testResult) facts() []collector.Fact {
	return r.result.Facts
}

func (r testResult) countByKind() map[string]int {
	out := map[string]int{}
	for _, fact := range r.result.Facts {
		out[fact.Kind]++
	}
	return out
}

func (r testResult) validate() error {
	_, err := collector.NewValidator(grantContract()).ValidateResult(r.result)
	return err
}

func TestCollectEmitsValidatorCleanResult(t *testing.T) {
	t.Parallel()

	source := testSource(t, &stubEvidenceClient{result: stubSnapshot()})
	got, err := source.Collect(context.Background(), testClaim())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if got.State != collector.ResultComplete {
		t.Errorf("State = %q, want complete", got.State)
	}
	// Source instance + two trace signals + one warning.
	if len(got.Facts) != 4 {
		t.Fatalf("len(Facts) = %d, want 4", len(got.Facts))
	}
	if got.Generation.ID != "tempo:8867:prod:1" {
		t.Errorf("Generation.ID = %q, want claim generation", got.Generation.ID)
	}
	if got.Generation.FreshnessHint != "tempo_observed_metadata" {
		t.Errorf("FreshnessHint = %q, want tempo_observed_metadata", got.Generation.FreshnessHint)
	}
	for _, fact := range got.Facts {
		if fact.SourceRef.ScopeID != "tempo:parity:prod" {
			t.Errorf("fact %q scope = %q, want tempo:parity:prod", fact.Kind, fact.SourceRef.ScopeID)
		}
		if fact.SourceRef.SourceSystem != CollectorKind {
			t.Errorf("fact %q source_system = %q, want tempo", fact.Kind, fact.SourceRef.SourceSystem)
		}
		if fact.SourceRef.FactKey != fact.StableKey {
			t.Errorf("fact %q source_ref.fact_key does not match stable_key", fact.Kind)
		}
	}
	if _, err := collector.NewValidator(grantContract()).ValidateResult(got); err != nil {
		t.Fatalf("ValidateResult(complete) error = %v, want validator-clean output", err)
	}
}

func TestCollectPartialReadStaysPartial(t *testing.T) {
	t.Parallel()

	snapshot := stubSnapshot()
	snapshot.Stats.Partial = true
	source := testSource(t, &stubEvidenceClient{result: snapshot})
	got, err := source.Collect(context.Background(), testClaim())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if got.State != collector.ResultPartial {
		t.Fatalf("State = %q, want partial", got.State)
	}
	if _, err := collector.NewValidator(grantContract()).ValidateResult(got); err != nil {
		t.Fatalf("ValidateResult(partial) error = %v, want validator-clean output", err)
	}
}

func TestCollectRejectsForeignClaim(t *testing.T) {
	t.Parallel()

	source := testSource(t, &stubEvidenceClient{result: stubSnapshot()})

	wrongInstance := testClaim()
	wrongInstance.InstanceID = "collector-other"
	if _, err := source.Collect(context.Background(), wrongInstance); err == nil {
		t.Error("Collect(foreign instance) = nil, want rejection")
	}

	wrongKind := testClaim()
	wrongKind.CollectorKind = "loki"
	if _, err := source.Collect(context.Background(), wrongKind); err == nil {
		t.Error("Collect(foreign kind) = nil, want rejection")
	}

	wrongSystem := testClaim()
	wrongSystem.SourceSystem = "loki"
	if _, err := source.Collect(context.Background(), wrongSystem); err == nil {
		t.Error("Collect(foreign source_system) = nil, want rejection")
	}

	blankGeneration := testClaim()
	blankGeneration.GenerationID = ""
	if _, err := source.Collect(context.Background(), blankGeneration); err == nil {
		t.Error("Collect(blank generation) = nil, want rejection")
	}
}

func TestCollectUnknownScopeIsRetryable(t *testing.T) {
	t.Parallel()

	source := testSource(t, &stubEvidenceClient{result: stubSnapshot()})
	claim := testClaim()
	claim.Scope.ID = "tempo:parity:missing"
	_, err := source.Collect(context.Background(), claim)
	if err == nil {
		t.Fatal("Collect(unknown scope) = nil, want retryable failure")
	}
	var failure transport.ProviderFailure
	if !errors.As(err, &failure) {
		t.Fatalf("Collect(unknown scope) error type = %T, want transport.ProviderFailure", err)
	}
	if failure.FailureClass() != FailureRetryable {
		t.Errorf("FailureClass() = %q, want retryable", failure.FailureClass())
	}
}

func TestCollectClassifiesProviderFailure(t *testing.T) {
	t.Parallel()

	providerErr := ProviderHTTPError{Provider: "tempo", StatusCode: 429, Message: "Too Many Requests"}
	source := testSource(t, &stubEvidenceClient{err: providerErr})
	_, err := source.Collect(context.Background(), testClaim())
	if err == nil {
		t.Fatal("Collect(provider 429) = nil, want classified failure")
	}
	var failure transport.ProviderFailure
	if !errors.As(err, &failure) {
		t.Fatalf("Collect(provider 429) error type = %T, want transport.ProviderFailure", err)
	}
	if failure.FailureClass() != FailureRateLimited {
		t.Errorf("FailureClass() = %q, want rate_limited", failure.FailureClass())
	}
}

func TestCollectClassifiesTerminalFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		err          error
		wantClass    string
		wantTerminal bool
	}{
		{
			name:         "auth denied http",
			err:          ProviderHTTPError{Provider: "tempo", StatusCode: http.StatusForbidden, Message: "forbidden token-secret"},
			wantClass:    FailureAuthDenied,
			wantTerminal: true,
		},
		{
			name:         "bad request terminal",
			err:          ProviderHTTPError{Provider: "tempo", StatusCode: http.StatusBadRequest, Message: "bad query"},
			wantClass:    FailureTerminal,
			wantTerminal: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			source := testSource(t, &stubEvidenceClient{err: tt.err})
			_, err := source.Collect(context.Background(), testClaim())
			if err == nil {
				t.Fatal("Collect() error = nil, want provider failure")
			}
			var failure transport.ProviderFailure
			if !errors.As(err, &failure) {
				t.Fatalf("Collect() error = %T, want transport.ProviderFailure", err)
			}
			if got := failure.FailureClass(); got != tt.wantClass {
				t.Fatalf("FailureClass() = %q, want %q", got, tt.wantClass)
			}
			if got := failure.TerminalFailure(); got != tt.wantTerminal {
				t.Fatalf("TerminalFailure() = %v, want %v", got, tt.wantTerminal)
			}
		})
	}
}

func TestClaimedSourceSkipsDisabledTargets(t *testing.T) {
	t.Parallel()

	_, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-tempo-prod",
		Targets: []TargetConfig{{
			ScopeID:    "tempo:parity:prod",
			InstanceID: "tempo-parity",
			BaseURL:    "https://tempo.example.internal",
			Enabled:    false,
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) {
			t.Fatal("disabled target should not construct a client")
			return nil, nil
		},
	})
	if err == nil {
		t.Fatal("NewClaimedSource() error = nil, want no enabled target error")
	}
}
