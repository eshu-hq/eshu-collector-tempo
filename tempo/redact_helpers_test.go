// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"testing"
	"time"
)

// TestFingerprintRefusesBlankInput proves blank values yield no digest so
// redaction never persists a hash of nothing.
func TestFingerprintRefusesBlankInput(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "   ", "\t\n "} {
		if got := fingerprint(value); got != "" {
			t.Fatalf("fingerprint(%q) = %q, want empty", value, got)
		}
	}
	if got := fingerprint("checkout-prod"); len(got) != 64 {
		t.Fatalf("fingerprint(live value) has length %d, want 64 hex digits", len(got))
	}
}

// TestFingerprintJoinedRefusesBlankInput proves an all-blank join yields no
// digest while live parts join deterministically.
func TestFingerprintJoinedRefusesBlankInput(t *testing.T) {
	t.Parallel()

	if got := fingerprintJoined("", "   "); got != "" {
		t.Fatalf("fingerprintJoined(blanks) = %q, want empty", got)
	}
	first := fingerprintJoined("tags", "service.name")
	if first == "" {
		t.Fatal("fingerprintJoined(live parts) is empty, want a digest")
	}
	if second := fingerprintJoined("tags", "service.name"); second != first {
		t.Fatal("fingerprintJoined is not deterministic")
	}
}

// TestCleanStringSliceDropsBlanksAndDuplicates proves the slice helper
// trims, dedupes, and sorts so payloads carry canonical tag lists.
func TestCleanStringSliceDropsBlanksAndDuplicates(t *testing.T) {
	t.Parallel()

	got := cleanStringSlice([]string{" b ", "", "a", "b", "  ", "a"})
	want := []string{"a", "b"}
	if len(got) != len(want) {
		t.Fatalf("cleanStringSlice() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cleanStringSlice() = %q, want %q", got, want)
		}
	}
}

// TestFirstNonBlankFallsThrough proves the helper returns the first live
// value and empty when every candidate is blank.
func TestFirstNonBlankFallsThrough(t *testing.T) {
	t.Parallel()

	if got := firstNonBlank("", "  ", "live", "other"); got != "live" {
		t.Fatalf("firstNonBlank() = %q, want %q", got, "live")
	}
	if got := firstNonBlank("", "   "); got != "" {
		t.Fatalf("firstNonBlank(blanks) = %q, want empty", got)
	}
}

// TestNormalizedObservedAtDefaultsZeroClock proves a zero observation time
// resolves to now while a live timestamp keeps its instant in UTC.
func TestNormalizedObservedAtDefaultsZeroClock(t *testing.T) {
	t.Parallel()

	before := time.Now().UTC()
	got := normalizedObservedAt(time.Time{})
	after := time.Now().UTC()
	if got.Before(before) || got.After(after) {
		t.Fatalf("normalizedObservedAt(zero) = %v, want within [%v, %v]", got, before, after)
	}
	live := time.Date(2026, 6, 1, 12, 0, 0, 0, time.FixedZone("east", 3600))
	if resolved := normalizedObservedAt(live); !resolved.Equal(live) || resolved.Location() != time.UTC {
		t.Fatalf("normalizedObservedAt(live) = %v, want the same instant in UTC", resolved)
	}
}
