// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"testing"
)

// TestWarningOutcomeMapsEveryReason proves each warning reason carries its
// contract outcome, with unknown reasons degrading to partial.
func TestWarningOutcomeMapsEveryReason(t *testing.T) {
	t.Parallel()

	for _, row := range []struct {
		name   string
		reason string
		want   string
	}{
		{"permission hidden", WarningPermissionHidden, OutcomePermissionHidden},
		{"unsupported", WarningUnsupported, OutcomeUnsupported},
		{"manual provider resource", WarningManualProviderResource, OutcomeObserved},
		{"stale", WarningStale, OutcomeStale},
		{"high cardinality", WarningHighCardinality, OutcomeRejected},
		{"blank defaults to partial", "", OutcomePartial},
		{"unknown defaults to partial", "rate_limited", OutcomePartial},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			if got := warningOutcome(row.reason); got != row.want {
				t.Fatalf("warningOutcome(%q) = %q, want %q", row.reason, got, row.want)
			}
		})
	}
}

// TestWarningFreshnessMapsEveryReason proves each warning reason carries its
// freshness state, with unlisted reasons degrading to unknown.
func TestWarningFreshnessMapsEveryReason(t *testing.T) {
	t.Parallel()

	for _, row := range []struct {
		name   string
		reason string
		want   string
	}{
		{"permission hidden", WarningPermissionHidden, FreshnessPermissionHidden},
		{"stale", WarningStale, FreshnessStale},
		{"partial defaults to unknown", WarningPartial, FreshnessUnknown},
		{"unsupported defaults to unknown", WarningUnsupported, FreshnessUnknown},
		{"blank defaults to unknown", "", FreshnessUnknown},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			if got := warningFreshness(row.reason); got != row.want {
				t.Fatalf("warningFreshness(%q) = %q, want %q", row.reason, got, row.want)
			}
		})
	}
}
