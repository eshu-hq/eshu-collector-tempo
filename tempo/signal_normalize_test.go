// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"testing"
)

// TestNormalizedOutcomeKeepsKnownOutcomes proves live signal outcomes pass
// through untouched while blank or foreign values degrade to observed.
func TestNormalizedOutcomeKeepsKnownOutcomes(t *testing.T) {
	t.Parallel()

	for _, row := range []struct {
		name  string
		value string
		want  string
	}{
		{"partial", OutcomePartial, OutcomePartial},
		{"permission hidden", OutcomePermissionHidden, OutcomePermissionHidden},
		{"unsupported", OutcomeUnsupported, OutcomeUnsupported},
		{"rejected", OutcomeRejected, OutcomeRejected},
		{"stale", OutcomeStale, OutcomeStale},
		{"padded input is trimmed", "  stale  ", OutcomeStale},
		{"blank defaults to observed", "", OutcomeObserved},
		{"foreign defaults to observed", "bogus", OutcomeObserved},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizedOutcome(row.value); got != row.want {
				t.Fatalf("normalizedOutcome(%q) = %q, want %q", row.value, got, row.want)
			}
		})
	}
}

// TestNormalizedFreshnessKeepsKnownStates proves live freshness states pass
// through untouched while blank or foreign values degrade to current.
func TestNormalizedFreshnessKeepsKnownStates(t *testing.T) {
	t.Parallel()

	for _, row := range []struct {
		name  string
		value string
		want  string
	}{
		{"unknown", FreshnessUnknown, FreshnessUnknown},
		{"stale", FreshnessStale, FreshnessStale},
		{"permission hidden", FreshnessPermissionHidden, FreshnessPermissionHidden},
		{"blank defaults to current", "", FreshnessCurrent},
		{"foreign defaults to current", "bogus", FreshnessCurrent},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizedFreshness(row.value); got != row.want {
				t.Fatalf("normalizedFreshness(%q) = %q, want %q", row.value, got, row.want)
			}
		})
	}
}

// TestNormalizedSignalKindKeepsKnownKinds proves tag-value and freshness
// probe signals keep their kind while anything else degrades to tag_set.
func TestNormalizedSignalKindKeepsKnownKinds(t *testing.T) {
	t.Parallel()

	for _, row := range []struct {
		name  string
		value string
		want  string
	}{
		{"tag values", SignalKindTagValues, SignalKindTagValues},
		{"freshness probe", SignalKindFreshnessProbe, SignalKindFreshnessProbe},
		{"blank defaults to tag set", "", SignalKindTagSet},
		{"foreign defaults to tag set", "bogus", SignalKindTagSet},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizedSignalKind(row.value); got != row.want {
				t.Fatalf("normalizedSignalKind(%q) = %q, want %q", row.value, got, row.want)
			}
		})
	}
}
