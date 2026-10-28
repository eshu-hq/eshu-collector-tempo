// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"context"
	"testing"
	"time"
)

// recordingMetrics keeps the last call per recorder method so tests prove
// the Collect path reports retries, staleness, and cardinality.
type recordingMetrics struct {
	fetchStatus string
	retries     int
	stale       int
	rejected    int
	facts       map[string]int64
}

func (m *recordingMetrics) RecordFetch(_ string, status string, _ time.Duration) {
	m.fetchStatus = status
}

func (m *recordingMetrics) RecordRateLimited(_ string, _ int) {}

func (m *recordingMetrics) RecordFactsEmitted(_ string, counts map[string]int64) {
	m.facts = counts
}

func (m *recordingMetrics) RecordRedactions(_ string, _ int) {}

func (m *recordingMetrics) RecordRetries(_ string, count int) {
	m.retries = count
}

func (m *recordingMetrics) RecordStale(_ string, count int) {
	m.stale = count
}

func (m *recordingMetrics) RecordHighCardinalityRejected(_ string, count int) {
	m.rejected = count
}

// TestCollectFallsBackToSourceClock proves a provider snapshot without an
// observation timestamp is stamped with the source clock instead of the
// zero time.
func TestCollectFallsBackToSourceClock(t *testing.T) {
	t.Parallel()

	snapshot := stubSnapshot()
	snapshot.ObservedAt = time.Time{}
	source := testSource(t, &stubEvidenceClient{result: snapshot})
	result, err := source.Collect(context.Background(), testClaim())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if !result.Generation.ObservedAt.Equal(parityObservedAt) {
		t.Fatalf("Generation.ObservedAt = %v, want source clock %v", result.Generation.ObservedAt, parityObservedAt)
	}
}

// TestCollectRejectsUnidentifiableSignal proves a signal without provider
// identity fails the claim instead of emitting an anonymous fact.
func TestCollectRejectsUnidentifiableSignal(t *testing.T) {
	t.Parallel()

	snapshot := stubSnapshot()
	snapshot.Signals = []TraceSignal{{ProviderObjectID: "   ", SignalKind: SignalKindTagSet}}
	source := testSource(t, &stubEvidenceClient{result: snapshot})
	if _, err := source.Collect(context.Background(), testClaim()); err == nil {
		t.Fatal("Collect(blank signal identity) = nil, want non-nil")
	}
}

// TestCollectReportsBoundedCounters proves retries, stale observations,
// and cardinality rejections reach the recorder with their snapshot
// counts while the claim stays complete.
func TestCollectReportsBoundedCounters(t *testing.T) {
	t.Parallel()

	recorder := &recordingMetrics{}
	snapshot := stubSnapshot()
	snapshot.Stats.Retries = 2
	snapshot.Stats.Stale = 1
	snapshot.Stats.HighCardinalityRejected = 4
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-tempo-parity",
		Targets: []TargetConfig{{
			ScopeID:       "tempo:parity:prod",
			InstanceID:    "tempo-parity",
			BaseURL:       "https://tempo.example.test",
			ResourceLimit: 50,
			Enabled:       true,
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) {
			return &stubEvidenceClient{result: snapshot}, nil
		},
		Now:     func() time.Time { return parityObservedAt },
		Metrics: recorder,
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	result, err := source.Collect(context.Background(), testClaim())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if recorder.retries != 2 {
		t.Fatalf("RecordRetries count = %d, want 2", recorder.retries)
	}
	if recorder.stale != 1 {
		t.Fatalf("RecordStale count = %d, want 1", recorder.stale)
	}
	if recorder.rejected != 4 {
		t.Fatalf("RecordHighCardinalityRejected count = %d, want 4", recorder.rejected)
	}
	if len(result.Facts) == 0 {
		t.Fatal("Collect() emitted no facts, want the bounded snapshot")
	}
}

// TestMetricsFallBackToDiscard proves a sourceless recorder never panics
// and an empty fact batch records nothing.
func TestMetricsFallBackToDiscard(t *testing.T) {
	t.Parallel()

	var source *ClaimedSource
	source.recordFacts(TargetConfig{}, nil)
	(&ClaimedSource{}).recordFacts(TargetConfig{}, nil)
	if got := source.metrics(); got == nil {
		t.Fatal("metrics() = nil, want the discard recorder")
	}
}
