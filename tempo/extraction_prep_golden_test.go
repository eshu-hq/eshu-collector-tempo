// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Golden vectors cut from the in-tree constructors at the merged prep
// (eshu-hq/eshu a91b8dd8, #9155) by running the in-tree parity harness
// against the recorded parity run (scope tempo:parity:prod, generation
// tempo:8867:prod:1). These pin the extracted producer to byte-identical
// emission behavior: any change to stable keys, fingerprinting, or payload
// bytes breaks this test. Regenerate only from the in-tree constructors,
// never from this package's own output.
const (
	goldenTenantFingerprint = "3a04c5d29101b4160a401683829f6e1dee38501d745caedd9ca715f6def13c70"

	goldenKeySourceInstance = "observability.source_instance:tempo:8867:prod:1:bb5bc634e694a23957399a05a508ae1386bec194cefb12d3a64eed308e784d41"
	goldenKeyTagSet         = "observability.observed_trace_signal:tempo:8867:prod:1:bcc39d4aa7007f4a4fa3b3188077afa4be82a73d5dc0c9f60dda965be5e31311"
	goldenKeyTagValues      = "observability.observed_trace_signal:tempo:8867:prod:1:4016de034dfe562e0ea2e55165d0af4b82a65603688c21477bf223de83bf0196"
)

const goldenTagValuesPayload = `{"collector_instance_id":"collector-tempo-parity","declared_match_state":"not_compared","freshness_state":"current","generation_id":"tempo:8867:prod:1","observed_at":"2026-10-01T12:00:00Z","outcome":"observed","provenance":{"provider":"tempo","scope_id":"tempo:parity:prod","source_instance_id":"tempo-parity"},"provider":"tempo","provider_object_uid":"130b0a3e9ff39af9a9581bfb6d0a9bb7f61ab6b4584d090ca706202fb7c2f343","redaction_applied":true,"redaction_policy_version":"observability-live-tempo-v1","redaction_version":"observability-live-tempo-v1","resource_class":"trace_signal","scope_id":"tempo:parity:prod","signal_kind":"tag_values","source_class":"observed","source_instance_id":"tempo-parity","source_kind":"tempo","tag_name":"resource.service.name","tag_scope":"resource","tag_value_count":2,"tag_value_hashes":["0c95c7ece1ce1a9750275ef1c6d7ad6b278f70d66592207783ffe7d58474cc01","c7761e58969f7edd498186641b2021e8477e1bcd230de4cf3435242da4a40d14"]}`

const goldenSourcePayload = `{"collector_instance_id":"collector-tempo-parity","freshness_state":"current","generation_id":"tempo:8867:prod:1","high_cardinality_rejected":0,"observed_at":"2026-10-01T12:00:00Z","outcome":"observed","pages_fetched":3,"partial":false,"provenance":{"provider":"tempo","scope_id":"tempo:parity:prod","source_instance_id":"tempo-parity"},"provider":"tempo","redaction_applied":true,"redaction_policy_version":"observability-live-tempo-v1","redaction_version":"observability-live-tempo-v1","resource_count":2,"scope_id":"tempo:parity:prod","source_class":"observed","source_instance_id":"tempo-parity","source_kind":"tempo","warning_count":0}`

// parityObservedAt is the fixed generation timestamp recorded in the
// parity pack. Stub results carry it explicitly, so every
// identity-bearing field is deterministic across runs.
var parityObservedAt = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// parityCollectionResult mirrors the recorded parity run: one tag-set
// signal plus one tag-value signal for resource.service.name, exactly as
// the in-tree httptest harness normalizes them.
func parityCollectionResult() CollectionResult {
	return CollectionResult{
		Source: SourceInstance{
			Provider:         ProviderTempo,
			SourceInstanceID: "tempo-parity",
		},
		Signals: []TraceSignal{{
			ProviderObjectID:   fingerprintJoined("tags", "resource.service.name,service.name,span.http.route"),
			SignalKind:         SignalKindTagSet,
			TagKeys:            []string{"resource.service.name", "service.name", "span.http.route"},
			DeclaredMatchState: MatchStateNotCompared,
		}, {
			ProviderObjectID:   fingerprintJoined("tag_values", "resource.service.name"),
			SignalKind:         SignalKindTagValues,
			TagScope:           "resource",
			TagName:            "resource.service.name",
			TagValueCount:      2,
			TagValueHashes:     cleanStringSlice([]string{fingerprint("checkout"), fingerprint("billing")}),
			DeclaredMatchState: MatchStateNotCompared,
		}},
		ObservedAt: parityObservedAt,
		Stats: CollectionStats{
			PagesFetched: 3,
		},
	}
}

func collectParityResult(t *testing.T, source *ClaimedSource) testResult {
	t.Helper()

	got, err := source.Collect(context.Background(), parityTestClaim(t, defaultClaimSpec()))
	if err != nil {
		t.Fatalf("Collect(parity) error = %v, want nil", err)
	}
	if len(got.Facts) == 0 {
		t.Fatal("Collect(parity) emitted no facts")
	}
	return testResult{result: got}
}

func TestExtractionPrepGoldenVectors(t *testing.T) {
	t.Parallel()

	if got := fingerprint("tenant-prod"); got != goldenTenantFingerprint {
		t.Errorf("fingerprint(tenant) = %q, want in-tree golden %q", got, goldenTenantFingerprint)
	}
	res := collectParityResult(t, testSource(t, &stubEvidenceClient{result: parityCollectionResult()}))
	wantKeys := []string{goldenKeySourceInstance, goldenKeyTagSet, goldenKeyTagValues}
	if len(res.facts()) != len(wantKeys) {
		t.Fatalf("parity facts = %d, want %d", len(res.facts()), len(wantKeys))
	}
	for i, want := range wantKeys {
		if res.facts()[i].StableKey != want {
			t.Errorf("parity fact[%d] StableKey = %q, want in-tree golden %q", i, res.facts()[i].StableKey, want)
		}
	}
	counts := res.countByKind()
	for kind, want := range map[string]int{KindSourceInstance: 1, KindObservedTraceSignal: 2} {
		if got := counts[kind]; got != want {
			t.Errorf("parity kind %s count = %d, want %d", kind, got, want)
		}
		delete(counts, kind)
	}
	for kind, got := range counts {
		t.Errorf("parity emitted unexpected kind %s x%d", kind, got)
	}
	if err := res.validate(); err != nil {
		t.Errorf("parity ValidateResult() error = %v, want validator-clean output", err)
	}
}

func TestExtractionPrepGoldenTagValuesPayload(t *testing.T) {
	t.Parallel()

	res := collectParityResult(t, testSource(t, &stubEvidenceClient{result: parityCollectionResult()}))
	var found bool
	for _, fact := range res.facts() {
		uid, _ := fact.Payload["provider_object_uid"].(string)
		if fact.Kind != KindObservedTraceSignal || uid != fingerprintJoined("tag_values", "resource.service.name") {
			continue
		}
		found = true
		raw, err := json.Marshal(fact.Payload)
		if err != nil {
			t.Fatalf("json.Marshal(tag_values payload) error = %v", err)
		}
		if string(raw) != goldenTagValuesPayload {
			t.Errorf("tag_values payload bytes differ from in-tree golden:\n got: %s\nwant: %s", string(raw), goldenTagValuesPayload)
		}
	}
	if !found {
		t.Fatal("no observed_trace_signal fact for the recorded tag-values identity")
	}
}

func TestExtractionPrepGoldenSourcePayload(t *testing.T) {
	t.Parallel()

	res := collectParityResult(t, testSource(t, &stubEvidenceClient{result: parityCollectionResult()}))
	var found bool
	for _, fact := range res.facts() {
		if fact.Kind != KindSourceInstance {
			continue
		}
		found = true
		raw, err := json.Marshal(fact.Payload)
		if err != nil {
			t.Fatalf("json.Marshal(source_instance payload) error = %v", err)
		}
		if string(raw) != goldenSourcePayload {
			t.Errorf("source_instance payload bytes differ from in-tree golden:\n got: %s\nwant: %s", string(raw), goldenSourcePayload)
		}
	}
	if !found {
		t.Fatal("no observability.source_instance fact")
	}
}

// parityRecordedEnvelope is one recorded envelope from the carried parity
// pack (testdata/8867-parity-tempo.json, byte-identical to the in-tree
// pack). Only identity fields are read: FactID and numeric FencingToken
// have no emission-half counterpart and are intentionally not compared.
type parityRecordedEnvelope struct {
	FactKind      string `json:"FactKind"`
	StableFactKey string `json:"StableFactKey"`
}

func loadRecordedParityKeys(t *testing.T) []parityRecordedEnvelope {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "8867-parity-tempo.json"))
	if err != nil {
		t.Fatalf("ReadFile(8867-parity-tempo.json) error = %v, want nil", err)
	}
	var envs []parityRecordedEnvelope
	if err := json.Unmarshal(raw, &envs); err != nil {
		t.Fatalf("Unmarshal parity pack error = %v, want nil", err)
	}
	return envs
}

func TestParityPackCarriedIntact(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "8867-parity-tempo.json"))
	if err != nil {
		t.Fatalf("ReadFile(8867-parity-tempo.json) error = %v, want nil", err)
	}
	var envs []struct {
		FactKind      string `json:"FactKind"`
		GenerationID  string `json:"GenerationID"`
		SchemaVersion string `json:"SchemaVersion"`
		CollectorKind string `json:"CollectorKind"`
		FencingToken  int64  `json:"FencingToken"`
		StableFactKey string `json:"StableFactKey"`
	}
	if err := json.Unmarshal(raw, &envs); err != nil {
		t.Fatalf("Unmarshal parity pack error = %v, want nil", err)
	}
	if len(envs) != 3 {
		t.Fatalf("len(parity pack) = %d, want 3 (source_instance + 2 trace_signals)", len(envs))
	}
	wantKinds := []string{"observability.source_instance", "observability.observed_trace_signal", "observability.observed_trace_signal"}
	for i, want := range wantKinds {
		if envs[i].FactKind != want {
			t.Errorf("pack[%d].FactKind = %q, want %q", i, envs[i].FactKind, want)
		}
		if envs[i].SchemaVersion != SchemaVersionV1 {
			t.Errorf("pack[%d].SchemaVersion = %q, want 1.0.0", i, envs[i].SchemaVersion)
		}
		if envs[i].GenerationID != "tempo:8867:prod:1" || envs[i].FencingToken != 7 {
			t.Errorf("pack[%d] lost generation binding: %+v", i, envs[i])
		}
		if envs[i].CollectorKind != CollectorKind {
			t.Errorf("pack[%d].CollectorKind = %q, want %q", i, envs[i].CollectorKind, CollectorKind)
		}
		if envs[i].StableFactKey == "" {
			t.Errorf("pack[%d] carries no stable key", i)
		}
	}
}

// parityLiveServer mirrors the in-tree parity httptest server: echo plus
// the tag search and one tag-values endpoint with a fixed recording.
func parityLiveServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/echo", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/v2/search/tags", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tagNames":["resource.service.name","span.http.route"],` +
			`"scopes":[{"name":"resource","tags":["resource.service.name"]}]}`))
	})
	mux.HandleFunc("/api/v2/search/tag/resource.service.name/values", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tagValues":[` +
			`{"type":"string","value":"checkout"},` +
			`{"type":"string","value":"billing"}]}`))
	})
	return httptest.NewServer(mux)
}

// TestExtractionPrepLiveKeysMatchParityPack drives the real HTTP client
// against the recorded-shape server and proves the live emission half
// reproduces the recorded stable keys: the same keys the stub path pins
// above and the ones carried in the byte-identical parity pack.
func TestExtractionPrepLiveKeysMatchParityPack(t *testing.T) {
	t.Parallel()

	server := parityLiveServer(t)
	t.Cleanup(server.Close)

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-tempo-parity",
		Targets: []TargetConfig{{
			ScopeID:              "tempo:parity:prod",
			InstanceID:           "tempo-parity",
			BaseURL:              server.URL,
			ResourceLimit:        100,
			TagValueNames:        []string{"resource.service.name"},
			MaxTagValuesPerTag:   20,
			Enabled:              true,
			FreshnessProbeEnable: true,
			Lookback:             time.Hour,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource(live) error = %v, want nil", err)
	}
	got, err := source.Collect(context.Background(), parityTestClaim(t, defaultClaimSpec()))
	if err != nil {
		t.Fatalf("Collect(live) error = %v, want nil", err)
	}
	wantKeys := []string{goldenKeySourceInstance, goldenKeyTagSet, goldenKeyTagValues}
	if len(got.Facts) != len(wantKeys) {
		t.Fatalf("live facts = %d, want %d", len(got.Facts), len(wantKeys))
	}
	for i, want := range wantKeys {
		if got.Facts[i].StableKey != want {
			t.Errorf("live fact[%d] StableKey = %q, want in-tree golden %q", i, got.Facts[i].StableKey, want)
		}
	}
	recorded := loadRecordedParityKeys(t)
	if len(recorded) != len(wantKeys) {
		t.Fatalf("recorded pack facts = %d, want %d", len(recorded), len(wantKeys))
	}
	for i, want := range wantKeys {
		if recorded[i].StableFactKey != want {
			t.Errorf("recorded pack fact[%d] StableFactKey = %q, want in-tree golden %q", i, recorded[i].StableFactKey, want)
		}
		if got.Facts[i].StableKey != recorded[i].StableFactKey {
			t.Errorf("live fact[%d] diverged from recorded pack: %q vs %q", i, got.Facts[i].StableKey, recorded[i].StableFactKey)
		}
	}
}
