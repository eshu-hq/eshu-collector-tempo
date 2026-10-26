// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"testing"
	"time"
)

func TestHTTPClientCollectsBoundedTempoMetadata(t *testing.T) {
	t.Parallel()

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.String())
		if got := r.Header.Get("X-Scope-OrgID"); got != "tenant-secret-prod" {
			t.Fatalf("tenant header = %q, want configured tenant", got)
		}
		switch r.URL.Path {
		case "/api/echo":
			_, _ = w.Write([]byte("echo"))
		case "/api/v2/search/tags":
			writeJSON(t, w, map[string]any{
				"scopes": []map[string]any{
					{"name": "resource", "tags": []string{"service.name", "deployment.environment", "service.name"}},
					{"name": "span", "tags": []string{"http.method"}},
				},
				"metrics": map[string]any{"inspectedBytes": "12345"},
			})
		case "/api/v2/search/tag/resource.service.name/values":
			writeJSON(t, w, map[string]any{
				"tagValues": []map[string]any{
					{"type": "string", "value": "checkout-prod"},
					{"type": "string", "value": "billing-prod"},
					{"type": "string", "value": "checkout-prod"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	before := time.Now().UTC()
	result, err := client.CollectObservedMetadata(t.Context(), TargetConfig{
		ScopeID:              "tempo-prod",
		InstanceID:           "tempo-main",
		BaseURL:              server.URL,
		TenantID:             "tenant-secret-prod",
		ResourceLimit:        10,
		TagValueNames:        []string{"resource.service.name"},
		MaxTagValuesPerTag:   5,
		ObservedOnlyHint:     true,
		DeclaredIDs:          map[string]struct{}{},
		FreshnessProbeEnable: true,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}

	if !result.Source.TenantPresent || result.Source.TenantFingerprint == "" || !result.Source.TenantRedacted {
		t.Fatalf("tenant metadata = %#v, want redacted tenant presence and fingerprint", result.Source)
	}
	if result.Source.TenantFingerprint == "tenant-secret-prod" {
		t.Fatal("tenant fingerprint leaked raw tenant")
	}
	if len(result.Signals) != 2 {
		t.Fatalf("len(Signals) = %d, want tag set and tag-value signal", len(result.Signals))
	}
	if got := result.Signals[0].TagKeys; !slices.Equal(got, []string{"deployment.environment", "http.method", "service.name"}) {
		t.Fatalf("tag keys = %#v", got)
	}
	if got := result.Signals[1].TagValueCount; got != 2 {
		t.Fatalf("service tag value count = %d, want duplicate values deduped to 2", got)
	}
	if got := len(result.Signals[1].TagValueHashes); got != 2 {
		t.Fatalf("tag value hashes = %d, want 2", got)
	}
	for _, signal := range result.Signals {
		if signal.ManuallyCreated != true {
			t.Fatalf("signal %#v should be a manual provider drift candidate", signal)
		}
	}
	if result.Stats.PagesFetched != 3 {
		t.Fatalf("PagesFetched = %d, want echo/tags/tag-values", result.Stats.PagesFetched)
	}
	assertRequestWindowIsRecent(t, requests, before)
	assertNoRawQueryParam(t, requests, "q")
	assertPayloadOmitsString(t, map[string]any{"result": result}, "checkout-prod")
	assertPayloadOmitsString(t, map[string]any{"result": result}, "billing-prod")
}

func TestHTTPClientRejectsHighCardinalityTagValues(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/search/tags":
			writeJSON(t, w, map[string]any{"scopes": []map[string]any{{"name": "resource", "tags": []string{"service.name"}}}})
		case "/api/v2/search/tag/resource.service.name/values":
			writeJSON(t, w, map[string]any{"tagValues": []map[string]any{
				{"type": "string", "value": "one"},
				{"type": "string", "value": "two"},
				{"type": "string", "value": "three"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectObservedMetadata(t.Context(), TargetConfig{
		ScopeID:            "tempo-prod",
		InstanceID:         "tempo-main",
		BaseURL:            server.URL,
		TagValueNames:      []string{"resource.service.name"},
		MaxTagValuesPerTag: 2,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}

	if got := result.Stats.HighCardinalityRejected; got != 1 {
		t.Fatalf("HighCardinalityRejected = %d, want 1", got)
	}
	if len(result.Signals) != 2 {
		t.Fatalf("len(Signals) = %d, want tag-set and tag-value signal", len(result.Signals))
	}
	if got := len(result.Signals[1].TagValueHashes); got != 0 {
		t.Fatalf("tag hashes = %d, want none for high-cardinality values", got)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Reason != WarningHighCardinality {
		t.Fatalf("warnings = %#v, want high-cardinality warning", result.Warnings)
	}
}

func TestHTTPClientDeduplicatesRepeatedTagScopeResponses(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"scopes": []map[string]any{
				{"name": "resource", "tags": []string{"service.name", "service.name"}},
				{"name": "resource", "tags": []string{"service.name"}},
			},
		})
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectObservedMetadata(t.Context(), TargetConfig{
		ScopeID:    "tempo-prod",
		InstanceID: "tempo-main",
		BaseURL:    server.URL,
	})
	if err != nil {
		t.Fatalf("CollectObservedMetadata() error = %v, want nil", err)
	}
	if len(result.Signals) != 1 {
		t.Fatalf("len(Signals) = %d, want duplicate tag set collapsed", len(result.Signals))
	}
	if got := result.Signals[0].TagKeys; !slices.Equal(got, []string{"service.name"}) {
		t.Fatalf("TagKeys = %#v, want one service.name", got)
	}
}

func TestHTTPClientReturnsProviderFailureAfterRateLimitRetries(t *testing.T) {
	t.Parallel()

	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		http.Error(w, "rate limited tenant-secret-prod", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectObservedMetadata(t.Context(), TargetConfig{
		ScopeID:    "tempo-prod",
		InstanceID: "tempo-main",
		BaseURL:    server.URL,
	})
	if err == nil {
		t.Fatalf("CollectObservedMetadata() error = nil, want rate-limited provider failure")
	}
	failure := classifiedProviderFailure(err)
	if got := failure.FailureClass(); got != FailureRateLimited {
		t.Fatalf("FailureClass = %q, want %q", got, FailureRateLimited)
	}
	if attempts != maxHTTPRetries+1 {
		t.Fatalf("attempts = %d, want %d", attempts, maxHTTPRetries+1)
	}
	if result.Stats.RateLimits != maxHTTPRetries+1 {
		t.Fatalf("RateLimits = %d, want attempts counted", result.Stats.RateLimits)
	}
	if err.Error() == "rate limited tenant-secret-prod" {
		t.Fatalf("provider error leaked response body")
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("write JSON: %v", err)
	}
}

// assertRequestWindowIsRecent checks the tag-search window the client sent:
// start/end epoch seconds must bracket the collection under test. The
// extracted client stamps time.Now at collection, so the test pins a window
// around the call instead of a fixed generation timestamp.
func assertRequestWindowIsRecent(t *testing.T, requests []string, before time.Time) {
	t.Helper()
	after := time.Now().UTC()
	for _, raw := range requests {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse request URL %q: %v", raw, err)
		}
		values := parsed.Query()
		if values.Get("start") == "" || values.Get("end") == "" {
			continue
		}
		start, err := strconv.ParseInt(values.Get("start"), 10, 64)
		if err != nil {
			t.Fatalf("start = %q, want epoch seconds", values.Get("start"))
		}
		end, err := strconv.ParseInt(values.Get("end"), 10, 64)
		if err != nil {
			t.Fatalf("end = %q, want epoch seconds", values.Get("end"))
		}
		if start <= 0 || end <= start {
			t.Fatalf("window start=%d end=%d is not a positive range", start, end)
		}
		if end < before.Unix()-60 || end > after.Unix()+60 {
			t.Fatalf("end = %d, want within the collection window", end)
		}
		return
	}
	t.Fatalf("no request carried start/end query params: %#v", requests)
}

func assertNoRawQueryParam(t *testing.T, requests []string, key string) {
	t.Helper()
	for _, raw := range requests {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse request URL %q: %v", raw, err)
		}
		if parsed.Query().Get(key) != "" {
			t.Fatalf("request %q carried forbidden query param %q", raw, key)
		}
	}
}
