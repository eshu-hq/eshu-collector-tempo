// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Extraction census mirror (#8867, producer path): the contracts the
// extracted collector preserves — admission at schema 1.0.0, stable keys,
// generation binding, tag-value hash flow-through, and credential
// redaction — proved against the extracted constructors, including hostile
// inputs.
//
// Ported from go/internal/collector/observability/tempo/
// extraction_prep_mirror_test.go in eshu-hq/eshu. Two in-tree assertions
// have no emission-half counterpart and are intentionally not ported:
// FactID (assigned by the core host at admission) and the numeric
// FencingToken (enforced by the host from claim.fencing_token).
// Provider identity is asserted through SourceRef.SourceSystem instead of
// CollectorKind, which the collector-sdk Fact wire shape carries on the
// claim.

func extractionPrepContext() EnvelopeContext {
	return EnvelopeContext{
		ScopeID:             "tempo:prod",
		GenerationID:        "tempo:generation-9",
		CollectorInstanceID: "collector-tempo",
		ObservedAt:          time.Date(2026, 5, 16, 11, 0, 0, 0, time.UTC),
		SourceURI:           "https://tempo.example.test",
		SourceInstanceID:    "prod",
	}
}

func extractionPrepSource() SourceInstance {
	return SourceInstance{
		Provider:          ProviderTempo,
		SourceInstanceID:  "prod",
		TenantPresent:     true,
		TenantFingerprint: fingerprint("tenant-prod"),
		TenantRedacted:    true,
	}
}

func extractionPrepTagSet() TraceSignal {
	return TraceSignal{
		ProviderObjectID:   "tagset-prod",
		SignalKind:         SignalKindTagSet,
		TagKeys:            []string{"resource.service.name", "span.http.route"},
		DeclaredMatchState: MatchStateNotCompared,
		FreshnessState:     FreshnessCurrent,
		Outcome:            OutcomeObserved,
	}
}

func extractionPrepTagValues() TraceSignal {
	return TraceSignal{
		ProviderObjectID:   "tagvalues-prod",
		SignalKind:         SignalKindTagValues,
		TagScope:           "resource",
		TagName:            "resource.service.name",
		TagValueCount:      2,
		TagValueHashes:     []string{fingerprint("checkout"), fingerprint("billing")},
		QueryRedacted:      true,
		DeclaredMatchState: MatchStateNotCompared,
		FreshnessState:     FreshnessCurrent,
		Outcome:            OutcomeObserved,
		ManuallyCreated:    true,
	}
}

func extractionPrepFacts(t *testing.T, ctx EnvelopeContext) []emittedFact {
	t.Helper()

	source, err := NewSourceInstanceEnvelope(ctx, extractionPrepSource(), CollectionStats{Signals: 2, Warnings: 1})
	if err != nil {
		t.Fatalf("NewSourceInstanceEnvelope() error = %v, want nil", err)
	}
	tagSet, err := NewObservedTraceSignalEnvelope(ctx, extractionPrepTagSet())
	if err != nil {
		t.Fatalf("NewObservedTraceSignalEnvelope(tag set) error = %v, want nil", err)
	}
	tagValues, err := NewObservedTraceSignalEnvelope(ctx, extractionPrepTagValues())
	if err != nil {
		t.Fatalf("NewObservedTraceSignalEnvelope(tag values) error = %v, want nil", err)
	}
	warning, err := NewCoverageWarningEnvelope(ctx, Warning{ResourceClass: ResourceClassTraceSignal, Reason: WarningRateLimited})
	if err != nil {
		t.Fatalf("NewCoverageWarningEnvelope() error = %v, want nil", err)
	}
	return []emittedFact{source, tagSet, tagValues, warning}
}

func TestExtractionPrepAllKindsAdmitAtV1(t *testing.T) {
	t.Parallel()

	kinds := []string{
		KindSourceInstance,
		KindObservedTraceSignal,
		KindCoverageWarning,
	}
	for _, kind := range kinds {
		version, ok := SchemaVersionForKind(kind)
		if !ok || version != SchemaVersionV1 {
			t.Errorf("SchemaVersionForKind(%q) = %q, %v; want 1.0.0, true", kind, version, ok)
		}
		if err := CheckSchemaVersion(kind, SchemaVersionV1); err != nil {
			t.Errorf("CheckSchemaVersion(%q, 1.0.0) error = %v, want nil", kind, err)
		}
	}
	for _, fact := range extractionPrepFacts(t, extractionPrepContext()) {
		if fact.SchemaVersion != SchemaVersionV1 {
			t.Errorf("fact %q SchemaVersion = %q, want 1.0.0", fact.Kind, fact.SchemaVersion)
		}
		if fact.SourceRef.SourceSystem != CollectorKind {
			t.Errorf("fact %q SourceSystem = %q, want %q", fact.Kind, fact.SourceRef.SourceSystem, CollectorKind)
		}
		if string(fact.SourceConfidence) != string(reportedConfidence) {
			t.Errorf("fact %q SourceConfidence = %q, want reported", fact.Kind, fact.SourceConfidence)
		}
		if fact.SourceRef.FactKey != fact.StableKey {
			t.Errorf("fact %q source_ref.fact_key does not match stable_key", fact.Kind)
		}
	}
	if err := CheckSchemaVersion(KindObservedTraceSignal, "9.9.9"); err == nil {
		t.Error("CheckSchemaVersion(observed_trace_signal, 9.9.9) = nil, want incompatible-schema rejection")
	}
}

func TestExtractionPrepDeterminism(t *testing.T) {
	t.Parallel()

	first := extractionPrepFacts(t, extractionPrepContext())
	second := extractionPrepFacts(t, extractionPrepContext())
	if len(first) != len(second) {
		t.Fatalf("len(first) = %d, len(second) = %d, want equal", len(first), len(second))
	}
	for i := range first {
		if first[i].StableKey != second[i].StableKey {
			t.Errorf("fact[%d] StableKey not deterministic: %q vs %q", i, first[i].StableKey, second[i].StableKey)
		}
	}
}

func TestExtractionPrepGenerationRebind(t *testing.T) {
	t.Parallel()

	first := extractionPrepFacts(t, extractionPrepContext())
	rebound := extractionPrepContext()
	rebound.GenerationID = "tempo:generation-10"
	second := extractionPrepFacts(t, rebound)
	if len(first) != len(second) {
		t.Fatalf("len(first) = %d, len(second) = %d, want equal", len(first), len(second))
	}
	for i := range first {
		// Stable keys bind the generation: a rebind must change identity,
		// never silently reuse the prior generation's key.
		if first[i].StableKey == second[i].StableKey {
			t.Errorf("fact[%d] StableKey identical across generations; generation must rebind identity", i)
		}
		if second[i].SourceRef.GenerationID != rebound.GenerationID {
			t.Errorf("fact[%d] generation binding lost: %+v", i, second[i])
		}
		// Provider identity survives the rebind.
		if second[i].SourceRef.SourceSystem != CollectorKind {
			t.Errorf("fact[%d] SourceSystem = %q, want %q", i, second[i].SourceRef.SourceSystem, CollectorKind)
		}
	}
}

func TestExtractionPrepTagValueHashesFlowThrough(t *testing.T) {
	t.Parallel()

	facts := extractionPrepFacts(t, extractionPrepContext())
	var found bool
	for _, fact := range facts {
		if fact.Kind != KindObservedTraceSignal {
			continue
		}
		if fact.Payload["signal_kind"] != SignalKindTagValues {
			continue
		}
		found = true
		hashes, ok := fact.Payload["tag_value_hashes"].([]string)
		if !ok || len(hashes) != 2 {
			t.Fatalf("tag_value_hashes = %#v, want two recorded hashes", fact.Payload["tag_value_hashes"])
		}
		if fact.Payload["tag_value_count"] != 2 {
			t.Errorf("tag_value_count = %#v, want 2", fact.Payload["tag_value_count"])
		}
	}
	if !found {
		t.Fatal("no observed_trace_signal fact with signal_kind tag_values")
	}
}

func TestExtractionPrepRedactionSentinel(t *testing.T) {
	t.Parallel()

	const sentinel = "PREP-SENTINEL-8867"
	ctx := extractionPrepContext()

	hostileValues := extractionPrepTagValues()
	hostileValues.Query = `{ resource.service.name = "` + sentinel + `" }`
	hostileValues.QueryRedacted = true
	hostileValues.TagValueHashes = []string{fingerprint(sentinel)}
	signalFact, err := NewObservedTraceSignalEnvelope(ctx, hostileValues)
	if err != nil {
		t.Fatalf("NewObservedTraceSignalEnvelope(hostile TraceQL/value) error = %v, want nil", err)
	}
	hostileSource := extractionPrepSource()
	hostileSource.TenantFingerprint = fingerprint(sentinel)
	sourceFact, err := NewSourceInstanceEnvelope(ctx, hostileSource, CollectionStats{})
	if err != nil {
		t.Fatalf("NewSourceInstanceEnvelope(hostile tenant) error = %v, want nil", err)
	}
	for _, fact := range []emittedFact{signalFact, sourceFact} {
		raw, err := json.Marshal(fact)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(raw), sentinel) {
			t.Errorf("credential sentinel reached persisted fact %q", fact.Kind)
		}
	}
	if signalFact.Payload["query_redacted"] != true {
		t.Error("signal query_redacted = false, want TraceQL redaction flag")
	}
	for _, fact := range extractionPrepFacts(t, ctx) {
		raw, err := json.Marshal(fact)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		// "tag_values" is a legitimate signal_kind enum value, so it is
		// not in the forbidden list: raw value leakage is proved by
		// the sentinel assertions above.
		for _, key := range []string{`"token"`, `"tenant_id"`, `"query"`, `"traceql"`} {
			if strings.Contains(string(raw), key) {
				t.Errorf("fact %q carries forbidden payload key %s", fact.Kind, key)
			}
		}
		for _, forbidden := range []string{"tenant-prod"} {
			if strings.Contains(string(raw), forbidden) {
				t.Errorf("fact %q carries forbidden raw value %q", fact.Kind, forbidden)
			}
		}
	}
	// Tenant identity never travels raw: the source-instance fact carries
	// only presence plus a fingerprint.
	sourceRaw, _ := json.Marshal(extractionPrepFacts(t, ctx)[0])
	if strings.Contains(string(sourceRaw), "tenant-prod") {
		t.Error("raw tenant value reached persisted source_instance fact")
	}
	// Positive control: benign provider identity survives so the absence
	// above is meaningful.
	benign := extractionPrepFacts(t, ctx)
	benignRaw, _ := json.Marshal(benign)
	if !strings.Contains(string(benignRaw), "tagset-prod") {
		t.Error("positive control: benign provider object ID missing from persisted facts")
	}
}

func TestExtractionPrepHostileInputsFailClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*EnvelopeContext)
	}{
		{"blank scope", func(c *EnvelopeContext) { c.ScopeID = "  " }},
		{"blank generation", func(c *EnvelopeContext) { c.GenerationID = "" }},
		{"blank collector instance", func(c *EnvelopeContext) { c.CollectorInstanceID = "" }},
		{"blank source instance", func(c *EnvelopeContext) { c.SourceInstanceID = "" }},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := extractionPrepContext()
			tt.mutate(&ctx)
			if _, err := NewSourceInstanceEnvelope(ctx, extractionPrepSource(), CollectionStats{}); err == nil {
				t.Errorf("NewSourceInstanceEnvelope() = nil, want fail-closed error for %s", tt.name)
			}
			if _, err := NewObservedTraceSignalEnvelope(ctx, extractionPrepTagSet()); err == nil {
				t.Errorf("NewObservedTraceSignalEnvelope() = nil, want fail-closed error for %s", tt.name)
			}
			if _, err := NewCoverageWarningEnvelope(ctx, Warning{Reason: WarningPartial}); err == nil {
				t.Errorf("NewCoverageWarningEnvelope() = nil, want fail-closed error for %s", tt.name)
			}
		})
	}
	// Blank signal identity fails closed; blank warning reason degrades
	// to a partial-read warning instead of failing.
	ctx := extractionPrepContext()
	badSignal := extractionPrepTagSet()
	badSignal.ProviderObjectID = "  "
	if _, err := NewObservedTraceSignalEnvelope(ctx, badSignal); err == nil {
		t.Error("NewObservedTraceSignalEnvelope(blank identity) = nil, want error")
	}
	warning, err := NewCoverageWarningEnvelope(ctx, Warning{})
	if err != nil {
		t.Fatalf("NewCoverageWarningEnvelope(blank) error = %v, want partial-degrade not fatal", err)
	}
	if warning.Payload["warning_kind"] != WarningPartial {
		t.Errorf("warning_kind = %v, want %q degrade", warning.Payload["warning_kind"], WarningPartial)
	}
}
