// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"reflect"
	"strconv"
	"testing"
	"time"
)

// TestNormalizeTagSetRejectsEmptyResponse proves an empty tag search yields
// no signal, so the collector emits nothing it cannot identify.
func TestNormalizeTagSetRejectsEmptyResponse(t *testing.T) {
	t.Parallel()

	if got := normalizeTagSet(tagSearchResponse{}, TargetConfig{}); !reflect.DeepEqual(got, TraceSignal{}) {
		t.Fatalf("normalizeTagSet(empty) = %+v, want zero signal", got)
	}
}

// TestTagAvailableMatchesAcrossScopes proves availability checks ignore the
// scope prefix, and that an absent tag reports unavailable.
func TestTagAvailableMatchesAcrossScopes(t *testing.T) {
	t.Parallel()

	available := []string{"resource.service.name", "span.http.route"}
	if !tagAvailable("service.name", available) {
		t.Fatal("tagAvailable(unscoped present tag) = false, want true")
	}
	if !tagAvailable("resource.service.name", available) {
		t.Fatal("tagAvailable(scoped present tag) = false, want true")
	}
	if tagAvailable("resource.unknown.tag", available) {
		t.Fatal("tagAvailable(absent tag) = true, want false")
	}
}

// TestTagScopeFromNameRefusesUnknownScopes proves only the known Tempo
// scopes resolve; dotted-but-foreign and scopeless names yield no scope.
func TestTagScopeFromNameRefusesUnknownScopes(t *testing.T) {
	t.Parallel()

	for _, row := range []struct {
		name  string
		value string
		want  string
	}{
		{"resource scope", "resource.service.name", "resource"},
		{"span scope", "span.http.route", "span"},
		{"foreign dotted name", "custom.label", ""},
		{"scopeless name", "servicename", ""},
		{"blank", "", ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			if got := tagScopeFromName(row.value); got != row.want {
				t.Fatalf("tagScopeFromName(%q) = %q, want %q", row.value, got, row.want)
			}
		})
	}
}

// TestQueryRangeBoundsLiveWindow proves a zero clock yields no range while a
// non-positive lookback falls back to the default window.
func TestQueryRangeBoundsLiveWindow(t *testing.T) {
	t.Parallel()

	if got := queryRange(time.Time{}, time.Hour); len(got) != 0 {
		t.Fatalf("queryRange(zero clock) = %v, want empty", got)
	}
	end := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	got := queryRange(end, -time.Minute)
	wantStart := strconv.FormatInt(end.Add(-defaultLookback).Unix(), 10)
	wantEnd := strconv.FormatInt(end.Unix(), 10)
	if got.Get("start") != wantStart || got.Get("end") != wantEnd {
		t.Fatalf("queryRange(negative lookback) = %v, want start=%s end=%s (default %v fallback)", got, wantStart, wantEnd, defaultLookback)
	}
}

// TestDeclaredMatchStateAdmitsDeclaredIDs proves a configured identity
// reports matched while anything else stays uncompared.
func TestDeclaredMatchStateAdmitsDeclaredIDs(t *testing.T) {
	t.Parallel()

	target := TargetConfig{DeclaredIDs: map[string]struct{}{"abc123": {}}}
	if got := declaredMatchState(target, "abc123"); got != MatchStateMatchedDeclared {
		t.Fatalf("declaredMatchState(declared) = %q, want matched", got)
	}
	if got := declaredMatchState(target, "other"); got != MatchStateNotCompared {
		t.Fatalf("declaredMatchState(absent) = %q, want not_compared", got)
	}
}

// TestNormalizedLimitsClampToBounds proves non-positive limits take the
// defaults and oversized limits clamp to the maxima.
func TestNormalizedLimitsClampToBounds(t *testing.T) {
	t.Parallel()

	if got := normalizedResourceLimit(maxResourceLimit + 1); got != maxResourceLimit {
		t.Fatalf("normalizedResourceLimit(over max) = %d, want %d", got, maxResourceLimit)
	}
	if got := normalizedResourceLimit(0); got != defaultResourceLimit {
		t.Fatalf("normalizedResourceLimit(0) = %d, want default %d", got, defaultResourceLimit)
	}
	if got := normalizedResourceLimit(-1); got != defaultResourceLimit {
		t.Fatalf("normalizedResourceLimit(-1) = %d, want default %d", got, defaultResourceLimit)
	}
	if got := normalizedResourceLimit(42); got != 42 {
		t.Fatalf("normalizedResourceLimit(42) = %d, want in-range passthrough 42", got)
	}
	if got := normalizedTagValueLimit(0); got != defaultTagValueLimit {
		t.Fatalf("normalizedTagValueLimit(0) = %d, want default %d", got, defaultTagValueLimit)
	}
	if got := normalizedTagValueLimit(maxTagValueLimit + 1); got != maxTagValueLimit {
		t.Fatalf("normalizedTagValueLimit(over max) = %d, want %d", got, maxTagValueLimit)
	}
}
